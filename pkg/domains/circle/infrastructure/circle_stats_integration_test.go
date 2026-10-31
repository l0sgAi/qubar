//go:build integration

// circle_stats_integration_test.go 圈子统计 Hash 冷回种对开发栈 PostgreSQL + Redis 的集成测试（#180）：
// 冷 Hash 下并发入圈 / 退圈，Redis member_count 等于初始值 ± N（回种不覆盖并发的 HINCRBY）；
// 回种只写缺失字段、冷 Hash 上的增减不建半截 Hash、发帖计数冷 Hash 先回种再 +1。
package infrastructure_test

import (
	"context"
	"sync"
	"testing"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/domains/circle/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

const coldStatsConcurrency = 20

// assertColdStats 统计 Hash 不存在（每个用例从冷 Hash 开始）。
func (f *joinFixture) assertColdStats(t *testing.T) {
	t.Helper()
	n, err := redispkg.Client.Exists(t.Context(), redispkg.GetCircleStatsKey(f.circleID)).Result()
	if err != nil || n != 0 {
		t.Fatalf("stats hash should be cold: exists=%d err=%v", n, err)
	}
}

// TestJoinCircle_ColdStatsConcurrentJoinsAllCounted 冷 Hash 下 N 个不同用户并发入圈：member_count = 1 + N。
func TestJoinCircle_ColdStatsConcurrentJoinsAllCounted(t *testing.T) {
	t.Parallel()
	f := newJoinFixture(t, domain.CircleJoinTypeDirect)
	users := make([]uuid.UUID, coldStatsConcurrency)
	for i := range users {
		users[i] = f.user(t)
	}
	f.assertColdStats(t)

	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f.svc.JoinCircle(context.WithoutCancel(t.Context()), u, f.circleID); err != nil {
				t.Errorf("JoinCircle: %v", err)
			}
		}()
	}
	wg.Wait()

	want := 1 + coldStatsConcurrency
	if got := f.memberCount(t); got != want || f.publisher.sum() != coldStatsConcurrency {
		t.Errorf("count=%d events=%d, want %d / +%d", got, f.publisher.sum(), want, coldStatsConcurrency)
	}
}

// TestLeaveCircle_ColdStatsConcurrentLeavesAllCounted 冷 Hash 下 N 个成员并发退圈：member_count = 初始值 - N。
func TestLeaveCircle_ColdStatsConcurrentLeavesAllCounted(t *testing.T) {
	t.Parallel()
	f := newJoinFixture(t, domain.CircleJoinTypeDirect)
	users := make([]uuid.UUID, coldStatsConcurrency)
	for i := range users {
		users[i] = f.user(t)
		f.seed(t, users[i], domain.MemberRoleMember, domain.MemberStatusNormal)
	}
	initial := 1 + coldStatsConcurrency
	if err := testkit.PG(t).Exec("UPDATE domains.circle SET member_count = ? WHERE id = ?", initial, f.circleID).Error; err != nil {
		t.Fatalf("set member_count: %v", err)
	}
	f.assertColdStats(t)

	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f.svc.LeaveCircle(context.WithoutCancel(t.Context()), u, f.circleID); err != nil {
				t.Errorf("LeaveCircle: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := f.memberCount(t); got != 1 || f.publisher.sum() != -coldStatsConcurrency {
		t.Errorf("count=%d events=%d, want 1 / -%d", got, f.publisher.sum(), coldStatsConcurrency)
	}
}

// TestCircleStatsCache_SeedIfAbsentAndGuardedIncr 回种只写缺失字段、不覆盖已有增量；
// 冷 Hash 上的增减不写（不建只有一个字段的半截 Hash）。
func TestCircleStatsCache_SeedIfAbsentAndGuardedIncr(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	cache := infrastructure.NewCircleStatsCache()
	circleID := sharedomain.NewID()
	key := redispkg.GetCircleStatsKey(circleID)
	testkit.CleanupKeys(t, key)

	if err := cache.IncrMemberCount(ctx, circleID); err != nil {
		t.Fatalf("incr on cold hash: %v", err)
	}
	if err := cache.IncrPostCount(ctx, circleID); err != nil {
		t.Fatalf("incr post on cold hash: %v", err)
	}
	if n, _ := redispkg.Client.Exists(ctx, key).Result(); n != 0 {
		t.Fatalf("incr on cold hash created a partial hash")
	}
	if ok, err := cache.StatsExists(ctx, circleID); err != nil || ok {
		t.Fatalf("StatsExists on cold hash = %v, %v", ok, err)
	}

	if err := cache.SeedStatsIfAbsent(ctx, circleID, &domain.CircleStatistics{MemberCount: 5, PostCount: 3, Hot: 7}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for range 2 {
		if err := cache.IncrMemberCount(ctx, circleID); err != nil {
			t.Fatalf("incr: %v", err)
		}
	}
	// 后到的回种（并发请求读到的旧 DB 值）是空操作。
	if err := cache.SeedStatsIfAbsent(ctx, circleID, &domain.CircleStatistics{MemberCount: 5, PostCount: 0, Hot: 0}); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	got, err := cache.GetStats(ctx, circleID)
	if err != nil || got == nil || *got != (domain.CircleStatistics{MemberCount: 7, PostCount: 3, Hot: 7}) {
		t.Fatalf("stats = %+v, %v; want {7 3 7}", got, err)
	}
	if ttl, _ := redispkg.Client.TTL(ctx, key).Result(); ttl <= 0 {
		t.Errorf("ttl = %v, want > 0", ttl)
	}

	// 减到 0 以下钳到 0。
	for range 10 {
		if err := cache.DecrMemberCount(ctx, circleID); err != nil {
			t.Fatalf("decr: %v", err)
		}
	}
	if got, _ := cache.GetStats(ctx, circleID); got == nil || got.MemberCount != 0 {
		t.Errorf("member_count after over-decrement = %+v, want 0", got)
	}
}

// TestIncrPostCount_ColdStatsSeedsThenCounts 冷 Hash 下发帖计数：先从 DB 回种，再 +1（不丢、不建半截 Hash）。
func TestIncrPostCount_ColdStatsSeedsThenCounts(t *testing.T) {
	t.Parallel()
	f := newJoinFixture(t, domain.CircleJoinTypeDirect)
	if err := testkit.PG(t).Exec("UPDATE domains.circle SET post_count = 4 WHERE id = ?", f.circleID).Error; err != nil {
		t.Fatalf("set post_count: %v", err)
	}
	f.assertColdStats(t)

	if err := f.svc.IncrPostCount(t.Context(), f.circleID); err != nil {
		t.Fatalf("IncrPostCount: %v", err)
	}
	got, err := f.stats.GetStats(t.Context(), f.circleID)
	if err != nil || got == nil || got.PostCount != 5 || got.MemberCount != 1 {
		t.Errorf("stats = %+v, %v; want post_count 5 / member_count 1", got, err)
	}
}
