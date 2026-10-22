//go:build integration

// session_index_redis_integration_test.go sessionIndexRedis 对开发栈 Redis 的集成测试（make test-integration）。
package infrastructure_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"interestBar/pkg/domains/auth/domain"
	"interestBar/pkg/domains/auth/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"
)

// newIndexUser 返回一个随机 loginID，并注册它的索引 key 清理。
func newIndexUser(t *testing.T) string {
	t.Helper()
	testkit.Redis(t)
	loginID := sharedomain.NewID().String()
	testkit.CleanupKeys(t, redispkg.GetAuthSessionsKey(loginID), redispkg.GetAuthSessionMetaKey(loginID))
	return loginID
}

func listIndex(t *testing.T, idx domain.SessionIndex, loginID string) []string {
	t.Helper()
	tokens, err := idx.List(context.Background(), loginID)
	if err != nil {
		t.Fatalf("List(%s): %v", loginID, err)
	}
	return tokens
}

// 按登录先后排序；元数据与 token 成对写入；两个 key 都带 TTL。
func TestSessionIndex_AddListOrderAndMeta(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := infrastructure.NewSessionIndex()
	loginID := newIndexUser(t)

	if got := listIndex(t, idx, loginID); len(got) != 0 {
		t.Fatalf("empty index List = %v", got)
	}

	meta := domain.TerminalMeta{Type: "web", DeviceID: sharedomain.NewID().String(), LoginTime: time.Now().Unix(), IP: "10.0.0.1", UA: "ua"}
	for _, tk := range []string{"tok-c", "tok-a", "tok-b"} {
		if err := idx.Add(ctx, loginID, tk, meta, time.Minute); err != nil {
			t.Fatalf("Add(%s): %v", tk, err)
		}
		time.Sleep(2 * time.Millisecond) // score 是毫秒：保证三次写入有先后
	}
	if got, want := listIndex(t, idx, loginID), []string{"tok-c", "tok-a", "tok-b"}; !slices.Equal(got, want) {
		t.Fatalf("List = %v, want %v (login order)", got, want)
	}

	raw, err := redispkg.Client.HGet(ctx, redispkg.GetAuthSessionMetaKey(loginID), "tok-a").Result()
	if err != nil {
		t.Fatalf("HGet meta: %v", err)
	}
	var got domain.TerminalMeta
	if err := json.Unmarshal([]byte(raw), &got); err != nil || got != meta {
		t.Fatalf("meta = %+v (err %v), want %+v", got, err, meta)
	}

	for _, key := range []string{redispkg.GetAuthSessionsKey(loginID), redispkg.GetAuthSessionMetaKey(loginID)} {
		ttl, err := redispkg.Client.TTL(ctx, key).Result()
		if err != nil || ttl <= 0 || ttl > time.Minute {
			t.Errorf("TTL(%s) = %v (err %v), want (0, 1m]", key, ttl, err)
		}
	}
}

// 同一 token 重复 Add 只保留一份，排到最后；TTL 按最近一次写入刷新。
func TestSessionIndex_ReAddMovesToEnd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := infrastructure.NewSessionIndex()
	loginID := newIndexUser(t)

	for _, tk := range []string{"tok-1", "tok-2", "tok-1"} {
		if err := idx.Add(ctx, loginID, tk, domain.TerminalMeta{Type: "web"}, time.Hour); err != nil {
			t.Fatalf("Add(%s): %v", tk, err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got, want := listIndex(t, idx, loginID), []string{"tok-2", "tok-1"}; !slices.Equal(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	if ttl, _ := redispkg.Client.TTL(ctx, redispkg.GetAuthSessionsKey(loginID)).Result(); ttl <= time.Minute {
		t.Errorf("TTL should be refreshed to ~1h, got %v", ttl)
	}
}

// Remove 同时删除 member 与元数据，不存在的 token 忽略；Clear 删除两个 key；不影响其他用户。
func TestSessionIndex_RemoveAndClear(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idx := infrastructure.NewSessionIndex()
	loginID, other := newIndexUser(t), newIndexUser(t)

	for _, tk := range []string{"tok-1", "tok-2", "tok-3"} {
		if err := idx.Add(ctx, loginID, tk, domain.TerminalMeta{Type: "web"}, time.Minute); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if err := idx.Add(ctx, other, "tok-other", domain.TerminalMeta{Type: "web"}, time.Minute); err != nil {
		t.Fatalf("Add other: %v", err)
	}

	if err := idx.Remove(ctx, loginID); err != nil {
		t.Fatalf("Remove without tokens: %v", err)
	}
	if err := idx.Remove(ctx, loginID, "tok-1", "tok-3", "tok-missing"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := listIndex(t, idx, loginID); !slices.Equal(got, []string{"tok-2"}) {
		t.Fatalf("List after Remove = %v, want [tok-2]", got)
	}
	fields, err := redispkg.Client.HKeys(ctx, redispkg.GetAuthSessionMetaKey(loginID)).Result()
	if err != nil || !slices.Equal(fields, []string{"tok-2"}) {
		t.Fatalf("meta fields after Remove = %v (err %v), want [tok-2]", fields, err)
	}

	if err := idx.Clear(ctx, loginID); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	n, err := redispkg.Client.Exists(ctx, redispkg.GetAuthSessionsKey(loginID), redispkg.GetAuthSessionMetaKey(loginID)).Result()
	if err != nil || n != 0 {
		t.Fatalf("keys after Clear: exists=%d err=%v", n, err)
	}
	if got := listIndex(t, idx, other); !slices.Equal(got, []string{"tok-other"}) {
		t.Fatalf("other user's index = %v, want [tok-other]", got)
	}
}
