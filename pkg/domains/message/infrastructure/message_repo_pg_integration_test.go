//go:build integration

// message_repo_pg_integration_test.go 私信仓储 + 应用服务对开发栈 PostgreSQL 的集成测试（#238）：
// 并发首发只建一个会话且计数准确、会话列表 / 消息历史分页无重复无遗漏、非会话成员不可读、
// 标记已读、删除会话后对方再发消息时重现且只显示删除之后的消息。
// 用户 ID 全部随机生成（仓储不读 users 表；接收方校验与用户信息用桩），测试之间互不影响。
package infrastructure_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"interestBar/pkg/domains/message/application"
	"interestBar/pkg/domains/message/domain"
	"interestBar/pkg/domains/message/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// okRecipients 所有接收方都可以收私信。
type okRecipients struct{}

func (okRecipients) CheckRecipient(context.Context, uuid.UUID) (application.RecipientStatus, error) {
	return application.RecipientOK, nil
}

// echoUsers 把每个 ID 都当作正常用户返回（用户名 = ID）。
type echoUsers struct{}

func (echoUsers) GetBriefs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]application.UserBrief, error) {
	out := make(map[uuid.UUID]application.UserBrief, len(ids))
	for _, id := range ids {
		out[id] = application.UserBrief{ID: id.String(), Username: id.String()}
	}
	return out, nil
}

type msgEnv struct {
	db   *gorm.DB
	repo domain.Repository
	svc  application.MessageService
}

func newMsgEnv(t *testing.T) *msgEnv {
	t.Helper()
	db := testkit.PG(t)
	repo := infrastructure.NewRepository(db)
	return &msgEnv{db: db, repo: repo, svc: application.NewMessageService(repo, okRecipients{}, echoUsers{})}
}

// newUsers 生成 n 个随机用户 ID，并注册清理它们的会话、成员行与消息。
// 测试里每个会话的双方都来自 newUsers，所以按 user_low / user_id / sender_id 清理即可覆盖全部数据。
func (e *msgEnv) newUsers(t *testing.T, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	vals := make([]any, n)
	for i := range ids {
		ids[i] = sharedomain.NewID()
		vals[i] = ids[i]
	}
	testkit.CleanupRows(t, "domains.conversation", "user_low", vals...)
	testkit.CleanupRows(t, "domains.conversation_member", "user_id", vals...)
	testkit.CleanupRows(t, "domains.message", "sender_id", vals...)
	return ids
}

func (e *msgEnv) send(t *testing.T, from, to uuid.UUID, content string) *application.MessageVO {
	t.Helper()
	vo, err := e.svc.Send(t.Context(), from, to, content)
	if err != nil {
		t.Fatalf("send %s -> %s: %v", from, to, err)
	}
	return vo
}

func (e *msgEnv) member(t *testing.T, conv string, user uuid.UUID) *domain.ConversationMember {
	t.Helper()
	m, err := e.repo.GetMember(t.Context(), uuid.MustParse(conv), user)
	if err != nil {
		t.Fatalf("GetMember(%s, %s): %v", conv, user, err)
	}
	return m
}

// allMessages 用游标翻完 user 在会话里能看到的全部消息（新 → 旧）。
func (e *msgEnv) allMessages(t *testing.T, user uuid.UUID, conv string, size int) []application.MessageVO {
	t.Helper()
	var out []application.MessageVO
	cursor := ""
	for page := 0; ; page++ {
		if page > 100 {
			t.Fatal("too many pages")
		}
		vo, err := e.svc.ListMessages(t.Context(), user, uuid.MustParse(conv), cursor, size)
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}
		out = append(out, vo.Items...)
		if !vo.HasMore {
			if vo.NextCursor != "" {
				t.Fatal("next_cursor must be empty when has_more=false")
			}
			return out
		}
		cursor = vo.NextCursor
	}
}

// allConversations 用游标翻完 user 的会话列表。
func (e *msgEnv) allConversations(t *testing.T, user uuid.UUID, size int) []application.ConversationItemVO {
	t.Helper()
	var out []application.ConversationItemVO
	cursor := ""
	for page := 0; ; page++ {
		if page > 100 {
			t.Fatal("too many pages")
		}
		vo, err := e.svc.ListConversations(t.Context(), user, cursor, size)
		if err != nil {
			t.Fatalf("ListConversations: %v", err)
		}
		out = append(out, vo.Items...)
		if !vo.HasMore {
			return out
		}
		cursor = vo.NextCursor
	}
}

func TestSend_ConcurrentFirstSendCreatesOneConversation(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	// 双方同时首发：每个方向 10 条。
	const perSide = 10
	var wg sync.WaitGroup
	convIDs := make(chan string, 2*perSide)
	errs := make(chan error, 2*perSide)
	for i := 0; i < perSide; i++ {
		for _, pair := range [][2]uuid.UUID{{a, b}, {b, a}} {
			wg.Add(1)
			go func(from, to uuid.UUID, i int) {
				defer wg.Done()
				vo, err := e.svc.Send(context.Background(), from, to, fmt.Sprintf("msg %d", i))
				if err != nil {
					errs <- err
					return
				}
				convIDs <- vo.ConversationID
			}(pair[0], pair[1], i)
		}
	}
	wg.Wait()
	close(errs)
	close(convIDs)
	for err := range errs {
		t.Fatalf("concurrent send: %v", err)
	}
	seen := map[string]bool{}
	for id := range convIDs {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatalf("got %d conversations, want 1: %v", len(seen), seen)
	}
	var conv string
	for id := range seen {
		conv = id
	}

	low, high := domain.OrderPair(a, b)
	var rows int64
	if err := e.db.Model(&domain.Conversation{}).Where("user_low = ? AND user_high = ?", low, high).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("conversation rows = %d, want 1", rows)
	}

	// 双方各自发了 perSide 条、收到 perSide 条未读，最新消息位置一致。
	ma, mb := e.member(t, conv, a), e.member(t, conv, b)
	for name, m := range map[string]*domain.ConversationMember{"a": ma, "b": mb} {
		if m.SentCount != perSide || m.UnreadCount != perSide || m.FirstSentTime == nil {
			t.Errorf("member %s: sent=%d unread=%d first_sent=%v, want %d / %d / set", name, m.SentCount, m.UnreadCount, m.FirstSentTime, perSide, perSide)
		}
	}
	if *ma.PeerID != b || *mb.PeerID != a {
		t.Errorf("peer ids = %s / %s", ma.PeerID, mb.PeerID)
	}
	msgs := e.allMessages(t, a, conv, 50)
	if len(msgs) != 2*perSide {
		t.Fatalf("messages = %d, want %d", len(msgs), 2*perSide)
	}
	if ma.LastMessageID == nil || *ma.LastMessageID != uuid.MustParse(msgs[0].ID) || *mb.LastMessageID != *ma.LastMessageID {
		t.Errorf("last_message_id a=%v b=%v, want newest %s", ma.LastMessageID, mb.LastMessageID, msgs[0].ID)
	}
}

func TestListMessages_PaginationNoDuplicatesNoGaps(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]

	const total = 23
	sent := make([]string, total)
	var conv string
	for i := 0; i < total; i++ {
		from, to := a, b
		if i%3 == 0 {
			from, to = b, a
		}
		vo := e.send(t, from, to, fmt.Sprintf("m%02d", i))
		sent[i], conv = vo.ID, vo.ConversationID
	}
	for _, size := range []int{1, 5, 7, total, 50} {
		got := e.allMessages(t, b, conv, size)
		if len(got) != total {
			t.Fatalf("size %d: got %d messages, want %d", size, len(got), total)
		}
		for i, m := range got {
			// 新 → 旧
			if want := sent[total-1-i]; m.ID != want {
				t.Fatalf("size %d: item %d = %s, want %s", size, i, m.ID, want)
			}
		}
	}
}

func TestListConversations_PaginationOrderAndPreview(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	const peers = 7
	u := e.newUsers(t, peers+1)
	me := u[0]

	// 与每个对方各一个会话；按发送顺序，最后发送的会话排最前。
	order := make([]string, 0, peers)
	for i := 1; i <= peers; i++ {
		vo := e.send(t, u[i], me, fmt.Sprintf("hello %d", i))
		order = append(order, vo.ConversationID)
	}
	// 第 2 个会话来了新消息，移到最前（由我发出：不加我的未读）。
	long := ""
	for i := 0; i < 150; i++ {
		long += "字"
	}
	e.send(t, me, u[2], long)
	want := append([]string{order[1]}, order[6], order[5], order[4], order[3], order[2], order[0])

	for _, size := range []int{1, 3, peers, 20} {
		got := e.allConversations(t, me, size)
		if len(got) != peers {
			t.Fatalf("size %d: got %d conversations, want %d", size, len(got), peers)
		}
		for i, item := range got {
			if item.ConversationID != want[i] {
				t.Fatalf("size %d: item %d = %s, want %s", size, i, item.ConversationID, want[i])
			}
		}
	}

	got := e.allConversations(t, me, 20)
	top := got[0]
	if top.PeerID != u[2].String() || top.Peer == nil || top.LastMessage.SenderID != me.String() || top.UnreadCount != 1 {
		t.Errorf("top = %+v", top)
	}
	if n := len([]rune(top.LastMessage.Content)); n != application.PreviewChars {
		t.Errorf("preview chars = %d, want %d", n, application.PreviewChars)
	}
	if got[1].LastMessage.Content != "hello 7" || got[1].UnreadCount != 1 {
		t.Errorf("second = %+v", got[1])
	}
}

func TestNonMemberCannotReadOrModify(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 3)
	a, b, outsider := u[0], u[1], u[2]
	conv := uuid.MustParse(e.send(t, a, b, "secret").ConversationID)

	if _, err := e.svc.ListMessages(t.Context(), outsider, conv, "", 20); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("outsider ListMessages err = %v, want ErrConversationNotFound", err)
	}
	if err := e.svc.MarkRead(t.Context(), outsider, conv); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("outsider MarkRead err = %v", err)
	}
	if err := e.svc.DeleteConversation(t.Context(), outsider, conv); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("outsider DeleteConversation err = %v", err)
	}
	if _, err := e.svc.ListMessages(t.Context(), a, sharedomain.NewID(), "", 20); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("unknown conversation err = %v", err)
	}
	// 外人的会话列表里没有它，双方各自能读。
	if items := e.allConversations(t, outsider, 20); len(items) != 0 {
		t.Errorf("outsider conversations = %d, want 0", len(items))
	}
	for _, user := range []uuid.UUID{a, b} {
		if msgs := e.allMessages(t, user, conv.String(), 20); len(msgs) != 1 || msgs[0].Content != "secret" {
			t.Errorf("member %s messages = %+v", user, msgs)
		}
	}
}

func TestMarkRead(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]
	e.send(t, a, b, "1")
	last := e.send(t, a, b, "2")

	if m := e.member(t, last.ConversationID, b); m.UnreadCount != 2 || m.LastReadMessageID != nil {
		t.Fatalf("before read: unread=%d last_read=%v", m.UnreadCount, m.LastReadMessageID)
	}
	if err := e.svc.MarkRead(t.Context(), b, uuid.MustParse(last.ConversationID)); err != nil {
		t.Fatal(err)
	}
	m := e.member(t, last.ConversationID, b)
	if m.UnreadCount != 0 || m.LastReadMessageID == nil || m.LastReadMessageID.String() != last.ID {
		t.Errorf("after read: unread=%d last_read=%v, want 0 / %s", m.UnreadCount, m.LastReadMessageID, last.ID)
	}
	page, err := e.svc.ListMessages(t.Context(), b, uuid.MustParse(last.ConversationID), "", 20)
	if err != nil || page.LastReadMessageID != last.ID {
		t.Errorf("history last_read = %q (err %v), want %s", page.LastReadMessageID, err, last.ID)
	}
	// 再来一条：未读从 0 开始累加，发送方自己的未读不变。
	e.send(t, a, b, "3")
	if m := e.member(t, last.ConversationID, b); m.UnreadCount != 1 {
		t.Errorf("unread after new message = %d, want 1", m.UnreadCount)
	}
	if m := e.member(t, last.ConversationID, a); m.UnreadCount != 0 || m.SentCount != 3 {
		t.Errorf("sender unread=%d sent=%d, want 0 / 3", m.UnreadCount, m.SentCount)
	}
}

func TestDeleteConversation_HiddenThenReappears(t *testing.T) {
	t.Parallel()
	e := newMsgEnv(t)
	u := e.newUsers(t, 2)
	a, b := u[0], u[1]
	e.send(t, a, b, "old 1")
	old := e.send(t, b, a, "old 2")
	conv := uuid.MustParse(old.ConversationID)

	if err := e.svc.DeleteConversation(t.Context(), b, conv); err != nil {
		t.Fatal(err)
	}
	// b 的列表里没有了、历史为空；a 不受影响。
	if items := e.allConversations(t, b, 20); len(items) != 0 {
		t.Fatalf("b conversations after delete = %d, want 0", len(items))
	}
	if msgs := e.allMessages(t, b, conv.String(), 20); len(msgs) != 0 {
		t.Fatalf("b messages after delete = %d, want 0", len(msgs))
	}
	if m := e.member(t, conv.String(), b); m.UnreadCount != 0 || m.ClearedMessageID == nil || m.ClearedMessageID.String() != old.ID {
		t.Errorf("b member after delete: unread=%d cleared=%v", m.UnreadCount, m.ClearedMessageID)
	}
	if items := e.allConversations(t, a, 20); len(items) != 1 {
		t.Fatalf("a conversations = %d, want 1", len(items))
	}
	if msgs := e.allMessages(t, a, conv.String(), 20); len(msgs) != 2 {
		t.Fatalf("a messages = %d, want 2", len(msgs))
	}
	// 重复删除幂等。
	if err := e.svc.DeleteConversation(t.Context(), b, conv); err != nil {
		t.Fatal(err)
	}

	// 对方再发消息：会话重新出现，只显示删除之后的消息。
	fresh := e.send(t, a, b, "new 1")
	if fresh.ConversationID != conv.String() {
		t.Fatalf("new message went to conversation %s, want %s", fresh.ConversationID, conv)
	}
	items := e.allConversations(t, b, 20)
	if len(items) != 1 || items[0].ConversationID != conv.String() || items[0].UnreadCount != 1 || items[0].LastMessage.ID != fresh.ID {
		t.Fatalf("b conversations after reappear = %+v", items)
	}
	msgs := e.allMessages(t, b, conv.String(), 1)
	if len(msgs) != 1 || msgs[0].ID != fresh.ID {
		t.Fatalf("b messages after reappear = %+v, want only %s", msgs, fresh.ID)
	}
	if msgs := e.allMessages(t, a, conv.String(), 20); len(msgs) != 3 {
		t.Errorf("a messages = %d, want 3", len(msgs))
	}

	// 删除方自己再发消息也会让会话重现（同样只显示截止位置之后的消息）。
	if err := e.svc.DeleteConversation(t.Context(), a, conv); err != nil {
		t.Fatal(err)
	}
	mine := e.send(t, a, b, "after a deleted")
	if msgs := e.allMessages(t, a, conv.String(), 20); len(msgs) != 1 || msgs[0].ID != mine.ID {
		t.Errorf("a messages after own delete + send = %+v", msgs)
	}
	if items := e.allConversations(t, a, 20); len(items) != 1 || items[0].UnreadCount != 0 {
		t.Errorf("a conversations after own delete + send = %+v", items)
	}
}
