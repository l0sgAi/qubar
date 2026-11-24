//go:build integration

// idempotent_integration_test.go 发送幂等（#248）在仓储层的集成测试：直接调 SendDirect(clientMsgID)，
// 覆盖重放返回原消息且不改任何计数、正文不同仍返回原消息、并发同键只落一条、已达 #239 上限时重放返回原消息、
// 不带键行为不变、键按发送人隔离、同键发给另一个接收方返回原消息。
package infrastructure_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

func (e *msgEnv) sendKeyed(t *testing.T, from, to uuid.UUID, content string, limit int, key string) *domain.SendResult {
	t.Helper()
	res, err := e.repo.SendDirect(t.Context(), from, to, domain.MessageTypeText, content, limit, &key)
	if err != nil {
		t.Fatalf("send %s -> %s key=%s: %v", from, to, key, err)
	}
	return res
}

// newKey 生成随机幂等键（UUID），测试之间不共享。
func newKey() string { return sharedomain.NewID().String() }

// countByKey 统计 sender 用 key 落库的消息条数。
func (e *msgEnv) countByKey(t *testing.T, sender uuid.UUID, key string) int64 {
	t.Helper()
	var n int64
	if err := e.db.Model(&domain.Message{}).Where("sender_id = ? AND client_msg_id = ?", sender, key).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIdempotent_ReplayReturnsOriginalWithoutSideEffects(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]
	key := newKey()

	first := e.sendKeyed(t, a, b, "hello", 0, key)
	if first.Replayed || first.Message.ClientMsgID == nil || *first.Message.ClientMsgID != key {
		t.Fatalf("first send = %+v, want new message with client_msg_id", first)
	}
	conv := first.Message.ConversationID.String()
	senderBefore, receiverBefore := e.member(t, conv, a), e.member(t, conv, b)

	// 重放（正文不同也忽略）：返回原消息，什么都不写。
	for _, content := range []string{"hello", "different body"} {
		res := e.sendKeyed(t, a, b, content, 0, key)
		if !res.Replayed {
			t.Fatalf("replay(%q) Replayed = false", content)
		}
		m := res.Message
		if m.ID != first.Message.ID || m.ConversationID != first.Message.ConversationID || m.SenderID != a ||
			m.Content != "hello" || m.MsgType != domain.MessageTypeText || !m.CreateTime.Equal(first.Message.CreateTime) {
			t.Fatalf("replay(%q) message = %+v, want original %+v", content, m, first.Message)
		}
		if res.Remaining != nil {
			t.Errorf("replay remaining = %d, want nil (limit=0)", *res.Remaining)
		}
	}
	if n := e.countByKey(t, a, key); n != 1 {
		t.Errorf("messages with key = %d, want 1", n)
	}
	if n := e.countMessages(t, first.Message.ConversationID, a); n != 1 {
		t.Errorf("messages in conversation = %d, want 1", n)
	}
	senderAfter, receiverAfter := e.member(t, conv, a), e.member(t, conv, b)
	if senderAfter.SentCount != 1 || senderAfter.SentCount != senderBefore.SentCount {
		t.Errorf("sent_count %d -> %d, want 1", senderBefore.SentCount, senderAfter.SentCount)
	}
	if receiverAfter.UnreadCount != 1 || *receiverAfter.LastMessageID != first.Message.ID ||
		*receiverBefore.LastMessageID != *receiverAfter.LastMessageID {
		t.Errorf("receiver member changed on replay: %+v -> %+v", receiverBefore, receiverAfter)
	}

	// 新键 / 不带键照常发送；不带键的消息 client_msg_id 为 NULL，可以有任意多条。
	if res := e.sendKeyed(t, a, b, "next", 0, newKey()); res.Replayed || res.Message.ID == first.Message.ID {
		t.Fatalf("new key send = %+v, want a new message", res)
	}
	for i := 0; i < 2; i++ {
		res, err := e.repo.SendDirect(t.Context(), a, b, domain.MessageTypeText, "plain", 0, nil)
		if err != nil || res.Replayed || res.Message.ClientMsgID != nil {
			t.Fatalf("send without key #%d: res=%+v err=%v", i+1, res, err)
		}
	}
	if m := e.member(t, conv, b); m.UnreadCount != 4 {
		t.Errorf("receiver unread = %d, want 4", m.UnreadCount)
	}
}

// 已达 #239 上限：第 5 条（remaining=0）超时重试返回原消息而不是 ErrSendLimitReached，sent_count 不再 +1；
// 中途的旧键重放按当前已发条数重算 remaining。
func TestIdempotent_ReplayBeforeSendLimit(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	keys := make([]string, testLimit)
	var results []*domain.SendResult
	for i := range keys {
		keys[i] = newKey()
		res := e.sendKeyed(t, a, b, fmt.Sprintf("msg %d", i), testLimit, keys[i])
		if want := testLimit - i - 1; res.Remaining == nil || *res.Remaining != want {
			t.Fatalf("send #%d remaining = %v, want %d", i+1, res.Remaining, want)
		}
		results = append(results, res)

		if i == 1 {
			// 发了 2 条时重放第 1 条：remaining = 5 − 2 = 3（与第 2 条的响应一致），不计数。
			replay := e.sendKeyed(t, a, b, "x", testLimit, keys[0])
			if !replay.Replayed || replay.Message.ID != results[0].Message.ID || replay.Remaining == nil || *replay.Remaining != 3 {
				t.Fatalf("mid replay = %+v (remaining %v), want original with remaining 3", replay, replay.Remaining)
			}
		}
	}
	conv := results[0].Message.ConversationID

	last := results[testLimit-1]
	replay := e.sendKeyed(t, a, b, "retry", testLimit, keys[testLimit-1])
	if !replay.Replayed || replay.Message.ID != last.Message.ID || replay.Remaining == nil || *replay.Remaining != 0 {
		t.Fatalf("replay at limit = %+v (remaining %v), want original with remaining 0", replay, replay.Remaining)
	}
	if m := e.member(t, conv.String(), a); m.SentCount != testLimit {
		t.Errorf("sent_count = %d, want %d", m.SentCount, testLimit)
	}
	// 新键仍被拒。
	key := newKey()
	if _, err := e.repo.SendDirect(t.Context(), a, b, domain.MessageTypeText, "new", testLimit, &key); !errors.Is(err, domain.ErrSendLimitReached) {
		t.Fatalf("new key at limit err = %v, want ErrSendLimitReached", err)
	}
	if n := e.countMessages(t, conv, a); n != testLimit {
		t.Errorf("messages = %d, want %d", n, testLimit)
	}

	// 对方回复后限制永久解除：重放的 remaining 为 nil。
	e.sendKeyed(t, b, a, "reply", testLimit, newKey())
	if res := e.sendKeyed(t, a, b, "retry", testLimit, keys[0]); !res.Replayed || res.Remaining != nil {
		t.Errorf("replay after peer replied = %+v (remaining %v), want replayed with nil remaining", res, res.Remaining)
	}
}

// 并发：同一发送人同一键同时发 N 次只落一条，全部返回同一条消息，计数只加一次。
func TestIdempotent_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]
	key := newKey()

	const n = 12
	var wg sync.WaitGroup
	results := make([]*domain.SendResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = e.repo.SendDirect(context.Background(), a, b, domain.MessageTypeText,
				fmt.Sprintf("msg %d", i), testLimit, &key)
		}(i)
	}
	wg.Wait()

	created := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("send #%d: %v", i, errs[i])
		}
		if !results[i].Replayed {
			created++
		}
		if results[i].Message.ID != results[0].Message.ID {
			t.Fatalf("send #%d message %s, want %s", i, results[i].Message.ID, results[0].Message.ID)
		}
	}
	if created != 1 {
		t.Errorf("created = %d, want 1", created)
	}
	if c := e.countByKey(t, a, key); c != 1 {
		t.Errorf("messages with key = %d, want 1", c)
	}
	conv := results[0].Message.ConversationID.String()
	if m := e.member(t, conv, a); m.SentCount != 1 {
		t.Errorf("sent_count = %d, want 1", m.SentCount)
	}
	if m := e.member(t, conv, b); m.UnreadCount != 1 {
		t.Errorf("receiver unread = %d, want 1", m.UnreadCount)
	}
}

// 键按发送人隔离；同一发送人把同一键发给另一个接收方（客户端误用）时返回原消息，不在新会话里落消息。
// 两个接收方同时发也只落一条：两次请求锁的是不同会话行，由唯一索引兜底（ON CONFLICT DO NOTHING 后查回）。
func TestIdempotent_KeyScopeAndCrossReceiver(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 4)
	a, b, c, d := u[0], u[1], u[2], u[3]
	key := newKey()

	fromA := e.sendKeyed(t, a, b, "from a", 0, key)
	fromC := e.sendKeyed(t, c, b, "from c", 0, key)
	if fromC.Replayed || fromC.Message.ID == fromA.Message.ID {
		t.Fatalf("same key from another sender = %+v, want a new message", fromC)
	}

	other := e.sendKeyed(t, a, d, "to d", 0, key)
	if !other.Replayed || other.Message.ID != fromA.Message.ID || other.Message.ConversationID != fromA.Message.ConversationID {
		t.Fatalf("same key to another receiver = %+v, want original message", other)
	}
	if _, err := e.repo.GetMember(t.Context(), other.Message.ConversationID, d); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("receiver d should not become a member of a's conversation: %v", err)
	}
	var dMessages int64
	if err := e.db.Model(&domain.Message{}).
		Joins("JOIN domains.conversation_member m ON m.conversation_id = domains.message.conversation_id").
		Where("m.user_id = ?", d).Count(&dMessages).Error; err != nil {
		t.Fatal(err)
	}
	if dMessages != 0 {
		t.Errorf("messages visible to d = %d, want 0", dMessages)
	}

	// 并发：同一键同时发给两个不同接收方。
	key2 := newKey()
	var wg sync.WaitGroup
	results := make([]*domain.SendResult, 2)
	errs := make([]error, 2)
	for i, to := range []uuid.UUID{b, d} {
		wg.Add(1)
		go func(i int, to uuid.UUID) {
			defer wg.Done()
			results[i], errs[i] = e.repo.SendDirect(context.Background(), a, to, domain.MessageTypeText, "race", 0, &key2)
		}(i, to)
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("cross-receiver race #%d: %v", i, errs[i])
		}
	}
	if results[0].Message.ID != results[1].Message.ID || results[0].Replayed == results[1].Replayed {
		t.Errorf("cross-receiver race results = %+v / %+v, want one created and one replayed of the same message", results[0], results[1])
	}
	if n := e.countByKey(t, a, key2); n != 1 {
		t.Errorf("messages with key2 = %d, want 1", n)
	}
}
