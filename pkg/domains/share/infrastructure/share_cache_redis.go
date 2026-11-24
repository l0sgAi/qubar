package infrastructure

import (
	"context"

	"interestBar/pkg/domains/share/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// shareCountCacheRedis 基于 Redis String 的 ShareCountCache 实现（脚本见 redis/share_lua.go）。
type shareCountCacheRedis struct{}

// NewShareCountCache 构造 ShareCountCache。
func NewShareCountCache() domain.ShareCountCache {
	return &shareCountCacheRedis{}
}

func (c *shareCountCacheRedis) IncrIfCached(ctx context.Context, postID uuid.UUID) (int64, bool, error) {
	return redispkg.IncrPostShareCountIfCached(ctx, postID)
}

func (c *shareCountCacheRedis) Get(ctx context.Context, postID uuid.UUID) (int64, bool, error) {
	return redispkg.GetPostShareCount(ctx, postID)
}

func (c *shareCountCacheRedis) Seed(ctx context.Context, postID uuid.UUID, count int64) (int64, error) {
	return redispkg.SeedPostShareCount(ctx, postID, count)
}
