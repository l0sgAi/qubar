// auto_reply.go 机器人自动回复高热度帖子 / 评论（#99，设计见 docs/agent-auto-reply-design.md）。
//
// 定时扫描器每轮调用 RunAutoReplyScan：逐个加载开启 auto_reply 的启用机器人，经 post / comment
// Facade 按门槛取候选（作者非机器人账号、在机器人圈子范围内、排除已触发 / 已饱和目标），
// 再复用 executeReply（分类器、生成、限频、写日志、楼层挂载全部同被动触发）。
// 每机器人每目标最多一次由「先插占位行、撞部分唯一索引即跳过」保证（多实例 / 并发兜底）；
// 每目标机器人数上限 max_agents_per_target 为执行前计数的软限。
package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/logger"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

// ===== 跨域端口（composition 桥接 post / comment Facade）=====

// AutoPostQuery 自动回复帖子候选查询条件。
type AutoPostQuery struct {
	CircleID   uuid.UUID   // 圈子范围；uuid.Nil = 全站（全局机器人）
	HotMin     int         // 热度门槛（含）
	Since      time.Time   // 只看该时间之后发布的帖子
	ExcludeIDs []uuid.UUID // 已触发 / 已饱和的帖子
	Limit      int
}

// AutoPostCandidate 自动回复帖子候选。
type AutoPostCandidate struct {
	ID       uuid.UUID
	CircleID uuid.UUID
	AuthorID uuid.UUID
	Hot      int
}

// AutoCommentQuery 自动回复评论候选查询条件（LikeMin / ReplyMin 满足任一，<=0 不参与）。
type AutoCommentQuery struct {
	CircleID   uuid.UUID // 所属帖子的圈子范围；uuid.Nil = 全站
	LikeMin    int
	ReplyMin   int
	Since      time.Time
	ExcludeIDs []uuid.UUID // 已触发 / 已饱和的评论
	Limit      int
}

// AutoCommentCandidate 自动回复评论候选。
type AutoCommentCandidate struct {
	ID           uuid.UUID
	PostID       uuid.UUID
	PostCircleID uuid.UUID
	UserID       uuid.UUID
	RootID       *uuid.UUID // nil = 顶层评论
	Content      string
}

// AutoReplyCandidateReader 自动回复候选读取端口（桥接 post / comment 域）。
// 实现方负责：已发布未锁定未删、时间窗口、门槛、作者非机器人账号（role=2）、圈子范围、
// 排除名单，按热度降序取 top-N。
type AutoReplyCandidateReader interface {
	ListHotPosts(ctx context.Context, q AutoPostQuery) ([]AutoPostCandidate, error)
	ListHotComments(ctx context.Context, q AutoCommentQuery) ([]AutoCommentCandidate, error)
}

// errAutoTargetTaken 自动回复目标已被本机器人触发过 / 已达每目标机器人数上限（静默跳过）。
var errAutoTargetTaken = errors.New("auto reply target already taken")

// autonomyConfig 平台自主行为配置（<=0 字段兜底默认值）。
func autonomyConfig() conf.AiAgentAutonomy {
	return conf.Config.AiAgent.Autonomy.WithDefaults()
}

// RunAutoReplyScan 执行一轮自动回复扫描（同步，逐个机器人顺序执行）。
//
// 顺序执行 + 共享信号量：一轮最多占用一个回复并发槽，不挤占关键词 / @提及 的实时触发；
// 每个机器人受 min_interval_sec 约束本就每轮至多回复一次，顺序执行不损失吞吐。
func (s *replyServiceImpl) RunAutoReplyScan(ctx context.Context) {
	ac := autonomyConfig()
	if !ac.Enabled || s.candidates == nil {
		return
	}
	agents, err := s.agentRepo.ListAutoReplyEnabled(ctx)
	if err != nil {
		logger.Log.Error("agent auto reply: list agents failed: " + err.Error())
		return
	}
	if len(agents) == 0 {
		return
	}

	// 全部机器人的时间窗口取最大值，一次加载窗口内全部自动回复目标（超集用于排除是安全的：
	// 窗口外的目标本就不会被候选查询返回）。
	cfgs := make([]domain.AutoReplyConfig, len(agents))
	maxWindow := 0
	for i := range agents {
		cfg, err := agents[i].AutoConfig.AutoReply()
		if err != nil {
			logger.Log.Warn(fmt.Sprintf("agent auto reply: agent=%s invalid auto_config ignored: %s", agents[i].ID, err.Error()))
			continue
		}
		cfgs[i] = cfg
		if cfg.Enabled && cfg.WindowHours > maxWindow {
			maxWindow = cfg.WindowHours
		}
	}
	if maxWindow == 0 {
		return
	}
	now := time.Now()
	records, err := s.replyLogRepo.ListAutoTargetsSince(ctx, now.Add(-time.Duration(maxWindow)*time.Hour))
	if err != nil {
		logger.Log.Error("agent auto reply: list auto targets failed: " + err.Error())
		return
	}

	// 奇偶轮交替帖子 / 评论优先：同一轮内回复一次后通常被 min_interval_sec 截停，
	// 固定顺序会让排在后面的目标类型长期饿死。
	commentsFirst := s.autoRound.Add(1)%2 == 0
	for i := range agents {
		if ctx.Err() != nil {
			return
		}
		cfg := cfgs[i]
		if !cfg.Enabled || cfg.WindowHours <= 0 {
			continue
		}
		posts, comments := autoReplyExclusions(records, agents[i].ID, ac.MaxAgentsPerTarget)
		s.scanAgent(ctx, &agents[i], cfg, ac, now, posts, comments, commentsFirst)
	}
}

// scanAgent 单个机器人的一轮扫描：按配置的目标类型取候选并逐个执行，
// 撞到限频（每小时 / 最小间隔 / 自动回复每小时上限）即结束本机器人本轮。
func (s *replyServiceImpl) scanAgent(
	ctx context.Context,
	agent *domain.AiAgent,
	cfg domain.AutoReplyConfig,
	ac conf.AiAgentAutonomy,
	now time.Time,
	excludePosts, excludeComments []uuid.UUID,
	commentsFirst bool,
) {
	since := now.Add(-time.Duration(cfg.WindowHours) * time.Hour)
	circleID := uuid.Nil
	if agent.CircleID != nil {
		circleID = *agent.CircleID // 圈内机器人只看本圈；全局机器人全站
	}

	scanPosts := func() bool {
		if !cfg.HasTarget(domain.AutoReplyTargetPost) || cfg.PostHotMin <= 0 {
			return true
		}
		cands, err := s.candidates.ListHotPosts(ctx, AutoPostQuery{
			CircleID: circleID, HotMin: cfg.PostHotMin, Since: since,
			ExcludeIDs: excludePosts, Limit: ac.MaxCandidatesPerRun,
		})
		if err != nil {
			logger.Log.Error(fmt.Sprintf("agent auto reply: agent=%s list hot posts failed: %s", agent.ID, err.Error()))
			return true
		}
		for _, c := range cands {
			if !agentInScope(agent, c.CircleID) {
				continue
			}
			if !s.runAutoReply(ctx, agent, c.ID, nil) {
				return false
			}
		}
		return true
	}
	scanComments := func() bool {
		if !cfg.HasTarget(domain.AutoReplyTargetComment) || (cfg.CommentLikeMin <= 0 && cfg.CommentReplyMin <= 0) {
			return true
		}
		cands, err := s.candidates.ListHotComments(ctx, AutoCommentQuery{
			CircleID: circleID, LikeMin: cfg.CommentLikeMin, ReplyMin: cfg.CommentReplyMin, Since: since,
			ExcludeIDs: excludeComments, Limit: ac.MaxCandidatesPerRun,
		})
		if err != nil {
			logger.Log.Error(fmt.Sprintf("agent auto reply: agent=%s list hot comments failed: %s", agent.ID, err.Error()))
			return true
		}
		for _, c := range cands {
			if !agentInScope(agent, c.PostCircleID) {
				continue
			}
			evt := &CommentEvent{
				CommentID: c.ID, PostID: c.PostID, PostCircleID: c.PostCircleID,
				UserID: c.UserID, RootID: c.RootID, Content: c.Content,
			}
			if !s.runAutoReply(ctx, agent, c.PostID, evt) {
				return false
			}
		}
		return true
	}

	first, second := scanPosts, scanComments
	if commentsFirst {
		first, second = scanComments, scanPosts
	}
	if first() {
		second()
	}
}

// runAutoReply 对单个目标执行自动回复。返回 false 表示本机器人本轮应停止
// （限频 / 扫描被取消），true 表示继续下一个候选。
func (s *replyServiceImpl) runAutoReply(ctx context.Context, agent *domain.AiAgent, postID uuid.UUID, trigger *CommentEvent) bool {
	// 阻塞获取并发槽（与被动触发共享上限）；扫描被取消时放弃。
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	defer func() { <-s.sem }()

	runCtx, cancel := context.WithTimeout(ctx, 2*replyTimeout())
	defer cancel()
	_, err := s.executeReply(runCtx, agent, postID, &replyRun{source: domain.TriggerSourceAuto, trigger: trigger})
	switch {
	case err == nil:
		logger.Log.Info(fmt.Sprintf("agent auto reply ok: agent=%s post=%s target_comment=%v", agent.ID, postID, trigger != nil))
	case errors.Is(err, errRateLimited):
		return false
	case errors.Is(err, errPostNotReplyable), errors.Is(err, errSkippedByFilter), errors.Is(err, errAutoTargetTaken):
		// 前置跳过 / 分类器判定不回复：已在 executeReply 内打 Info，继续下一个候选。
	default:
		logger.Log.Error(fmt.Sprintf("agent auto reply: agent=%s post=%s: %s", agent.ID, postID, err.Error()))
	}
	return ctx.Err() == nil
}

// claimAutoTarget 自动回复执行前的占位（executeReply 在限频检查之后调用）：
//  1. auto_reply.max_per_hour（>0 时）：最近 1h 自动回复行数达到上限 → errRateLimited；
//  2. 每目标机器人数（max_agents_per_target，软限）：已达上限 → errAutoTargetTaken；
//  3. 插入 status=4 占位行：撞部分唯一索引（本机器人已触发过该目标）→ errAutoTargetTaken。
//
// 占位成功后 run.placeholderID 回填，后续终态（成功 / 失败 / 分类器跳过）原地更新这一行。
func (s *replyServiceImpl) claimAutoTarget(ctx context.Context, agent *domain.AiAgent, post *PostBrief, run *replyRun) error {
	cfg, err := agent.AutoConfig.AutoReply()
	if err != nil {
		return err
	}
	if cfg.MaxPerHour > 0 {
		n, err := s.replyLogRepo.CountAutoSinceByAgent(ctx, agent.ID, time.Now().Add(-time.Hour))
		if err != nil {
			return err
		}
		if n >= int64(cfg.MaxPerHour) {
			logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=auto_rate_limited_hourly count=%d limit=%d",
				agent.ID, post.ID, n, cfg.MaxPerHour))
			return errRateLimited
		}
	}
	target := run.targetCommentID()
	n, err := s.replyLogRepo.CountAutoByTarget(ctx, post.ID, target)
	if err != nil {
		return err
	}
	if limit := autonomyConfig().MaxAgentsPerTarget; n >= int64(limit) {
		logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=auto_target_saturated count=%d limit=%d",
			agent.ID, post.ID, n, limit))
		return errAutoTargetTaken
	}
	placeholder := &domain.ReplyLog{
		ID:              sharedomain.NewID(),
		AgentID:         agent.ID,
		PostID:          post.ID,
		UserID:          post.AuthorID,
		TriggerSource:   domain.TriggerSourceAuto,
		TargetCommentID: target,
		Status:          domain.ReplyStatusPending,
	}
	ok, err := s.replyLogRepo.CreateAutoPlaceholder(ctx, placeholder)
	if err != nil {
		return err
	}
	if !ok {
		logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=auto_target_already_triggered", agent.ID, post.ID))
		return errAutoTargetTaken
	}
	run.placeholderID = placeholder.ID
	return nil
}

// autoTargetKey 自动回复目标（comment=Nil 表示回复帖子本身）。
type autoTargetKey struct {
	post    uuid.UUID
	comment uuid.UUID
}

// autoReplyExclusions 由窗口内自动回复记录算出某机器人本轮应排除的帖子 / 评论：
// 本机器人已触发过的目标 + 已达 maxAgents 个机器人自动回复的目标（status=2 分类器判定
// 不回复的不占名额）。排除在候选 SQL 里做，避免已处理目标长期占住 top-N 名额。
func autoReplyExclusions(records []domain.AutoTargetRecord, agentID uuid.UUID, maxAgents int) (posts, comments []uuid.UUID) {
	counts := make(map[autoTargetKey]int)
	excluded := make(map[autoTargetKey]bool)
	for _, r := range records {
		k := autoTargetKey{post: r.PostID}
		if r.TargetCommentID != nil {
			k.comment = *r.TargetCommentID
		}
		if r.AgentID == agentID {
			excluded[k] = true
		}
		if r.Status != domain.ReplyStatusSkipped {
			counts[k]++
		}
	}
	for k, n := range counts {
		if n >= maxAgents {
			excluded[k] = true
		}
	}
	for k := range excluded {
		if k.comment == uuid.Nil {
			posts = append(posts, k.post)
		} else {
			comments = append(comments, k.comment)
		}
	}
	return posts, comments
}
