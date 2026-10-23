package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"

	"github.com/google/uuid"
)

// 帖子人工审核（#45 第 4 步，#118）。
//
//	AI 判为人工待审（或 fail_open=false 时审核失败）的帖子留在审核中（status=2），一直不可见；
//	本圈圈主 / 管理员（role>=20 且成员状态正常）在待审列表里看到它们和 AI 结论，逐条「通过 / 驳回」：
//	  通过 → ResolveReview(status=1) + 发布副作用（onPostPublishedByStored，只一次）+ 通知作者
//	  驳回 → ResolveReview(status=3) + 通知作者
//	ResolveReview 与 worker 共用 WHERE status = 2 条件更新，状态与人工审计行同一事务：
//	多个管理员 / worker 并发处理同一帖子时只有一方生效，其余返回 404（已不是审核中）。

// 圈内角色与成员状态（与 circle 域取值一致）。
const (
	circleRoleAdmin        int16 = 20
	memberStatusNormalCode int16 = 1
)

// reviewReasonMaxRunes 人工审核备注上限（post_moderation_log.reason VARCHAR(200)）。
const reviewReasonMaxRunes = 200

// ReviewAction 人工审核动作。
type ReviewAction string

const (
	ReviewApprove ReviewAction = "approve" // 通过 → 发布（status 1）
	ReviewReject  ReviewAction = "reject"  // 驳回（status 3）
)

// AIVerdictVO 帖子最近一条 AI 审核结论（post_moderation_log，source=1）。
type AIVerdictVO struct {
	// Decision AI 分流结果：review 人工待审（常见）；审核调用失败且 fail_open=false 时同样是 review，ErrorMsg 非空。
	Decision string `json:"decision"`
	// HitHazards 命中（概率 >= 转人工阈值）的风险，按概率降序。
	HitHazards []string `json:"hit_hazards"`
	// Hazards 全部风险概率（0–1）；审核调用失败时为空对象。
	Hazards            map[string]float64 `json:"hazards"`
	Severity           *float64           `json:"severity"`
	SeverityConfidence *float64           `json:"severity_confidence"`
	Model              string             `json:"model"`
	ErrorMsg           *string            `json:"error_msg"`
	CreateTime         time.Time          `json:"create_time"`
}

// ReviewQueueItem 待审帖子列表项。
type ReviewQueueItem struct {
	ID           uuid.UUID             `json:"id"`
	CircleID     uuid.UUID             `json:"circle_id"`
	UserID       uuid.UUID             `json:"user_id"`
	Type         int16                 `json:"type"`
	Title        string                `json:"title"`
	Summary      string                `json:"summary"`
	Content      string                `json:"content"`
	MediaExtra   domain.MediaExtraJSON `json:"media_extra"`
	Status       int16                 `json:"status"`
	CreateTime   time.Time             `json:"create_time"`
	AuthorName   string                `json:"author_name"`
	AuthorAvatar string                `json:"author_avatar"`
	// AIVerdict 为 null：AI 还没给出结论（审核事件排队中），也可以直接人工处理。
	AIVerdict *AIVerdictVO `json:"ai_verdict"`
}

// ReviewQueueResult 待审帖子列表（keyset 游标分页，create_time 倒序）。
type ReviewQueueResult struct {
	Posts  []ReviewQueueItem `json:"posts"`
	Size   int               `json:"size"`
	Cursor string            `json:"cursor"` // 下一页游标，无更多为 ""
}

// ReviewPostInput 人工审核入参。
type ReviewPostInput struct {
	CircleID uuid.UUID
	PostID   uuid.UUID
	Action   ReviewAction
	Reason   string // 可选备注（驳回理由），只进审核日志
}

// ReviewPostResult 人工审核结果。
type ReviewPostResult struct {
	PostID uuid.UUID `json:"post_id"`
	Status int16     `json:"status"` // 1 已发布 / 3 已驳回
}

// requireCircleModerator 校验操作者是该圈 admin+（role>=20）且成员状态正常（与 /circle/agent/* 同一规则）。
// 成员校验端口未注入 / 非成员 / 圈子不存在 / 状态非正常（禁言 / 拉黑 / 待审 / 退出）一律 403（fail-closed，
// 不区分非成员与普通成员，防成员身份探测）；读库失败原样返回（500）。
func (s *postServiceImpl) requireCircleModerator(ctx context.Context, circleID, operatorID uuid.UUID) error {
	if s.memberCheck == nil {
		return errNotCircleModerator
	}
	m, err := s.memberCheck.GetMember(ctx, circleID, operatorID)
	if err != nil {
		return err
	}
	if m == nil || m.Role < circleRoleAdmin || m.Status != memberStatusNormalCode {
		return errNotCircleModerator
	}
	return nil
}

// ListReviewQueue 圈子待审帖子列表（实现 PostService）。先鉴权后查询：非管理员拿不到任何帖子信息。
func (s *postServiceImpl) ListReviewQueue(ctx context.Context, operatorID, circleID uuid.UUID, size int, cursor string) (*ReviewQueueResult, error) {
	if circleID == uuid.Nil {
		return nil, errCircleIDRequired
	}
	if err := s.requireCircleModerator(ctx, circleID, operatorID); err != nil {
		return nil, err
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	after, err := decodeReviewCursor(cursor)
	if err != nil {
		return nil, err
	}

	posts, err := s.repo.ListReviewingByCircle(ctx, circleID, after, size+1)
	if err != nil {
		return nil, err
	}
	next := ""
	if len(posts) > size {
		posts = posts[:size]
		last := posts[size-1]
		next = encodeReviewCursor(last.CreateTime, last.ID)
	}

	items := make([]ReviewQueueItem, 0, len(posts))
	if len(posts) == 0 {
		return &ReviewQueueResult{Posts: items, Size: size, Cursor: next}, nil
	}

	postIDs := make([]uuid.UUID, 0, len(posts))
	authorIDs := make([]string, 0, len(posts))
	for _, p := range posts {
		postIDs = append(postIDs, p.ID)
		authorIDs = append(authorIDs, p.UserID.String())
	}
	verdicts := s.loadAIVerdicts(ctx, postIDs)
	authors := map[string]UserBrief{}
	if s.userFacade != nil {
		if m, err := s.userFacade.GetBriefs(ctx, authorIDs); err != nil {
			logger.Log.Warn("Failed to load authors for review queue: " + err.Error())
		} else {
			authors = m
		}
	}

	for _, p := range posts {
		media := p.MediaExtra
		if media == nil {
			media = domain.MediaExtraJSON{}
		}
		item := ReviewQueueItem{
			ID: p.ID, CircleID: p.CircleID, UserID: p.UserID, Type: p.Type,
			Title: p.Title, Summary: p.Summary, Content: p.Content, MediaExtra: media,
			Status: p.Status, CreateTime: p.CreateTime,
			AIVerdict: verdicts[p.ID],
		}
		if a, ok := authors[p.UserID.String()]; ok {
			item.AuthorName, item.AuthorAvatar = a.Username, a.AvatarURL
		}
		items = append(items, item)
	}
	return &ReviewQueueResult{Posts: items, Size: size, Cursor: next}, nil
}

// loadAIVerdicts 批量取 AI 结论；失败降级为空（列表仍可用于人工处理）。
func (s *postServiceImpl) loadAIVerdicts(ctx context.Context, postIDs []uuid.UUID) map[uuid.UUID]*AIVerdictVO {
	out := make(map[uuid.UUID]*AIVerdictVO, len(postIDs))
	if s.moderationLog == nil {
		return out
	}
	logs, err := s.moderationLog.LatestAIByPostIDs(ctx, postIDs)
	if err != nil {
		logger.Log.Error("Failed to load AI verdicts for review queue: " + err.Error())
		return out
	}
	threshold := s.moderationCfg.WithDefaults().ReviewThreshold
	for id, l := range logs {
		out[id] = buildAIVerdict(l, threshold)
	}
	return out
}

// buildAIVerdict 审核日志 → AI 结论 VO；HitHazards 为概率 >= 转人工阈值的风险（降序）。
func buildAIVerdict(l *domain.PostModerationLog, reviewThreshold float64) *AIVerdictVO {
	v := &AIVerdictVO{
		Decision:           string(l.Decision),
		HitHazards:         []string{},
		Hazards:            make(map[string]float64, len(l.Hazards)),
		Severity:           l.Severity,
		SeverityConfidence: l.SeverityConfidence,
		Model:              l.Model,
		ErrorMsg:           l.ErrorMsg,
		CreateTime:         l.CreateTime,
	}
	for h, p := range l.Hazards {
		v.Hazards[string(h)] = p
		if p >= reviewThreshold {
			v.HitHazards = append(v.HitHazards, string(h))
		}
	}
	sort.Slice(v.HitHazards, func(i, j int) bool {
		pi, pj := v.Hazards[v.HitHazards[i]], v.Hazards[v.HitHazards[j]]
		if pi != pj {
			return pi > pj
		}
		return v.HitHazards[i] < v.HitHazards[j]
	})
	return v
}

// ReviewPost 人工审核一篇待审帖子（实现 PostService）。
//
// 顺序：校验动作 → 鉴权（403）→ 读帖子，不存在 / 不在该圈 / 不是审核中一律 404 →
// ResolveReview 条件更新 + 写人工审计行（同一事务；并发输掉的一方 404）→ 通过时执行发布副作用 → 通知作者。
func (s *postServiceImpl) ReviewPost(ctx context.Context, operatorID uuid.UUID, in ReviewPostInput) (*ReviewPostResult, error) {
	var status int16
	switch in.Action {
	case ReviewApprove:
		status = domain.PostStatusPublished
	case ReviewReject:
		status = domain.PostStatusRejected
	default:
		return nil, errInvalidReviewAction
	}
	if in.CircleID == uuid.Nil {
		return nil, errCircleIDRequired
	}
	if err := s.requireCircleModerator(ctx, in.CircleID, operatorID); err != nil {
		return nil, err
	}

	post, err := s.repo.GetByID(ctx, in.PostID)
	if errors.Is(err, domain.ErrPostNotFound) {
		return nil, errReviewPostNotFound
	}
	if err != nil {
		return nil, err
	}
	if post.CircleID != in.CircleID || post.Deleted != 0 || post.Status != domain.PostStatusReviewing {
		return nil, errReviewPostNotFound
	}

	decision := domain.ModerationReject
	if status == domain.PostStatusPublished {
		decision = domain.ModerationPublish
	}
	operator := operatorID
	entry := &domain.PostModerationLog{
		PostID:     post.ID,
		Decision:   decision,
		Hazards:    domain.HazardScores{},
		Source:     domain.ModerationSourceManual,
		OperatorID: &operator,
	}
	if reason := normalizeReviewReason(in.Reason); reason != "" {
		entry.Reason = &reason
	}

	updated, err := s.repo.ResolveReview(ctx, post.ID, status, entry)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, errReviewPostNotFound // 并发处理 / worker 抢先 / 刚被删除
	}
	post.Status = status

	// 状态已落库：副作用与通知不跟随请求取消，避免已发布但计数 / 通知没做
	ctx = context.WithoutCancel(ctx)
	if status == domain.PostStatusPublished {
		s.onPostPublishedByStored(ctx, post)
	}
	if s.publisher != nil {
		if err := s.publisher.PublishReviewResultNotice(ctx, operatorID, post.ID, status == domain.PostStatusPublished); err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to publish review result notice for post %s: %s", post.ID, err.Error()))
		}
	}
	return &ReviewPostResult{PostID: post.ID, Status: status}, nil
}

// normalizeReviewReason 清洗备注：去首尾空白、PG 安全、截断到 200 字符。
func normalizeReviewReason(reason string) string {
	reason = strings.TrimSpace(utils.SanitizeForPg(reason))
	if r := []rune(reason); len(r) > reviewReasonMaxRunes {
		reason = string(r[:reviewReasonMaxRunes])
	}
	return reason
}

// ===== 待审列表游标（base64 JSON {t: create_time UnixMicro, i: id}，对齐排序键 create_time DESC, id DESC）=====

func encodeReviewCursor(t time.Time, id uuid.UUID) string {
	b, _ := json.Marshal(struct {
		T int64  `json:"t"`
		I string `json:"i"`
	}{T: t.UnixMicro(), I: id.String()})
	return base64.StdEncoding.EncodeToString(b)
}

// decodeReviewCursor 防御性解析游标（用户可控参数）：空串为第一页，非法输入返回 errInvalidReviewCursor，绝不 panic。
func decodeReviewCursor(s string) (*domain.PostKeyset, error) {
	if s == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errInvalidReviewCursor
	}
	var raw struct {
		T *int64 `json:"t"`
		I string `json:"i"`
	}
	if err := json.Unmarshal(b, &raw); err != nil || raw.T == nil {
		return nil, errInvalidReviewCursor
	}
	id, err := uuid.Parse(raw.I)
	if err != nil {
		return nil, errInvalidReviewCursor
	}
	return &domain.PostKeyset{CreateTime: time.UnixMicro(*raw.T), ID: id}, nil
}
