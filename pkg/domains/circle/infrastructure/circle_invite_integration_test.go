//go:build integration

// circle_invite_integration_test.go 邀请链接对开发栈 PostgreSQL + Redis 的集成测试（#169）：
// 条件 UPDATE 并发不超用、过期 / 撤销 / 用尽不可消耗、批量撤销；
// 经 service 接受邀请：私密圈新建 / 已退出回圈与直接加入副作用一致、已是成员不消耗、拉黑拒绝、
// 并发接受只占满 max_uses、同一用户并发重复接受只计一次、私密圈改公开后链接失效。
package infrastructure_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/domains/circle/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// newLinkRow 直接写一条链接（mut 可改过期 / 次数 / 撤销），注册清理，返回链接。
func newLinkRow(t *testing.T, circleID uuid.UUID, mut func(*domain.CircleInviteLink)) *domain.CircleInviteLink {
	t.Helper()
	db := testkit.PG(t)
	id := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.circle_invite_link", "id", id)
	l := &domain.CircleInviteLink{
		ID: id, CircleID: circleID, CreatorID: sharedomain.NewID(),
		TokenHash: domain.HashInviteToken("it-" + id.String()), ExpireTime: time.Now().Add(time.Hour),
	}
	if mut != nil {
		mut(l)
	}
	if err := infrastructure.NewInviteLinkRepository(db).Create(t.Context(), l); err != nil {
		t.Fatalf("create link: %v", err)
	}
	// revoked=0 / used_count=0 是零值会走列默认值；非零值随 INSERT 写入。
	return l
}

func intPtr(v int) *int { return &v }

func usedCount(t *testing.T, id uuid.UUID) int {
	t.Helper()
	l, err := infrastructure.NewInviteLinkRepository(testkit.PG(t)).GetByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get link: %v", err)
	}
	return l.UsedCount
}

// TestInviteLinkRepo_ConsumeUseConcurrentNeverOverflows 20 个并发消耗 max_uses=5 的链接：恰好 5 个成功。
func TestInviteLinkRepo_ConsumeUseConcurrentNeverOverflows(t *testing.T) {
	t.Parallel()
	repo := infrastructure.NewInviteLinkRepository(testkit.PG(t))
	l := newLinkRow(t, sharedomain.NewID(), func(l *domain.CircleInviteLink) { l.MaxUses = intPtr(5) })

	var ok, unusable atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := repo.ConsumeUse(context.WithoutCancel(t.Context()), l.ID); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, domain.ErrInviteLinkUnusable):
				unusable.Add(1)
			default:
				t.Errorf("unexpected err %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 5 || unusable.Load() != 15 || usedCount(t, l.ID) != 5 {
		t.Errorf("ok=%d unusable=%d used=%d, want 5 / 15 / 5", ok.Load(), unusable.Load(), usedCount(t, l.ID))
	}
}

// TestInviteLinkRepo_ConsumeUseRejects 过期 / 撤销 / 用尽不可消耗；不限次数可一直消耗；归还不减到负数。
func TestInviteLinkRepo_ConsumeUseRejects(t *testing.T) {
	t.Parallel()
	repo := infrastructure.NewInviteLinkRepository(testkit.PG(t))
	ctx := t.Context()
	circleID := sharedomain.NewID()

	for name, mut := range map[string]func(*domain.CircleInviteLink){
		"expired": func(l *domain.CircleInviteLink) { l.ExpireTime = time.Now().Add(-time.Second) },
		"revoked": func(l *domain.CircleInviteLink) { l.Revoked = 1 },
		"used up": func(l *domain.CircleInviteLink) { l.MaxUses, l.UsedCount = intPtr(2), 2 },
	} {
		l := newLinkRow(t, circleID, mut)
		if err := repo.ConsumeUse(ctx, l.ID); !errors.Is(err, domain.ErrInviteLinkUnusable) {
			t.Errorf("%s: err = %v, want unusable", name, err)
		}
	}

	unlimited := newLinkRow(t, circleID, nil)
	for range 3 {
		if err := repo.ConsumeUse(ctx, unlimited.ID); err != nil {
			t.Fatalf("unlimited consume: %v", err)
		}
	}
	for range 5 {
		if err := repo.ReleaseUse(ctx, unlimited.ID); err != nil {
			t.Fatalf("release: %v", err)
		}
	}
	if got := usedCount(t, unlimited.ID); got != 0 {
		t.Errorf("used after over-release = %d, want 0", got)
	}

	got, err := repo.GetByTokenHash(ctx, unlimited.TokenHash)
	if err != nil || got.ID != unlimited.ID {
		t.Errorf("GetByTokenHash = %v, %v", got, err)
	}
	if _, err := repo.GetByTokenHash(ctx, domain.HashInviteToken("missing")); !errors.Is(err, domain.ErrInviteLinkNotFound) {
		t.Errorf("missing hash err = %v, want not found", err)
	}
}

func TestInviteLinkRepo_RevokeByCircle(t *testing.T) {
	t.Parallel()
	repo := infrastructure.NewInviteLinkRepository(testkit.PG(t))
	ctx := t.Context()
	circleID, other := sharedomain.NewID(), sharedomain.NewID()
	a, b := newLinkRow(t, circleID, nil), newLinkRow(t, circleID, func(l *domain.CircleInviteLink) { l.Revoked = 1 })
	keep := newLinkRow(t, other, nil)
	op := sharedomain.NewID()

	n, err := repo.RevokeByCircle(ctx, circleID, op)
	if err != nil || n != 1 {
		t.Fatalf("RevokeByCircle = %d, %v; want 1 (already revoked skipped)", n, err)
	}
	for _, l := range []*domain.CircleInviteLink{a, b} {
		got, _ := repo.GetByID(ctx, l.ID)
		if got.Revoked != 1 {
			t.Errorf("link %s not revoked", l.ID)
		}
	}
	if got, _ := repo.GetByID(ctx, a.ID); got.UpdateBy == nil || *got.UpdateBy != op {
		t.Errorf("update_by = %v, want operator", got.UpdateBy)
	}
	if got, _ := repo.GetByID(ctx, keep.ID); got.Revoked != 0 {
		t.Error("other circle's link revoked")
	}
	links, total, err := repo.ListByCircle(ctx, circleID, 0, 10)
	if err != nil || total != 2 || len(links) != 2 {
		t.Errorf("ListByCircle = %d rows / total %d, %v; want 2 / 2", len(links), total, err)
	}
}

// inviteFixture 私密圈 + 注入真实邀请仓储的 service；createLink 经 service 创建并注册清理。
type inviteFixture struct {
	*joinFixture
}

func newInviteFixture(t *testing.T) *inviteFixture {
	t.Helper()
	f := &inviteFixture{newJoinFixture(t, domain.CircleJoinTypePrivate)}
	f.svc.SetInviteLinkRepository(infrastructure.NewInviteLinkRepository(testkit.PG(t)))
	testkit.CleanupRows(t, "domains.circle_invite_link", "circle_id", f.circleID)
	return f
}

func (f *inviteFixture) createLink(t *testing.T, maxUses *int) *application.InviteLinkCreated {
	t.Helper()
	got, err := f.svc.CreateInviteLink(t.Context(), f.ownerID, f.circleID, application.CreateInviteLinkInput{MaxUses: maxUses})
	if err != nil {
		t.Fatalf("CreateInviteLink: %v", err)
	}
	return got
}

// TestAcceptInvite_JoinsPrivateCircle 无记录 / 已退出经邀请进入私密圈：状态 1、计数 +1、事件 +1、ZSET 写入、消耗一次；
// 已是成员（正常 / 禁言）幂等成功不消耗；拉黑拒绝不消耗。直接加入仍被拒。
func TestAcceptInvite_JoinsPrivateCircle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		from    int16
		wantErr func(error) bool
		already bool
		want    int16
	}{
		{"new", noRecord, nil, false, domain.MemberStatusNormal},
		{"left rejoins", domain.MemberStatusLeft, nil, false, domain.MemberStatusNormal},
		{"normal is idempotent", domain.MemberStatusNormal, nil, true, domain.MemberStatusNormal},
		{"muted is idempotent", domain.MemberStatusMuted, nil, true, domain.MemberStatusMuted},
		{"banned rejected", domain.MemberStatusBanned, application.IsBannedFromCircleErr, false, domain.MemberStatusBanned},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newInviteFixture(t)
			u := f.user(t)
			if c.from != noRecord {
				f.seed(t, u, domain.MemberRoleMember, c.from)
			}
			if err := f.svc.JoinCircle(t.Context(), u, f.circleID); err == nil {
				t.Fatal("direct join to private circle succeeded")
			}
			link := f.createLink(t, intPtr(1))

			got, err := f.svc.AcceptInvite(t.Context(), u, link.Token)
			if f.status(t, u) != c.want {
				t.Errorf("status = %d, want %d", f.status(t, u), c.want)
			}
			wantUsed := 1
			if c.wantErr != nil || c.already {
				wantUsed = 0
			}
			if used := usedCount(t, link.ID); used != wantUsed {
				t.Errorf("used_count = %d, want %d", used, wantUsed)
			}
			if c.wantErr != nil {
				if !c.wantErr(err) {
					t.Fatalf("err = %v, want rejection", err)
				}
				return
			}
			if err != nil || got.CircleID != f.circleID || got.AlreadyMember != c.already {
				t.Fatalf("AcceptInvite = %+v, %v", got, err)
			}
			if c.already {
				if f.memberCount(t) != 1 || f.publisher.sum() != 0 {
					t.Errorf("idempotent accept had side effects: count=%d events=%d", f.memberCount(t), f.publisher.sum())
				}
				return
			}
			if f.memberCount(t) != 2 || f.publisher.sum() != 1 || !f.inJoined(t, u) {
				t.Errorf("side effects: count=%d events=%d inJoined=%v, want 2 / +1 / true",
					f.memberCount(t), f.publisher.sum(), f.inJoined(t, u))
			}
		})
	}
}

// TestAcceptInvite_ConcurrentUsersCappedByMaxUses 10 个不同用户并发接受 max_uses=3 的链接：恰好 3 人入圈。
func TestAcceptInvite_ConcurrentUsersCappedByMaxUses(t *testing.T) {
	t.Parallel()
	f := newInviteFixture(t)
	link := f.createLink(t, intPtr(3))
	// 预热统计 Hash：冷 Hash 下并发入圈的「DB 恢复覆盖并发 HINCRBY」是 incrMemberCountWithRecovery
	// 既有的软计数竞态（与邀请无关），这里只验证邀请次数与入圈人数。
	if err := f.stats.SetStats(t.Context(), f.circleID, &domain.CircleStatistics{MemberCount: 1}); err != nil {
		t.Fatalf("warm stats: %v", err)
	}
	users := make([]uuid.UUID, 10)
	for i := range users {
		users[i] = f.user(t)
	}

	var ok, invalid atomic.Int32
	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.AcceptInvite(context.WithoutCancel(t.Context()), u, link.Token)
			switch {
			case err == nil:
				ok.Add(1)
			case application.IsInviteInvalidErr(err):
				invalid.Add(1)
			default:
				t.Errorf("unexpected err %v", err)
			}
		}()
	}
	wg.Wait()

	joined := 0
	for _, u := range users {
		if f.status(t, u) == domain.MemberStatusNormal {
			joined++
		}
	}
	if ok.Load() != 3 || invalid.Load() != 7 || joined != 3 || usedCount(t, link.ID) != 3 {
		t.Errorf("ok=%d invalid=%d joined=%d used=%d, want 3 / 7 / 3 / 3", ok.Load(), invalid.Load(), joined, usedCount(t, link.ID))
	}
	if f.memberCount(t) != 4 || f.publisher.sum() != 3 {
		t.Errorf("count=%d events=%d, want 4 / +3", f.memberCount(t), f.publisher.sum())
	}
}

// TestAcceptInvite_SameUserConcurrentCountsOnce 同一用户并发重复接受：全部成功（幂等），只入圈一次、只消耗一次。
func TestAcceptInvite_SameUserConcurrentCountsOnce(t *testing.T) {
	t.Parallel()
	f := newInviteFixture(t)
	link := f.createLink(t, nil)
	u := f.user(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.svc.AcceptInvite(context.WithoutCancel(t.Context()), u, link.Token); err != nil {
				t.Errorf("AcceptInvite: %v", err)
			}
		}()
	}
	wg.Wait()
	if f.status(t, u) != domain.MemberStatusNormal || usedCount(t, link.ID) != 1 {
		t.Errorf("status=%d used=%d, want 1 / 1", f.status(t, u), usedCount(t, link.ID))
	}
	if f.memberCount(t) != 2 || f.publisher.sum() != 1 {
		t.Errorf("count=%d events=%d, want 2 / +1", f.memberCount(t), f.publisher.sum())
	}
}

// TestInvite_RevokedAndPublicCircleLinksInvalid 撤销后、私密圈改公开后（即使再改回私密），预览与接受都 404。
func TestInvite_RevokedAndPublicCircleLinksInvalid(t *testing.T) {
	t.Parallel()
	f := newInviteFixture(t)
	ctx := t.Context()
	revoked, kept := f.createLink(t, nil), f.createLink(t, nil)

	if _, err := f.svc.PreviewInvite(ctx, kept.Token); err != nil {
		t.Fatalf("preview valid link: %v", err)
	}
	if err := f.svc.RevokeInviteLink(ctx, f.ownerID, revoked.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.svc.PreviewInvite(ctx, revoked.Token); !application.IsInviteInvalidErr(err) {
		t.Errorf("preview revoked err = %v, want invalid", err)
	}

	public, private := int16(domain.CircleJoinTypeDirect), int16(domain.CircleJoinTypePrivate)
	for _, jt := range []*int16{&public, &private} {
		if err := f.svc.UpdateCircleProfile(ctx, f.ownerID, f.circleID, application.UpdateCircleProfileInput{JoinType: jt}); err != nil {
			t.Fatalf("set join_type %d: %v", *jt, err)
		}
	}
	u := f.user(t)
	if _, err := f.svc.PreviewInvite(ctx, kept.Token); !application.IsInviteInvalidErr(err) {
		t.Errorf("preview after public err = %v, want invalid", err)
	}
	if _, err := f.svc.AcceptInvite(ctx, u, kept.Token); !application.IsInviteInvalidErr(err) {
		t.Errorf("accept after public err = %v, want invalid", err)
	}
	if f.status(t, u) != noRecord {
		t.Error("invalid accept created member row")
	}
}
