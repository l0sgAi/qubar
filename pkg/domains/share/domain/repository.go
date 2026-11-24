package domain

import (
	"context"

	"github.com/google/uuid"
)

// PostTarget share 领域需要的帖子查询能力（composition 桥接 post.application.PostService）。
type PostTarget interface {
	// GetShareable 帖子已发布、未删除且对 userID 可见（PostService.CanViewPost）时返回归属信息；
	// 否则返回 nil, nil。私密圈帖子对成员可见，因此成员可以分享（链接只有成员能打开）。
	GetShareable(ctx context.Context, userID, postID uuid.UUID) (*SharedPost, error)
}

// PostShareRepository post_share 记录表持久化（去重真值）。
type PostShareRepository interface {
	// Create 插入分享记录（INSERT … ON CONFLICT (user_id, post_id) DO NOTHING）。
	// created：是否真正插入（首次分享）；并发同一用户只有一个请求返回 true。
	Create(ctx context.Context, share *PostShare) (created bool, err error)
	// Delete 按主键删除分享记录（仅供事件投递失败时回滚刚插入的行）。
	Delete(ctx context.Context, id uuid.UUID) error
	// CountByPost 帖子的分享记录数（= 去重后的分享人数，分享数缓存回种用）。
	CountByPost(ctx context.Context, postID uuid.UUID) (int64, error)
}

// ShareCountCache 帖子分享数缓存（读路径真值，Redis String post:sharecnt:{post_id}）。
type ShareCountCache interface {
	// IncrIfCached 缓存存在时原子 +1 并返回新值；不存在返回 ok=false（调用方回种）。
	IncrIfCached(ctx context.Context, postID uuid.UUID) (count int64, ok bool, err error)
	// Get 读缓存；不存在返回 ok=false。
	Get(ctx context.Context, postID uuid.UUID) (count int64, ok bool, err error)
	// Seed 用 DB 行数回种（仅当不存在时写入），返回回种后的当前值。
	Seed(ctx context.Context, postID uuid.UUID, count int64) (int64, error)
}

// ShareEventPublisher 首次分享的事件发布。
type ShareEventPublisher interface {
	// PublishPostShare 同步发布首次分享事件（失败返回错误，调用方回滚分享记录），
	// 成功后 best-effort 发布分享数落库标记与帖子热度增量（失败只记日志）。
	PublishPostShare(ctx context.Context, share *PostShare, post *SharedPost) error
}
