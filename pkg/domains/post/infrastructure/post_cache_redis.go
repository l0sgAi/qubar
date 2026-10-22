package infrastructure

import (
	"context"

	"interestBar/pkg/domains/post/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// postStatsCacheRedis 基于 Redis Hash 的 PostStatsCache 实现。
type postStatsCacheRedis struct{}

// NewPostStatsCache 构造 PostStatsCache。
func NewPostStatsCache() domain.PostStatsCache {
	return &postStatsCacheRedis{}
}

func (c *postStatsCacheRedis) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	return redispkg.PostStatisticsExists(postID)
}

func (c *postStatsCacheRedis) Get(ctx context.Context, postID uuid.UUID) (*domain.PostStatistics, error) {
	stats, err := redispkg.GetPostStatistics(postID)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		return nil, nil
	}
	return &domain.PostStatistics{
		ViewCount:    stats.ViewCount,
		CommentCount: stats.CommentCount,
		LikeCount:    stats.LikeCount,
		CollectCount: stats.CollectCount,
	}, nil
}

func (c *postStatsCacheRedis) Set(ctx context.Context, postID uuid.UUID, stats *domain.PostStatistics) error {
	return redispkg.UpdatePostStatistics(postID, &redispkg.PostStatistics{
		ViewCount:    stats.ViewCount,
		CommentCount: stats.CommentCount,
		LikeCount:    stats.LikeCount,
		CollectCount: stats.CollectCount,
	})
}

func (c *postStatsCacheRedis) BatchGet(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]*domain.PostStatistics, error) {
	raw, err := redispkg.GetPostStatisticsBatch(postIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]*domain.PostStatistics, len(raw))
	for pid, st := range raw {
		result[pid] = &domain.PostStatistics{
			ViewCount:    st.ViewCount,
			CommentCount: st.CommentCount,
			LikeCount:    st.LikeCount,
			CollectCount: st.CollectCount,
		}
	}
	return result, nil
}

func (c *postStatsCacheRedis) SetIfAbsent(ctx context.Context, postID uuid.UUID, stats *domain.PostStatistics) error {
	return redispkg.SeedPostStatisticsIfAbsent(postID, &redispkg.PostStatistics{
		ViewCount:    stats.ViewCount,
		CommentCount: stats.CommentCount,
		LikeCount:    stats.LikeCount,
		CollectCount: stats.CollectCount,
	})
}

func (c *postStatsCacheRedis) IncrViewCount(ctx context.Context, postID, userID uuid.UUID) (int64, error) {
	return redispkg.IncrementPostViewCount(postID, userID)
}

func (c *postStatsCacheRedis) IncrCommentCount(ctx context.Context, postID uuid.UUID) error {
	return redispkg.IncrementPostCommentCount(postID)
}

// postLikeCacheRedis 基于 Redis ZSET 的 PostLikeCache 实现。
type postLikeCacheRedis struct{}

// NewPostLikeCache 构造 PostLikeCache。
func NewPostLikeCache() domain.PostLikeCache {
	return &postLikeCacheRedis{}
}

func (c *postLikeCacheRedis) BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (domain.InteractionCheck, error) {
	r, err := redispkg.BatchCheckPostLiked(ctx, userID, postIDs)
	return toInteractionCheck(r), err
}

func (c *postLikeCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, likedPostIDs []uuid.UUID) error {
	return redispkg.BackfillPostLikes(userID, likedPostIDs)
}

func (c *postLikeCacheRedis) LoadAll(ctx context.Context, userID uuid.UUID, entries []domain.InteractionEntry, complete bool) error {
	return redispkg.LoadPostLikes(ctx, userID, toRedisEntries(entries), complete)
}

// postCollectCacheRedis 基于 Redis ZSET 的 PostCollectCache 实现（只读 BatchCheck/Backfill）。
type postCollectCacheRedis struct{}

// NewPostCollectCache 构造 PostCollectCache。
func NewPostCollectCache() domain.PostCollectCache {
	return &postCollectCacheRedis{}
}

func (c *postCollectCacheRedis) BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (domain.InteractionCheck, error) {
	r, err := redispkg.BatchCheckPostCollected(ctx, userID, postIDs)
	return toInteractionCheck(r), err
}

func (c *postCollectCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, collectedPostIDs []uuid.UUID) error {
	return redispkg.BackfillPostCollects(userID, collectedPostIDs)
}

func (c *postCollectCacheRedis) LoadAll(ctx context.Context, userID uuid.UUID, entries []domain.InteractionEntry, complete bool) error {
	return redispkg.LoadPostCollects(ctx, userID, toRedisEntries(entries), complete)
}

func toInteractionCheck(r redispkg.InteractionCheck) domain.InteractionCheck {
	return domain.InteractionCheck{Hits: r.Hits, Missed: r.Missed, Loadable: r.State == redispkg.InteractionUnloaded}
}

func toRedisEntries(entries []domain.InteractionEntry) []redispkg.InteractionEntry {
	out := make([]redispkg.InteractionEntry, len(entries))
	for i, e := range entries {
		out[i] = redispkg.InteractionEntry{ID: e.ID, At: e.At}
	}
	return out
}
