//go:build integration

// send_limit_integration_test.go 未关注发送限制在仓储层的集成测试（#239）：直接调 SendDirect(limit)，
// 覆盖剩余条数递减、满额拒绝且不落任何数据、对方回复后永久解除、并发发送不突破上限。
// 关注关系在应用层判断（limit=0 即对方关注了我），端到端规则见 pkg/composition/message_send_limit_integration_test.go。
package infrastructure_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"interestBar/pkg/domains/message/domain"

	"github.com/google/uuid"
)

const testLimit = 5

func (e *msgEnv) sendLimited(t *testing.T, from, to uuid.UUID, limit int) (*domain.SendResult, error) {
	t.Helper()
	return e.repo.SendDirect(t.Context(), from, to, domain.MessageTypeText, "hi", limit)
}

func (e *msgEnv) countMessages(t *testing.T, conv uuid.UUID, sender uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&domain.Message{}).Where("conversation_id = ? AND sender_id = ?", conv, sender).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSendLimit_RemainingAndReject(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	var conv uuid.UUID
	for i := 0; i < testLimit; i++ {
		res, err := e.sendLimited(t, a, b, testLimit)
		if err != nil {
			t.Fatalf("send #%d: %v", i+1, err)
		}
		if want := testLimit - i - 1; res.Remaining == nil || *res.Remaining != want {
			t.Fatalf("send #%d remaining = %v, want %d", i+1, res.Remaining, want)
		}
		conv = res.Message.ConversationID
	}
	before := e.member(t, conv.String(), b)

	// 第 6 条被拒：消息、计数、对方未读与最新消息都不变（事务回滚）。
	if _, err := e.sendLimited(t, a, b, testLimit); !errors.Is(err, domain.ErrSendLimitReached) {
		t.Fatalf("6th send err = %v, want ErrSendLimitReached", err)
	}
	if n := e.countMessages(t, conv, a); n != testLimit {
		t.Errorf("messages = %d, want %d", n, testLimit)
	}
	after := e.member(t, conv.String(), b)
	if after.UnreadCount != before.UnreadCount || *after.LastMessageID != *before.LastMessageID {
		t.Errorf("receiver member changed on rejected send: %+v -> %+v", before, after)
	}
	if m := e.member(t, conv.String(), a); m.SentCount != testLimit {
		t.Errorf("sent_count = %d, want %d", m.SentCount, testLimit)
	}

	// limit=0（应用层确认对方关注了我）：不限制，remaining 为 nil，sent_count 照常累加。
	res, err := e.sendLimited(t, a, b, 0)
	if err != nil || res.Remaining != nil {
		t.Fatalf("unlimited send: remaining=%v err=%v", res, err)
	}
	// 关注取消后恢复限制，按已发总数（6）计算：仍被拒。
	if _, err := e.sendLimited(t, a, b, testLimit); !errors.Is(err, domain.ErrSendLimitReached) {
		t.Fatalf("send after unfollow err = %v, want ErrSendLimitReached", err)
	}
}

func TestSendLimit_PeerReplyUnlocksForever(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	for i := 0; i < testLimit; i++ {
		if _, err := e.sendLimited(t, a, b, testLimit); err != nil {
			t.Fatalf("send #%d: %v", i+1, err)
		}
	}
	// b 回复一次：a 已经发过消息，所以 b 本身不受限制（remaining=nil）。
	res, err := e.sendLimited(t, b, a, testLimit)
	if err != nil || res.Remaining != nil {
		t.Fatalf("reply: res=%+v err=%v, want unlimited", res, err)
	}
	// 之后 a 不再受限制（即使应用层仍判定 b 没关注 a，传 limit=5）。
	for i := 0; i < testLimit+2; i++ {
		res, err := e.sendLimited(t, a, b, testLimit)
		if err != nil || res.Remaining != nil {
			t.Fatalf("send after reply #%d: res=%+v err=%v, want unlimited", i+1, res, err)
		}
	}
	// b 删除会话也不影响「回复过」。
	if err := e.repo.Hide(t.Context(), res.Message.ConversationID, b); err != nil {
		t.Fatal(err)
	}
	if res, err := e.sendLimited(t, a, b, testLimit); err != nil || res.Remaining != nil {
		t.Fatalf("send after peer hid conversation: res=%+v err=%v", res, err)
	}
}

func TestSendLimit_ConcurrentSendsNeverExceedLimit(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	// 会话不存在时并发首发：建会话与计数都在会话行锁内，恰好 testLimit 条成功。
	const attempts = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, rejected := 0, 0
	remainings := map[int]int{}
	var conv uuid.UUID
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := e.repo.SendDirect(context.Background(), a, b, domain.MessageTypeText, fmt.Sprintf("msg %d", i), testLimit)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, domain.ErrSendLimitReached):
				rejected++
			case err != nil:
				errs <- err
			default:
				ok++
				conv = res.Message.ConversationID
				if res.Remaining != nil {
					remainings[*res.Remaining]++
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent send: %v", err)
	}
	if ok != testLimit || rejected != attempts-testLimit {
		t.Fatalf("ok=%d rejected=%d, want %d / %d", ok, rejected, testLimit, attempts-testLimit)
	}
	// 每个剩余条数 4..0 恰好出现一次（串行化的证据）。
	for r := 0; r < testLimit; r++ {
		if remainings[r] != 1 {
			t.Errorf("remaining %d seen %d times, want 1 (all: %v)", r, remainings[r], remainings)
		}
	}
	if n := e.countMessages(t, conv, a); n != testLimit {
		t.Errorf("messages = %d, want %d", n, testLimit)
	}
	if m := e.member(t, conv.String(), a); m.SentCount != testLimit {
		t.Errorf("sent_count = %d, want %d", m.SentCount, testLimit)
	}
	if m := e.member(t, conv.String(), b); m.UnreadCount != testLimit {
		t.Errorf("receiver unread = %d, want %d", m.UnreadCount, testLimit)
	}
}
