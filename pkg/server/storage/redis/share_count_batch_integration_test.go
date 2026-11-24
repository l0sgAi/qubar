//go:build integration

// share_count_batch_integration_test.go 帖子分享数批量读 / 批量回种（#233）对开发栈 Redis 的集成测试（make test-integration）。
package redis_test

import (
	"maps"
	"testing"

	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func newSharePost(t *testing.T) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	testkit.CleanupKeys(t, redispkg.GetPostShareCountKey(id))
	return id
}

// MGET 只返回存在的 key；SET NX 回种不覆盖已有值（含分享自增后的新值），新 key 带 TTL。
func TestPostShareCounts_BatchGetAndSeed(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	cached, cold, dirty := newSharePost(t), newSharePost(t), newSharePost(t)

	if got, err := redispkg.GetPostShareCounts(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("empty batch = %v, %v", got, err)
	}
	if _, err := redispkg.SeedPostShareCount(ctx, cached, 2); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, ok, err := redispkg.IncrPostShareCountIfCached(ctx, cached); err != nil || !ok {
		t.Fatalf("incr = %v, %v", ok, err)
	}
	if err := redispkg.Client.Set(ctx, redispkg.GetPostShareCountKey(dirty), "x", 0).Err(); err != nil {
		t.Fatalf("set dirty: %v", err)
	}

	got, err := redispkg.GetPostShareCounts(ctx, []uuid.UUID{cached, cold, dirty})
	if err != nil || !maps.Equal(got, map[uuid.UUID]int64{cached: 3}) {
		t.Fatalf("GetPostShareCounts = %v, %v; want only cached=3 (cold missing, dirty value treated as miss)", got, err)
	}

	// 回种：已有 key（含自增后的 3）不被 PG 旧值覆盖；冷 key 写入并带 TTL
	if err := redispkg.SeedPostShareCounts(ctx, map[uuid.UUID]int64{cached: 1, cold: 5}); err != nil {
		t.Fatalf("SeedPostShareCounts: %v", err)
	}
	if err := redispkg.SeedPostShareCounts(ctx, nil); err != nil {
		t.Fatalf("empty seed: %v", err)
	}
	got, err = redispkg.GetPostShareCounts(ctx, []uuid.UUID{cached, cold})
	if err != nil || !maps.Equal(got, map[uuid.UUID]int64{cached: 3, cold: 5}) {
		t.Fatalf("after seed = %v, %v; want cached=3 kept, cold=5", got, err)
	}
	if ttl := redispkg.Client.TTL(ctx, redispkg.GetPostShareCountKey(cold)).Val(); ttl <= 0 {
		t.Fatalf("seeded key TTL = %v, want > 0", ttl)
	}

	// 回种后的 key 继续走分享自增与落库校准
	if v, ok, err := redispkg.IncrPostShareCountIfCached(ctx, cold); err != nil || !ok || v != 6 {
		t.Fatalf("incr after seed = %d, %v, %v; want 6", v, ok, err)
	}
}
