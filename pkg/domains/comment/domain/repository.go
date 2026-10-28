package domain

import (
	"context"
	"time"

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
	// ListActiveLikes 列出用户的有效评论点赞（最多 limit 条，新→旧），供点赞 ZSET 首次全量装载（#129）。
	ListActiveLikes(ctx context.Context, userID uuid.UUID, limit int) ([]CommentLike, error)
	// CreateMentions 批量写入评论 @提及 名单（发评论时的最终落库名单）。
	// 名单须先经 application 层校验（存在/去自/截断）；重复行幂等忽略。
	CreateMentions(ctx context.Context, commentID uuid.UUID, userIDs []uuid.UUID) error
	// GetMentionUserIDsByCommentIDs 批量获取评论的提及用户ID（按提及写入顺序）。
	// 返回以 commentID 为 key 的映射；无提及的评论不出现在 map 中。
	GetMentionUserIDsByCommentIDs(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
	// ListHotForAutoReply 机器人自动回复的评论候选：评论正常未删、Since 之后发布、点赞数 / 回复数
	// 达到任一门槛、作者不是机器人账号（users.role=2）、所属帖子已发布未锁定未删且在圈子范围内、
	// 不在排除名单，按 like_count + reply_count 降序取前 Limit 条。
	ListHotForAutoReply(ctx context.Context, f AutoReplyCommentFilter) ([]AutoReplyCommentRow, error)
}

// AutoReplyCommentFilter 机器人自动回复的评论候选过滤条件（aiagent 扫描器经 Facade 传入）。
// LikeMin / ReplyMin 满足任一即可，<=0 表示该条件不参与；两者都 <=0 时不返回任何候选。
type AutoReplyCommentFilter struct {
	CircleID   uuid.UUID   // 所属帖子的圈子范围；uuid.Nil = 全站
	LikeMin    int         // 点赞数门槛（含）
	ReplyMin   int         // 回复数门槛（含）
	Since      time.Time   // 只看该时间之后发布的评论
	ExcludeIDs []uuid.UUID // 已触发 / 已饱和的评论
	Limit      int
}

// AutoReplyCommentRow 评论候选行（评论字段 + 所属帖子的圈子）。
type AutoReplyCommentRow struct {
	ID           uuid.UUID  `gorm:"column:id"`
	PostID       uuid.UUID  `gorm:"column:post_id"`
	UserID       uuid.UUID  `gorm:"column:user_id"`
	RootID       *uuid.UUID `gorm:"column:root_id"`
	Content      string     `gorm:"column:content"`
	LikeCount    int        `gorm:"column:like_count"`
	ReplyCount   int        `gorm:"column:reply_count"`
	PostCircleID uuid.UUID  `gorm:"column:post_circle_id"`
}

// CommentStatsCache 评论统计信息缓存（like_count）。
type CommentStatsCache interface {
	// Exists 检查统计 Hash 是否存在。
	Exists(ctx context.Context, commentID uuid.UUID) (bool, error)
	// Set 设置统计信息（用于从 DB 恢复）。
	Set(ctx context.Context, commentID uuid.UUID, likeCount int) error
}

// LikeCheck 用户评论点赞 ZSET 批查结果。
type LikeCheck struct {
	// Hits 每个查询 commentID → 是否命中缓存（已赞）；未命中为 false。
	Hits map[uuid.UUID]bool
	// Missed 缓存无法判定、需回源 DB 的 commentID；ZSET 已完整加载时为空（缺失即未赞）。
	Missed []uuid.UUID
	// Loadable 完整加载标记不存在：调用方可一次性全量装载该用户的评论点赞（#129）。
	Loadable bool
}

// CommentLikeCache 评论点赞状态缓存（Redis ZSET + 完整加载标记）。
type CommentLikeCache interface {
	// BatchCheck 批量检查用户是否点赞了多条评论。
	BatchCheck(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (LikeCheck, error)
	// Backfill 回填 DB 查询确认的点赞状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, likedCommentIDs []uuid.UUID) error
	// LoadAll 全量装载用户的评论点赞并写完整加载标记；complete=false（超过装载上限）只写超限标记。
	LoadAll(ctx context.Context, userID uuid.UUID, likes []CommentLike, complete bool) error
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
