// Package application 的列表计数覆盖单元测试（#250）。
//
// 覆盖：BatchGetCachedStats 只返回 Redis post:stats 命中的帖子、Redis 出错降级为空 map 不报错、一页一次 BatchGet；
// applyStatsOverlay 改为复用它后行为不变（命中覆盖，未命中保留快照）。
package application

import (
	"context"
	"errors"
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

func TestBatchGetCachedStats(t *testing.T) {
	t.Parallel()
	hit, miss := uuid.New(), uuid.New()
	cache := &fakeOverlayStatsCache{vals: map[uuid.UUID]*domain.PostStatistics{
		hit: {ViewCount: 11, CommentCount: 3, LikeCount: 4, CollectCount: 2},
	}}
	svc := &postServiceImpl{statsCache: cache}

	got := svc.BatchGetCachedStats(context.Background(), []uuid.UUID{hit, miss})
	if len(got) != 1 || got[hit] != (domain.PostStatistics{ViewCount: 11, CommentCount: 3, LikeCount: 4, CollectCount: 2}) {
		t.Fatalf("BatchGetCachedStats = %v; want only hit", got)
	}
	if cache.calls != 1 {
		t.Fatalf("BatchGet calls = %d; want 1", cache.calls)
	}
	if got := svc.BatchGetCachedStats(context.Background(), nil); len(got) != 0 || cache.calls != 1 {
		t.Fatalf("empty ids: got %v, calls %d; want empty map without Redis call", got, cache.calls)
	}
}

func TestBatchGetCachedStats_CacheErrorDegrades(t *testing.T) {
	t.Parallel()
	svc := &postServiceImpl{statsCache: &fakeOverlayStatsCache{err: errors.New("redis down")}}
	got := svc.BatchGetCachedStats(context.Background(), []uuid.UUID{uuid.New()})
	if got == nil || len(got) != 0 {
		t.Fatalf("BatchGetCachedStats on error = %v; want empty non-nil map", got)
	}
}

func TestApplyStatsOverlay(t *testing.T) {
	t.Parallel()
	hit, miss := uuid.New(), uuid.New()
	newPosts := func() []PostListItem {
		return []PostListItem{
			{ID: hit, ViewCount: 10, CommentCount: 2, LikeCount: 3, CollectCount: 1},
			{ID: miss, ViewCount: 20, CommentCount: 4, LikeCount: 5, CollectCount: 6},
		}
	}

	svc := &postServiceImpl{statsCache: &fakeOverlayStatsCache{vals: map[uuid.UUID]*domain.PostStatistics{
		hit: {ViewCount: 11, CommentCount: 3, LikeCount: 4, CollectCount: 2},
	}}}
	posts := newPosts()
	svc.applyStatsOverlay(context.Background(), posts)
	if p := posts[0]; p.ViewCount != 11 || p.CommentCount != 3 || p.LikeCount != 4 || p.CollectCount != 2 {
		t.Fatalf("hit = %+v; want overlaid", p)
	}
	if p := posts[1]; p.ViewCount != 20 || p.CommentCount != 4 || p.LikeCount != 5 || p.CollectCount != 6 {
		t.Fatalf("miss = %+v; want snapshot kept", p)
	}

	failing := &postServiceImpl{statsCache: &fakeOverlayStatsCache{err: errors.New("redis down")}}
	posts = newPosts()
	failing.applyStatsOverlay(context.Background(), posts)
	if p := posts[0]; p.ViewCount != 10 || p.LikeCount != 3 {
		t.Fatalf("on error = %+v; want snapshot kept", p)
	}
}
