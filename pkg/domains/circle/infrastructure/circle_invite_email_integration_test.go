//go:build integration

// circle_invite_email_integration_test.go email 邀请仓储对开发栈 PostgreSQL 的集成测试（#169）：
// AddPending 的 ON CONFLICT 语义（新建 / 撤销与拒绝重置 / 待接受与已接受不变，只 RETURNING 新建与重置的行），
// 待接受列表按 email 走部分索引，状态 CAS。
package infrastructure_test

import (
	"errors"
	"slices"
	"testing"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/domains/circle/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// randEmail 随机邮箱（已是存储口径：小写）。
func randEmail() string { return "it169-" + sharedomain.NewID().String() + "@example.test" }

func emailRow(t *testing.T, repo domain.InviteEmailRepository, circleID uuid.UUID, email string) domain.CircleInviteEmail {
	t.Helper()
	rows, _, err := repo.ListByCircle(t.Context(), circleID, -1, 0, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.Email == email {
			return r
		}
	}
	t.Fatalf("no row for %s", email)
	return domain.CircleInviteEmail{}
}

func TestInviteEmailRepo_AddPendingConflictSemantics(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewInviteEmailRepository(db)
	ctx := t.Context()
	circleID, owner, admin := sharedomain.NewID(), sharedomain.NewID(), sharedomain.NewID()
	testkit.CleanupRows(t, "domains.circle_invite_email", "circle_id", circleID)

	pending, accepted, revoked, rejected := randEmail(), randEmail(), randEmail(), randEmail()
	added, err := repo.AddPending(ctx, circleID, owner, []string{pending, accepted, revoked, rejected})
	if err != nil || len(added) != 4 {
		t.Fatalf("first add = %v, %v; want 4 rows", added, err)
	}
	for email, to := range map[string]int16{
		accepted: domain.InviteEmailAccepted, revoked: domain.InviteEmailRevoked, rejected: domain.InviteEmailRejected,
	} {
		if err := repo.UpdateStatus(ctx, emailRow(t, repo, circleID, email).ID, domain.InviteEmailPending, to, owner); err != nil {
			t.Fatalf("set status %d: %v", to, err)
		}
	}

	fresh := randEmail()
	added, err = repo.AddPending(ctx, circleID, admin, []string{pending, accepted, revoked, rejected, fresh})
	if err != nil {
		t.Fatalf("second add: %v", err)
	}
	slices.Sort(added)
	want := []string{revoked, rejected, fresh}
	slices.Sort(want)
	if !slices.Equal(added, want) {
		t.Errorf("added = %v, want %v (reset + new only)", added, want)
	}
	for email, wantStatus := range map[string]int16{
		pending: domain.InviteEmailPending, accepted: domain.InviteEmailAccepted,
		revoked: domain.InviteEmailPending, rejected: domain.InviteEmailPending, fresh: domain.InviteEmailPending,
	} {
		r := emailRow(t, repo, circleID, email)
		if r.Status != wantStatus {
			t.Errorf("%s status = %d, want %d", email, r.Status, wantStatus)
		}
		reset := email == revoked || email == rejected
		if reset && (r.InviterID != admin || r.UpdateBy == nil || *r.UpdateBy != admin) {
			t.Errorf("%s reset: inviter=%s update_by=%v, want admin", email, r.InviterID, r.UpdateBy)
		}
		if email == pending && r.InviterID != owner {
			t.Errorf("pending row inviter changed to %s", r.InviterID)
		}
	}
	if _, total, _ := repo.ListByCircle(ctx, circleID, domain.InviteEmailPending, 0, 100); total != 4 {
		t.Errorf("pending total = %d, want 4", total)
	}
}

func TestInviteEmailRepo_PendingByEmailAndCAS(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewInviteEmailRepository(db)
	ctx := t.Context()
	email := randEmail()
	c1, c2, c3 := sharedomain.NewID(), sharedomain.NewID(), sharedomain.NewID()
	for _, c := range []uuid.UUID{c1, c2, c3} {
		testkit.CleanupRows(t, "domains.circle_invite_email", "circle_id", c)
		if _, err := repo.AddPending(ctx, c, sharedomain.NewID(), []string{email}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	r3 := emailRow(t, repo, c3, email)
	if err := repo.UpdateStatus(ctx, r3.ID, domain.InviteEmailPending, domain.InviteEmailRevoked, sharedomain.NewID()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := repo.UpdateStatus(ctx, r3.ID, domain.InviteEmailPending, domain.InviteEmailAccepted, sharedomain.NewID()); !errors.Is(err, domain.ErrInviteEmailStateConflict) {
		t.Errorf("CAS from wrong status err = %v, want conflict", err)
	}

	rows, total, err := repo.ListPendingByEmail(ctx, email, 0, 10)
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("pending by email = %d rows / total %d, %v; want 2", len(rows), total, err)
	}
	if rows[0].CircleID != c2 || rows[1].CircleID != c1 {
		t.Errorf("order = %s, %s; want newest first", rows[0].CircleID, rows[1].CircleID)
	}
	if _, err := repo.GetByID(ctx, sharedomain.NewID()); !errors.Is(err, domain.ErrInviteEmailNotFound) {
		t.Errorf("missing id err = %v", err)
	}
}
