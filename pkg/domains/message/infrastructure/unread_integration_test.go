//go:build integration

// unread_integration_test.go 私信未读总数（#241）对开发栈 PostgreSQL + Redis 的集成测试：
// SumUnread 口径（各会话我的未读之和，已读 / 删除会话后扣掉）、Redis 缓存的 Get / Set / IncrIfExists / Invalidate 语义、
// 应用服务「缓存优先、miss 回源」在发送 / 标记已读 / 删除会话前后与 DB 真值一致。
package infrastructure_test

import (
	"testing"
	"time"

	"interestBar/pkg/domains/message/application"
	"interestBar/pkg/domains/message/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestSumUnread(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 4)
	me, a, b, c := u[0], u[1], u[2], u[3]

	sum := func() int64 {
		t.Helper()
		n, err := e.repo.SumUnread(t.Context(), me)
		if err != nil {
			t.Fatalf("SumUnread: %v", err)
		}
		return n
	}
	if got := sum(); got != 0 {
		t.Fatalf("no conversations: sum = %d, want 0", got)
	}

	convA := e.send(t, a, me, "a1").ConversationID
	e.send(t, a, me, "a2")
	convB := e.send(t, b, me, "b1").ConversationID
	e.send(t, me, c, "mine") // 我发出的不计入我的未读
	if got := sum(); got != 3 {
		t.Fatalf("after 3 incoming: sum = %d, want 3", got)
	}

	if err := e.svc.MarkRead(t.Context(), me, uuid.MustParse(convA)); err != nil {
		t.Fatal(err)
	}
	if got := sum(); got != 1 {
		t.Fatalf("after reading a: sum = %d, want 1", got)
	}
	if err := e.svc.DeleteConversation(t.Context(), me, uuid.MustParse(convB)); err != nil {
		t.Fatal(err)
	}
	if got := sum(); got != 0 {
		t.Fatalf("after deleting b: sum = %d, want 0", got)
	}
	// 删除后对方再发：会话重现，未读照常累加。
	e.send(t, b, me, "b2")
	if got := sum(); got != 1 {
		t.Fatalf("after b sends again: sum = %d, want 1", got)
	}
}

func TestUnreadCacheRedis(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	cache := infrastructure.NewUnreadCache()
	uid := sharedomain.NewID()
	key := redispkg.GetMessageUnreadKey(uid)
	testkit.CleanupKeys(t, key)
	ctx := t.Context()

	if _, ok, err := cache.Get(ctx, uid); err != nil || ok {
		t.Fatalf("Get on missing key: ok=%v err=%v, want miss", ok, err)
	}
	// key 不存在时 IncrIfExists 不建 key（避免从 0 起算少计）。
	if err := cache.IncrIfExists(ctx, uid, 1); err != nil {
		t.Fatal(err)
	}
	if n, err := redispkg.Client.Exists(ctx, key).Result(); err != nil || n != 0 {
		t.Fatalf("IncrIfExists on missing key created it (exists=%d err=%v)", n, err)
	}

	if err := cache.Set(ctx, uid, 4); err != nil {
		t.Fatal(err)
	}
	ttl, err := redispkg.Client.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 || ttl > redispkg.MessageUnreadTTL {
		t.Fatalf("TTL after Set = %v (err %v), want (0, %v]", ttl, err, redispkg.MessageUnreadTTL)
	}
	if err := cache.IncrIfExists(ctx, uid, 2); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := cache.Get(ctx, uid); err != nil || !ok || got != 6 {
		t.Fatalf("Get after incr = %d ok=%v err=%v, want 6", got, ok, err)
	}
	// INCRBY 不改 TTL。
	if ttl2, _ := redispkg.Client.TTL(ctx, key).Result(); ttl2 <= 0 || ttl2 > ttl+time.Second {
		t.Fatalf("TTL after incr = %v, want kept (~%v)", ttl2, ttl)
	}

	if err := cache.Invalidate(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(ctx, uid); err != nil || ok {
		t.Fatalf("Get after Invalidate: ok=%v err=%v, want miss", ok, err)
	}
	// 脏值视同 miss。
	if err := redispkg.Client.Set(ctx, key, "x", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(ctx, uid); err != nil || ok {
		t.Fatalf("Get on garbage: ok=%v err=%v, want miss", ok, err)
	}
}

// 应用服务 + 真实缓存：每一步写之后，GetUnreadTotal（缓存优先）都与 DB 真值一致。
func TestGetUnreadTotal_ConsistentWithDB(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	testkit.Redis(t)
	svc := application.NewMessageService(e.repo, okRecipients{}, echoUsers{}, followAll{})
	svc.SetUnreadCache(infrastructure.NewUnreadCache())
	u := e.newUsers(t, 3)
	me, a, b := u[0], u[1], u[2]
	testkit.CleanupKeys(t, redispkg.GetMessageUnreadKey(me), redispkg.GetMessageUnreadKey(a), redispkg.GetMessageUnreadKey(b))

	check := func(step string) {
		t.Helper()
		want, err := e.repo.SumUnread(t.Context(), me)
		if err != nil {
			t.Fatal(err)
		}
		got, err := svc.GetUnreadTotal(t.Context(), me)
		if err != nil {
			t.Fatalf("%s: GetUnreadTotal: %v", step, err)
		}
		if got != want {
			t.Fatalf("%s: cached total = %d, DB = %d", step, got, want)
		}
		// 第二次读必然命中缓存，仍一致。
		if again, _ := svc.GetUnreadTotal(t.Context(), me); again != want {
			t.Fatalf("%s: second read = %d, DB = %d", step, again, want)
		}
	}

	check("empty")
	send := func(from, to uuid.UUID, content string) string {
		t.Helper()
		vo, err := svc.Send(t.Context(), from, to, content)
		if err != nil {
			t.Fatal(err)
		}
		return vo.ConversationID
	}
	convA := send(a, me, "1") // 缓存已回填为 0，发送后 IncrIfExists → 1
	check("after first incoming")
	send(a, me, "2")
	convB := send(b, me, "3")
	check("after three incoming")
	if err := svc.MarkRead(t.Context(), me, uuid.MustParse(convA)); err != nil {
		t.Fatal(err)
	}
	check("after mark read")
	if err := svc.DeleteConversation(t.Context(), me, uuid.MustParse(convB)); err != nil {
		t.Fatal(err)
	}
	check("after delete")
	send(b, me, "4")
	check("after reappear")
}
