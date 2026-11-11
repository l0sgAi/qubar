//go:build integration

// message_send_limit_integration_test.go 未关注发送限制经 composition 装配后的端到端集成测试（#239）：真实 PG + Redis，
// 关注关系走 messageFollowChecker → FollowService.GetRelations（真实关注 / 取关）。
// 覆盖：第 6 条被拒、关注后立即解锁、取关后恢复并按已发总数计算、回复后永久解锁（之后取关也不再限制）、并发不超限。
package composition

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	messageapp "interestBar/pkg/domains/message/application"
	messagedomain "interestBar/pkg/domains/message/domain"

	"github.com/google/uuid"
)

func (e *messageEnv) mustSend(t *testing.T, from, to uuid.UUID) *messageapp.SendVO {
	t.Helper()
	vo, err := e.svc.Send(t.Context(), from, to, "hi")
	if err != nil {
		t.Fatalf("send %s -> %s: %v", from, to, err)
	}
	return vo
}

func (e *messageEnv) assertRejected(t *testing.T, from, to uuid.UUID) {
	t.Helper()
	if _, err := e.svc.Send(t.Context(), from, to, "hi"); !errors.Is(err, messagedomain.ErrSendLimitReached) {
		t.Fatalf("send %s -> %s err = %v, want ErrSendLimitReached", from, to, err)
	}
}

func assertRemaining(t *testing.T, vo *messageapp.SendVO, want *int) {
	t.Helper()
	switch {
	case want == nil && vo.RemainingCount != nil:
		t.Fatalf("remaining_count = %d, want null", *vo.RemainingCount)
	case want != nil && (vo.RemainingCount == nil || *vo.RemainingCount != *want):
		t.Fatalf("remaining_count = %v, want %d", vo.RemainingCount, *want)
	}
}

// 第 6 条被拒 → 对方关注后立即解锁 → 对方取关后恢复限制，按已发总数计算（已超过 5 条，仍被拒）。
func TestMessageSendLimit_FollowUnlocksAndUnfollowRestores(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	ctx := t.Context()
	a := e.insertUser(t, "it239 a", 0, 1, 0)
	b := e.insertUser(t, "it239 b", 0, 1, 0)

	// a 关注 b 不影响限制（看的是 b 是否关注 a）。
	if _, err := e.follow.Follow(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < messageapp.UnfollowedSendLimit; i++ {
		want := messageapp.UnfollowedSendLimit - i - 1
		assertRemaining(t, e.mustSend(t, a, b), &want)
	}
	e.assertRejected(t, a, b)

	if _, err := e.follow.Follow(ctx, b, a); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		assertRemaining(t, e.mustSend(t, a, b), nil)
	}

	if _, err := e.follow.Unfollow(ctx, b, a); err != nil {
		t.Fatal(err)
	}
	e.assertRejected(t, a, b)
}

// 取关后按已发总数继续算：发了 2 条后对方关注又取关，再发 1 条后剩余 2（5 - 3）。
func TestMessageSendLimit_UnfollowCountsTotalSent(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	ctx := t.Context()
	a := e.insertUser(t, "it239 a", 0, 1, 0)
	b := e.insertUser(t, "it239 b", 0, 1, 0)

	e.mustSend(t, a, b)
	e.mustSend(t, a, b)
	if _, err := e.follow.Follow(ctx, b, a); err != nil {
		t.Fatal(err)
	}
	if _, err := e.follow.Unfollow(ctx, b, a); err != nil {
		t.Fatal(err)
	}
	two := 2
	assertRemaining(t, e.mustSend(t, a, b), &two)
}

// b 回复一次后永久解除：之后 b 取关（或从未关注）a 都不再限制；b 回复本身也不受限制（a 已在会话里发过消息）。
func TestMessageSendLimit_ReplyUnlocksForever(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	ctx := t.Context()
	a := e.insertUser(t, "it239 a", 0, 1, 0)
	b := e.insertUser(t, "it239 b", 0, 1, 0)

	for i := 0; i < messageapp.UnfollowedSendLimit; i++ {
		e.mustSend(t, a, b)
	}
	e.assertRejected(t, a, b)

	// b 先关注再回复再取关。
	if _, err := e.follow.Follow(ctx, b, a); err != nil {
		t.Fatal(err)
	}
	assertRemaining(t, e.mustSend(t, b, a), nil)
	if _, err := e.follow.Unfollow(ctx, b, a); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < messageapp.UnfollowedSendLimit+1; i++ {
		assertRemaining(t, e.mustSend(t, a, b), nil)
	}
}

// 双方互不关注时并发发送：a 同时发 15 条，恰好 5 条成功；b 的会话视角未读也是 5。
func TestMessageSendLimit_ConcurrentSendsNeverExceedLimit(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	a := e.insertUser(t, "it239 a", 0, 1, 0)
	b := e.insertUser(t, "it239 b", 0, 1, 0)

	const attempts = 15
	var wg sync.WaitGroup
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := e.svc.Send(context.Background(), a, b, fmt.Sprintf("msg %d", i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	ok, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, messagedomain.ErrSendLimitReached):
			rejected++
		default:
			t.Fatalf("concurrent send: %v", err)
		}
	}
	if ok != messageapp.UnfollowedSendLimit || rejected != attempts-messageapp.UnfollowedSendLimit {
		t.Fatalf("ok=%d rejected=%d, want %d / %d", ok, rejected, messageapp.UnfollowedSendLimit, attempts-messageapp.UnfollowedSendLimit)
	}
	conv, err := e.svc.GetConversationWithUser(t.Context(), b, a)
	if err != nil || conv.Conversation == nil || conv.Conversation.UnreadCount != messageapp.UnfollowedSendLimit {
		t.Fatalf("receiver conversation = %+v (err %v), want unread %d", conv, err, messageapp.UnfollowedSendLimit)
	}
}
