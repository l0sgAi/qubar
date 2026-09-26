package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// PostRepository 是 post 领域的持久化接口。
type PostRepository interface {
	// GetByID 根据 ID 获取帖子（不限制 status）。未找到返回 ErrPostNotFound。
	GetByID(ctx context.Context, postID uuid.UUID) (*Post, error)
	// Create 创建帖子。
	Create(ctx context.Context, post *Post) error
	// GetMediaByPostIDs 批量获取帖子的图片列表（仅查 id + media_extra）。
	GetMediaByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]MediaExtraJSON, error)
	// ListByIDs 批量获取帖子（仅未删除 + 已发布），供 collect「我的收藏」组装用。
	ListByIDs(ctx context.Context, postIDs []uuid.UUID) ([]Post, error)
	// IsLiked 检查用户是否点赞了帖子（DB 回源用）。
	IsLiked(ctx context.Context, userID, postID uuid.UUID) (bool, error)
	// IsCollected 检查用户是否收藏了帖子（DB 回源用，详情页 is_collected 缓存 miss 时调用）。
	IsCollected(ctx context.Context, userID, postID uuid.UUID) (bool, error)
	// BatchIsLiked 批量检查用户点赞了哪些帖子（DB 回源用，信息流 is_liked 缓存 miss 时调用）。
	// 返回已赞 postID 集合（未赞的不在 map 中）。
	BatchIsLiked(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, error)
	// BatchIsCollected 批量检查用户收藏了哪些帖子（DB 回源用，信息流 is_collected 缓存 miss 时调用）。
	// 返回已收藏 postID 集合（未收藏的不在 map 中）。
	BatchIsCollected(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, error)
	// IncrCommentCount 同步递增帖子评论计数（DB UPDATE comment_count + 1）。
	// 供 comment 领域发评论后调用，替代旧的 Redpanda 异步聚合。
	IncrCommentCount(ctx context.Context, postID uuid.UUID) error
	// CreateMentions 批量写入帖子 @提及 名单（发帖时的最终落库名单）。
	// 名单须先经 application 层校验（存在/去自/截断）；重复行幂等忽略。
	CreateMentions(ctx context.Context, postID uuid.UUID, userIDs []uuid.UUID) error
	// GetMentionUserIDsByPostIDs 批量获取帖子的提及用户ID（按提及写入顺序）。
	// 返回以 postID 为 key 的映射；无提及的帖子不出现在 map 中。
	GetMentionUserIDsByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

// PostStatsCache 帖子统计信息缓存（view_count/comment_count/like_count 等）。
type PostStatsCache interface {
	// Exists 检查统计 Hash 是否存在。
	Exists(ctx context.Context, postID uuid.UUID) (bool, error)
	// Get 获取统计信息。未命中返回 nil, nil。
	Get(ctx context.Context, postID uuid.UUID) (*PostStatistics, error)
	// Set 设置统计信息（用于从 DB 恢复）。
	Set(ctx context.Context, postID uuid.UUID, stats *PostStatistics) error
	// BatchGet 批量获取统计信息（pipeline，1 RTT）。仅返回字段齐全的 postID；
	// 未命中/部分缺失的不计入（调用方走 DB/ES baseline）。
	BatchGet(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]*PostStatistics, error)
	// SetIfAbsent 仅当字段不存在时回种（HSetNX）。用于读路径 miss 回种，
	// 避免覆盖并发浏览量自增。
	SetIfAbsent(ctx context.Context, postID uuid.UUID, stats *PostStatistics) error
	// IncrViewCount 增加浏览量（带去重），返回新计数值。
	IncrViewCount(ctx context.Context, postID, userID uuid.UUID) (int64, error)
	// IncrCommentCount 递增帖子评论计数（Hash 字段 comment_count +1）。
	// 供 comment 领域发评论后调用。
	IncrCommentCount(ctx context.Context, postID uuid.UUID) error
}

// PostLikeCache 帖子点赞状态缓存（Redis ZSET）。
type PostLikeCache interface {
	// BatchCheck 批量检查用户是否点赞了多个帖子。
	// 返回：已点赞的 map、未命中的 postID 列表、error。
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (liked map[uuid.UUID]bool, missed []uuid.UUID, err error)
	// Backfill 回填 DB 查询确认的点赞状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, likedPostIDs []uuid.UUID) error
}

// PostCollectCache 帖子收藏状态缓存（Redis ZSET，只读）。
// post 领域仅读取它来判断"当前用户是否收藏"（详情页 is_collected 回显）；
// 收藏的原子切换由 collect 领域负责。
type PostCollectCache interface {
	// BatchCheck 批量检查用户是否收藏了多个帖子。
	// 返回：已收藏的 map、未命中的 postID 列表、error。
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (collected map[uuid.UUID]bool, missed []uuid.UUID, err error)
	// Backfill 回填 DB 查询确认的收藏状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, collectedPostIDs []uuid.UUID) error
}

// PostEventPublisher 帖子事件发布（异步持久化统计到 DB）。
type PostEventPublisher interface {
	// PublishViewCount 发布浏览量变化事件。
	PublishViewCount(ctx context.Context, postID uuid.UUID) error
	// PublishMentionNotice 发布 @提及 通知事件（消息中心）。
	// mentionUserIDs 由调用方校验（存在性/去自/截断）后传入；snippet 为帖子标题。
	PublishMentionNotice(ctx context.Context, actorID, postID uuid.UUID, mentionUserIDs []uuid.UUID, snippet string) error
}

// PostStatistics 帖子统计信息。
type PostStatistics struct {
	ViewCount    int `json:"view_count"`
	CommentCount int `json:"comment_count"`
	LikeCount    int `json:"like_count"`
	CollectCount int `json:"collect_count"`
}

// PostMediaFetcherError 保留：未来可在领域层定义更细的错误类型。
var ErrMediaFetchFailed = errors.New("failed to fetch post media")
