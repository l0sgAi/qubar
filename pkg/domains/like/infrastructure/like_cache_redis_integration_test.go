//go:build integration

// like_cache_redis_integration_test.go 点赞 Lua 原子设值脚本对开发栈 Redis 的集成测试（make test-integration）。
package infrastructure_test

import (
	"strconv"
	"sync"
	"testing"

	"interestBar/pkg/domains/like/domain"
	"interestBar/pkg/domains/like/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func hashInt(t *testing.T, key, field string) int {
	t.Helper()
	v, err := redispkg.Client.HGet(t.Context(), key, field).Result()
	if err != nil {
		t.Fatalf("HGET %s %s: %v", key, field, err)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("HGET %s %s = %q: %v", key, field, v, err)
	}
	return n
}

// 设值语义（非 toggle）：只有状态真正变化才改 ZSET 与 like_count；重复设值是 no-op。
func TestPostLikeCache_SetIsIdempotent(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	cache := infrastructure.NewPostLikeCache()
	user, postID := sharedomain.NewID(), sharedomain.NewID()
	statsKey, zsetKey := redispkg.GetPostStatsKey(postID), redispkg.GetUserPostLikeListKey(user)
	testkit.CleanupKeys(t, statsKey, zsetKey)

	// 读路径先回种统计 Hash（与 RestoreStats 一致），like_count 从 DB 基线 4 开始
	if err := redispkg.UpdatePostStatistics(postID, &redispkg.PostStatistics{LikeCount: 4}); err != nil {
		t.Fatalf("seed stats: %v", err)
	}

	steps := []struct {
		liked     bool
		want      domain.ToggleResult
		wantCount int
	}{
		{true, domain.ToggleResultLiked, 5},
		{true, domain.ToggleResultUnchanged, 5},
		{false, domain.ToggleResultUnliked, 4},
		{false, domain.ToggleResultUnchanged, 4},
		{true, domain.ToggleResultLiked, 5},
	}
	for i, s := range steps {
		got, err := cache.Set(ctx, user, postID, s.liked)
		if err != nil {
			t.Fatalf("step %d Set(%v): %v", i, s.liked, err)
		}
		if got != s.want {
			t.Fatalf("step %d Set(%v) = %d, want %d", i, s.liked, got, s.want)
		}
		if n := hashInt(t, statsKey, "like_count"); n != s.wantCount {
			t.Fatalf("step %d like_count = %d, want %d", i, n, s.wantCount)
		}
	}

	liked, missed, err := redispkg.BatchCheckPostLiked(user, []uuid.UUID{postID, sharedomain.NewID()})
	if err != nil {
		t.Fatalf("BatchCheckPostLiked: %v", err)
	}
	if !liked[postID] || len(missed) != 1 {
		t.Fatalf("BatchCheckPostLiked = %v missed %v", liked, missed)
	}
	for _, key := range []string{statsKey, zsetKey} {
		if ttl := redispkg.Client.TTL(ctx, key).Val(); ttl <= 0 {
			t.Fatalf("%s TTL = %v, want renewed positive TTL", key, ttl)
		}
	}
	if ok, err := cache.StatsExists(ctx, postID); err != nil || !ok {
		t.Fatalf("StatsExists = %v, %v", ok, err)
	}
}

// 计数下限 0：统计 Hash 已是 0（如过期后从 DB 回种），取消赞不能减成负数。
func TestPostLikeCache_UnlikeClampsAtZero(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	cache := infrastructure.NewPostLikeCache()
	user, postID := sharedomain.NewID(), sharedomain.NewID()
	statsKey := redispkg.GetPostStatsKey(postID)
	testkit.CleanupKeys(t, statsKey, redispkg.GetUserPostLikeListKey(user))

	if err := redispkg.BackfillPostLikes(user, []uuid.UUID{postID}); err != nil {
		t.Fatalf("BackfillPostLikes: %v", err)
	}
	if err := redispkg.UpdatePostStatistics(postID, &redispkg.PostStatistics{}); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
	got, err := cache.Set(ctx, user, postID, false)
	if err != nil || got != domain.ToggleResultUnliked {
		t.Fatalf("Set(false) = %d, %v; want unliked", got, err)
	}
	if n := hashInt(t, statsKey, "like_count"); n != 0 {
		t.Fatalf("like_count = %d, want clamped 0", n)
	}
}

// 并发：N 个用户同时赞同一帖，计数必须恰好 +N（Lua 原子性）。
func TestPostLikeCache_ConcurrentLikes(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	cache := infrastructure.NewPostLikeCache()
	postID := sharedomain.NewID()
	statsKey := redispkg.GetPostStatsKey(postID)
	testkit.CleanupKeys(t, statsKey)

	const n = 50
	users := make([]uuid.UUID, n)
	for i := range users {
		users[i] = sharedomain.NewID()
		testkit.CleanupKeys(t, redispkg.GetUserPostLikeListKey(users[i]))
	}

	var wg sync.WaitGroup
	errs := make(chan error, n*2)
	for _, u := range users {
		for range 2 { // 每人双击：第二次必须是 no-op
			wg.Add(1)
			go func(u uuid.UUID) {
				defer wg.Done()
				if _, err := cache.Set(ctx, u, postID, true); err != nil {
					errs <- err
				}
			}(u)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Set: %v", err)
	}
	if got := hashInt(t, statsKey, "like_count"); got != n {
		t.Fatalf("like_count = %d, want %d", got, n)
	}
}

func TestCommentLikeCache_SetIsIdempotent(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	cache := infrastructure.NewCommentLikeCache()
	user, commentID := sharedomain.NewID(), sharedomain.NewID()
	statsKey := redispkg.GetCommentStatsKey(commentID)
	testkit.CleanupKeys(t, statsKey, redispkg.GetUserCommentLikeListKey(user))

	if err := redispkg.UpdateCommentStatistics(ctx, commentID, 2); err != nil {
		t.Fatalf("seed comment stats: %v", err)
	}
	for i, s := range []struct {
		liked     bool
		want      domain.ToggleResult
		wantCount int
	}{
		{true, domain.ToggleResultLiked, 3},
		{true, domain.ToggleResultUnchanged, 3},
		{false, domain.ToggleResultUnliked, 2},
	} {
		got, err := cache.Set(ctx, user, commentID, s.liked)
		if err != nil || got != s.want {
			t.Fatalf("step %d Set(%v) = %d, %v; want %d", i, s.liked, got, err, s.want)
		}
		if n := hashInt(t, statsKey, "like_count"); n != s.wantCount {
			t.Fatalf("step %d like_count = %d, want %d", i, n, s.wantCount)
		}
	}
	checked, err := redispkg.BatchCheckCommentLiked(ctx, user, []uuid.UUID{commentID})
	if err != nil || checked[commentID] {
		t.Fatalf("BatchCheckCommentLiked after unlike = %v, %v; want not liked", checked, err)
	}
}
