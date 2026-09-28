// Package infrastructure 提供 like 领域基础设施层实现。
package infrastructure

import (
	"context"

	"interestBar/pkg/domains/like/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// postLikeCacheRedis 基于 Redis 的 PostLikeCache 实现。
//
// 复用 pkg/server/storage/redis 中的 Lua 原子设值脚本和 stats 操作。
type postLikeCacheRedis struct{}

// NewPostLikeCache 构造 PostLikeCache。
func NewPostLikeCache() domain.PostLikeCache {
	return &postLikeCacheRedis{}
}

func (c *postLikeCacheRedis) Set(ctx context.Context, userID, postID uuid.UUID, liked bool) (domain.ToggleResult, error) {
	r, err := redispkg.SetPostLike(ctx, userID, postID, liked)
	if err != nil {
		return 0, err
	}
	return domain.ToggleResult(r), nil
}

func (c *postLikeCacheRedis) StatsExists(ctx context.Context, postID uuid.UUID) (bool, error) {
	return redispkg.PostStatisticsExists(postID)
}

// commentLikeCacheRedis 基于 Redis 的 CommentLikeCache 实现。
type commentLikeCacheRedis struct{}

// NewCommentLikeCache 构造 CommentLikeCache。
func NewCommentLikeCache() domain.CommentLikeCache {
	return &commentLikeCacheRedis{}
}

func (c *commentLikeCacheRedis) Set(ctx context.Context, userID, commentID uuid.UUID, liked bool) (domain.ToggleResult, error) {
	r, err := redispkg.SetCommentLike(ctx, userID, commentID, liked)
	if err != nil {
		return 0, err
	}
	return domain.ToggleResult(r), nil
}

func (c *commentLikeCacheRedis) StatsExists(ctx context.Context, commentID uuid.UUID) (bool, error) {
	return redispkg.CommentStatisticsExists(ctx, commentID)
}
