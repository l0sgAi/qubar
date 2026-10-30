// Package application 的邀请链接单元测试（#169）。
//
// 覆盖：创建的参数 / 权限 / 私密圈校验与 token 只存 hash；预览与接受对无效、过期、撤销、用尽、
// 圈子改公开统一 errInviteInvalid；接受时已是成员不消耗、拉黑拒绝、入圈失败归还次数；
// 私密圈改公开批量撤销。条件 UPDATE 的并发正确性由 infrastructure 集成测试覆盖。
package application

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
)

// fakeInviteLinkRepo 内存邀请链接表；ConsumeUse 复刻条件 UPDATE 的判断。
type fakeInviteLinkRepo struct {
	links          map[uuid.UUID]*domain.CircleInviteLink
	consumes       int
	releases       int
	revokedCircles []uuid.UUID
}

func newFakeInviteLinkRepo() *fakeInviteLinkRepo {
	return &fakeInviteLinkRepo{links: map[uuid.UUID]*domain.CircleInviteLink{}}
}

func (r *fakeInviteLinkRepo) Create(_ context.Context, l *domain.CircleInviteLink) error {
	c := *l
	r.links[l.ID] = &c
	return nil
}

func (r *fakeInviteLinkRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.CircleInviteLink, error) {
	l, ok := r.links[id]
	if !ok {
		return nil, domain.ErrInviteLinkNotFound
	}
	c := *l
	return &c, nil
}

func (r *fakeInviteLinkRepo) GetByTokenHash(_ context.Context, h string) (*domain.CircleInviteLink, error) {
	for _, l := range r.links {
		if l.TokenHash == h {
			c := *l
			return &c, nil
		}
	}
	return nil, domain.ErrInviteLinkNotFound
}

func (r *fakeInviteLinkRepo) ListByCircle(_ context.Context, circleID uuid.UUID, _, _ int) ([]domain.CircleInviteLink, int64, error) {
	var out []domain.CircleInviteLink
	for _, l := range r.links {
		if l.CircleID == circleID {
			out = append(out, *l)
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeInviteLinkRepo) Revoke(_ context.Context, id, _ uuid.UUID) error {
	r.links[id].Revoked = 1
	return nil
}

func (r *fakeInviteLinkRepo) RevokeByCircle(_ context.Context, circleID, _ uuid.UUID) (int64, error) {
	r.revokedCircles = append(r.revokedCircles, circleID)
	return 0, nil
}

func (r *fakeInviteLinkRepo) ConsumeUse(_ context.Context, id uuid.UUID) error {
	l := r.links[id]
	if !l.Usable(time.Now()) {
		return domain.ErrInviteLinkUnusable
	}
	l.UsedCount++
	r.consumes++
	return nil
}

func (r *fakeInviteLinkRepo) ReleaseUse(_ context.Context, id uuid.UUID) error {
	r.links[id].UsedCount--
	r.releases++
	return nil
}

// GetStats 补齐 fakeStatsCache（预览读成员数）。
func (c *fakeStatsCache) GetStats(context.Context, uuid.UUID) (*domain.CircleStatistics, error) {
	return &domain.CircleStatistics{MemberCount: 7}, nil
}

type inviteFixture struct {
	*membershipFixture
	links *fakeInviteLinkRepo
	owner uuid.UUID
}

func newInviteFixture(joinType int16) *inviteFixture {
	f := &inviteFixture{membershipFixture: newMembershipFixture(joinType), links: newFakeInviteLinkRepo()}
	f.circle.Name, f.circle.AvatarURL = "私密圈", "https://example.com/a.png"
	f.svc.SetInviteLinkRepository(f.links)
	f.owner = f.add(domain.MemberRoleOwner, domain.MemberStatusNormal)
	return f
}

// seedLink 直接放一条链接，返回明文 token。
func (f *inviteFixture) seedLink(mut func(*domain.CircleInviteLink)) (string, *domain.CircleInviteLink) {
	token, _ := newInviteToken()
	l := &domain.CircleInviteLink{
		ID: uuid.New(), CircleID: f.circle.ID, CreatorID: f.owner,
		TokenHash: domain.HashInviteToken(token), ExpireTime: time.Now().Add(time.Hour),
	}
	if mut != nil {
		mut(l)
	}
	f.links.links[l.ID] = l
	return token, l
}

func intPtr(v int) *int { return &v }

func TestCreateInviteLink_Validation(t *testing.T) {
	cases := []struct {
		name     string
		joinType int16
		opRole   int16 // 0 = 非成员
		input    CreateInviteLinkInput
		is       func(error) bool
	}{
		{"public circle", domain.CircleJoinTypeDirect, domain.MemberRoleOwner, CreateInviteLinkInput{}, IsInviteRequiresPrivateErr},
		{"member cannot create", domain.CircleJoinTypePrivate, domain.MemberRoleMember, CreateInviteLinkInput{}, IsNotCircleAdminErr},
		{"non-member cannot create", domain.CircleJoinTypePrivate, 0, CreateInviteLinkInput{}, IsNotCircleAdminErr},
		{"expire over 30 days", domain.CircleJoinTypePrivate, domain.MemberRoleOwner, CreateInviteLinkInput{ExpireHours: 721}, IsInvalidInviteExpireErr},
		{"negative expire", domain.CircleJoinTypePrivate, domain.MemberRoleOwner, CreateInviteLinkInput{ExpireHours: -1}, IsInvalidInviteExpireErr},
		{"zero max uses", domain.CircleJoinTypePrivate, domain.MemberRoleOwner, CreateInviteLinkInput{MaxUses: intPtr(0)}, IsInvalidInviteMaxUsesErr},
		{"too many max uses", domain.CircleJoinTypePrivate, domain.MemberRoleOwner, CreateInviteLinkInput{MaxUses: intPtr(maxInviteUses + 1)}, IsInvalidInviteMaxUsesErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newInviteFixture(tc.joinType)
			op := uuid.New()
			if tc.opRole != 0 {
				op = f.add(tc.opRole, domain.MemberStatusNormal)
			}
			if _, err := f.svc.CreateInviteLink(context.Background(), op, f.circle.ID, tc.input); !tc.is(err) {
				t.Fatalf("err = %v", err)
			}
			if len(f.links.links) != 0 {
				t.Errorf("rejected create wrote %d links", len(f.links.links))
			}
		})
	}
}

// TestCreateInviteLink_StoresHashOnly 管理员可创建；默认 7 天；token 为 32 字节 base64url，库内只有 hash。
func TestCreateInviteLink_StoresHashOnly(t *testing.T) {
	f := newInviteFixture(domain.CircleJoinTypePrivate)
	admin := f.add(domain.MemberRoleAdmin, domain.MemberStatusNormal)

	got, err := f.svc.CreateInviteLink(context.Background(), admin, f.circle.ID, CreateInviteLinkInput{MaxUses: intPtr(5)})
	if err != nil {
		t.Fatalf("CreateInviteLink: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(got.Token)
	if err != nil || len(raw) != inviteTokenBytes {
		t.Fatalf("token %q: decoded %d bytes, err %v; want 32-byte base64url", got.Token, len(raw), err)
	}
	stored := f.links.links[got.ID]
	if stored == nil || stored.TokenHash != domain.HashInviteToken(got.Token) || stored.TokenHash == got.Token {
		t.Fatalf("stored link = %+v, want sha256 of token", stored)
	}
	if stored.CreatorID != admin || *stored.MaxUses != 5 {
		t.Errorf("creator=%s max_uses=%d", stored.CreatorID, *stored.MaxUses)
	}
	if d := time.Until(stored.ExpireTime); d < 167*time.Hour || d > 168*time.Hour {
		t.Errorf("expire in %v, want default 7 days", d)
	}
}

// TestInvite_InvalidTokensAre404 预览 / 接受对各种无效状态统一 errInviteInvalid，且接受不写库。
func TestInvite_InvalidTokensAre404(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *inviteFixture) string
	}{
		{"unknown", func(*inviteFixture) string { tok, _ := newInviteToken(); return tok }},
		{"empty", func(*inviteFixture) string { return "" }},
		{"expired", func(f *inviteFixture) string {
			tok, _ := f.seedLink(func(l *domain.CircleInviteLink) { l.ExpireTime = time.Now().Add(-time.Second) })
			return tok
		}},
		{"revoked", func(f *inviteFixture) string {
			tok, _ := f.seedLink(func(l *domain.CircleInviteLink) { l.Revoked = 1 })
			return tok
		}},
		{"used up", func(f *inviteFixture) string {
			tok, _ := f.seedLink(func(l *domain.CircleInviteLink) { l.MaxUses, l.UsedCount = intPtr(2), 2 })
			return tok
		}},
		{"circle became public", func(f *inviteFixture) string {
			tok, _ := f.seedLink(nil)
			f.circle.JoinType = domain.CircleJoinTypeDirect
			return tok
		}},
		{"circle banned", func(f *inviteFixture) string {
			tok, _ := f.seedLink(nil)
			f.circle.Status = domain.CircleStatusBanned
			return tok
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newInviteFixture(domain.CircleJoinTypePrivate)
			token := tc.setup(f)
			if _, err := f.svc.PreviewInvite(context.Background(), token); !IsInviteInvalidErr(err) {
				t.Errorf("preview err = %v, want invite invalid", err)
			}
			if _, err := f.svc.AcceptInvite(context.Background(), uuid.New(), token); !IsInviteInvalidErr(err) {
				t.Errorf("accept err = %v, want invite invalid", err)
			}
			if f.members.joinCalls != 0 || f.links.consumes != 0 {
				t.Errorf("invalid accept wrote: joins=%d consumes=%d", f.members.joinCalls, f.links.consumes)
			}
		})
	}
}

func TestPreviewInvite_OnlyBriefFields(t *testing.T) {
	f := newInviteFixture(domain.CircleJoinTypePrivate)
	token, _ := f.seedLink(nil)
	got, err := f.svc.PreviewInvite(context.Background(), token)
	if err != nil {
		t.Fatalf("PreviewInvite: %v", err)
	}
	want := InvitePreviewVO{Name: "私密圈", AvatarURL: "https://example.com/a.png", MemberCount: 7}
	if *got != want {
		t.Errorf("preview = %+v, want %+v", *got, want)
	}
}

// TestAcceptInvite_JoinsWithSideEffects 接受走与自助加入相同的状态机（跳过 join_type 门槛）与副作用，并消耗一次。
func TestAcceptInvite_JoinsWithSideEffects(t *testing.T) {
	f := newInviteFixture(domain.CircleJoinTypePrivate)
	token, link := f.seedLink(func(l *domain.CircleInviteLink) { l.MaxUses = intPtr(3) })
	user := uuid.New()
	f.joined.warm[user] = true
	f.members.joinRet = &domain.CircleMember{UserID: user, Status: domain.MemberStatusNormal, CreateTime: time.Now()}

	got, err := f.svc.AcceptInvite(context.Background(), user, token)
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}
	if got.CircleID != f.circle.ID || got.AlreadyMember {
		t.Errorf("result = %+v", got)
	}
	if f.members.gotJoinType != domain.CircleJoinTypeDirect {
		t.Errorf("repo gate join_type = %d, want 0 (invite bypasses private gate)", f.members.gotJoinType)
	}
	if link.UsedCount != 1 || f.stats.memberDelta != 1 || len(f.publisher.deltas) != 1 || !f.joined.has(user, f.circle.ID) {
		t.Errorf("used=%d delta=%d events=%v inJoined=%v, want 1 / +1 / [1] / true",
			link.UsedCount, f.stats.memberDelta, f.publisher.deltas, f.joined.has(user, f.circle.ID))
	}
}

// TestAcceptInvite_ExistingMemberStates 已是成员（正常 / 禁言）幂等成功不消耗；拉黑拒绝不消耗。
func TestAcceptInvite_ExistingMemberStates(t *testing.T) {
	cases := []struct {
		status  int16
		already bool
		wantErr func(error) bool
	}{
		{domain.MemberStatusNormal, true, nil},
		{domain.MemberStatusMuted, true, nil},
		{domain.MemberStatusBanned, false, IsBannedFromCircleErr},
	}
	for _, tc := range cases {
		f := newInviteFixture(domain.CircleJoinTypePrivate)
		token, link := f.seedLink(func(l *domain.CircleInviteLink) { l.MaxUses = intPtr(1) })
		user := f.add(domain.MemberRoleMember, tc.status)

		got, err := f.svc.AcceptInvite(context.Background(), user, token)
		if tc.wantErr != nil {
			if !tc.wantErr(err) {
				t.Errorf("status %d: err = %v", tc.status, err)
			}
		} else if err != nil || !got.AlreadyMember {
			t.Errorf("status %d: result=%+v err=%v, want already member", tc.status, got, err)
		}
		if link.UsedCount != 0 || f.members.joinCalls != 0 {
			t.Errorf("status %d: used=%d joins=%d, want no consumption", tc.status, link.UsedCount, f.members.joinCalls)
		}
	}
}

// TestAcceptInvite_JoinFailureReleasesUse 消耗后入圈失败（并发重复接受 / 并发拉黑）归还次数；
// 「已是成员」按幂等成功返回。
func TestAcceptInvite_JoinFailureReleasesUse(t *testing.T) {
	cases := []struct {
		repoErr string
		already bool
		is      func(error) bool
	}{
		{"user is already a member of this circle", true, nil},
		{"user is banned from this circle", false, IsBannedFromCircleErr},
	}
	for _, tc := range cases {
		f := newInviteFixture(domain.CircleJoinTypePrivate)
		token, link := f.seedLink(func(l *domain.CircleInviteLink) { l.MaxUses = intPtr(1) })
		f.members.joinErr = errors.New(tc.repoErr)

		got, err := f.svc.AcceptInvite(context.Background(), uuid.New(), token)
		if tc.already {
			if err != nil || !got.AlreadyMember {
				t.Errorf("%s: result=%+v err=%v", tc.repoErr, got, err)
			}
		} else if !tc.is(err) {
			t.Errorf("%s: err = %v", tc.repoErr, err)
		}
		if link.UsedCount != 0 || f.links.releases != 1 || f.stats.memberDelta != 0 {
			t.Errorf("%s: used=%d releases=%d delta=%d, want 0 / 1 / 0", tc.repoErr, link.UsedCount, f.links.releases, f.stats.memberDelta)
		}
	}
}

func TestRevokeInviteLink_Permissions(t *testing.T) {
	f := newInviteFixture(domain.CircleJoinTypePrivate)
	_, link := f.seedLink(nil)
	member := f.add(domain.MemberRoleMember, domain.MemberStatusNormal)
	admin := f.add(domain.MemberRoleAdmin, domain.MemberStatusNormal)
	ctx := context.Background()

	if err := f.svc.RevokeInviteLink(ctx, member, link.ID); !IsNotCircleAdminErr(err) {
		t.Errorf("member revoke err = %v, want not admin", err)
	}
	if link.Revoked != 0 {
		t.Fatal("rejected revoke changed link")
	}
	if err := f.svc.RevokeInviteLink(ctx, admin, link.ID); err != nil || link.Revoked != 1 {
		t.Errorf("admin revoke: err=%v revoked=%d", err, link.Revoked)
	}
	if err := f.svc.RevokeInviteLink(ctx, admin, uuid.New()); !errors.Is(err, domain.ErrInviteLinkNotFound) {
		t.Errorf("unknown link err = %v, want not found", err)
	}
}

// TestUpdateCircleProfile_PublicRevokesInviteLinks 私密 → 公开批量撤销；公开 → 私密不撤销。
func TestUpdateCircleProfile_PublicRevokesInviteLinks(t *testing.T) {
	for _, tc := range []struct{ from, to int16 }{
		{domain.CircleJoinTypePrivate, domain.CircleJoinTypeDirect},
		{domain.CircleJoinTypeDirect, domain.CircleJoinTypePrivate},
	} {
		f := newInviteFixture(tc.from)
		to := tc.to
		if err := f.svc.UpdateCircleProfile(context.Background(), f.owner, f.circle.ID, UpdateCircleProfileInput{JoinType: &to}); err != nil {
			t.Fatalf("%d→%d: %v", tc.from, tc.to, err)
		}
		wantRevoke := tc.from == domain.CircleJoinTypePrivate
		if got := len(f.links.revokedCircles) == 1; got != wantRevoke {
			t.Errorf("%d→%d: revoked=%v, want %v", tc.from, tc.to, got, wantRevoke)
		}
	}
}

// TestAcceptInvite_MemberReopensSpentLink 成员重复点开已用尽 / 撤销的链接：幂等成功；非成员同一链接 404。
func TestAcceptInvite_MemberReopensSpentLink(t *testing.T) {
	for name, mut := range map[string]func(*domain.CircleInviteLink){
		"used up": func(l *domain.CircleInviteLink) { l.MaxUses, l.UsedCount = intPtr(1), 1 },
		"revoked": func(l *domain.CircleInviteLink) { l.Revoked = 1 },
	} {
		f := newInviteFixture(domain.CircleJoinTypePrivate)
		token, _ := f.seedLink(mut)
		member := f.add(domain.MemberRoleMember, domain.MemberStatusNormal)

		got, err := f.svc.AcceptInvite(context.Background(), member, token)
		if err != nil || !got.AlreadyMember {
			t.Errorf("%s: member result=%+v err=%v, want already member", name, got, err)
		}
		if _, err := f.svc.AcceptInvite(context.Background(), uuid.New(), token); !IsInviteInvalidErr(err) {
			t.Errorf("%s: non-member err = %v, want invalid", name, err)
		}
	}
}
