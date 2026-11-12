package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

// HasClientMsgID 让 fakeRepo 满足 domain.Repository：预置了重放消息即视为 client_msg_id 已发过。
func (f *fakeRepo) HasClientMsgID(context.Context, uuid.UUID, string) (bool, error) {
	return f.replay != nil, nil
}

// replayLookupRepo 记录 HasClientMsgID 调用，可预置查询错误。
type replayLookupRepo struct {
	*fakeRepo
	lookupErr error
	lookups   []string
}

func (r *replayLookupRepo) HasClientMsgID(ctx context.Context, sender uuid.UUID, key string) (bool, error) {
	r.lookups = append(r.lookups, key)
	if r.lookupErr != nil {
		return false, r.lookupErr
	}
	return r.fakeRepo.HasClientMsgID(ctx, sender, key)
}

// 幂等重放（#248）占用配额后由 SendDirect 判定为重放：全部退还，不推送，返回原消息。
func TestSendWithClientMsgID_ReplayRefundsRateQuota(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	orig := &domain.Message{ID: sharedomain.NewID(), ConversationID: sharedomain.NewID(), SenderID: me, Content: "first"}
	for _, tc := range []struct {
		name         string
		reserved     bool
		wantReleases [][2]bool
	}{
		{"with new conversation reservation", true, [][2]bool{{true, true}}},
		{"without reservation", false, [][2]bool{{true, false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &replayLookupRepo{fakeRepo: &fakeRepo{replay: orig, direct: &domain.ConversationEntry{}}}
			limiter := &fakeRateLimiter{decision: domain.SendRateDecision{NewConversationReserved: tc.reserved}}
			svc := newRateLimitedService(repo, limiter)
			n := &fakeNotifier{}
			svc.SetUnreadNotifier(n)
			vo, err := svc.SendWithClientMsgID(t.Context(), me, other, "again", "k1")
			if err != nil || vo.ID != orig.ID.String() {
				t.Fatalf("vo = %+v err = %v, want original message", vo, err)
			}
			if !equalReleases(limiter.releases, tc.wantReleases) {
				t.Errorf("releases = %v, want %v", limiter.releases, tc.wantReleases)
			}
			if len(repo.lookups) != 0 {
				t.Errorf("lookups = %v, want none when acquire succeeded", repo.lookups)
			}
			if len(n.msgTo) != 0 || len(n.changed) != 0 {
				t.Errorf("replay pushed %v / %v, want nothing", n.msgTo, n.changed)
			}
		})
	}
}

// 已达频率上限时重试（client_msg_id 已发过）不被拒绝：放行交给 SendDirect 返回原消息，不占也不退配额。
// client_msg_id 没发过、不带 client_msg_id、或 Redis 出错时仍拒绝。
func TestSendWithClientMsgID_RateLimitedReplayPasses(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	orig := &domain.Message{ID: sharedomain.NewID(), ConversationID: sharedomain.NewID(), SenderID: me, Content: "first"}
	perMinute := domain.SendRateDecision{Exceeded: domain.SendRatePerMinute}
	newConvFull := domain.SendRateDecision{NewConversationReserved: false}
	redisDown := errors.New("redis down")
	lookupDown := errors.New("db down")
	for _, tc := range []struct {
		name        string
		decision    domain.SendRateDecision
		limiterErr  error
		lookupErr   error
		replay      *domain.Message
		key         string
		wantErr     error
		wantLookups int
		wantSend    int
	}{
		{name: "per minute, replay passes", decision: perMinute, replay: orig, key: "k1", wantLookups: 1, wantSend: 1},
		{name: "new conversation full, replay passes", decision: newConvFull, replay: orig, key: "k1", wantLookups: 1, wantSend: 1},
		{name: "per minute, new key rejected", decision: perMinute, key: "k2", wantErr: domain.ErrSendRateLimited, wantLookups: 1},
		{name: "new conversation full, new key rejected", decision: newConvFull, key: "k2",
			wantErr: domain.ErrNewConversationLimited, wantLookups: 1},
		{name: "per minute, no key rejected without lookup", decision: perMinute, replay: orig, wantErr: domain.ErrSendRateLimited},
		{name: "redis error not bypassed", limiterErr: redisDown, replay: orig, key: "k1", wantErr: redisDown},
		{name: "lookup error keeps rejection", decision: perMinute, lookupErr: lookupDown, replay: orig, key: "k1",
			wantErr: domain.ErrSendRateLimited, wantLookups: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &replayLookupRepo{
				fakeRepo:  &fakeRepo{replay: tc.replay, directErr: domain.ErrConversationNotFound},
				lookupErr: tc.lookupErr,
			}
			limiter := &fakeRateLimiter{decision: tc.decision, err: tc.limiterErr}
			vo, err := newRateLimitedService(repo, limiter).SendWithClientMsgID(t.Context(), me, other, "again", tc.key)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && vo.ID != orig.ID.String() {
				t.Errorf("vo id = %s, want original %s", vo.ID, orig.ID)
			}
			if len(repo.lookups) != tc.wantLookups || repo.sendCalls != tc.wantSend {
				t.Errorf("lookups %v / send calls %d, want %d / %d", repo.lookups, repo.sendCalls, tc.wantLookups, tc.wantSend)
			}
			if tc.wantLookups > 0 && repo.lookups[0] != tc.key {
				t.Errorf("lookup key = %q, want %q", repo.lookups[0], tc.key)
			}
			// 被拒时没有占用配额（新会话名额已满时 acquire 自己退还了本条额度），放行的重放不再退还。
			want := [][2]bool(nil)
			if tc.decision == newConvFull && tc.limiterErr == nil {
				want = [][2]bool{{true, false}}
			}
			if !equalReleases(limiter.releases, want) {
				t.Errorf("releases = %v, want %v", limiter.releases, want)
			}
		})
	}
}
