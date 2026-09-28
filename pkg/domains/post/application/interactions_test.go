package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// fakeInteractionCache 模拟 ZSET：members 之外的 ID 都算 miss。
type fakeInteractionCache struct {
	members    map[uuid.UUID]bool
	err        error
	backfilled []uuid.UUID
}

func (c *fakeInteractionCache) BatchCheck(_ context.Context, _ uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, []uuid.UUID, error) {
	if c.err != nil {
		return nil, nil, c.err
	}
	hits := make(map[uuid.UUID]bool, len(postIDs))
	var missed []uuid.UUID
	for _, id := range postIDs {
		if c.members[id] {
			hits[id] = true
		} else {
			hits[id] = false
			missed = append(missed, id)
		}
	}
	return hits, missed, nil
}

func (c *fakeInteractionCache) Backfill(_ context.Context, _ uuid.UUID, postIDs []uuid.UUID) error {
	c.backfilled = append(c.backfilled, postIDs...)
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

// 回归 #46：ZSET 过期后信息流不应把已赞帖显示为未赞。
func TestResolveInteraction_CacheMissFallsBackToDBAndBackfills(t *testing.T) {
	cached, expired, notLiked := uuid.New(), uuid.New(), uuid.New()
	cache := &fakeInteractionCache{members: map[uuid.UUID]bool{cached: true}}
	db, calls := dbWith(expired)

	got := resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{cached, expired, notLiked}, cache, db)

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

func TestResolveInteraction_AllHitsSkipDB(t *testing.T) {
	id := uuid.New()
	cache := &fakeInteractionCache{members: map[uuid.UUID]bool{id: true}}
	db, calls := dbWith()

	got := resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{id}, cache, db)

	if !got[id] || len(*calls) != 0 {
		t.Fatalf("got %v, db calls %v", got, *calls)
	}
}

func TestResolveInteraction_CacheErrorUsesDB(t *testing.T) {
	id := uuid.New()
	cache := &fakeInteractionCache{err: errors.New("redis down")}
	db, calls := dbWith(id)

	got := resolveInteraction(context.Background(), "collect", uuid.New(), []uuid.UUID{id}, cache, db)

	if !got[id] || len(*calls) != 1 {
		t.Fatalf("got %v, db calls %v", got, *calls)
	}
}

func TestResolveInteraction_NilCacheUsesDBOnly(t *testing.T) {
	id := uuid.New()
	db, _ := dbWith(id)

	got := resolveInteraction(context.Background(), "collect", uuid.New(), []uuid.UUID{id}, nil, db)

	if !got[id] {
		t.Fatalf("got %v", got)
	}
}

func TestResolveInteraction_DBErrorKeepsCacheResults(t *testing.T) {
	hit, miss := uuid.New(), uuid.New()
	cache := &fakeInteractionCache{members: map[uuid.UUID]bool{hit: true}}
	failing := func(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]bool, error) {
		return nil, errors.New("db down")
	}

	got := resolveInteraction(context.Background(), "like", uuid.New(), []uuid.UUID{hit, miss}, cache, failing)

	if !got[hit] || got[miss] {
		t.Fatalf("got %v", got)
	}
}
