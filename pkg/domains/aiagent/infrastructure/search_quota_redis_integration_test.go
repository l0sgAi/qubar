//go:build integration

// search_quota_redis_integration_test.go 机器人每日搜索配额对开发栈 Redis 的集成测试（make test-integration）。
package infrastructure

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"interestBar/pkg/domains/aiagent/domain"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// 超过上限返回 ErrSearchQuotaExhausted；key 带自然日过期时间。
func TestSearchQuota_ExhaustAndExpire(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := context.Background()
	now := time.Now()
	agentID := sharedomain.NewID()
	day, expireAt := quotaDay(now)
	key := redispkg.GetAiAgentSearchQuotaKey(agentID, day)
	testkit.CleanupKeys(t, key)
	q := &searchQuotaRedis{now: func() time.Time { return now }}

	for i := 1; i <= 3; i++ {
		if err := q.Consume(ctx, agentID, 3); err != nil {
			t.Fatalf("consume %d: %v", i, err)
		}
	}
	if err := q.Consume(ctx, agentID, 3); !errors.Is(err, domain.ErrSearchQuotaExhausted) {
		t.Fatalf("4th consume err = %v, want ErrSearchQuotaExhausted", err)
	}
	// 上限调高（机器人配置变更）后同日剩余额度立即可用：计数是已占用次数
	if err := q.Consume(ctx, agentID, 10); err != nil {
		t.Fatalf("consume with raised limit: %v", err)
	}

	ttl, err := redispkg.Client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	want := time.Until(expireAt)
	if ttl <= 0 || ttl > want+time.Minute || ttl < want-time.Minute {
		t.Errorf("TTL = %v, want ≈ %v (next midnight + buffer)", ttl, want)
	}
}

// 不同机器人、不同自然日各自独立计数。
func TestSearchQuota_IsolatedPerAgentAndDay(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := context.Background()
	today := time.Now()
	tomorrow := today.Add(24 * time.Hour)
	a, b := sharedomain.NewID(), sharedomain.NewID()
	for _, id := range []uuid.UUID{a, b} {
		for _, d := range []time.Time{today, tomorrow} {
			day, _ := quotaDay(d)
			testkit.CleanupKeys(t, redispkg.GetAiAgentSearchQuotaKey(id, day))
		}
	}
	qToday := &searchQuotaRedis{now: func() time.Time { return today }}
	qTomorrow := &searchQuotaRedis{now: func() time.Time { return tomorrow }}

	if err := qToday.Consume(ctx, a, 1); err != nil {
		t.Fatal(err)
	}
	if err := qToday.Consume(ctx, a, 1); !errors.Is(err, domain.ErrSearchQuotaExhausted) {
		t.Fatalf("a today 2nd err = %v", err)
	}
	if err := qToday.Consume(ctx, b, 1); err != nil {
		t.Fatalf("b today: %v", err)
	}
	if err := qTomorrow.Consume(ctx, a, 1); err != nil {
		t.Fatalf("a tomorrow: %v", err)
	}
}

// 并发占用：恰好 limit 次成功（INCR 原子）。
func TestSearchQuota_Concurrent(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := context.Background()
	now := time.Now()
	agentID := sharedomain.NewID()
	day, _ := quotaDay(now)
	testkit.CleanupKeys(t, redispkg.GetAiAgentSearchQuotaKey(agentID, day))
	q := &searchQuotaRedis{now: func() time.Time { return now }}

	const limit, workers = 5, 20
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := q.Consume(ctx, agentID, limit); {
			case err == nil:
				ok.Add(1)
			case !errors.Is(err, domain.ErrSearchQuotaExhausted):
				t.Errorf("consume: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := ok.Load(); got != limit {
		t.Errorf("successful consumes = %d, want %d", got, limit)
	}
}
