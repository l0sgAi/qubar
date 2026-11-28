// 列表按页查询缓存，合并未命中 ID 回源窄表。
package application

import (
	"context"
	"errors"
	"slices"
	"testing"

	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// fakeOverlayStatsCache 模拟 post:stats：vals 为字段齐全的 Hash，未命中不返回。
type fakeOverlayStatsCache struct {
	domain.PostStatsCache
	vals  map[uuid.UUID]*domain.PostStatistics
	err   error
	calls int
}

func (c *fakeOverlayStatsCache) BatchGet(_ context.Context, postIDs []uuid.UUID) (map[uuid.UUID]*domain.PostStatistics, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	out := map[uuid.UUID]*domain.PostStatistics{}
	for _, id := range postIDs {
		if st, ok := c.vals[id]; ok {
			out[id] = st
		}
	}
	return out, nil
}

type statsRepo struct {
	domain.PostRepository
	rows  map[uuid.UUID]domain.PostStatistics
	calls [][]uuid.UUID
}

func (r *statsRepo) GetStats(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.PostStatistics, error) {
	r.calls = append(r.calls, slices.Clone(ids))
	return r.rows, nil
}

func TestApplyStatsOverlay(t *testing.T) {
	t.Parallel()
	hit, miss, empty := uuid.New(), uuid.New(), uuid.New()
	live := domain.PostStatistics{ViewCount: 11, LikeCount: 5}
	persisted := domain.PostStatistics{ViewCount: 7, CommentCount: 2, CollectCount: 3}
	cache := &fakeOverlayStatsCache{vals: map[uuid.UUID]*domain.PostStatistics{hit: &live}}
	repo := &statsRepo{rows: map[uuid.UUID]domain.PostStatistics{miss: persisted}}
	svc := &postServiceImpl{statsCache: cache, repo: repo}
	posts := []PostListItem{{ID: hit}, {ID: miss}, {ID: empty, LikeCount: 123}}
	svc.applyStatsOverlay(t.Context(), posts)
	if cache.calls != 1 || len(repo.calls) != 1 || !slices.Equal(repo.calls[0], []uuid.UUID{miss, empty}) {
		t.Fatalf("cache calls = %d, PG batches = %v", cache.calls, repo.calls)
	}
	if posts[0].LikeCount != 5 || posts[1].ViewCount != 7 || posts[1].CollectCount != 3 || posts[2].LikeCount != 0 {
		t.Fatalf("unexpected counts: %+v", posts)
	}
	svc.BatchGetStats(t.Context(), []uuid.UUID{hit})
	svc.BatchGetStats(t.Context(), nil)
	if len(repo.calls) != 1 || cache.calls != 2 {
		t.Fatalf("hit/empty added a lookup: cache %d, PG %v", cache.calls, repo.calls)
	}
	cache.err = errors.New("redis down")
	if got := svc.BatchGetStats(t.Context(), []uuid.UUID{miss}); got[miss] != persisted || len(repo.calls) != 2 {
		t.Fatalf("Redis unavailable: %v, PG calls %v", got, repo.calls)
	}
}
