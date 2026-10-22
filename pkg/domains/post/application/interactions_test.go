package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// fakeInteractionCache 模拟 ZSET + 完整加载标记：members 之外的 ID 在未完整加载时算 miss。
type fakeInteractionCache struct {
	members    map[uuid.UUID]bool
	complete   bool // 标记 "1"：缺失即 false
	overflow   bool // 标记 "0"：不可全量装载
	err        error
	backfilled []uuid.UUID
	loads      int
}

func (c *fakeInteractionCache) BatchCheck(_ context.Context, _ uuid.UUID, postIDs []uuid.UUID) (domain.InteractionCheck, error) {
	if c.err != nil {
		return domain.InteractionCheck{}, c.err
	}
	chk := domain.InteractionCheck{Hits: make(map[uuid.UUID]bool, len(postIDs)), Loadable: !c.complete && !c.overflow}
	for _, id := range postIDs {
		chk.Hits[id] = c.members[id]
		if !c.members[id] && !c.complete {
			chk.Missed = append(chk.Missed, id)
		}
	}
	return chk, nil
}

func (c *fakeInteractionCache) Backfill(_ context.Context, _ uuid.UUID, postIDs []uuid.UUID) error {
	c.backfilled = append(c.backfilled, postIDs...)
	return nil
}

func (c *fakeInteractionCache) LoadAll(_ context.Context, _ uuid.UUID, entries []domain.InteractionEntry, complete bool) error {
	c.loads++
	if !complete {
		c.overflow = true
		return nil
	}
	if c.members == nil {
		c.members = map[uuid.UUID]bool{}
	}
	for _, e := range entries {
		c.members[e.ID] = true
	}
	c.complete = true
	return nil
}

func dbWith(liked ...uuid.UUID) (func(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]bool, error), *[][]uuid.UUID) {
	var calls [][]uuid.UUID
	set := make(map[uuid.UUID]bool, len(liked))
	for _, id := range liked {
		set[id] = true
	}
	return func(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
		calls = append(calls, ids)
		out := map[uuid.UUID]bool{}
		for _, id := range ids {
			if set[id] {
				out[id] = true
			}
		}
		return out, nil
	}, &calls
}

// listWith 模拟全量列表查询：返回 n 条（含 liked），记录调用次数。
func listWith(n int, liked ...uuid.UUID) (func(context.Context, uuid.UUID, int) ([]domain.InteractionEntry, error), *int) {
	calls := 0
	return func(_ context.Context, _ uuid.UUID, limit int) ([]domain.InteractionEntry, error) {
		calls++
		var out []domain.InteractionEntry
		for _, id := range liked {
			out = append(out, domain.InteractionEntry{ID: id})
		}
		for len(out) < n && len(out) < limit {
			out = append(out, domain.InteractionEntry{ID: uuid.New()})
		}
		return out, nil
	}, &calls
}

var noList = func(context.Context, uuid.UUID, int) ([]domain.InteractionEntry, error) {
	return nil, errors.New("list must not be called")
}

// 回归 #46：ZSET 过期后信息流不应把已赞帖显示为未赞（超限用户走逐批回源）。
func TestResolveInteraction_CacheMissFallsBackToDBAndBackfills(t *testing.T) {
	cached, expired, notLiked := uuid.New(), uuid.New(), uuid.New()
	cache := &fakeInteractionCache{members: map[uuid.UUID]bool{cached: true}, overflow: true}
	db, calls := dbWith(expired)

	got := resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{cached, expired, notLiked}, cache, db, noList)

	if !got[cached] || !got[expired] || got[notLiked] {
		t.Fatalf("got %v", got)
	}
	if len(*calls) != 1 || len((*calls)[0]) != 2 {
		t.Fatalf("DB must be queried once with only the misses, got %v", *calls)
	}
	if len(cache.backfilled) != 1 || cache.backfilled[0] != expired {
		t.Fatalf("only DB-confirmed IDs must be backfilled, got %v", cache.backfilled)
	}
}

// #129：标记缺失时首次回源全量装载，结果来自全量集合，不走 IN 查询；之后 miss 即 false 不再回源。
func TestResolveInteraction_FirstMissLoadsAllThenSkipsDB(t *testing.T) {
	liked, notLiked := uuid.New(), uuid.New()
	cache := &fakeInteractionCache{}
	db, dbCalls := dbWith(liked)
	list, listCalls := listWith(3, liked)

	got := resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{liked, notLiked}, cache, db, list)
	if !got[liked] || got[notLiked] {
		t.Fatalf("got %v", got)
	}
	if *listCalls != 1 || len(*dbCalls) != 0 || cache.loads != 1 || !cache.complete {
		t.Fatalf("want one full load and no IN query, list=%d db=%v loads=%d", *listCalls, *dbCalls, cache.loads)
	}

	got = resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{liked, notLiked, uuid.New()}, cache, db, list)
	if !got[liked] || got[notLiked] {
		t.Fatalf("second call got %v", got)
	}
	if *listCalls != 1 || len(*dbCalls) != 0 {
		t.Fatalf("complete marker must skip DB, list=%d db=%v", *listCalls, *dbCalls)
	}
}

// #129：超过装载上限只写超限标记，本次与之后都退回逐批 IN 回源，不重复全量查询。
func TestResolveInteraction_OverflowFallsBackToBatchQuery(t *testing.T) {
	liked := uuid.New()
	cache := &fakeInteractionCache{}
	db, dbCalls := dbWith(liked)
	list, listCalls := listWith(interactionFullLoadLimit + 1)

	for i := 0; i < 2; i++ {
		got := resolveInteraction(context.Background(), "collect", uuid.New(), []uuid.UUID{liked}, cache, db, list)
		if !got[liked] {
			t.Fatalf("call %d got %v", i, got)
		}
	}
	if *listCalls != 1 || len(*dbCalls) != 2 || !cache.overflow || cache.complete {
		t.Fatalf("list=%d db=%v overflow=%v complete=%v", *listCalls, *dbCalls, cache.overflow, cache.complete)
	}
}

func TestResolveInteraction_FullLoadErrorUsesDB(t *testing.T) {
	id := uuid.New()
	cache := &fakeInteractionCache{}
	db, calls := dbWith(id)

	got := resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{id}, cache, db, noList)

	if !got[id] || len(*calls) != 1 || cache.loads != 0 {
		t.Fatalf("got %v, db calls %v, loads %d", got, *calls, cache.loads)
	}
}

func TestResolveInteraction_AllHitsSkipDB(t *testing.T) {
	id := uuid.New()
	cache := &fakeInteractionCache{members: map[uuid.UUID]bool{id: true}}
	db, calls := dbWith()

	got := resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{id}, cache, db, noList)

	if !got[id] || len(*calls) != 0 {
		t.Fatalf("got %v, db calls %v", got, *calls)
	}
}

func TestResolveInteraction_CacheErrorUsesDB(t *testing.T) {
	id := uuid.New()
	cache := &fakeInteractionCache{err: errors.New("redis down")}
	db, calls := dbWith(id)

	got := resolveInteraction(context.Background(), "collect", uuid.New(), []uuid.UUID{id}, cache, db, noList)

	if !got[id] || len(*calls) != 1 {
		t.Fatalf("got %v, db calls %v", got, *calls)
	}
}

func TestResolveInteraction_NilCacheUsesDBOnly(t *testing.T) {
	id := uuid.New()
	db, _ := dbWith(id)

	got := resolveInteraction(context.Background(), "collect", uuid.New(), []uuid.UUID{id}, nil, db, noList)

	if !got[id] {
		t.Fatalf("got %v", got)
	}
}

func TestResolveInteraction_DBErrorKeepsCacheResults(t *testing.T) {
	hit, miss := uuid.New(), uuid.New()
	cache := &fakeInteractionCache{members: map[uuid.UUID]bool{hit: true}, overflow: true}
	failing := func(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]bool, error) {
		return nil, errors.New("db down")
	}

	got := resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{hit, miss}, cache, failing, noList)

	if !got[hit] || got[miss] {
		t.Fatalf("got %v", got)
	}
}
