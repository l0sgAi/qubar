package domain

import (
	"context"
	"errors"
	"time"

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
	// ListActiveLikes 列出用户的有效点赞（最多 limit 条，新→旧），供点赞 ZSET 首次全量装载（#129）。
	ListActiveLikes(ctx context.Context, userID uuid.UUID, limit int) ([]InteractionEntry, error)
	// ListActiveCollects 列出用户的有效收藏（最多 limit 条，新→旧），供收藏 ZSET 首次全量装载（#129）。
	ListActiveCollects(ctx context.Context, userID uuid.UUID, limit int) ([]InteractionEntry, error)
	// CreateMentions 批量写入帖子 @提及 名单（发帖时的最终落库名单）。
	// 名单须先经 application 层校验（存在/去自/截断）；重复行幂等忽略。
	CreateMentions(ctx context.Context, postID uuid.UUID, userIDs []uuid.UUID) error
	// GetMentionUserIDsByPostIDs 批量获取帖子的提及用户ID（按提及写入顺序）。
	// 返回以 postID 为 key 的映射；无提及的帖子不出现在 map 中。
	GetMentionUserIDsByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
	// ListHotForAutoReply 机器人自动回复的帖子候选：已发布、未锁定、未删除、hot >= HotMin、
	// Since 之后发布、作者不是机器人账号（users.role=2）、在圈子范围内、不在排除名单，
	// 按 hot 降序取前 Limit 条（只填 id / circle_id / user_id / hot）。
	ListHotForAutoReply(ctx context.Context, f AutoReplyPostFilter) ([]Post, error)
	// UpdateStatusFromReviewing 条件更新帖子状态：仅当帖子未删除且仍是审核中（status=2）时改为 status。
	// 返回是否真正更新了一行；false 表示已被其他路径处理（重复投递 / 人工审核 / 删帖），调用方跳过后续副作用。
	UpdateStatusFromReviewing(ctx context.Context, postID uuid.UUID, status int16) (bool, error)
	// ResolveReview 人工审核（#118）：同一事务内条件更新状态（同 UpdateStatusFromReviewing）并写审核日志。
	// 返回 false 表示帖子已不是审核中（并发处理 / worker 抢先 / 已删除），此时不写日志。
	ResolveReview(ctx context.Context, postID uuid.UUID, status int16, log *PostModerationLog) (bool, error)
	// ListReviewingByCircle 圈内审核中（status=2、未删除）的帖子，按 (create_time, id) 倒序 keyset 翻页；
	// after 为 nil 表示第一页，最多返回 limit 条。
	ListReviewingByCircle(ctx context.Context, circleID uuid.UUID, after *PostKeyset, limit int) ([]Post, error)
	// CountReviewingByCircle 圈内审核中（status=2、未删除）的帖子数，口径与 ListReviewingByCircle 一致（#156）。
	CountReviewingByCircle(ctx context.Context, circleID uuid.UUID) (int64, error)
	// GetCircleIDsByIDs 批量取 帖子ID → 所属 circle_id，不过滤删除 / 状态（通知跳转用，#156）；查不到的 ID 不在结果里。
	GetCircleIDsByIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
}

// AutoReplyPostFilter 机器人自动回复的帖子候选过滤条件（aiagent 扫描器经 Facade 传入）。
type AutoReplyPostFilter struct {
	CircleID   uuid.UUID   // 圈子范围；uuid.Nil = 全站（全局机器人）
	HotMin     int         // 热度门槛（含）
	Since      time.Time   // 只看该时间之后发布的帖子
	ExcludeIDs []uuid.UUID // 已触发 / 已饱和的帖子
	Limit      int
}

// PostKeyset 帖子 (create_time, id) 倒序翻页位置（上一页最后一条）。
type PostKeyset struct {
	CreateTime time.Time
	ID         uuid.UUID
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

// InteractionCheck 用户点赞 / 收藏 ZSET 批查结果。
type InteractionCheck struct {
	// Hits 每个查询 postID → 是否命中缓存（已赞 / 已藏）；未命中为 false。
	Hits map[uuid.UUID]bool
	// Missed 缓存无法判定、需回源 DB 的 postID；ZSET 已完整加载时为空（缺失即 false）。
	Missed []uuid.UUID
	// Loadable 完整加载标记不存在：调用方可一次性全量装载该用户的记录（#129）。
	// 记录数超过装载上限的用户标记为"超限"，Loadable=false，只能逐批回源。
	Loadable bool
}

// InteractionEntry 一条有效的点赞 / 收藏记录（全量装载用）。
type InteractionEntry struct {
	ID uuid.UUID // postID
	At time.Time // 交互时间（ZSET score）
}

// PostLikeCache 帖子点赞状态缓存（Redis ZSET + 完整加载标记）。
type PostLikeCache interface {
	// BatchCheck 批量检查用户是否点赞了多个帖子。
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (InteractionCheck, error)
	// Backfill 回填 DB 查询确认的点赞状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, likedPostIDs []uuid.UUID) error
	// LoadAll 全量装载用户的点赞并写完整加载标记；complete=false（超过装载上限）只写超限标记。
	LoadAll(ctx context.Context, userID uuid.UUID, entries []InteractionEntry, complete bool) error
}

// PostCollectCache 帖子收藏状态缓存（Redis ZSET + 完整加载标记，只读）。
// post 领域仅读取它来判断"当前用户是否收藏"（详情页 / 信息流 is_collected 回显）；
// 收藏的原子切换由 collect 领域负责。
type PostCollectCache interface {
	// BatchCheck 批量检查用户是否收藏了多个帖子。
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (InteractionCheck, error)
	// Backfill 回填 DB 查询确认的收藏状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, collectedPostIDs []uuid.UUID) error
	// LoadAll 全量装载用户的收藏并写完整加载标记；complete=false（超过装载上限）只写超限标记。
	LoadAll(ctx context.Context, userID uuid.UUID, entries []InteractionEntry, complete bool) error
}

// PostEventPublisher 帖子事件发布（异步持久化统计到 DB / 通知 / 审核）。
type PostEventPublisher interface {
	// PublishViewCount 发布浏览量变化事件。
	PublishViewCount(ctx context.Context, postID uuid.UUID) error
	// PublishCommentCount 发布评论数变化事件（评论创建 / 删除后调用）。
	// 消费者批量落库时按评论明细重算 comment_count，不累加增量（#130）。
	PublishCommentCount(ctx context.Context, postID uuid.UUID) error
	// PublishMentionNotice 发布 @提及 通知事件（消息中心）。
	// mentionUserIDs 由调用方校验（存在性/去自/截断）后传入；snippet 为帖子标题。
	PublishMentionNotice(ctx context.Context, actorID, postID uuid.UUID, mentionUserIDs []uuid.UUID, snippet string) error
	// PublishModeration 发布帖子审核事件（只带 post_id，worker 回源读库）。同步写入，返回错误表示事件未发出。
	PublishModeration(ctx context.Context, postID uuid.UUID) error
	// PublishReviewResultNotice 发布人工审核结果通知事件（通过 / 驳回 → 帖子作者，#118）。
	// 接收人与标题快照由通知 consumer 反查帖子；best-effort，失败只记日志。
	PublishReviewResultNotice(ctx context.Context, operatorID, postID uuid.UUID, approved bool) error
	// PublishModeratorNotice 发布圈管通知事件（帖子被举报达到阈值 / 已发布帖子疑似自伤 → 本圈圈主 + 管理员，#153）。
	// 接收人由通知 consumer 按帖子所属圈子反查；actorID 举报通知传 uuid.Nil（不暴露举报人），自伤通知传帖子作者。
	// best-effort，失败只记日志。
	PublishModeratorNotice(ctx context.Context, kind ModeratorNoticeKind, actorID, postID uuid.UUID) error
	// PublishPostRemovedNotice 发布「帖子被举报后经复核下架」通知事件（→ 帖子作者，#153）。best-effort。
	PublishPostRemovedNotice(ctx context.Context, operatorID, postID uuid.UUID) error
}

// ModeratorNoticeKind 圈管通知种类（#153）。
type ModeratorNoticeKind int

const (
	ModeratorNoticeReported ModeratorNoticeKind = iota + 1 // 帖子待处理举报达到通知阈值
	ModeratorNoticeSelfHarm                                // 已发布帖子疑似自伤 / 轻生倾向，需要人工关怀
)

// PostStatistics 帖子统计信息。
type PostStatistics struct {
	ViewCount    int `json:"view_count"`
	CommentCount int `json:"comment_count"`
	LikeCount    int `json:"like_count"`
	CollectCount int `json:"collect_count"`
}

// PostMediaFetcherError 保留：未来可在领域层定义更细的错误类型。
var ErrMediaFetchFailed = errors.New("failed to fetch post media")
