package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// 帖子 AI 审核流（#45 第 3 步，#117）。
//
//	CreatePost（moderation 启用）→ 落库 status=2 → 发审核事件（Redpanda）→ 立即返回
//	worker → ModeratePost：读帖子（不是审核中就跳过）→ Jev 打分 → domain.RouteModeration 分流
//	      → 条件更新 UPDATE ... WHERE status = 2 → 写 post_moderation_log → 转为已发布时执行发布副作用（只一次）
//
// 人工待审的帖子留在审核中，一直不可见，由 #118 的人工审核处理。

// moderationErrMaxRunes error_msg 列上限（VARCHAR(2048)）。
const moderationErrMaxRunes = 2048

// ModerationCircle 审核输入里的圈子语境。
type ModerationCircle struct {
	Name     string
	Category string // 分类名，未分类为空
}

// ModerationCircleReader 读取审核用的圈子信息（composition 桥接 circle / category）。
type ModerationCircleReader interface {
	// GetModerationCircle 返回圈子名称与分类名；圈子不存在返回 nil, nil。
	GetModerationCircle(ctx context.Context, circleID uuid.UUID) (*ModerationCircle, error)
}

func (s *postServiceImpl) SetModeration(moderator domain.ContentModerator, logRepo domain.PostModerationLogRepository, cfg conf.Moderation) {
	s.moderator = moderator
	s.moderationLog = logRepo
	s.moderationCfg = cfg.WithDefaults()
}

func (s *postServiceImpl) SetModerationCircleReader(r ModerationCircleReader) { s.modCircle = r }

// moderationActive 发帖是否走审核流：已注入审核依赖，且开关打开、配置了 api_key。
func (s *postServiceImpl) moderationActive() bool {
	return s.moderator != nil && s.moderationLog != nil && s.moderationCfg.Active()
}

func (s *postServiceImpl) moderationThresholds() domain.ModerationThresholds {
	c := s.moderationCfg
	return domain.ModerationThresholds{
		Review:                c.ReviewThreshold,
		Action:                c.ActionThreshold,
		SeverityBlock:         c.SeverityBlock,
		LowConfidence:         c.LowConfidence,
		LowConfidenceSeverity: c.LowConfidenceSeverity,
	}
}

// failureDecision 审核拿不到结论（Jev 不可用 / 配置错误 / 事件发不出去）时的分流：
// fail_open=true 直接发布，否则留在审核中转人工。
func (s *postServiceImpl) failureDecision() domain.ModerationDecision {
	if s.moderationCfg.FailOpenEnabled() {
		return domain.ModerationFailOpen
	}
	return domain.ModerationReview
}

// enqueueModeration 发帖后发审核事件。发不出去时按 fail_open 兜底，避免帖子永远卡在审核中
// （fail_open=false 时留在审核中，等 #118 人工处理）。mentionIDs 为已落库的最终 @提及 名单。
func (s *postServiceImpl) enqueueModeration(ctx context.Context, post *domain.Post, mentionIDs []uuid.UUID) {
	var err error
	if s.publisher == nil {
		err = errors.New("post event publisher is not configured")
	} else {
		err = s.publisher.PublishModeration(ctx, post.ID)
	}
	if err == nil {
		return
	}
	logger.Log.Error(fmt.Sprintf("Failed to enqueue moderation for post %s, applying fail_open=%v: %s",
		post.ID, s.moderationCfg.FailOpenEnabled(), err.Error()))

	ctx = context.WithoutCancel(ctx)
	enqueueErr := fmt.Errorf("enqueue moderation event: %w", err)
	published, aerr := s.applyModeration(ctx, post, s.failureDecision(), nil, enqueueErr, 0)
	if aerr != nil {
		logger.Log.Error(fmt.Sprintf("Failed to apply moderation fallback for post %s: %s", post.ID, aerr.Error()))
		return
	}
	if published {
		s.onPostPublished(ctx, post, mentionIDs)
	}
}

// ModeratePost 审核 worker 入口（实现 PostService）。
//
// 幂等：帖子已删除 / 不是审核中 / 已有审核日志（人工待审）都直接跳过；状态变更用条件更新，
// 重复投递或并发处理时只有一方更新成功并执行发布副作用。
// 返回错误（读库 / 写库失败、关停）时 worker 不提交 offset，稍后重试。
func (s *postServiceImpl) ModeratePost(ctx context.Context, postID uuid.UUID) error {
	post, err := s.repo.GetByID(ctx, postID)
	if errors.Is(err, domain.ErrPostNotFound) {
		return nil // 已删除
	}
	if err != nil {
		return err
	}
	if post.Status != domain.PostStatusReviewing {
		return nil // 已处理（重复投递 / 人工审核抢先）
	}
	if s.moderationLog == nil {
		return errors.New("post moderation log repository is not configured")
	}
	done, err := s.moderationLog.ExistsByPostID(ctx, postID)
	if err != nil {
		return err
	}
	if done {
		return nil // 已审核过且转人工待审，重复投递不再调 Jev
	}

	start := time.Now()
	a, aerr := s.assess(ctx, post)
	latency := time.Since(start)
	if aerr != nil && ctx.Err() != nil {
		return ctx.Err() // 关停导致的取消不是「服务不可用」，不能按 fail_open 放行；不提交，重启后重投
	}

	var decision domain.ModerationDecision
	if aerr != nil {
		if errors.Is(aerr, domain.ErrModerationUnavailable) {
			logger.Log.Warn(fmt.Sprintf("Content moderation unavailable for post %s, applying fail_open=%v: %s",
				postID, s.moderationCfg.FailOpenEnabled(), aerr.Error()))
		} else {
			// 401 / 422 / 配置错误 / 响应异常：需要人工排查配置，打错误日志
			logger.Log.Error(fmt.Sprintf("Content moderation failed for post %s, applying fail_open=%v: %s",
				postID, s.moderationCfg.FailOpenEnabled(), aerr.Error()))
		}
		decision = s.failureDecision()
	} else {
		decision = domain.RouteModeration(*a, s.moderationThresholds())
	}

	// 审核结论已拿到：后续落库与副作用不跟随关停取消，避免状态已改但计数 / 通知没做
	ctx = context.WithoutCancel(ctx)
	published, err := s.applyModeration(ctx, post, decision, a, aerr, latency)
	if err != nil {
		return err
	}
	if published {
		s.onPostPublishedByStored(ctx, post)
	}
	return nil
}

// assess 组装审核输入并调用 ContentModerator。
func (s *postServiceImpl) assess(ctx context.Context, post *domain.Post) (*domain.Assessment, error) {
	if s.moderator == nil {
		return nil, domain.ErrModerationDisabled
	}
	in := domain.ModerationInput{Title: post.Title, Content: post.Content}
	if s.modCircle != nil {
		c, err := s.modCircle.GetModerationCircle(ctx, post.CircleID)
		if err != nil {
			logger.Log.Warn(fmt.Sprintf("Failed to load circle for moderation of post %s: %s", post.ID, err.Error()))
		} else if c != nil {
			in.CircleName, in.CircleCategory = c.Name, c.Category
		}
	}
	return s.moderator.Assess(ctx, in)
}

// applyModeration 按分流结果改状态并写审核日志，返回帖子是否刚转为已发布（调用方据此执行发布副作用）。
//
//   - 发布 / fail_open / 驳回：条件更新 WHERE status = 2，更新 0 行说明已被处理，什么都不做；
//     更新成功后写日志（best-effort：状态已改，重试会被条件更新挡掉，日志失败只记错误）。
//   - 人工待审：不改状态，只写日志；日志写失败返回错误让 worker 重试（未改任何状态，重试安全）。
func (s *postServiceImpl) applyModeration(ctx context.Context, post *domain.Post, decision domain.ModerationDecision,
	a *domain.Assessment, aerr error, latency time.Duration) (bool, error) {
	entry := buildModerationLog(post.ID, decision, a, aerr, latency)

	status, change := decision.TargetStatus()
	if !change {
		return false, s.moderationLog.Create(ctx, entry)
	}
	updated, err := s.repo.UpdateStatusFromReviewing(ctx, post.ID, status)
	if err != nil {
		return false, err
	}
	if !updated {
		return false, nil
	}
	post.Status = status
	if err := s.moderationLog.Create(ctx, entry); err != nil {
		logger.Log.Error(fmt.Sprintf("Failed to write moderation log for post %s (decision=%s): %s", post.ID, decision, err.Error()))
	}
	return status == domain.PostStatusPublished, nil
}

// buildModerationLog 组装一行审核日志。a 为 nil（审核失败）时只记错误与耗时。
func buildModerationLog(postID uuid.UUID, decision domain.ModerationDecision, a *domain.Assessment, aerr error, latency time.Duration) *domain.PostModerationLog {
	entry := &domain.PostModerationLog{
		PostID:    postID,
		Decision:  decision,
		Hazards:   domain.HazardScores{},
		LatencyMs: int(latency.Milliseconds()),
	}
	if a != nil {
		entry.Hazards = domain.HazardScores(a.Risks)
		severity, confidence := a.Severity, a.SeverityConfidence
		entry.Severity, entry.SeverityConfidence = &severity, &confidence
		entry.Model = a.Model
		entry.InputTokens = a.InputTokens
		entry.LatencyMs = int(a.Latency.Milliseconds())
	}
	if aerr != nil {
		msg := aerr.Error()
		if r := []rune(msg); len(r) > moderationErrMaxRunes {
			msg = string(r[:moderationErrMaxRunes])
		}
		entry.ErrorMsg = &msg
	}
	return entry
}
