package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

// fakeRateLimiter 频率计数桩：按预置结果返回，记录占用与退还。
type fakeRateLimiter struct {
	decision   domain.SendRateDecision
	err        error
	acquires   int
	gotLimits  domain.SendRateLimits
	releases   [][2]bool // 每次 Release 的 (message, newConversation)
	releaseErr error
}

func (f *fakeRateLimiter) Acquire(_ context.Context, _ uuid.UUID, limits domain.SendRateLimits) (domain.SendRateDecision, error) {
	f.acquires++
	f.gotLimits = limits
	return f.decision, f.err
}

func (f *fakeRateLimiter) Release(_ context.Context, _ uuid.UUID, message, newConversation bool) error {
	f.releases = append(f.releases, [2]bool{message, newConversation})
	return f.releaseErr
}

// createdRepo 在 fakeRepo 基础上让 SendDirect 返回指定的 Created。
type createdRepo struct {
	*fakeRepo
	created bool
}

func (r createdRepo) SendDirect(ctx context.Context, s, rcv uuid.UUID, t int16, c string, limit int) (*domain.SendResult, error) {
	res, err := r.fakeRepo.SendDirect(ctx, s, rcv, t, c, limit)
	if res != nil {
		res.Created = r.created
	}
	return res, err
}

// recipientsByID 按用户返回状态（未登记的为 RecipientOK）。
type recipientsByID map[uuid.UUID]RecipientStatus

func (r recipientsByID) CheckRecipient(_ context.Context, id uuid.UUID) (RecipientStatus, error) {
	return r[id], nil
}

var testRateLimits = domain.SendRateLimits{PerMinute: 3, PerDay: 7, NewConversationsPerDay: 2}

func newRateLimitedService(repo domain.Repository, limiter domain.SendRateLimiter) MessageService {
	svc := NewMessageService(repo, fakeRecipients{}, &fakeUsers{}, fakeFollows{followed: true})
	svc.SetSendRateLimiter(limiter, func() domain.SendRateLimits { return testRateLimits })
	return svc
}

func TestSend_RateLimitExceeded(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	for _, tc := range []struct {
		name     string
		exceeded domain.SendRateExceeded
		wantMsg  string
	}{
		{"per minute", domain.SendRatePerMinute, "message rate limit exceeded: at most 3 messages per minute"},
		{"per day", domain.SendRatePerDay, "message rate limit exceeded: at most 7 messages per day"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			limiter := &fakeRateLimiter{decision: domain.SendRateDecision{Exceeded: tc.exceeded}}
			_, err := newRateLimitedService(repo, limiter).Send(t.Context(), me, other, "hi")
			if !errors.Is(err, domain.ErrSendRateLimited) || err.Error() != tc.wantMsg {
				t.Fatalf("err = %v, want %q", err, tc.wantMsg)
			}
			if repo.sendCalls != 0 || repo.directCalls != 0 {
				t.Errorf("repo called (send %d, direct %d) after rate limit", repo.sendCalls, repo.directCalls)
			}
			if len(limiter.releases) != 0 {
				t.Errorf("released %v, want nothing (rejected acquire holds no quota)", limiter.releases)
			}
			if limiter.gotLimits != testRateLimits {
				t.Errorf("limits = %+v, want %+v", limiter.gotLimits, testRateLimits)
			}
		})
	}
}

func TestSend_RateLimitSettlement(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	boom := errors.New("boom")
	for _, tc := range []struct {
		name         string
		reserved     bool
		created      bool
		directErr    error
		sendErr      error
		wantErr      error
		wantSend     int
		wantDirect   int
		wantReleases [][2]bool
	}{
		{name: "new conversation keeps reservation", reserved: true, created: true, wantSend: 1},
		{name: "existing conversation returns reservation", reserved: true, created: false, wantSend: 1,
			wantReleases: [][2]bool{{false, true}}},
		{name: "quota used up, existing conversation", reserved: false, wantSend: 1, wantDirect: 1},
		{name: "quota used up, would create", reserved: false, directErr: domain.ErrConversationNotFound,
			wantErr: domain.ErrNewConversationLimited, wantDirect: 1, wantReleases: [][2]bool{{true, false}}},
		{name: "quota used up, lookup error", reserved: false, directErr: boom,
			wantErr: boom, wantDirect: 1, wantReleases: [][2]bool{{true, false}}},
		{name: "send failed returns everything", reserved: true, sendErr: domain.ErrSendLimitReached,
			wantErr: domain.ErrSendLimitReached, wantSend: 1, wantReleases: [][2]bool{{true, true}}},
		{name: "send failed without reservation", reserved: false, sendErr: boom,
			wantErr: boom, wantSend: 1, wantDirect: 1, wantReleases: [][2]bool{{true, false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &fakeRepo{sendErr: tc.sendErr, directErr: tc.directErr, direct: &domain.ConversationEntry{}}
			limiter := &fakeRateLimiter{decision: domain.SendRateDecision{NewConversationReserved: tc.reserved}}
			vo, err := newRateLimitedService(createdRepo{base, tc.created}, limiter).Send(t.Context(), me, other, "hi")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if errors.Is(tc.wantErr, domain.ErrNewConversationLimited) && !strings.Contains(err.Error(), "at most 2 new conversations per day") {
				t.Errorf("err = %q, want limit in message", err)
			}
			if tc.wantErr == nil && vo == nil {
				t.Fatal("vo is nil on success")
			}
			if base.sendCalls != tc.wantSend || base.directCalls != tc.wantDirect {
				t.Errorf("send calls %d / direct calls %d, want %d / %d", base.sendCalls, base.directCalls, tc.wantSend, tc.wantDirect)
			}
			if tc.wantDirect > 0 && (base.gotPeer != other) {
				t.Errorf("lookup peer = %s, want %s", base.gotPeer, other)
			}
			if !equalReleases(limiter.releases, tc.wantReleases) {
				t.Errorf("releases = %v, want %v", limiter.releases, tc.wantReleases)
			}
		})
	}
}

func TestSend_RateLimiterError(t *testing.T) {
	repo := &fakeRepo{}
	boom := errors.New("redis down")
	limiter := &fakeRateLimiter{err: boom}
	_, err := newRateLimitedService(repo, limiter).Send(t.Context(), sharedomain.NewID(), sharedomain.NewID(), "hi")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped %v (fail closed)", err, boom)
	}
	if repo.sendCalls != 0 {
		t.Errorf("repo called %d times when limiter failed", repo.sendCalls)
	}
}

func TestSend_ReleaseErrorDoesNotFailSend(t *testing.T) {
	limiter := &fakeRateLimiter{decision: domain.SendRateDecision{NewConversationReserved: true}, releaseErr: errors.New("redis down")}
	vo, err := newRateLimitedService(createdRepo{&fakeRepo{}, false}, limiter).
		Send(t.Context(), sharedomain.NewID(), sharedomain.NewID(), "hi")
	if err != nil || vo == nil {
		t.Fatalf("Send = %v, %v; want success even if release fails", vo, err)
	}
}

func TestSend_SenderUnavailable(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	for _, tc := range []struct {
		name    string
		status  RecipientStatus
		wantErr error
	}{
		{"disabled or deleted", RecipientNotFound, domain.ErrSenderUnavailable},
		{"bot sender unchanged", RecipientBot, nil},
		{"active", RecipientOK, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			limiter := &fakeRateLimiter{decision: domain.SendRateDecision{NewConversationReserved: true}}
			svc := NewMessageService(repo, recipientsByID{me: tc.status}, &fakeUsers{}, fakeFollows{followed: true})
			svc.SetSendRateLimiter(limiter, func() domain.SendRateLimits { return testRateLimits })
			_, err := svc.Send(t.Context(), me, other, "hi")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && (repo.sendCalls != 0 || limiter.acquires != 0) {
				t.Errorf("send %d / acquire %d after sender rejected, want 0 / 0", repo.sendCalls, limiter.acquires)
			}
		})
	}
}

func equalReleases(got, want [][2]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// 被频率限制拒绝的发送不改未读缓存、不推送（#241 钩子只在 SendDirect 成功后）。
func TestSend_RateLimitedDoesNotNotify(t *testing.T) {
	me, other := sharedomain.NewID(), sharedomain.NewID()
	for _, d := range []domain.SendRateDecision{
		{Exceeded: domain.SendRatePerMinute},
		{Exceeded: domain.SendRateOK, NewConversationReserved: false},
	} {
		repo := &fakeRepo{directErr: domain.ErrConversationNotFound}
		svc := newRateLimitedService(repo, &fakeRateLimiter{decision: d})
		cache, n := newFakeCache(), &fakeNotifier{}
		svc.SetUnreadCache(cache)
		svc.SetUnreadNotifier(n)
		if _, err := svc.Send(t.Context(), me, other, "hi"); err == nil {
			t.Fatalf("decision %+v: send succeeded, want 429", d)
		}
		if len(cache.incrs) != 0 || len(n.changed) != 0 || len(n.msgTo) != 0 {
			t.Errorf("decision %+v: incrs %v / changed %v / pushed %v, want none", d, cache.incrs, n.changed, n.msgTo)
		}
	}
}
