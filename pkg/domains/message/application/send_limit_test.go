package application

import (
	"encoding/json"
	"errors"
	"testing"

	"interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

func TestSend_UnfollowedLimit(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	boom := errors.New("boom")
	three := 3
	for _, tc := range []struct {
		name          string
		follows       fakeFollows
		repoRemaining *int
		repoErr       error
		wantLimit     int
		wantErr       error
		wantRemaining *int
		wantRepoCalls int
	}{
		{"peer follows me: no limit", fakeFollows{followed: true}, nil, nil, 0, nil, nil, 1},
		{"not followed: limit passed, remaining returned", fakeFollows{}, &three, nil, UnfollowedSendLimit, nil, &three, 1},
		{"not followed but peer replied: repo returns nil remaining", fakeFollows{}, nil, nil, UnfollowedSendLimit, nil, nil, 1},
		{"limit reached", fakeFollows{}, nil, domain.ErrSendLimitReached, UnfollowedSendLimit, domain.ErrSendLimitReached, nil, 1},
		{"follow checker error", fakeFollows{err: boom}, nil, nil, 0, boom, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pair [2]uuid.UUID
			tc.follows.gotPair = &pair
			repo := &fakeRepo{remaining: tc.repoRemaining, sendErr: tc.repoErr}
			svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, tc.follows)
			vo, err := svc.Send(t.Context(), me, other, "hi")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if repo.sendCalls != tc.wantRepoCalls {
				t.Fatalf("repo calls = %d, want %d", repo.sendCalls, tc.wantRepoCalls)
			}
			// 查的是「接收方是否关注了发送方」。
			if pair != [2]uuid.UUID{other, me} {
				t.Errorf("IsFollowing(%s, %s), want (other, me)", pair[0], pair[1])
			}
			if tc.wantErr != nil {
				return
			}
			if repo.gotSendLim != tc.wantLimit {
				t.Errorf("limit = %d, want %d", repo.gotSendLim, tc.wantLimit)
			}
			switch {
			case tc.wantRemaining == nil && vo.RemainingCount != nil:
				t.Errorf("remaining = %d, want nil", *vo.RemainingCount)
			case tc.wantRemaining != nil && (vo.RemainingCount == nil || *vo.RemainingCount != *tc.wantRemaining):
				t.Errorf("remaining = %v, want %d", vo.RemainingCount, *tc.wantRemaining)
			}
		})
	}
}

// 发送响应：消息字段平铺在顶层（与 #238 一致），remaining_count 无限制时为 null。
func TestSendVO_JSON(t *testing.T) {
	two := 2
	for _, tc := range []struct {
		remaining *int
		want      string
	}{
		{nil, "null"},
		{&two, "2"},
	} {
		b, err := json.Marshal(SendVO{MessageVO: MessageVO{ID: "m1", ConversationID: "c1"}, RemainingCount: tc.remaining})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if string(got["id"]) != `"m1"` || string(got["conversation_id"]) != `"c1"` {
			t.Errorf("message fields not flattened: %s", b)
		}
		if raw, ok := got["remaining_count"]; !ok || string(raw) != tc.want {
			t.Errorf("remaining_count = %s (present=%v), want %s", raw, ok, tc.want)
		}
	}
}
