package domain

import (
	"context"

	"github.com/google/uuid"
)

// UnreadCache 私信未读总数缓存（#241，读路径 Redis 优先、miss 回源 DB）。
//
// 真值是 DB：SUM(conversation_member.unread_count) WHERE user_id = ? AND deleted = 0。缓存只是加速：
// 发送后对接收方 IncrIfExists（key 不存在不建，等读 miss 回源，避免从 0 起算少计）；
// 标记已读 / 删除会话后 Invalidate（清零的条数不确定，直接删，下一次读回源校正）。
type UnreadCache interface {
	// Get 读取未读总数。miss（key 不存在）返回 ok=false。
	Get(ctx context.Context, userID uuid.UUID) (count int64, ok bool, err error)
	// Set 回填未读总数（读 miss 回源后调用）。
	Set(ctx context.Context, userID uuid.UUID, count int64) error
	// IncrIfExists key 存在时原子加 delta（不改 TTL），不存在时不动作。
	IncrIfExists(ctx context.Context, userID uuid.UUID, delta int64) error
	// Invalidate 删除缓存（下一次读回源 DB）。
	Invalidate(ctx context.Context, userID uuid.UUID) error
}
