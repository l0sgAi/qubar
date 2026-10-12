package domain

import (
	"context"

	"github.com/google/uuid"
)

// PostLikeCache 帖子点赞缓存（Redis ZSET + stats Hash + Lua 原子设值）。
//
// like 领域用这个接口完成"帖子点赞"的原子设值（ZSET 增删 + stats Hash 增减）。
type PostLikeCache interface {
	// Set 原子设置帖子点赞状态（liked=true 赞 / false 取消）。
	// 已处于期望状态时返回 ToggleResultUnchanged，不修改计数。
	Set(ctx context.Context, userID, postID uuid.UUID, liked bool) (ToggleResult, error)
	// StatsExists 检查帖子统计 Hash 是否存在（用于恢复缓存）。
	StatsExists(ctx context.Context, postID uuid.UUID) (bool, error)
}

// CommentLikeCache 评论点赞缓存（Redis ZSET + stats Hash + Lua 原子设值）。
type CommentLikeCache interface {
	// Set 原子设置评论点赞状态（liked=true 赞 / false 取消）。
	// 已处于期望状态时返回 ToggleResultUnchanged，不修改计数。
	Set(ctx context.Context, userID, commentID uuid.UUID, liked bool) (ToggleResult, error)
	// StatsExists 检查评论统计 Hash 是否存在（用于恢复缓存）。
	StatsExists(ctx context.Context, commentID uuid.UUID) (bool, error)
}

// LikeEventPublisher 点赞事件发布（异步持久化到 DB）。
type LikeEventPublisher interface {
	// PublishPostLike 发布帖子点赞事件。
	// amount: 1=点赞, -1=取消点赞。
	PublishPostLike(ctx context.Context, userID, postID uuid.UUID, amount int64) error
	// PublishCommentLike 发布评论点赞事件。
	// postID 是冗余字段（评论所属帖子ID），用于消费者更新 comment.post_id。
	PublishCommentLike(ctx context.Context, userID, commentID, postID uuid.UUID, amount int64) error
}
