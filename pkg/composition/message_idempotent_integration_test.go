//go:build integration

// message_idempotent_integration_test.go 发送幂等（#248）经 composition 装配后的端到端集成测试：真实 PG + Redis，
// 关注关系走真实 FollowService（未关注时 #239 的 5 条限制生效）。
// 覆盖：重放返回原消息且不改未读（含 #241 的 Redis 未读缓存）/ 计数、已达 5 条上限时重放返回原消息（remaining_count=0）而新键 403、非法键 400 前置拒绝。
package composition

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	messageapp "interestBar/pkg/domains/message/application"
	messagedomain "interestBar/pkg/domains/message/domain"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func (e *messageEnv) mustSendKeyed(t *testing.T, from, to uuid.UUID, content, key string) *messageapp.SendVO {
	t.Helper()
	vo, err := e.svc.SendWithClientMsgID(t.Context(), from, to, content, key)
	if err != nil {
		t.Fatalf("send %s -> %s key=%s: %v", from, to, key, err)
	}
	return vo
}

func TestMessageIdempotent_ReplayAtSendLimit(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	ctx := t.Context()
	a := e.insertUser(t, "it248 a", 0, 1, 0)
	b := e.insertUser(t, "it248 b", 0, 1, 0)
	testkit.CleanupKeys(t, redispkg.GetMessageUnreadKey(a), redispkg.GetMessageUnreadKey(b))

	keys := make([]string, messageapp.UnfollowedSendLimit)
	sent := make([]*messageapp.SendVO, messageapp.UnfollowedSendLimit)
	for i := range keys {
		keys[i] = sharedomain.NewID().String()
		sent[i] = e.mustSendKeyed(t, a, b, fmt.Sprintf("msg %d", i), keys[i])
		want := messageapp.UnfollowedSendLimit - i - 1
		assertRemaining(t, sent[i], &want)
	}

	// 先读一次接收方未读总数，回填 Redis 缓存（#241：之后的发送只在 key 存在时 INCR）。
	if n, err := e.svc.GetUnreadTotal(ctx, b); err != nil || n != int64(messageapp.UnfollowedSendLimit) {
		t.Fatalf("unread total = %d err %v, want %d", n, err, messageapp.UnfollowedSendLimit)
	}

	// 第 5 条超时重试（正文不同）：返回原消息，remaining_count 仍为 0，不是 403。
	last := sent[len(sent)-1]
	replay := e.mustSendKeyed(t, a, b, "retry body", keys[len(keys)-1])
	// 重放不自增接收方未读缓存（不调 afterSend）。
	if n, err := e.svc.GetUnreadTotal(ctx, b); err != nil || n != int64(messageapp.UnfollowedSendLimit) {
		t.Fatalf("unread total after replay = %d err %v, want %d", n, err, messageapp.UnfollowedSendLimit)
	}
	if replay.ID != last.ID || replay.ConversationID != last.ConversationID || replay.Content != last.Content ||
		!replay.CreateTime.Equal(last.CreateTime) {
		t.Fatalf("replay = %+v, want original %+v", replay.MessageVO, last.MessageVO)
	}
	zero := 0
	assertRemaining(t, replay, &zero)

	// 新键（或不带键）仍被 5 条限制拒绝。
	if _, err := e.svc.SendWithClientMsgID(ctx, a, b, "new", sharedomain.NewID().String()); !errors.Is(err, messagedomain.ErrSendLimitReached) {
		t.Fatalf("new key at limit err = %v, want ErrSendLimitReached", err)
	}
	e.assertRejected(t, a, b)

	// 重放没有写任何东西：对方会话里只有 5 条、未读 5；消息历史里第 5 条是原正文。
	list, err := e.svc.ListConversations(ctx, b, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].UnreadCount != messageapp.UnfollowedSendLimit ||
		list.Items[0].LastMessage == nil || list.Items[0].LastMessage.ID != last.ID {
		t.Fatalf("receiver conversations = %+v, want 1 item with unread %d and last message %s",
			list.Items, messageapp.UnfollowedSendLimit, last.ID)
	}
	hist, err := e.svc.ListMessages(ctx, b, uuid.MustParse(last.ConversationID), "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.Items) != messageapp.UnfollowedSendLimit || hist.Items[0].Content != last.Content {
		t.Fatalf("history = %d items (first %+v), want %d with original content", len(hist.Items), hist.Items[0], messageapp.UnfollowedSendLimit)
	}

	// 对方关注后不受限制：重放的 remaining_count 为 null。
	if _, err := e.follow.Follow(ctx, b, a); err != nil {
		t.Fatal(err)
	}
	assertRemaining(t, e.mustSendKeyed(t, a, b, "x", keys[0]), nil)
}

func TestMessageIdempotent_InvalidKeyRejectedBeforeWrite(t *testing.T) {
	t.Parallel()
	e := setupMessage(t)
	a := e.insertUser(t, "it248 c", 0, 1, 0)
	b := e.insertUser(t, "it248 d", 0, 1, 0)
	for _, key := range []string{strings.Repeat("k", messageapp.MaxClientMsgIDLen+1), "has space", "点"} {
		if _, err := e.svc.SendWithClientMsgID(t.Context(), a, b, "hi", key); !errors.Is(err, messagedomain.ErrInvalidClientMsgID) {
			t.Fatalf("key %q err = %v, want ErrInvalidClientMsgID", key, err)
		}
	}
	if _, err := e.svc.GetConversationWithUser(t.Context(), a, b); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := e.db.Raw(`SELECT count(*) FROM domains.message WHERE sender_id = ?`, a).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("messages written = %d, want 0", n)
	}
}
