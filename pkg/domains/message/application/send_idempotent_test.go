package application

import (
	"errors"
	"strings"
	"testing"

	"interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"
)

// client_msg_id 校验（#248）：空串 = 不提供（传 nil 给仓储）；1–64 个 [A-Za-z0-9_-]；非法时不调仓储。
func TestSendWithClientMsgID_Validation(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	uuidKey := sharedomain.NewID().String()
	for _, tc := range []struct {
		name    string
		id      string
		wantErr error
		wantKey *string
	}{
		{"absent", "", nil, nil},
		{"uuid", uuidKey, nil, &uuidKey},
		{"letters digits dash underscore", "aZ09-_", nil, strPtr("aZ09-_")},
		{"max length", strings.Repeat("a", MaxClientMsgIDLen), nil, strPtr(strings.Repeat("a", MaxClientMsgIDLen))},
		{"too long", strings.Repeat("a", MaxClientMsgIDLen+1), domain.ErrInvalidClientMsgID, nil},
		{"space", "a b", domain.ErrInvalidClientMsgID, nil},
		{"dot", "a.b", domain.ErrInvalidClientMsgID, nil},
		{"NUL", "a\x00", domain.ErrInvalidClientMsgID, nil},
		{"non-ascii", "消息1", domain.ErrInvalidClientMsgID, nil},
		{"only blank", " ", domain.ErrInvalidClientMsgID, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{followed: true})
			_, err := svc.SendWithClientMsgID(t.Context(), me, other, "hi", tc.id)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if repo.sendCalls != 0 {
					t.Errorf("repo called %d times on invalid client_msg_id", repo.sendCalls)
				}
				return
			}
			switch {
			case tc.wantKey == nil && repo.gotClientID != nil:
				t.Errorf("client_msg_id passed = %q, want nil", *repo.gotClientID)
			case tc.wantKey != nil && (repo.gotClientID == nil || *repo.gotClientID != *tc.wantKey):
				t.Errorf("client_msg_id passed = %v, want %q", repo.gotClientID, *tc.wantKey)
			}
		})
	}
}

// Send（不带幂等键）等同 SendWithClientMsgID(..., "")：仓储收到 nil。
func TestSend_NoClientMsgID(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{followed: true})
	if _, err := svc.Send(t.Context(), sharedomain.NewID(), sharedomain.NewID(), "hi"); err != nil {
		t.Fatal(err)
	}
	if repo.gotClientID != nil {
		t.Errorf("client_msg_id passed = %q, want nil", *repo.gotClientID)
	}
}

// 重放：响应是仓储返回的原消息（原正文），remaining_count 透传仓储重新计算的值。
func TestSendWithClientMsgID_Replay(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	orig := &domain.Message{
		ID: sharedomain.NewID(), ConversationID: sharedomain.NewID(), SenderID: me,
		MsgType: domain.MessageTypeText, Content: "first", ClientMsgID: strPtr("k1"),
	}
	zero := 0
	repo := &fakeRepo{replay: orig, remaining: &zero}
	svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{})
	vo, err := svc.SendWithClientMsgID(t.Context(), me, other, "second", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if vo.ID != orig.ID.String() || vo.ConversationID != orig.ConversationID.String() || vo.Content != "first" {
		t.Errorf("vo = %+v, want original message", vo)
	}
	if vo.RemainingCount == nil || *vo.RemainingCount != 0 {
		t.Errorf("remaining = %v, want 0", vo.RemainingCount)
	}
}

func strPtr(s string) *string { return &s }

// 重放不触发发送后的钩子（#241）：不自增接收方未读缓存、不推 dm-unread / dm-message；正常发送照常触发。
func TestSendWithClientMsgID_ReplayDoesNotNotify(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	orig := &domain.Message{ID: sharedomain.NewID(), ConversationID: sharedomain.NewID(), SenderID: me, Content: "first"}
	repo := &fakeRepo{replay: orig}
	svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{followed: true})
	cache, notifier := newFakeCache(), &fakeNotifier{}
	svc.SetUnreadCache(cache)
	svc.SetUnreadNotifier(notifier)

	if _, err := svc.SendWithClientMsgID(t.Context(), me, other, "again", "k1"); err != nil {
		t.Fatal(err)
	}
	if len(cache.incrs) != 0 || len(notifier.changed) != 0 || len(notifier.msgs) != 0 {
		t.Fatalf("replay triggered side effects: incrs=%v changed=%v msgs=%v", cache.incrs, notifier.changed, notifier.msgs)
	}

	repo.replay = nil
	if _, err := svc.SendWithClientMsgID(t.Context(), me, other, "new", "k2"); err != nil {
		t.Fatal(err)
	}
	if len(cache.incrs) != 1 || len(notifier.changed) != 1 || len(notifier.msgs) != 1 {
		t.Fatalf("new send side effects: incrs=%v changed=%v msgs=%v", cache.incrs, notifier.changed, notifier.msgs)
	}
}
