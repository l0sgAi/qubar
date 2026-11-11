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
	writeCalls  int
	direct      *domain.ConversationEntry
	directErr   error
	directCalls int
	gotPeer     uuid.UUID
	gotPreview  int
	gotSendLim  int
	remaining   *int
	// limitState / limitCalls：GetSendLimitState（#240 can_send，见 permission_test.go）。
	limitState domain.SendLimitState
	limitErr   error
	limitCalls int
}

func (f *fakeRepo) SendDirect(_ context.Context, sender, _ uuid.UUID, msgType int16, content string, limit int) (*domain.SendResult, error) {
	f.sendCalls++
	f.gotContent = content
	f.gotSendLim = limit
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	msg := &domain.Message{ID: sharedomain.NewID(), ConversationID: sharedomain.NewID(), SenderID: sender, MsgType: msgType, Content: content}
	return &domain.SendResult{Message: msg, Remaining: f.remaining}, nil
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

func (f *fakeRepo) GetDirectConversation(_ context.Context, _, peer uuid.UUID, preview int) (*domain.ConversationEntry, error) {
	f.directCalls++
	f.gotPeer, f.gotPreview = peer, preview
	return f.direct, f.directErr
}

func (f *fakeRepo) MarkRead(context.Context, uuid.UUID, uuid.UUID) error {
	f.writeCalls++
	return f.markReadErr
}

func (f *fakeRepo) Hide(context.Context, uuid.UUID, uuid.UUID) error {
	f.writeCalls++
	return f.hideErr
}

type fakeRecipients struct {
	status RecipientStatus
	err    error
}

func (f fakeRecipients) CheckRecipient(context.Context, uuid.UUID) (RecipientStatus, error) {
	return f.status, f.err
}

// fakeFollows 关注关系桩：followed 为 true 时任何人都关注了任何人。
type fakeFollows struct {
	followed bool
	err      error
	gotPair  *[2]uuid.UUID
}

func (f fakeFollows) IsFollowing(_ context.Context, follower, followee uuid.UUID) (bool, error) {
	if f.gotPair != nil {
		*f.gotPair = [2]uuid.UUID{follower, followee}
	}
	return f.followed, f.err
}

type fakeUsers struct {
	briefs map[uuid.UUID]UserBrief
	gotIDs []uuid.UUID
	calls  int
}

func (f *fakeUsers) GetBriefs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]UserBrief, error) {
	f.calls++
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
			svc := NewMessageService(repo, tc.recipient, &fakeUsers{}, fakeFollows{})
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
	svc := NewMessageService(repo, fakeRecipients{}, users, fakeFollows{})

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
	svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{})
	if _, err := svc.ListMessages(t.Context(), me, conv, "", 10); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Fatalf("non-member err = %v", err)
	}

	cleared, lastRead := sharedomain.NewID(), sharedomain.NewID()
	msgs := make([]domain.Message, 3)
	for i := range msgs {
		msgs[len(msgs)-1-i] = domain.Message{ID: sharedomain.NewID(), ConversationID: conv, SenderID: me, Content: "m"}
	}
	repo = &fakeRepo{member: &domain.ConversationMember{ClearedMessageID: &cleared, LastReadMessageID: &lastRead}, msgs: msgs}
	svc = NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{})
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

func TestGetConversationWithUser(t *testing.T) {
	me, peer := sharedomain.NewID(), sharedomain.NewID()
	conv, last := sharedomain.NewID(), sharedomain.NewID()
	visible := domain.ConversationEntry{
		ConversationID: conv, Type: domain.ConversationTypeDirect, PeerID: peer, UnreadCount: 3,
		LastMessageID: last, LastSenderID: peer, LastMsgType: domain.MessageTypeText, LastContent: "hello", LastMessageTime: time.Now(),
	}
	// 我删除过会话、之后没有新消息：仓储不返回最后一条消息，未读已在删除时清零。
	hidden := domain.ConversationEntry{ConversationID: conv, Type: domain.ConversationTypeDirect, PeerID: peer}
	alice := map[uuid.UUID]UserBrief{peer: {ID: peer.String(), Username: "alice"}}
	boom := errors.New("boom")

	for _, tc := range []struct {
		name      string
		peer      uuid.UUID
		repo      *fakeRepo
		briefs    map[uuid.UUID]UserBrief
		wantErr   error
		wantConv  bool
		wantLast  bool
		wantPeer  bool
		wantRepo  int
		wantUsers int
	}{
		{name: "has conversation", peer: peer, repo: &fakeRepo{direct: &visible}, briefs: alice,
			wantConv: true, wantLast: true, wantPeer: true, wantRepo: 1, wantUsers: 1},
		{name: "no conversation", peer: peer, repo: &fakeRepo{directErr: domain.ErrConversationNotFound}, briefs: alice,
			wantRepo: 1},
		{name: "self", peer: me, repo: &fakeRepo{direct: &visible}, wantErr: domain.ErrConversationWithSelf},
		{name: "hidden", peer: peer, repo: &fakeRepo{direct: &hidden}, briefs: alice,
			wantConv: true, wantPeer: true, wantRepo: 1, wantUsers: 1},
		{name: "peer gone", peer: peer, repo: &fakeRepo{direct: &visible},
			wantConv: true, wantLast: true, wantRepo: 1, wantUsers: 1},
		{name: "repo error", peer: peer, repo: &fakeRepo{directErr: boom}, wantErr: boom, wantRepo: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &fakeUsers{briefs: tc.briefs}
			vo, err := NewMessageService(tc.repo, fakeRecipients{}, users, fakeFollows{}).GetConversationWithUser(t.Context(), me, tc.peer)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.repo.directCalls != tc.wantRepo || users.calls != tc.wantUsers || tc.repo.writeCalls != 0 {
				t.Errorf("repo calls=%d users calls=%d writes=%d, want %d / %d / 0",
					tc.repo.directCalls, users.calls, tc.repo.writeCalls, tc.wantRepo, tc.wantUsers)
			}
			if tc.wantRepo > 0 && (tc.repo.gotPeer != tc.peer || tc.repo.gotPreview != PreviewChars) {
				t.Errorf("repo got peer=%s preview=%d", tc.repo.gotPeer, tc.repo.gotPreview)
			}
			if tc.wantErr != nil {
				return
			}
			item := vo.Conversation
			if (item != nil) != tc.wantConv {
				t.Fatalf("conversation = %+v, want present=%v", item, tc.wantConv)
			}
			if item == nil {
				return
			}
			if item.ConversationID != conv.String() || item.PeerID != peer.String() || item.Type != domain.ConversationTypeDirect {
				t.Errorf("item = %+v", item)
			}
			if (item.Peer != nil) != tc.wantPeer || (item.LastMessage != nil) != tc.wantLast {
				t.Errorf("peer = %+v last = %+v, want peer=%v last=%v", item.Peer, item.LastMessage, tc.wantPeer, tc.wantLast)
			}
			if tc.wantLast && (item.LastMessage.ID != last.String() || item.LastMessage.Content != "hello" || item.UnreadCount != 3) {
				t.Errorf("last = %+v unread = %d", item.LastMessage, item.UnreadCount)
			}
			if !tc.wantLast && item.UnreadCount != 0 {
				t.Errorf("hidden unread = %d, want 0", item.UnreadCount)
			}
		})
	}
}

func TestNormalizePageSize(t *testing.T) {
	for in, want := range map[int]int{-1: DefaultPageSize, 0: DefaultPageSize, 7: 7, MaxPageSize: MaxPageSize, MaxPageSize + 1: MaxPageSize} {
		if got := normalizePageSize(in); got != want {
			t.Errorf("normalizePageSize(%d) = %d, want %d", in, got, want)
		}
	}
}
