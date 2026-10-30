// Package application 的 email 邀请单元测试（#169）。
//
// 覆盖：邮箱规整与校验、添加的数量 / 权限 / 私密圈校验与忽略项回报；接受 / 拒绝只能处理发给自己 email 的邀请，
// 拉黑拒绝、已是成员与重复接受幂等、入圈失败邀请改回待接受；撤销的状态规则；我的邀请按 email 匹配。
// SQL（ON CONFLICT 重置、部分索引查询）由 infrastructure 集成测试覆盖。
package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
)

// fakeInviteEmailRepo 内存 email 邀请表，AddPending 复刻 ON CONFLICT ... WHERE status IN (2, 3) 语义。
type fakeInviteEmailRepo struct {
	rows     map[uuid.UUID]*domain.CircleInviteEmail
	gotAdded []string
}

func newFakeInviteEmailRepo() *fakeInviteEmailRepo {
	return &fakeInviteEmailRepo{rows: map[uuid.UUID]*domain.CircleInviteEmail{}}
}

func (r *fakeInviteEmailRepo) find(circleID uuid.UUID, email string) *domain.CircleInviteEmail {
	for _, row := range r.rows {
		if row.CircleID == circleID && row.Email == email {
			return row
		}
	}
	return nil
}

func (r *fakeInviteEmailRepo) AddPending(_ context.Context, circleID, inviterID uuid.UUID, emails []string) ([]string, error) {
	r.gotAdded = emails
	var added []string
	for _, e := range emails {
		row := r.find(circleID, e)
		switch {
		case row == nil:
			id := uuid.New()
			r.rows[id] = &domain.CircleInviteEmail{ID: id, CircleID: circleID, InviterID: inviterID, Email: e}
		case row.Status == domain.InviteEmailRevoked || row.Status == domain.InviteEmailRejected:
			row.Status, row.InviterID = domain.InviteEmailPending, inviterID
		default:
			continue
		}
		added = append(added, e)
	}
	return added, nil
}

func (r *fakeInviteEmailRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.CircleInviteEmail, error) {
	row, ok := r.rows[id]
	if !ok {
		return nil, domain.ErrInviteEmailNotFound
	}
	c := *row
	return &c, nil
}

func (r *fakeInviteEmailRepo) ListByCircle(_ context.Context, circleID uuid.UUID, status int16, _, _ int) ([]domain.CircleInviteEmail, int64, error) {
	var out []domain.CircleInviteEmail
	for _, row := range r.rows {
		if row.CircleID == circleID && (status < 0 || row.Status == status) {
			out = append(out, *row)
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeInviteEmailRepo) ListPendingByEmail(_ context.Context, email string, _, _ int) ([]domain.CircleInviteEmail, int64, error) {
	var out []domain.CircleInviteEmail
	for _, row := range r.rows {
		if row.Email == email && row.Status == domain.InviteEmailPending {
			out = append(out, *row)
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeInviteEmailRepo) UpdateStatus(_ context.Context, id uuid.UUID, from, to int16, _ uuid.UUID) error {
	row, ok := r.rows[id]
	if !ok || row.Status != from {
		return domain.ErrInviteEmailStateConflict
	}
	row.Status = to
	return nil
}

// fakeUserEmails userID → email。
type fakeUserEmails map[uuid.UUID]string

func (f fakeUserEmails) GetEmail(_ context.Context, id uuid.UUID) (string, error) { return f[id], nil }

// GetByIDs 补齐 fakeCircleRepo（我的邀请回填圈子信息）。
func (r *fakeCircleRepo) GetByIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*domain.Circle, error) {
	out := map[uuid.UUID]*domain.Circle{}
	for _, id := range ids {
		if r.circle != nil && r.circle.ID == id {
			c := *r.circle
			out[id] = &c
		}
	}
	return out, nil
}

type emailInviteFixture struct {
	*inviteFixture
	emails *fakeInviteEmailRepo
	users  fakeUserEmails
}

func newEmailInviteFixture(joinType int16) *emailInviteFixture {
	f := &emailInviteFixture{inviteFixture: newInviteFixture(joinType), emails: newFakeInviteEmailRepo(), users: fakeUserEmails{}}
	f.svc.SetInviteEmailRepository(f.emails)
	f.svc.SetUserEmailReader(f.users)
	return f
}

// invite 放一条邀请，返回其 ID。
func (f *emailInviteFixture) invite(email string, status int16) uuid.UUID {
	id := uuid.New()
	f.emails.rows[id] = &domain.CircleInviteEmail{ID: id, CircleID: f.circle.ID, InviterID: f.owner, Email: email, Status: status}
	return id
}

// invitee 一个 users.email 为 email 的用户（存储口径可能带大小写 / 空格）。
func (f *emailInviteFixture) invitee(email string) uuid.UUID {
	u := uuid.New()
	f.users[u] = email
	f.joined.warm[u] = true
	return u
}

func TestNormalizeInviteEmail(t *testing.T) {
	cases := map[string]string{
		" Alice@Example.COM ": "alice@example.com",
		"bob@example.test":    "bob@example.test",
		"":                    "",
		"no-at-sign":          "",
		"Alice <a@b.com>":     "",
		"a@b.com, c@d.com":    "",
		"two@@example.com":    "",
	}
	for in, want := range cases {
		got, ok := normalizeInviteEmail(in)
		if got != want || ok != (want != "") {
			t.Errorf("normalizeInviteEmail(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	long := make([]byte, 95)
	for i := range long {
		long[i] = 'a'
	}
	if _, ok := normalizeInviteEmail(string(long) + "@b.com"); ok {
		t.Error("101-char email accepted")
	}
}

func TestAddInviteEmails_Validation(t *testing.T) {
	ctx := context.Background()
	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	member := f.add(domain.MemberRoleMember, domain.MemberStatusNormal)
	if _, err := f.svc.AddInviteEmails(ctx, member, f.circle.ID, []string{"a@b.com"}); !IsNotCircleAdminErr(err) {
		t.Errorf("member add err = %v, want not admin", err)
	}
	if _, err := f.svc.AddInviteEmails(ctx, f.owner, f.circle.ID, nil); !IsInvalidInviteEmailsErr(err) {
		t.Errorf("empty add err = %v", err)
	}
	if _, err := f.svc.AddInviteEmails(ctx, f.owner, f.circle.ID, make([]string, 51)); !IsInvalidInviteEmailsErr(err) {
		t.Errorf("51 emails err = %v", err)
	}
	pub := newEmailInviteFixture(domain.CircleJoinTypeDirect)
	if _, err := pub.svc.AddInviteEmails(ctx, pub.owner, pub.circle.ID, []string{"a@b.com"}); !IsInviteRequiresPrivateErr(err) {
		t.Errorf("public circle add err = %v", err)
	}
	if len(f.emails.rows) != 0 || len(pub.emails.rows) != 0 {
		t.Error("rejected add wrote rows")
	}
}

// TestAddInviteEmails_FiltersAndReports 规整后去重；不合法 / 重复 / 已邀请回报原文；撤销 / 拒绝的重置为待接受。
func TestAddInviteEmails_FiltersAndReports(t *testing.T) {
	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	admin := f.add(domain.MemberRoleAdmin, domain.MemberStatusNormal)
	f.invite("pending@x.com", domain.InviteEmailPending)
	f.invite("accepted@x.com", domain.InviteEmailAccepted)
	revoked := f.invite("revoked@x.com", domain.InviteEmailRevoked)
	rejected := f.invite("rejected@x.com", domain.InviteEmailRejected)

	got, err := f.svc.AddInviteEmails(context.Background(), admin, f.circle.ID, []string{
		" New@X.com", "new@x.com", "bad", "Pending@x.com", "accepted@x.com", "revoked@x.com", "REJECTED@x.com ",
	})
	if err != nil {
		t.Fatalf("AddInviteEmails: %v", err)
	}
	wantAdded := []string{"new@x.com", "revoked@x.com", "rejected@x.com"}
	if len(got.Added) != len(wantAdded) {
		t.Fatalf("added = %v, want %v", got.Added, wantAdded)
	}
	for i := range wantAdded {
		if got.Added[i] != wantAdded[i] {
			t.Errorf("added[%d] = %q, want %q", i, got.Added[i], wantAdded[i])
		}
	}
	wantIgnored := map[string]string{
		"new@x.com": InviteEmailIgnoredDuplicate, "bad": InviteEmailIgnoredInvalid,
		"Pending@x.com": InviteEmailIgnoredExisting, "accepted@x.com": InviteEmailIgnoredExisting,
	}
	if len(got.Ignored) != len(wantIgnored) {
		t.Fatalf("ignored = %+v", got.Ignored)
	}
	for _, ig := range got.Ignored {
		if wantIgnored[ig.Email] != ig.Reason {
			t.Errorf("ignored %q reason %q, want %q", ig.Email, ig.Reason, wantIgnored[ig.Email])
		}
	}
	for _, id := range []uuid.UUID{revoked, rejected} {
		if row := f.emails.rows[id]; row.Status != domain.InviteEmailPending || row.InviterID != admin {
			t.Errorf("reset row = %+v, want pending by admin", row)
		}
	}
}

func TestRespondInviteEmail_OnlyOwnPendingInvite(t *testing.T) {
	ctx := context.Background()
	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	mine := f.invite("me@x.com", domain.InviteEmailPending)
	revoked := f.invite("me2@x.com", domain.InviteEmailRevoked)
	other := f.invitee("someone@x.com")
	noEmail := uuid.New()

	for name, tc := range map[string]struct {
		user uuid.UUID
		id   uuid.UUID
	}{
		"other email": {other, mine}, "user without email": {noEmail, mine},
		"unknown invite": {other, uuid.New()}, "revoked": {f.invitee("me2@x.com"), revoked},
	} {
		for _, accept := range []bool{true, false} {
			if _, err := f.svc.RespondInviteEmail(ctx, tc.user, tc.id, accept); !IsInviteInvalidErr(err) {
				t.Errorf("%s accept=%v: err = %v, want invalid", name, accept, err)
			}
		}
	}
	if f.emails.rows[mine].Status != domain.InviteEmailPending || f.members.joinCalls != 0 {
		t.Error("rejected responses changed state")
	}
}

// TestRespondInviteEmail_AcceptJoinsAndIsIdempotent 接受（email 大小写 / 空格不同也匹配）：入圈、邀请置 1；
// 再次接受幂等成功、不重复入圈。
func TestRespondInviteEmail_AcceptJoinsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	id := f.invite("me@x.com", domain.InviteEmailPending)
	u := f.invitee(" Me@X.com")
	f.members.joinRet = &domain.CircleMember{UserID: u, Status: domain.MemberStatusNormal}

	got, err := f.svc.RespondInviteEmail(ctx, u, id, true)
	if err != nil || got.AlreadyMember || got.CircleID != f.circle.ID {
		t.Fatalf("accept = %+v, %v", got, err)
	}
	if f.emails.rows[id].Status != domain.InviteEmailAccepted || f.members.gotJoinType != domain.CircleJoinTypeDirect {
		t.Errorf("status=%d gate=%d, want 1 / 0", f.emails.rows[id].Status, f.members.gotJoinType)
	}
	if f.stats.memberDelta != 1 || len(f.publisher.deltas) != 1 {
		t.Errorf("delta=%d events=%v, want +1 / [1]", f.stats.memberDelta, f.publisher.deltas)
	}

	f.members.members[u] = &domain.CircleMember{UserID: u, Status: domain.MemberStatusNormal}
	got, err = f.svc.RespondInviteEmail(ctx, u, id, true)
	if err != nil || !got.AlreadyMember || f.members.joinCalls != 1 {
		t.Errorf("repeat accept = %+v, %v, joins=%d; want already member, 1 join", got, err, f.members.joinCalls)
	}
}

func TestRespondInviteEmail_ExistingMemberStates(t *testing.T) {
	ctx := context.Background()

	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	id := f.invite("m@x.com", domain.InviteEmailPending)
	u := f.invitee("m@x.com")
	f.members.members[u] = &domain.CircleMember{UserID: u, Status: domain.MemberStatusMuted}
	if got, err := f.svc.RespondInviteEmail(ctx, u, id, true); err != nil || !got.AlreadyMember {
		t.Errorf("member accept = %+v, %v", got, err)
	}
	if f.emails.rows[id].Status != domain.InviteEmailAccepted || f.members.joinCalls != 0 {
		t.Errorf("member accept: status=%d joins=%d, want 1 / 0", f.emails.rows[id].Status, f.members.joinCalls)
	}

	f = newEmailInviteFixture(domain.CircleJoinTypePrivate)
	id = f.invite("b@x.com", domain.InviteEmailPending)
	u = f.invitee("b@x.com")
	f.members.members[u] = &domain.CircleMember{UserID: u, Status: domain.MemberStatusBanned}
	if _, err := f.svc.RespondInviteEmail(ctx, u, id, true); !IsBannedFromCircleErr(err) {
		t.Errorf("banned accept err = %v", err)
	}
	if f.emails.rows[id].Status != domain.InviteEmailPending || f.members.joinCalls != 0 {
		t.Errorf("banned accept changed state: status=%d joins=%d", f.emails.rows[id].Status, f.members.joinCalls)
	}
}

func TestRespondInviteEmail_JoinFailureRestoresPending(t *testing.T) {
	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	id := f.invite("me@x.com", domain.InviteEmailPending)
	u := f.invitee("me@x.com")
	f.members.joinErr = errors.New("user is banned from this circle")

	if _, err := f.svc.RespondInviteEmail(context.Background(), u, id, true); !IsBannedFromCircleErr(err) {
		t.Fatalf("err = %v, want banned", err)
	}
	if f.emails.rows[id].Status != domain.InviteEmailPending {
		t.Errorf("status = %d, want restored to 0", f.emails.rows[id].Status)
	}
}

func TestRespondInviteEmail_Reject(t *testing.T) {
	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	id := f.invite("me@x.com", domain.InviteEmailPending)
	u := f.invitee("me@x.com")

	if _, err := f.svc.RespondInviteEmail(context.Background(), u, id, false); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if f.emails.rows[id].Status != domain.InviteEmailRejected || f.members.joinCalls != 0 {
		t.Errorf("status=%d joins=%d, want 3 / 0", f.emails.rows[id].Status, f.members.joinCalls)
	}
	if _, err := f.svc.RespondInviteEmail(context.Background(), u, id, true); !IsInviteInvalidErr(err) {
		t.Errorf("accept after reject err = %v, want invalid", err)
	}
}

func TestRevokeInviteEmail_States(t *testing.T) {
	ctx := context.Background()
	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	pending := f.invite("p@x.com", domain.InviteEmailPending)
	accepted := f.invite("a@x.com", domain.InviteEmailAccepted)
	member := f.add(domain.MemberRoleMember, domain.MemberStatusNormal)

	if err := f.svc.RevokeInviteEmail(ctx, member, pending); !IsNotCircleAdminErr(err) {
		t.Errorf("member revoke err = %v", err)
	}
	if err := f.svc.RevokeInviteEmail(ctx, f.owner, pending); err != nil || f.emails.rows[pending].Status != domain.InviteEmailRevoked {
		t.Errorf("revoke pending: err=%v status=%d", err, f.emails.rows[pending].Status)
	}
	if err := f.svc.RevokeInviteEmail(ctx, f.owner, pending); err != nil {
		t.Errorf("repeat revoke err = %v, want idempotent", err)
	}
	if err := f.svc.RevokeInviteEmail(ctx, f.owner, accepted); !errors.Is(err, domain.ErrInviteEmailStateConflict) {
		t.Errorf("revoke accepted err = %v, want conflict", err)
	}
}

func TestListMyInvites_MatchesCurrentEmail(t *testing.T) {
	ctx := context.Background()
	f := newEmailInviteFixture(domain.CircleJoinTypePrivate)
	f.circle.Name, f.circle.AvatarURL = "私密圈", "https://example.com/a.png"
	mine := f.invite("me@x.com", domain.InviteEmailPending)
	f.invite("me2@x.com", domain.InviteEmailPending)
	u := f.invitee("ME@x.com")

	got, err := f.svc.ListMyInvites(ctx, u, 0, 0)
	if err != nil || len(got.Data) != 1 {
		t.Fatalf("ListMyInvites = %+v, %v", got, err)
	}
	if it := got.Data[0]; it.InviteID != mine || it.CircleName != "私密圈" || it.CircleAvatar != "https://example.com/a.png" {
		t.Errorf("item = %+v", it)
	}
	got, err = f.svc.ListMyInvites(ctx, uuid.New(), 0, 0)
	if err != nil || len(got.Data) != 0 {
		t.Errorf("user without email = %+v, %v; want empty", got, err)
	}
}
