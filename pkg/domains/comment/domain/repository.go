package domain

import (
	"context"

	"github.com/google/uuid"
)

// CommentRepository 是 comment 领域的持久化接口。
type CommentRepository interface {
	// Create 创建评论（事务内：插入评论 + 如为回复则递增根评论 reply_count）。
	Create(ctx context.Context, comment *Comment) error
	// GetByID 根据 ID 获取评论（未删除）。未找到返回 ErrCommentNotFound。
	GetByID(ctx context.Context, commentID uuid.UUID) (*Comment, error)
	// GetRootCommentsByCursor 游标分页获取帖子的顶层评论。
	// sort: 0=按点赞倒序, 1=按时间倒序。
	// 返回评论列表、下一页游标、是否有更多、错误。
	GetRootCommentsByCursor(ctx context.Context, postID uuid.UUID, size, sort int, cursor string) ([]Comment, string, bool, error)
	// GetRepliesByCursor 游标分页获取某条评论的子回复。
	// sort: 0=按点赞倒序, 1=按时间倒序（与 GetRootCommentsByCursor 同一套排序键映射）。
	GetRepliesByCursor(ctx context.Context, rootID uuid.UUID, size, sort int, cursor string) ([]Comment, string, bool, error)
	// LocateRootCursor 计算顶层列表的定位游标：把它作为列表接口的 cursor 传入时，
	// 返回页（页大小 size）包含 target。target 在首页时返回 ""。
	// sort 语义同 GetRootCommentsByCursor；target 必须是该帖的顶层评论。
	LocateRootCursor(ctx context.Context, postID uuid.UUID, sort int, target *Comment, size int) (string, error)
	// LocateReplyCursor 计算回复列表的定位游标与页码（从 1 开始，按页大小 size 计）：
	// 返回的游标作为回复列表接口的 cursor 传入时，返回页包含 target。
	// target 在回复首页时返回 "", 1。sort 语义同 GetRepliesByCursor；
	// target 必须是 rootID 下的直接回复。
	LocateReplyCursor(ctx context.Context, rootID uuid.UUID, sort int, target *Comment, size int) (string, int, error)
	// IsLiked 检查用户是否点赞了评论（DB 回源用）。
	IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error)
	// BatchCheckLiked 批量检查用户是否点赞了多条评论（DB 回源用）。
	BatchCheckLiked(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error)
	// CreateMentions 批量写入评论 @提及 名单（发评论时的最终落库名单）。
	// 名单须先经 application 层校验（存在/去自/截断）；重复行幂等忽略。
	CreateMentions(ctx context.Context, commentID uuid.UUID, userIDs []uuid.UUID) error
	// GetMentionUserIDsByCommentIDs 批量获取评论的提及用户ID（按提及写入顺序）。
	// 返回以 commentID 为 key 的映射；无提及的评论不出现在 map 中。
	GetMentionUserIDsByCommentIDs(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

// CommentStatsCache 评论统计信息缓存（like_count）。
type CommentStatsCache interface {
	// Exists 检查统计 Hash 是否存在。
	Exists(ctx context.Context, commentID uuid.UUID) (bool, error)
	// Set 设置统计信息（用于从 DB 恢复）。
	Set(ctx context.Context, commentID uuid.UUID, likeCount int) error
}

// CommentLikeCache 评论点赞状态缓存（Redis ZSET）。
type CommentLikeCache interface {
	// BatchCheck 批量检查用户是否点赞了多条评论。
	// 返回：已点赞的 map（未命中的 key 值为 false）、error。
	BatchCheck(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error)
	// Backfill 回填 DB 查询确认的点赞状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, likedCommentIDs []uuid.UUID) error
}

// CommentEventPublisher 评论事件发布（异步累积帖子热度 + CF 互动）。
type CommentEventPublisher interface {
	// PublishCommentHot 发布评论对帖子热度的贡献。
	// dir: +1 创建评论；-1 删除评论（TODO: 删除评论功能未实现，预留）。
	// 权重与 per-post 上限（cap.comment）由源头 Lua 原子 clamp，本方法只发布最终 Δ。
	PublishCommentHot(ctx context.Context, postID uuid.UUID, dir int) error
	// PublishCommentInteraction 发布评论者对帖子的 CF 互动（weight=comment）。
	// 由 CreateComment 调用（有 userID + postID；PublishCommentHot 不带 userID 故单独方法）。
	PublishCommentInteraction(ctx context.Context, userID, postID uuid.UUID) error
	// PublishCommentNotice 发布评论通知事件（消息中心）。
	// isReply=false → comment_post（接收人=帖子作者）；isReply=true → reply_comment
	// （接收人=被回复评论作者）。接收人均由 consumer 反查解析。
	PublishCommentNotice(ctx context.Context, userID, postID, commentID uuid.UUID, isReply bool, snippet string) error
	// PublishMentionNotice 发布 @提及 通知事件（消息中心）。
	// mentionUserIDs 由调用方校验（存在性/去自/截断）后传入；commentID 可空（帖子提及）。
	PublishMentionNotice(ctx context.Context, actorID uuid.UUID, postID, commentID *uuid.UUID, mentionUserIDs []uuid.UUID, snippet string) error
}
