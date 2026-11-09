package infrastructure

import (
	"context"

	"interestBar/pkg/domains/comment/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// commentLikeCacheRedis 基于 Redis ZSET 的 CommentLikeCache 实现。
type commentLikeCacheRedis struct{}

// NewCommentLikeCache 构造 CommentLikeCache。
func NewCommentLikeCache() domain.CommentLikeCache {
	return &commentLikeCacheRedis{}
}

func (c *commentLikeCacheRedis) BatchCheck(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (domain.LikeCheck, error) {
	r, err := redispkg.BatchCheckCommentLiked(ctx, userID, commentIDs)
	return domain.LikeCheck{Hits: r.Hits, Missed: r.Missed, Loadable: r.State == redispkg.InteractionUnloaded}, err
}

func (c *commentLikeCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, likedCommentIDs []uuid.UUID) error {
	return redispkg.BackfillCommentLikes(ctx, userID, likedCommentIDs)
}

func (c *commentLikeCacheRedis) LoadAll(ctx context.Context, userID uuid.UUID, likes []domain.CommentLike, complete bool) error {
	entries := make([]redispkg.InteractionEntry, len(likes))
	for i, l := range likes {
		entries[i] = redispkg.InteractionEntry{ID: l.CommentID, At: l.UpdateTime}
	}
	return redispkg.LoadCommentLikes(ctx, userID, entries, complete)
}
