// Package composition 的 agent_bridges.go：
// 实现 aiagent 领域的跨域端口（RoleReader / BotUserCreator），内部桥接到 user 领域。
//
// 与 user_session_store_bridge.go 同一模式：
//   - aiagent.application 只声明端口接口，不 import user 包；
//   - composition 层写桥接器把两者连起来（未来拆服务换 RPC 即可）。
package composition

import (
	"context"

	agentapp "interestBar/pkg/domains/aiagent/application"
	agentdomain "interestBar/pkg/domains/aiagent/domain"
	circleapp "interestBar/pkg/domains/circle/application"
	commentapp "interestBar/pkg/domains/comment/application"
	postapp "interestBar/pkg/domains/post/application"
	userapp "interestBar/pkg/domains/user/application"
	userdomain "interestBar/pkg/domains/user/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// agentRoleReader 桥接 aiagent.RoleReader -> user.UserService.GetByID（带 userinfo 缓存）。
//
// role 变更经手工改库，缓存 TTL 30 分钟内可能读到旧值（已确认接受，见设计确认项 #1）。
type agentRoleReader struct {
	delegate userapp.UserService
}

// GetUserRole 返回用户 role；用户不存在/已删/已禁用返回 false。
func (b *agentRoleReader) GetUserRole(ctx context.Context, userID uuid.UUID) (int, bool, error) {
	user, err := b.delegate.GetByID(ctx, userID)
	if err != nil {
		return 0, false, err
	}
	if user == nil {
		return 0, false, nil
	}
	return user.Role, true, nil
}

// agentBotUserCreator 桥接 aiagent.BotUserCreator -> 直写 domains.users（role=2 机器人账号）。
//
// 过渡期直持 *gorm.DB（与 userSessionStoreBridge 一致）；
// 后续可收口为 user 领域 Service 方法。
type agentBotUserCreator struct {
	db *gorm.DB
}

// botUserRole 机器人账号 role（预留标识位，区别于 0=普通用户 / 1=管理员）。
const botUserRole = 2

// CreateBotUser 创建 role=2 的机器人系统用户，返回其 ID。
// 机器人账号不登录不发帖，仅作为 ai_agent 以该身份发评论的载体。
// username 用机器人 name（允许重复）；email 由调用方保证唯一；avatarURL 为机器人头像。
// circleID 非 nil 时写入 users.agent_circle_id（圈内机器人 @提及 作用域投影；
// 全局链路传 nil）。
func (b *agentBotUserCreator) CreateBotUser(ctx context.Context, username, email, avatarURL string, circleID *uuid.UUID) (uuid.UUID, error) {
	u := userdomain.SysUser{
		ID:            sharedomain.NewID(),
		Username:      username,
		Email:         email,
		AvatarURL:     avatarURL,
		AgentCircleID: circleID,
		Role:          botUserRole,
		Status:        userdomain.UserStatusActive,
		Deleted:       0,
	}
	if err := b.db.WithContext(ctx).Create(&u).Error; err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

// agentBotUserUpdater 桥接 aiagent.BotUserProfileUpdater -> user.UserService.UpdateProfile。
//
// 走 UpdateProfile 而非直写库：更新后自动刷新 userinfo 缓存（避免评论区旧头像/旧名）。
type agentBotUserUpdater struct {
	delegate userapp.UserService
}

// UpdateBotUserProfile 部分更新机器人账号的 username/avatar_url（nil 字段不动）。
func (b *agentBotUserUpdater) UpdateBotUserProfile(ctx context.Context, userID uuid.UUID, username, avatarURL *string) error {
	_, err := b.delegate.UpdateProfile(ctx, userID, userapp.UpdateProfileInput{
		Username:  username,
		AvatarURL: avatarURL,
	})
	return err
}

// agentBotUserScopeCleaner 桥接 aiagent.BotUserScopeCleaner -> user.UserService.ClearAgentCircleScope。
//
// 走 user Service 而非直写库：清列后自动刷新 userinfo 缓存（避免 @列表/详情回显旧绑定）。
type agentBotUserScopeCleaner struct {
	delegate userapp.UserService
}

// ClearBotCircleScope 机器人软删后清 users.agent_circle_id（幂等）。
func (b *agentBotUserScopeCleaner) ClearBotCircleScope(ctx context.Context, userID uuid.UUID) error {
	return b.delegate.ClearAgentCircleScope(ctx, userID)
}

// 编译期保证：桥接器满足 aiagent 领域端口。
var (
	_ agentapp.RoleReader            = (*agentRoleReader)(nil)
	_ agentapp.BotUserCreator        = (*agentBotUserCreator)(nil)
	_ agentapp.BotUserProfileUpdater = (*agentBotUserUpdater)(nil)
	_ agentapp.BotUserScopeCleaner   = (*agentBotUserScopeCleaner)(nil)
	_ agentapp.PostReader            = (*agentPostReader)(nil)
	_ agentapp.CommentCreator        = (*agentCommentCreator)(nil)
	_ agentapp.CircleRoleReader      = (*circleRoleReaderForAgent)(nil)
	_ circleapp.CircleAgentCounter   = (*circleAgentCounterForCircle)(nil)
)

// circleRoleReaderForAgent 桥接 aiagent.CircleRoleReader -> circle 域成员角色 Facade。
//
// 薄转发：delegate 直查 member 记录（无缓存，含惰性解禁自愈），
// 圈内机器人管理权随角色变更即时生效。
type circleRoleReaderForAgent struct {
	delegate circleapp.CircleMemberRoleReader
}

// GetCircleMembership 返回操作者在圈内的 (role, status)；非成员/圈子不存在 ok=false。
func (b *circleRoleReaderForAgent) GetCircleMembership(ctx context.Context, circleID, userID uuid.UUID) (int16, int16, bool, error) {
	return b.delegate.GetMemberRole(ctx, circleID, userID)
}

// circleAgentCounterForCircle 桥接 circle.CircleAgentCounter -> aiagent.AgentRepository.CountByCircleIDs。
//
// 方向反转的桥接：circle 域声明端口（可管理圈子列表 agent_count 回填），
// 由 aiagent 同域仓储实现（代理数据属 ai_agent 表）。
type circleAgentCounterForCircle struct {
	repo agentdomain.AgentRepository
}

// CountByCircleIDs 批量统计各圈未删除机器人数。
func (b *circleAgentCounterForCircle) CountByCircleIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	return b.repo.CountByCircleIDs(ctx, circleIDs)
}

// agentPostReader 桥接 aiagent.PostReader -> post.PostService.GetPostBrief。
type agentPostReader struct {
	delegate postapp.PostService
}

// GetPostBrief 返回帖子摘要。未找到返回 nil, nil。
func (b *agentPostReader) GetPostBrief(ctx context.Context, postID uuid.UUID) (*agentapp.PostBrief, error) {
	brief, err := b.delegate.GetPostBrief(ctx, postID)
	if err != nil || brief == nil {
		return nil, err
	}
	return &agentapp.PostBrief{
		ID:       brief.ID,
		Title:    brief.Title,
		Summary:  brief.Summary,
		Status:   brief.Status,
		IsLock:   brief.IsLock,
		AuthorID: brief.AuthorID,
		CircleID: brief.CircleID,
	}, nil
}

// agentCommentCreator 桥接 aiagent.CommentCreator -> comment.CommentService.CreateComment。
//
// 机器人以 linked_user_id 身份发评论；帖子校验/清洗/计数/热度事件全部复用
// comment 域现有链路。机器人评论同样会回调 AgentReplyTrigger，由 aiagent 侧
// ExistsByLinkedUserID 防回环。
type agentCommentCreator struct {
	delegate commentapp.CommentService
}

// CreateComment 以指定用户身份创建评论，返回评论 ID。
func (b *agentCommentCreator) CreateComment(ctx context.Context, userID uuid.UUID, input agentapp.CommentCreateInput) (uuid.UUID, error) {
	return b.delegate.CreateComment(ctx, userID, commentapp.CreateCommentInput{
		PostID:    input.PostID,
		Content:   input.Content,
		RootID:    input.RootID,
		ReplyToID: input.ReplyToID,
	})
}

// commentAgentTrigger 桥接 comment.AgentReplyTrigger -> aiagent.ReplyService.OnCommentCreated。
//
// 同步回调、立即返回（ReplyService 内部 goroutine 异步执行 + recover），
// 不向评论创建链路传播任何错误。
type commentAgentTrigger struct {
	delegate agentapp.ReplyService
}

// OnCommentCreated 评论创建完成后的机器人触发入口。
// postCircleID 随事件透传（圈子级机器人只在同圈帖触发）。
func (b *commentAgentTrigger) OnCommentCreated(postID, postCircleID, commentID, userID uuid.UUID, rootID *uuid.UUID, content string) {
	b.delegate.OnCommentCreated(agentapp.CommentEvent{
		CommentID:    commentID,
		PostID:       postID,
		PostCircleID: postCircleID,
		UserID:       userID,
		RootID:       rootID,
		Content:      content,
	})
}

// 编译期保证：触发桥接器满足 comment 领域端口。
var _ commentapp.AgentReplyTrigger = (*commentAgentTrigger)(nil)

// postAgentTrigger 桥接 post.AgentPostTrigger -> aiagent.ReplyService.OnPostMentioned。
//
// 同步回调、立即返回（ReplyService 内部 goroutine 异步执行 + recover），
// 不向发帖链路传播任何错误。
type postAgentTrigger struct {
	delegate agentapp.ReplyService
}

// OnPostMentioned 发帖 @提及 后的机器人触发入口。
// circleID 随事件透传（圈子级机器人只在同圈帖触发）。
func (b *postAgentTrigger) OnPostMentioned(postID, circleID, authorID uuid.UUID, mentionUserIDs []uuid.UUID) {
	b.delegate.OnPostMentioned(agentapp.PostMentionEvent{
		PostID:         postID,
		PostCircleID:   circleID,
		UserID:         authorID,
		MentionUserIDs: mentionUserIDs,
	})
}

// 编译期保证：触发桥接器满足 post 领域端口。
var _ postapp.AgentPostTrigger = (*postAgentTrigger)(nil)

// agentCandidateReader 桥接 aiagent.AutoReplyCandidateReader -> post / comment 域的自动回复候选 Facade。
//
// 候选过滤（已发布未锁定、门槛、时间窗口、作者非机器人账号、圈子范围、排除名单）全部在
// 各自领域的仓储 SQL 内完成，桥接器只做 DTO 转换。
type agentCandidateReader struct {
	post    postapp.PostService
	comment commentapp.CommentService
}

// ListHotPosts 自动回复帖子候选（按热度降序 top-N）。
func (b *agentCandidateReader) ListHotPosts(ctx context.Context, q agentapp.AutoPostQuery) ([]agentapp.AutoPostCandidate, error) {
	rows, err := b.post.ListAutoReplyCandidates(ctx, postapp.AutoReplyPostQuery{
		CircleID:   q.CircleID,
		HotMin:     q.HotMin,
		Since:      q.Since,
		ExcludeIDs: q.ExcludeIDs,
		Limit:      q.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]agentapp.AutoPostCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, agentapp.AutoPostCandidate{ID: r.ID, CircleID: r.CircleID, AuthorID: r.AuthorID, Hot: r.Hot})
	}
	return out, nil
}

// ListHotComments 自动回复评论候选（按 like_count + reply_count 降序 top-N）。
func (b *agentCandidateReader) ListHotComments(ctx context.Context, q agentapp.AutoCommentQuery) ([]agentapp.AutoCommentCandidate, error) {
	rows, err := b.comment.ListAutoReplyCandidates(ctx, commentapp.AutoReplyCommentQuery{
		CircleID:   q.CircleID,
		LikeMin:    q.LikeMin,
		ReplyMin:   q.ReplyMin,
		Since:      q.Since,
		ExcludeIDs: q.ExcludeIDs,
		Limit:      q.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]agentapp.AutoCommentCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, agentapp.AutoCommentCandidate{
			ID: r.ID, PostID: r.PostID, PostCircleID: r.PostCircleID,
			UserID: r.UserID, RootID: r.RootID, Content: r.Content,
		})
	}
	return out, nil
}

// 编译期保证：候选桥接器满足 aiagent 领域端口。
var _ agentapp.AutoReplyCandidateReader = (*agentCandidateReader)(nil)
