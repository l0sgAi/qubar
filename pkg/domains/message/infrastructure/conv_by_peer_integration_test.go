//go:build integration

// conv_by_peer_integration_test.go 按对方用户 ID 查会话的集成测试（#246）：
// 双方各自按对方 ID 命中 (user_low, user_high) 唯一会话、不同对方各自命中自己的会话、没有会话时返回 null 且不建会话、
// 查自己报错、只读（不改未读数 / 已读位置 / 隐藏截止位置）、删除会话后返回会话 ID 但不返回删除之前的最后一条消息、
// 对方再发消息后恢复正常。复用 message_repo_pg_integration_test.go 的 msgEnv 夹具（随机用户 ID + 清理）。
package infrastructure_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"interestBar/pkg/domains/message/application"
	"interestBar/pkg/domains/message/domain"

	"github.com/google/uuid"
)

// withUser 调 GetConversationWithUser 并返回会话（没有会话时为 nil）。
func (e *msgEnv) withUser(t *testing.T, me, peer uuid.UUID) *application.ConversationItemVO {
	t.Helper()
	vo, err := e.svc.GetConversationWithUser(t.Context(), me, peer)
	if err != nil {
		t.Fatalf("GetConversationWithUser(%s, %s): %v", me, peer, err)
	}
	return vo.Conversation
}

// memberState 成员行里本接口不允许改动的字段。
type memberState struct {
	unread   int
	lastRead *uuid.UUID
	cleared  *uuid.UUID
	updated  int64
}

func (e *msgEnv) memberState(t *testing.T, conv string, user uuid.UUID) memberState {
	t.Helper()
	m := e.member(t, conv, user)
	return memberState{unread: m.UnreadCount, lastRead: m.LastReadMessageID, cleared: m.ClearedMessageID, updated: m.UpdateTime.UnixMicro()}
}

func sameID(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func TestGetConversationWithUser_HitsPairConversation(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 4)
	a, b, c, d := u[0], u[1], u[2], u[3]

	first := e.send(t, a, b, "hi")
	e.send(t, b, a, "yo")
	other := e.send(t, a, c, "other")
	long := strings.Repeat("字", application.PreviewChars+20)
	e.send(t, c, a, long)

	// 双方按对方 ID 查到的是同一个会话（OrderPair 后命中 uk_conversation_pair），各自看到自己的未读数。
	ab, ba := e.withUser(t, a, b), e.withUser(t, b, a)
	if ab == nil || ba == nil {
		t.Fatalf("a->b = %+v, b->a = %+v, want both present", ab, ba)
	}
	if ab.ConversationID != first.ConversationID || ba.ConversationID != first.ConversationID {
		t.Fatalf("conversation ids a->b=%s b->a=%s, want %s", ab.ConversationID, ba.ConversationID, first.ConversationID)
	}
	if ab.PeerID != b.String() || ab.Peer == nil || ab.Peer.ID != b.String() || ab.Type != domain.ConversationTypeDirect {
		t.Errorf("a->b item = %+v (peer %+v)", ab, ab.Peer)
	}
	if ab.LastMessage == nil || ab.LastMessage.Content != "yo" || ab.LastMessage.SenderID != b.String() || ab.UnreadCount != 1 {
		t.Errorf("a->b last = %+v unread = %d, want yo from b / 1", ab.LastMessage, ab.UnreadCount)
	}
	if ba.PeerID != a.String() || ba.UnreadCount != 1 {
		t.Errorf("b->a peer_id = %s unread = %d, want %s / 1", ba.PeerID, ba.UnreadCount, a)
	}

	// 与会话列表单项口径一致（同一个组装函数）。
	found := false
	for _, item := range e.allConversations(t, a, 10) {
		if item.ConversationID != ab.ConversationID {
			continue
		}
		found = true
		l, w := item.LastMessage, ab.LastMessage
		if item.UnreadCount != ab.UnreadCount || item.PeerID != ab.PeerID || l.ID != w.ID || l.Content != w.Content ||
			l.SenderID != w.SenderID || !l.CreateTime.Equal(w.CreateTime) {
			t.Errorf("list item = %+v / %+v, with-user item = %+v / %+v", item, l, ab, w)
		}
	}
	if !found {
		t.Fatalf("conversation %s not in a's list", ab.ConversationID)
	}

	// 不同对方各自命中自己的会话；摘要与会话列表一样截前 PreviewChars 个字符。
	ac := e.withUser(t, a, c)
	if ac == nil || ac.ConversationID != other.ConversationID || ac.ConversationID == first.ConversationID {
		t.Fatalf("a->c = %+v, want conversation %s", ac, other.ConversationID)
	}
	if ac.LastMessage == nil || utf8.RuneCountInString(ac.LastMessage.Content) != application.PreviewChars {
		t.Errorf("a->c preview = %+v, want %d chars", ac.LastMessage, application.PreviewChars)
	}

	// 没有会话：conversation 为 null，且不建会话。
	if got := e.withUser(t, b, c); got != nil {
		t.Fatalf("b->c = %+v, want nil", got)
	}
	if got := e.withUser(t, a, d); got != nil {
		t.Fatalf("a->d = %+v, want nil", got)
	}
	var n int64
	low, high := domain.OrderPair(b, c)
	if err := e.db.Table("domains.conversation").Where("user_low = ? AND user_high = ?", low, high).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("conversations for b/c = %d (err %v), want 0", n, err)
	}

	// 查自己。
	if _, err := e.svc.GetConversationWithUser(t.Context(), a, a); !errors.Is(err, domain.ErrConversationWithSelf) {
		t.Errorf("self err = %v, want ErrConversationWithSelf", err)
	}
}

func TestGetConversationWithUser_ReadOnlyAndHidden(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	e.send(t, b, a, "before delete 1")
	sent := e.send(t, b, a, "before delete 2")
	conv := sent.ConversationID

	// 只读：查询前后成员行不变（未读数、已读位置、隐藏截止位置、更新时间）。
	before := e.memberState(t, conv, a)
	if got := e.withUser(t, a, b); got == nil || got.UnreadCount != 2 {
		t.Fatalf("a->b = %+v, want unread 2", got)
	}
	if after := e.memberState(t, conv, a); after.unread != before.unread || !sameID(after.lastRead, before.lastRead) ||
		!sameID(after.cleared, before.cleared) || after.updated != before.updated {
		t.Fatalf("member changed by read-only query: before %+v after %+v", before, after)
	}

	// 删除（隐藏）后：仍返回会话 ID，不返回删除之前的最后一条消息，未读为 0；查询不解除隐藏。
	if err := e.svc.DeleteConversation(t.Context(), a, uuid.MustParse(conv)); err != nil {
		t.Fatal(err)
	}
	hiddenState := e.memberState(t, conv, a)
	hidden := e.withUser(t, a, b)
	if hidden == nil || hidden.ConversationID != conv || hidden.LastMessage != nil || hidden.UnreadCount != 0 ||
		hidden.PeerID != b.String() || hidden.Peer == nil {
		t.Fatalf("hidden a->b = %+v (last %+v), want conversation %s with no last message / unread 0", hidden, hidden.LastMessage, conv)
	}
	if after := e.memberState(t, conv, a); !sameID(after.cleared, hiddenState.cleared) || after.updated != hiddenState.updated {
		t.Fatalf("hidden member changed by read-only query: before %+v after %+v", hiddenState, after)
	}
	if list := e.allConversations(t, a, 10); len(list) != 0 {
		t.Fatalf("a conversations after delete = %d, want 0", len(list))
	}
	// 对方视角不受影响。
	if peerView := e.withUser(t, b, a); peerView == nil || peerView.LastMessage == nil || peerView.LastMessage.Content != "before delete 2" {
		t.Fatalf("b->a = %+v, want last message before delete 2", peerView)
	}
	// 拿到的会话 ID 可以直接拉历史：删除之前的消息不可见。
	if msgs := e.allMessages(t, a, hidden.ConversationID, 10); len(msgs) != 0 {
		t.Fatalf("a history after delete = %d messages, want 0", len(msgs))
	}

	// 对方再发消息：恢复正常，只显示删除之后的消息。
	e.send(t, b, a, "after delete")
	back := e.withUser(t, a, b)
	if back == nil || back.ConversationID != conv || back.LastMessage == nil || back.LastMessage.Content != "after delete" || back.UnreadCount != 1 {
		t.Fatalf("a->b after new message = %+v (last %+v), want after delete / unread 1", back, back.LastMessage)
	}
	if msgs := e.allMessages(t, a, conv, 10); len(msgs) != 1 || msgs[0].Content != "after delete" {
		t.Fatalf("a history = %+v, want only after delete", msgs)
	}
}
