//go:build integration

// send_rate_limit_integration_test.go 私信发送频率限制（#242）对开发栈 Redis / PostgreSQL 的集成测试：
// Lua 计数的窗口 / TTL / 退还、并发不超限、SendDirect 的 Created 标志、应用服务端到端（被拒不落库、新会话口径）。
// 发送者 ID 随机生成并注册清理计数 key，可并行、可在同一个栈上反复跑。
package infrastructure_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/domains/message/application"
	"interestBar/pkg/domains/message/domain"
	"interestBar/pkg/domains/message/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// newRateSender 生成随机发送者 ID 并注册清理它的三个频率计数 key。
func newRateSender(t *testing.T) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	registerRateKeys(t, id)
	return id
}

func registerRateKeys(t *testing.T, ids ...uuid.UUID) {
	t.Helper()
	for _, id := range ids {
		s := id.String()
		testkit.CleanupKeys(t, redispkg.GetMessageRateMinuteKey(s), redispkg.GetMessageRateDayKey(s), redispkg.GetMessageRateNewConvKey(s))
	}
}

func rateCount(t *testing.T, key string) int64 {
	t.Helper()
	n, err := redispkg.Client.Get(t.Context(), key).Int64()
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	return n
}

func TestSendRateLimiter_WindowsAndTTL(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	limiter := infrastructure.NewSendRateLimiter()
	sender := newRateSender(t)
	limits := domain.SendRateLimits{PerMinute: 3, PerDay: 100, NewConversationsPerDay: 2}

	for i := range 3 {
		d, err := limiter.Acquire(t.Context(), sender, limits)
		if err != nil {
			t.Fatal(err)
		}
		if d.Exceeded != domain.SendRateOK || d.NewConversationReserved != (i < 2) {
			t.Fatalf("acquire #%d = %+v, want ok, reserved=%v", i+1, d, i < 2)
		}
	}
	d, err := limiter.Acquire(t.Context(), sender, limits)
	if err != nil || d.Exceeded != domain.SendRatePerMinute || d.NewConversationReserved {
		t.Fatalf("4th acquire = %+v (err %v), want per-minute exceeded without reservation", d, err)
	}

	s := sender.String()
	// 超限请求不计数。
	if got := rateCount(t, redispkg.GetMessageRateMinuteKey(s)); got != 3 {
		t.Errorf("minute count = %d, want 3", got)
	}
	if got := rateCount(t, redispkg.GetMessageRateDayKey(s)); got != 3 {
		t.Errorf("day count = %d, want 3", got)
	}
	if got := rateCount(t, redispkg.GetMessageRateNewConvKey(s)); got != 2 {
		t.Errorf("newconv count = %d, want 2", got)
	}
	for key, window := range map[string]time.Duration{
		redispkg.GetMessageRateMinuteKey(s):  time.Minute,
		redispkg.GetMessageRateDayKey(s):     24 * time.Hour,
		redispkg.GetMessageRateNewConvKey(s): 24 * time.Hour,
	} {
		ttl, err := redispkg.Client.PTTL(t.Context(), key).Result()
		if err != nil || ttl <= window-time.Minute/2 || ttl > window {
			t.Errorf("ttl(%s) = %v (err %v), want within (%v, %v]", key, ttl, err, window-time.Minute/2, window)
		}
	}

	// 每天上限：每分钟放宽后按天拦截。
	d, err = limiter.Acquire(t.Context(), sender, domain.SendRateLimits{PerMinute: 100, PerDay: 3, NewConversationsPerDay: 2})
	if err != nil || d.Exceeded != domain.SendRatePerDay {
		t.Fatalf("acquire over day limit = %+v (err %v), want per-day exceeded", d, err)
	}
}

func TestSendRateLimiter_Release(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	limiter := infrastructure.NewSendRateLimiter()
	sender := newRateSender(t)
	s := sender.String()
	limits := domain.SendRateLimits{PerMinute: 5, PerDay: 5, NewConversationsPerDay: 5}

	for range 2 {
		if _, err := limiter.Acquire(t.Context(), sender, limits); err != nil {
			t.Fatal(err)
		}
	}
	if err := limiter.Release(t.Context(), sender, false, true); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Release(t.Context(), sender, true, false); err != nil {
		t.Fatal(err)
	}
	if m, d, n := rateCount(t, redispkg.GetMessageRateMinuteKey(s)), rateCount(t, redispkg.GetMessageRateDayKey(s)),
		rateCount(t, redispkg.GetMessageRateNewConvKey(s)); m != 1 || d != 1 || n != 1 {
		t.Errorf("counts after release = %d/%d/%d, want 1/1/1", m, d, n)
	}
	// 退还不改 TTL。
	if ttl, _ := redispkg.Client.PTTL(t.Context(), redispkg.GetMessageRateMinuteKey(s)).Result(); ttl <= 0 {
		t.Errorf("minute ttl after release = %v, want > 0", ttl)
	}

	// 窗口已过期（key 不存在）时退还不写：不会留下 -1 且没有 TTL 的 key。
	other := newRateSender(t)
	if err := limiter.Release(t.Context(), other, true, true); err != nil {
		t.Fatal(err)
	}
	o := other.String()
	if n, err := redispkg.Client.Exists(t.Context(), redispkg.GetMessageRateMinuteKey(o),
		redispkg.GetMessageRateDayKey(o), redispkg.GetMessageRateNewConvKey(o)).Result(); err != nil || n != 0 {
		t.Errorf("keys created by release on empty window: %d (err %v)", n, err)
	}
}

func TestSendRateLimiter_ConcurrentNeverExceeds(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	limiter := infrastructure.NewSendRateLimiter()
	sender := newRateSender(t)
	limits := domain.SendRateLimits{PerMinute: 5, PerDay: 100, NewConversationsPerDay: 3}

	const workers = 30
	var (
		wg               sync.WaitGroup
		mu               sync.Mutex
		allowed, reserve int
	)
	for range workers {
		wg.Go(func() {
			d, err := limiter.Acquire(t.Context(), sender, limits)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if d.Exceeded == domain.SendRateOK {
				allowed++
			}
			if d.NewConversationReserved {
				reserve++
			}
		})
	}
	wg.Wait()
	if allowed != 5 || reserve != 3 {
		t.Fatalf("allowed %d / reserved %d of %d concurrent acquires, want 5 / 3", allowed, reserve, workers)
	}
}

func TestSendDirect_CreatedFlag(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	first, err := e.repo.SendDirect(t.Context(), u[0], u[1], domain.MessageTypeText, "hi", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created {
		t.Error("first send: Created = false, want true")
	}
	for _, pair := range [][2]uuid.UUID{{u[0], u[1]}, {u[1], u[0]}} {
		res, err := e.repo.SendDirect(t.Context(), pair[0], pair[1], domain.MessageTypeText, "again", 0)
		if err != nil {
			t.Fatal(err)
		}
		if res.Created || res.Message.ConversationID != first.Message.ConversationID {
			t.Errorf("send %s→%s: Created = %v, conv %s; want false, %s",
				pair[0], pair[1], res.Created, res.Message.ConversationID, first.Message.ConversationID)
		}
	}
}

// newRateLimitedSvc 用真实仓储与真实 Redis 计数构造应用服务，上限固定为 limits。
func (e *msgEnv) newRateLimitedSvc(t *testing.T, limits domain.SendRateLimits) application.MessageService {
	t.Helper()
	testkit.Redis(t)
	svc := application.NewMessageService(e.repo, okRecipients{}, echoUsers{}, followAll{})
	svc.SetSendRateLimiter(infrastructure.NewSendRateLimiter(), func() domain.SendRateLimits { return limits })
	return svc
}

func (e *msgEnv) countSent(t *testing.T, sender uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := e.db.Table("domains.message").Where("sender_id = ?", sender).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSend_RateLimitedNotPersisted(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	registerRateKeys(t, u...)
	svc := e.newRateLimitedSvc(t, domain.SendRateLimits{PerMinute: 2, PerDay: 100, NewConversationsPerDay: 5})

	for i := range 2 {
		if _, err := svc.Send(t.Context(), u[0], u[1], "hi"); err != nil {
			t.Fatalf("send #%d: %v", i+1, err)
		}
	}
	if _, err := svc.Send(t.Context(), u[0], u[1], "too fast"); !errors.Is(err, domain.ErrSendRateLimited) {
		t.Fatalf("3rd send err = %v, want ErrSendRateLimited", err)
	}
	if n := e.countSent(t, u[0]); n != 2 {
		t.Errorf("messages persisted = %d, want 2", n)
	}
	var sent int
	if err := e.db.Raw(`SELECT sent_count FROM domains.conversation_member WHERE user_id = ?`, u[0]).Scan(&sent).Error; err != nil || sent != 2 {
		t.Errorf("sent_count = %d (err %v), want 2", sent, err)
	}
	// 发往已有会话：预占的新会话名额全部退还。
	if got := rateCount(t, redispkg.GetMessageRateNewConvKey(u[0].String())); got != 1 {
		t.Errorf("newconv count = %d, want 1 (only the first send created a conversation)", got)
	}
}

func TestSend_NewConversationLimit(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 3)
	a, b, c := u[0], u[1], u[2]
	registerRateKeys(t, u...)
	svc := e.newRateLimitedSvc(t, domain.SendRateLimits{PerMinute: 20, PerDay: 100, NewConversationsPerDay: 1})

	if _, err := svc.Send(t.Context(), a, b, "first conversation"); err != nil {
		t.Fatal(err)
	}
	// 当天新会话名额已用完：不能再开新会话，被拒绝时不建会话、不写消息、不占发送条数。
	if _, err := svc.Send(t.Context(), a, c, "second conversation"); !errors.Is(err, domain.ErrNewConversationLimited) {
		t.Fatalf("send to new peer err = %v, want ErrNewConversationLimited", err)
	}
	low, high := domain.OrderPair(a, c)
	var convs int64
	if err := e.db.Table("domains.conversation").Where("user_low = ? AND user_high = ?", low, high).Count(&convs).Error; err != nil || convs != 0 {
		t.Errorf("conversations a-c = %d (err %v), want 0", convs, err)
	}
	if n := e.countSent(t, a); n != 1 {
		t.Errorf("messages from a = %d, want 1", n)
	}
	if got := rateCount(t, redispkg.GetMessageRateMinuteKey(a.String())); got != 1 {
		t.Errorf("minute count of a = %d, want 1 (rejected send returned)", got)
	}
	// 已有会话不受影响；对方先开的会话也不算 a 开的。
	if _, err := svc.Send(t.Context(), a, b, "existing conversation"); err != nil {
		t.Fatalf("send in existing conversation: %v", err)
	}
	if _, err := svc.Send(t.Context(), c, a, "c opens"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(t.Context(), a, c, "reply"); err != nil {
		t.Fatalf("reply in conversation opened by peer: %v", err)
	}
	if got := rateCount(t, redispkg.GetMessageRateNewConvKey(a.String())); got != 1 {
		t.Errorf("newconv count of a = %d, want 1", got)
	}
}
