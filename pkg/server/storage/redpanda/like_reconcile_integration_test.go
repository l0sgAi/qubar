//go:build integration

// like_reconcile_integration_test.go 点赞落库后校正 Redis post:stats.like_count（#128）：
// 缓存在点赞落库前由 PG 回填会少计，落库后应被抬回 PG 值；缓存值更高（含未落库增量）时保持不变。
package redpanda_test

import (
	"context"
	"testing"

	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestPersistPostLikes_ReconcilesStaleStatsCache(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)

	stale, ahead, absent := sharedomain.NewID(), sharedomain.NewID(), sharedomain.NewID()
	ids := []uuid.UUID{stale, ahead, absent}
	testkit.CleanupRows(t, "domains.post", "id", toAny(ids)...)
	testkit.CleanupRows(t, "domains.post_like", "post_id", toAny(ids)...)
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redispkg.GetPostStatsKey(id)
	}
	testkit.CleanupKeys(t, keys...)

	circleID := sharedomain.NewID()
	for _, id := range ids {
		if err := db.Exec("INSERT INTO domains.post (id, circle_id, user_id) VALUES (?, ?, ?)",
			id, circleID, sharedomain.NewID()).Error; err != nil {
			t.Fatalf("insert post: %v", err)
		}
	}

	rctx := context.Background()
	// stale：缓存在点赞落库前由 PG 回填（0），漏掉了窗口内的点赞。
	// ahead：缓存带有尚未落库的增量（5 > 落库后 PG 的 1），不应被降下来。
	if err := redispkg.Client.HSet(rctx, keys[0], "like_count", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := redispkg.Client.HSet(rctx, keys[1], "like_count", 5).Err(); err != nil {
		t.Fatal(err)
	}

	if err := redpanda.PersistPostLikesForTest(sharedomain.NewID(), ids); err != nil {
		t.Fatalf("persist likes: %v", err)
	}

	if v, _ := redispkg.Client.HGet(rctx, keys[0], "like_count").Result(); v != "1" {
		t.Errorf("stale cache like_count = %q, want 1", v)
	}
	if v, _ := redispkg.Client.HGet(rctx, keys[1], "like_count").Result(); v != "5" {
		t.Errorf("ahead cache like_count = %q, want 5 (unflushed increments kept)", v)
	}
	if n, _ := redispkg.Client.Exists(rctx, keys[2]).Result(); n != 0 {
		t.Error("absent cache must not be created by reconcile")
	}
}
