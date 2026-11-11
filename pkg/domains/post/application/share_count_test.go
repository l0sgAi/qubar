package application

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// fakeShareCountCache 模拟 post:sharecnt：SeedIfAbsent 只写不存在的 key（SET NX）。
type fakeShareCountCache struct {
	vals    map[uuid.UUID]int64
	getErr  error
	seedErr error
	seeded  map[uuid.UUID]int64
}

func (c *fakeShareCountCache) BatchGet(_ context.Context, postIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	out := map[uuid.UUID]int64{}
	for _, id := range postIDs {
		if v, ok := c.vals[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func (c *fakeShareCountCache) SeedIfAbsent(_ context.Context, counts map[uuid.UUID]int64) error {
	if c.seedErr != nil {
		return c.seedErr
	}
	if c.vals == nil {
		c.vals = map[uuid.UUID]int64{}
	}
	if c.seeded == nil {
		c.seeded = map[uuid.UUID]int64{}
	}
	for id, n := range counts {
		c.seeded[id] = n
		if _, ok := c.vals[id]; !ok {
			c.vals[id] = n
		}
	}
	return nil
}

// fakeShareRepo 只实现分享数 / 分享状态用到的查询，记录 PG 回源的入参。
type fakeShareRepo struct {
	domain.PostRepository

	shareCounts map[uuid.UUID]int64
	countErr    error
	countCalls  [][]uuid.UUID
	shared      map[uuid.UUID]bool
	sharedErr   error
}

func (r *fakeShareRepo) GetShareCounts(_ context.Context, postIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	r.countCalls = append(r.countCalls, postIDs)
	if r.countErr != nil {
		return nil, r.countErr
	}
	out := map[uuid.UUID]int64{}
	for _, id := range postIDs {
		if n, ok := r.shareCounts[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func (r *fakeShareRepo) IsShared(_ context.Context, _, postID uuid.UUID) (bool, error) {
	return r.shared[postID], r.sharedErr
}

func (r *fakeShareRepo) BatchIsShared(_ context.Context, _ uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if r.sharedErr != nil {
		return nil, r.sharedErr
	}
	out := map[uuid.UUID]bool{}
	for _, id := range postIDs {
		if r.shared[id] {
			out[id] = true
		}
	}
	return out, nil
}

func newShareSvc(repo *fakeShareRepo, cache *fakeShareCountCache) *postServiceImpl {
	s := &postServiceImpl{repo: repo}
	if cache != nil {
		s.SetShareCountCache(cache)
	}
	return s
}

// 命中用缓存值；miss 先用调用方已读到的 PG 值，其余一次 PG 回源；只回种 miss 的帖子。
func TestResolveShareCounts_CacheHitKnownAndDBFallback(t *testing.T) {
	hit, known, cold, gone := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := &fakeShareRepo{shareCounts: map[uuid.UUID]int64{hit: 1, known: 99, cold: 4}}
	cache := &fakeShareCountCache{vals: map[uuid.UUID]int64{hit: 7}}
	s := newShareSvc(repo, cache)

	got := s.resolveShareCounts(t.Context(), []uuid.UUID{hit, known, cold, gone}, map[uuid.UUID]int64{known: 3})

	want := map[uuid.UUID]int64{hit: 7, known: 3, cold: 4}
	if !maps.Equal(got, want) {
		t.Fatalf("counts = %v, want %v", got, want)
	}
	if len(repo.countCalls) != 1 || !slices.Equal(repo.countCalls[0], []uuid.UUID{cold, gone}) {
		t.Fatalf("PG fallback calls = %v, want one call for [cold gone]", repo.countCalls)
	}
	if !maps.Equal(cache.seeded, map[uuid.UUID]int64{known: 3, cold: 4}) {
		t.Fatalf("seeded = %v, want only misses found in PG", cache.seeded)
	}
	if cache.vals[hit] != 7 {
		t.Fatalf("cached hit overwritten: %d", cache.vals[hit])
	}
}

// 全部命中不查 PG、不回种。
func TestResolveShareCounts_AllHitNoDB(t *testing.T) {
	a := uuid.New()
	repo := &fakeShareRepo{}
	cache := &fakeShareCountCache{vals: map[uuid.UUID]int64{a: 2}}
	got := newShareSvc(repo, cache).resolveShareCounts(t.Context(), []uuid.UUID{a}, nil)
	if got[a] != 2 || len(repo.countCalls) != 0 || len(cache.seeded) != 0 {
		t.Fatalf("got %v, PG calls %v, seeded %v", got, repo.countCalls, cache.seeded)
	}
}

// 缓存故障：全部回源 PG，不回种（避免故障期间写入）；未注入缓存同样直接读 PG。
func TestResolveShareCounts_CacheErrorOrNilUsesDB(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	for name, cache := range map[string]*fakeShareCountCache{
		"cache error": {getErr: errors.New("redis down"), vals: map[uuid.UUID]int64{a: 100}},
		"no cache":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeShareRepo{shareCounts: map[uuid.UUID]int64{a: 5}}
			got := newShareSvc(repo, cache).resolveShareCounts(t.Context(), []uuid.UUID{a, b}, map[uuid.UUID]int64{b: 1})
			if !maps.Equal(got, map[uuid.UUID]int64{a: 5, b: 1}) {
				t.Fatalf("counts = %v", got)
			}
			if cache != nil && len(cache.seeded) != 0 {
				t.Fatalf("seeded during cache failure: %v", cache.seeded)
			}
		})
	}
}

// PG 出错：已知值照常返回并回种，查不到的帖子不在结果里（按 0 展示），不返回错误。
func TestResolveShareCounts_DBErrorBestEffort(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &fakeShareRepo{countErr: errors.New("pg down")}
	cache := &fakeShareCountCache{}
	got := newShareSvc(repo, cache).resolveShareCounts(t.Context(), []uuid.UUID{a, b}, map[uuid.UUID]int64{a: 2})
	if !maps.Equal(got, map[uuid.UUID]int64{a: 2}) || !maps.Equal(cache.seeded, map[uuid.UUID]int64{a: 2}) {
		t.Fatalf("counts = %v, seeded = %v", got, cache.seeded)
	}
}

// 列表回填：按 ID 写回 share_count，回种失败不影响返回值。
func TestApplyShareCounts_FillsItems(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &fakeShareRepo{shareCounts: map[uuid.UUID]int64{b: 3}}
	cache := &fakeShareCountCache{vals: map[uuid.UUID]int64{a: 8}, seedErr: errors.New("seed failed")}
	posts := []PostListItem{{ID: a}, {ID: b}}
	newShareSvc(repo, cache).applyShareCounts(t.Context(), posts, nil)
	if posts[0].ShareCount != 8 || posts[1].ShareCount != 3 {
		t.Fatalf("posts = %+v", posts)
	}
}

// is_shared：访客不查库；DB 出错按未分享。
func TestBatchCheckShared(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &fakeShareRepo{shared: map[uuid.UUID]bool{a: true}}
	s := newShareSvc(repo, nil)
	if got := s.BatchCheckShared(t.Context(), uuid.New(), []uuid.UUID{a, b}); !got[a] || got[b] {
		t.Fatalf("shared = %v", got)
	}
	if got := s.BatchCheckShared(t.Context(), uuid.Nil, []uuid.UUID{a}); len(got) != 0 {
		t.Fatalf("guest shared = %v, want empty", got)
	}
	if !s.checkShared(t.Context(), uuid.New(), a) || s.checkShared(t.Context(), uuid.New(), b) {
		t.Fatal("checkShared mismatch")
	}
	repo.sharedErr = errors.New("pg down")
	if got := s.BatchCheckShared(t.Context(), uuid.New(), []uuid.UUID{a}); len(got) != 0 {
		t.Fatalf("shared on DB error = %v, want empty", got)
	}
	if s.checkShared(t.Context(), uuid.New(), a) {
		t.Fatal("checkShared on DB error should be false")
	}
}
