//go:build integration

// message_stream_integration_test.go 私信未读与新消息经 /notice/stream 实时推送的端到端集成测试（#241，make test-integration）：
// 真实 PG + Redis（私信与未读缓存、sa-token 登录态）+ 真实 TCP SSE 连接，hub 与 message 按 wireMessageStream 装配。
// 覆盖：首包同时下发 unread-count 与 dm-unread；发消息后接收方收到 dm-message 摘要与 dm-unread=1，发送方收不到摘要；
// 标记已读、删除会话后 dm-unread 归零；消息中心 unread-count 不受私信影响。
package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	messageapp "interestBar/pkg/domains/message/application"
	noticeapp "interestBar/pkg/domains/notice/application"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"github.com/sa-tokens/sa-token-go/stputil"
)

// sseEvent 一条解析后的 SSE 事件。
type sseEvent struct {
	name string
	data string
}

// nextEvent 读下一条完整事件（以空行结束）；注释行（: ping）与无事件名的残段忽略。流结束或超时返回 false。
func (s *sseStream) nextEvent(timeout time.Duration) (sseEvent, bool) {
	deadline := time.After(timeout)
	var ev sseEvent
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return sseEvent{}, false
			}
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			case line == "":
				if ev.name != "" {
					return ev, true
				}
				ev = sseEvent{}
			}
		case <-deadline:
			return sseEvent{}, false
		}
	}
}

// collectUntil 持续读事件直到 done(已读到的全部事件) 为 true，返回读到的事件；超时 t.Fatal。
func (s *sseStream) collectUntil(t *testing.T, timeout time.Duration, what string, done func([]sseEvent) bool) []sseEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var got []sseEvent
	for {
		left := time.Until(deadline)
		if left <= 0 {
			t.Fatalf("%s: not satisfied within %s, got %+v", what, timeout, got)
		}
		ev, ok := s.nextEvent(left)
		if !ok {
			t.Fatalf("%s: stream ended or timed out, got %+v", what, got)
		}
		got = append(got, ev)
		if done(got) {
			return got
		}
	}
}

// unreadValue 解析计数事件的 {"unread_count": N}。
func unreadValue(t *testing.T, ev sseEvent) int64 {
	t.Helper()
	var v struct {
		UnreadCount *int64 `json:"unread_count"`
	}
	if err := json.Unmarshal([]byte(ev.data), &v); err != nil || v.UnreadCount == nil {
		t.Fatalf("bad %s data %q: %v", ev.name, ev.data, err)
	}
	return *v.UnreadCount
}

// lastDMUnreadIs 事件列表里最后一条 dm-unread 的值是否为 want。
func lastDMUnreadIs(t *testing.T, evs []sseEvent, want int64) bool {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].name == sseEventDMUnread {
			return unreadValue(t, evs[i]) == want
		}
	}
	return false
}

func findEvent(evs []sseEvent, name string) (sseEvent, bool) {
	for _, ev := range evs {
		if ev.name == name {
			return ev, true
		}
	}
	return sseEvent{}, false
}

// loginStream 登录 userID 并注册 sa-token key 清理，返回 token。
func loginStream(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	loginID := userID.String()
	token, err := stputil.Login(loginID, "pc")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	p := streamSaTokenPrefix
	testkit.CleanupKeys(t,
		p+"token:"+token, p+"token-session:"+token, p+"renew:"+token,
		p+"account:"+loginID+":pc", p+"session:"+loginID,
	)
	return token
}

func TestMessageStream_DMUnreadAndMessagePush(t *testing.T) {
	t.Parallel()
	initStreamSaToken(t)
	e := setupMessage(t)
	sender := e.insertUser(t, "it241 sender", 0, 1, 0)
	receiver := e.insertUser(t, "it241 receiver", 0, 1, 0)
	testkit.CleanupKeys(t, redispkg.GetMessageUnreadKey(sender), redispkg.GetMessageUnreadKey(receiver))

	// 合并窗口 50ms 加快测试；消息中心通道用固定 0 的桩（只验证互不覆盖）。
	hub := noticeapp.NewStreamHub(5, 50)
	t.Cleanup(hub.Stop)
	hub.SetCountReader(func(context.Context, uuid.UUID) (int64, error) { return 0, nil })
	wireMessageStream(e.svc, hub)
	base := startNoticeStreamServer(t, hub)
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	// 首包：openStream 已读到 unread-count，紧接着是 dm-unread 全量 0。
	rs := openStream(t, client, base, loginStream(t, receiver))
	first := rs.collectUntil(t, 3*time.Second, "receiver first dm-unread", func(evs []sseEvent) bool {
		_, ok := findEvent(evs, sseEventDMUnread)
		return ok
	})
	if !lastDMUnreadIs(t, first, 0) {
		t.Fatalf("first dm-unread = %+v, want 0", first)
	}
	ss := openStream(t, client, base, loginStream(t, sender))
	ss.collectUntil(t, 3*time.Second, "sender first dm-unread", func(evs []sseEvent) bool {
		_, ok := findEvent(evs, sseEventDMUnread)
		return ok
	})

	// 发消息：接收方收到 dm-message 摘要 + dm-unread=1（顺序不限）。
	sent, err := e.svc.Send(t.Context(), sender, receiver, "hello #241")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	evs := rs.collectUntil(t, 3*time.Second, "dm-message and dm-unread=1", func(evs []sseEvent) bool {
		_, ok := findEvent(evs, sseEventDMMessage)
		return ok && lastDMUnreadIs(t, evs, 1)
	})
	if _, ok := findEvent(evs, sseEventUnreadCount); ok {
		t.Fatalf("notice unread-count should not be pushed by a private message: %+v", evs)
	}
	msgEv, _ := findEvent(evs, sseEventDMMessage)
	var push messageapp.NewMessageEvent
	if err := json.Unmarshal([]byte(msgEv.data), &push); err != nil {
		t.Fatalf("bad dm-message data %q: %v", msgEv.data, err)
	}
	if push.ID != sent.ID || push.ConversationID != sent.ConversationID || push.SenderID != sender.String() ||
		push.Content != "hello #241" || push.Truncated || push.Sender == nil || push.Sender.Username != "it241 sender" {
		t.Fatalf("dm-message = %+v (sender %+v), sent %+v", push, push.Sender, sent)
	}
	if got, err := e.svc.GetUnreadTotal(t.Context(), receiver); err != nil || got != 1 {
		t.Fatalf("GetUnreadTotal = %d (err %v), want 1 (same source as push)", got, err)
	}

	// 摘要只推给接收方：发送方连接收不到 dm-message（发送方未读也不变，不推 dm-unread）。
	if ev, ok := ss.nextEvent(500 * time.Millisecond); ok {
		t.Fatalf("sender should receive nothing after sending, got %+v", ev)
	}

	// 标记已读 → 归零。
	conv := uuid.MustParse(sent.ConversationID)
	if err := e.svc.MarkRead(t.Context(), receiver, conv); err != nil {
		t.Fatal(err)
	}
	rs.collectUntil(t, 3*time.Second, "dm-unread=0 after read", func(evs []sseEvent) bool {
		return lastDMUnreadIs(t, evs, 0)
	})

	// 再来一条后删除会话 → 归零。
	if _, err := e.svc.Send(t.Context(), sender, receiver, "again"); err != nil {
		t.Fatal(err)
	}
	rs.collectUntil(t, 3*time.Second, "dm-unread=1 again", func(evs []sseEvent) bool {
		return lastDMUnreadIs(t, evs, 1)
	})
	if err := e.svc.DeleteConversation(t.Context(), receiver, conv); err != nil {
		t.Fatal(err)
	}
	rs.collectUntil(t, 3*time.Second, "dm-unread=0 after delete", func(evs []sseEvent) bool {
		return lastDMUnreadIs(t, evs, 0)
	})

	// 重连：首包直接是当前全量（拉取为准）。
	if err := e.svc.MarkRead(t.Context(), receiver, conv); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Send(t.Context(), sender, receiver, "while offline"); err != nil {
		t.Fatal(err)
	}
	rs.cancel()
	rs2 := openStream(t, client, base, loginStream(t, receiver))
	again := rs2.collectUntil(t, 3*time.Second, "reconnect first dm-unread", func(evs []sseEvent) bool {
		_, ok := findEvent(evs, sseEventDMUnread)
		return ok
	})
	if !lastDMUnreadIs(t, again, 1) {
		t.Fatalf("reconnect first dm-unread = %+v, want 1", again)
	}
}
