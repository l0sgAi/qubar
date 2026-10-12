package domain

import (
	"context"

	"github.com/google/uuid"
)

// PostTarget collect 领域需要的帖子查询能力（镜像 like.PostTarget）。
type PostTarget interface {
	// Exists 检查帖子是否存在（未删除）。存在返回 true，不存在返回 false。
	Exists(ctx context.Context, postID uuid.UUID) (bool, error)
	// RestoreStats 恢复帖子统计缓存（如果不存在）。
	// 用于收藏前确保 Redis stats Hash 存在，避免 Lua 脚本读到空 stats。
	RestoreStats(ctx context.Context, postID uuid.UUID) error
}

// PostCollectCache 帖子收藏缓存（Redis ZSET + stats Hash + Lua 原子设值）。
type PostCollectCache interface {
	// Set 原子设置帖子收藏状态（collected=true 收藏 / false 取消）。
	// 已处于期望状态时返回 ToggleResultUnchanged，不修改计数。
	Set(ctx context.Context, userID, postID uuid.UUID, collected bool) (ToggleResult, error)
	// StatsExists 检查帖子统计 Hash 是否存在（用于恢复缓存）。
	StatsExists(ctx context.Context, postID uuid.UUID) (bool, error)
	// BatchCheck 批量检查用户是否收藏了多个帖子（信息流「是否已收藏」回显）。
	// 返回：已收藏的 map、未命中的 postID 列表、error。
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (collected map[uuid.UUID]bool, missed []uuid.UUID, err error)
	// Backfill 回填 DB 查询确认的收藏状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, collectedPostIDs []uuid.UUID) error
}

// CollectEventPublisher 收藏事件发布（异步持久化到 DB）。
type CollectEventPublisher interface {
	// PublishPostCollect 发布帖子收藏事件。
	// amount: 1=收藏, -1=取消收藏。
	PublishPostCollect(ctx context.Context, userID, postID uuid.UUID, amount int64) error
}

// PostCollectRepository post_collect 流水表持久化。
type PostCollectRepository interface {
	// ListCollectedPostIDs 按收藏时间倒序分页查询用户收藏的帖子ID（keyset 游标分页）。
	// cursor 为首页空串；返回 postID 列表、总数、下一页游标（末页为空串）、error。
	// keyword 非空时 JOIN domains.post 过滤 title/summary（ILIKE）+ 仅已发布未删帖，
	// 返回的 postIDs 与 total 均仅计匹配项。游标非法返回 ErrInvalidCursor。
	ListCollectedPostIDs(ctx context.Context, userID uuid.UUID, keyword string, size int, cursor string) (postIDs []uuid.UUID, total int64, nextCursor string, err error)
	// IsCollected 检查用户是否收藏了帖子（DB 回源用，缓存 miss 时调用）。
	IsCollected(ctx context.Context, userID, postID uuid.UUID) (bool, error)
	// SetCollected 同步 upsert 收藏流水行（active=true 新增/恢复，active=false 标记取消）。
	// 供 Toggle 即时入库：收藏流水是「我的收藏」列表的权威源，必须即时可见。
	// 返回 changed：行状态是否真正发生迁移（已处于期望态为 false）。幂等、并发安全。
	SetCollected(ctx context.Context, userID, postID uuid.UUID, active bool) (changed bool, err error)
}
