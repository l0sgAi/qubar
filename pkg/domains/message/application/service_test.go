package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

// fakeRepo 记录入参并返回预置结果。
type fakeRepo struct {
	sendCalls   int
	gotContent  string
	sendErr     error
	entries     []domain.ConversationEntry
	gotAfter    *uuid.UUID
	gotLimit    int
	member      *domain.ConversationMember
	memberErr   error
	msgs        []domain.Message
	gotCleared  *uuid.UUID
	gotBefore   *uuid.UUID
	markReadErr error
	hideErr     error
}

func (f *fakeRepo) SendDirect(_ context.Context, sender, _ uuid.UUID, msgType int16, content string) (*domain.Message, error) {
	f.sendCalls++
	f.gotContent = content
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return &domain.Message{ID: sharedomain.NewID(), ConversationID: sharedomain.NewID(), SenderID: sender, MsgType: msgType, Content: content}, nil
}

func (f *fakeRepo) GetMember(context.Context, uuid.UUID, uuid.UUID) (*domain.ConversationMember, error) {
	return f.member, f.memberErr
}

func (f *fakeRepo) ListConversations(_ context.Context, _ uuid.UUID, after *uuid.UUID, limit, _ int) ([]domain.ConversationEntry, error) {
	f.gotAfter, f.gotLimit = after, limit
	if len(f.entries) > limit {
		return f.entries[:limit], nil
	}
	return f.entries, nil
}

func (f *fakeRepo) ListMessages(_ context.Context, _ uuid.UUID, cleared, before *uuid.UUID, limit int) ([]domain.Message, error) {
	f.gotCleared, f.gotBefore, f.gotLimit = cleared, before, limit
	if len(f.msgs) > limit {
		return f.msgs[:limit], nil
	}
	return f.msgs, nil
}

func (f *fakeRepo) MarkRead(context.Context, uuid.UUID, uuid.UUID) error { return f.markReadErr }
func (f *fakeRepo) Hide(context.Context, uuid.UUID, uuid.UUID) error     { return f.hideErr }

type fakeRecipients struct {
	status RecipientStatus
	err    error
}

func (f fakeRecipients) CheckRecipient(context.Context, uuid.UUID) (RecipientStatus, error) {
	return f.status, f.err
}

type fakeUsers struct {
	briefs map[uuid.UUID]UserBrief
	gotIDs []uuid.UUID
}

func (f *fakeUsers) GetBriefs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]UserBrief, error) {
	f.gotIDs = ids
	return f.briefs, nil
}

func TestSend_ValidationAndRecipient(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	boom := errors.New("boom")
	for _, tc := range []struct {
		name      string
		receiver  uuid.UUID
		content   string
		recipient fakeRecipients
		wantErr   error
		wantSaved string
	}{
		{"self", me, "hi", fakeRecipients{}, domain.ErrCannotMessageSelf, ""},
		{"empty", other, "", fakeRecipients{}, domain.ErrEmptyContent, ""},
		{"blank", other, " \n\t ", fakeRecipients{}, domain.ErrEmptyContent, ""},
		{"only NUL", other, "\x00\x00", fakeRecipients{}, domain.ErrEmptyContent, ""},
		{"too long", other, strings.Repeat("字", MaxContentChars+1), fakeRecipients{}, domain.ErrContentTooLong, ""},
		{"not found", other, "hi", fakeRecipients{status: RecipientNotFound}, domain.ErrRecipientNotFound, ""},
		{"bot", other, "hi", fakeRecipients{status: RecipientBot}, domain.ErrRecipientUnavailable, ""},
		{"checker error", other, "hi", fakeRecipients{err: boom}, boom, ""},
		{"max length ok", other, strings.Repeat("字", MaxContentChars), fakeRecipients{}, nil, strings.Repeat("字", MaxContentChars)},
		{"sanitized, spaces kept", other, " a\x00b ", fakeRecipients{}, nil, " ab "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := NewMessageService(repo, tc.recipient, &fakeUsers{})
			vo, err := svc.Send(t.Context(), me, tc.receiver, tc.content)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if repo.sendCalls != 0 {
					t.Errorf("repo called %d times on error", repo.sendCalls)
				}
				return
			}
			if repo.gotContent != tc.wantSaved || vo.Content != tc.wantSaved || vo.SenderID != me.String() {
				t.Errorf("saved %q / vo %+v, want content %q", repo.gotContent, vo, tc.wantSaved)
			}
		})
	}
}

func TestListConversations_PeerBackfillAndCursor(t *testing.T) {
	me := sharedomain.NewID()
	active, gone := sharedomain.NewID(), sharedomain.NewID()
	entries := make([]domain.ConversationEntry, 3)
	for i := range entries {
		entries[i] = domain.ConversationEntry{
			ConversationID: sharedomain.NewID(), Type: domain.ConversationTypeDirect, PeerID: active,
			UnreadCount: i, LastMessageID: sharedomain.NewID(), LastSenderID: active,
			LastMsgType: domain.MessageTypeText, LastContent: "hello", LastMessageTime: time.Now(),
		}
	}
	entries[1].PeerID = gone
	repo := &fakeRepo{entries: entries}
	users := &fakeUsers{briefs: map[uuid.UUID]UserBrief{active: {ID: active.String(), Username: "alice"}}}
	svc := NewMessageService(repo, fakeRecipients{}, users)

	vo, err := svc.ListConversations(t.Context(), me, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotLimit != 3 || repo.gotAfter != nil {
		t.Errorf("repo limit=%d after=%v, want 3 / nil", repo.gotLimit, repo.gotAfter)
	}
	if len(vo.Items) != 2 || !vo.HasMore || vo.NextCursor == "" {
		t.Fatalf("items=%d has_more=%v cursor=%q", len(vo.Items), vo.HasMore, vo.NextCursor)
	}
	if vo.Items[0].Peer == nil || vo.Items[0].Peer.Username != "alice" {
		t.Errorf("item0 peer = %+v, want alice", vo.Items[0].Peer)
	}
	// 对方已禁用 / 删除：peer 为 null，但 peer_id 与会话仍在。
	if vo.Items[1].Peer != nil || vo.Items[1].PeerID != gone.String() {
		t.Errorf("item1 peer = %+v peer_id=%s, want nil / %s", vo.Items[1].Peer, vo.Items[1].PeerID, gone)
	}
	if vo.Items[1].UnreadCount != 1 || vo.Items[1].LastMessage.Content != "hello" {
		t.Errorf("item1 = %+v", vo.Items[1])
	}

	// 下一页：游标解出第 2 条的 last_message_id。
	if _, err := svc.ListConversations(t.Context(), me, vo.NextCursor, 2); err != nil {
		t.Fatal(err)
	}
	if repo.gotAfter == nil || *repo.gotAfter != entries[1].LastMessageID {
		t.Errorf("after = %v, want %s", repo.gotAfter, entries[1].LastMessageID)
	}

	// 别人的游标 / 消息历史的游标不能用在我的会话列表上。
	other := sharedomain.NewID()
	for _, cursor := range []string{
		encodeCursor(listConversations, other, entries[0].LastMessageID),
		encodeCursor(listMessages, me, entries[0].LastMessageID),
		"not-base64!!",
	} {
		if _, err := svc.ListConversations(t.Context(), me, cursor, 2); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Errorf("cursor %q: err = %v, want ErrInvalidCursor", cursor, err)
		}
	}
}

func TestListMessages_MembershipClearedAndCursor(t *testing.T) {
	me, conv := sharedomain.NewID(), sharedomain.NewID()

	repo := &fakeRepo{memberErr: domain.ErrConversationNotFound}
	svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{})
	if _, err := svc.ListMessages(t.Context(), me, conv, "", 10); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Fatalf("non-member err = %v", err)
	}

	cleared, lastRead := sharedomain.NewID(), sharedomain.NewID()
	msgs := make([]domain.Message, 3)
	for i := range msgs {
		msgs[len(msgs)-1-i] = domain.Message{ID: sharedomain.NewID(), ConversationID: conv, SenderID: me, Content: "m"}
	}
	repo = &fakeRepo{member: &domain.ConversationMember{ClearedMessageID: &cleared, LastReadMessageID: &lastRead}, msgs: msgs}
	svc = NewMessageService(repo, fakeRecipients{}, &fakeUsers{})
	vo, err := svc.ListMessages(t.Context(), me, conv, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotCleared == nil || *repo.gotCleared != cleared || repo.gotLimit != 3 {
		t.Errorf("cleared=%v limit=%d", repo.gotCleared, repo.gotLimit)
	}
	if len(vo.Items) != 2 || !vo.HasMore || vo.LastReadMessageID != lastRead.String() {
		t.Fatalf("vo = %+v", vo)
	}
	if _, err := svc.ListMessages(t.Context(), me, conv, vo.NextCursor, 2); err != nil {
		t.Fatal(err)
	}
	if repo.gotBefore == nil || *repo.gotBefore != msgs[1].ID {
		t.Errorf("before = %v, want %s", repo.gotBefore, msgs[1].ID)
	}
	// 其他会话的游标无效。
	if _, err := svc.ListMessages(t.Context(), me, sharedomain.NewID(), vo.NextCursor, 2); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Errorf("foreign cursor err = %v", err)
	}
}

func TestNormalizePageSize(t *testing.T) {
	for in, want := range map[int]int{-1: DefaultPageSize, 0: DefaultPageSize, 7: 7, MaxPageSize: MaxPageSize, MaxPageSize + 1: MaxPageSize} {
		if got := normalizePageSize(in); got != want {
			t.Errorf("normalizePageSize(%d) = %d, want %d", in, got, want)
		}
	}
}
