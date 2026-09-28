// Package infrastructure 提供 collect 领域基础设施层实现。
package infrastructure

import (
	"context"

	"interestBar/pkg/domains/collect/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// postCollectCacheRedis 基于 Redis 的 PostCollectCache 实现。
//
// 复用 pkg/server/storage/redis 中收藏专用的 Lua 原子设值脚本和 stats 操作。
type postCollectCacheRedis struct{}

// NewPostCollectCache 构造 PostCollectCache。
func NewPostCollectCache() domain.PostCollectCache {
	return &postCollectCacheRedis{}
}

func (c *postCollectCacheRedis) Set(ctx context.Context, userID, postID uuid.UUID, collected bool) (domain.ToggleResult, error) {
	r, err := redispkg.SetPostCollect(ctx, userID, postID, collected)
	if err != nil {
		return 0, err
	}
	return domain.ToggleResult(r), nil
}

func (c *postCollectCacheRedis) StatsExists(ctx context.Context, postID uuid.UUID) (bool, error) {
	return redispkg.PostStatisticsExists(postID)
}

func (c *postCollectCacheRedis) BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, []uuid.UUID, error) {
	return redispkg.BatchCheckPostCollected(userID, postIDs)
}

func (c *postCollectCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, collectedPostIDs []uuid.UUID) error {
	return redispkg.BackfillPostCollects(userID, collectedPostIDs)
}
