//go:build integration

// private_circle_invite_email_integration_test.go 指定 email 邀请（#169）的端到端集成测试：真实 PG + Redis，
// circle 域经 composition 桥接（circleUserEmailReader → user Facade）读取当前用户的 users.email。
// 覆盖：录入时大小写 / 空格规整后与 users.email 匹配、尚未注册的邮箱可添加、我的邀请、非本人邮箱 404、
// 接受后入圈且私密圈内容可见、重复接受幂等、拉黑拒绝、拒绝后不再出现在我的邀请。
package composition

import (
	"strings"
	"testing"

	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	circleinfra "interestBar/pkg/domains/circle/infrastructure"
	userapp "interestBar/pkg/domains/user/application"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type emailInviteEnv struct {
	db       *gorm.DB
	svc      circleapp.CircleService
	vis      circleapp.CircleVisibility
	circleID uuid.UUID
	owner    uuid.UUID
}

func setupEmailInvite(t *testing.T) *emailInviteEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	deps := NewDeps()

	circleRepo := circleinfra.NewCircleRepository(db)
	memberRepo := circleinfra.NewMemberRepository(db)
	privateCache := circleinfra.NewPrivateCircleCache()
	svc := circleapp.NewCircleService(circleRepo, memberRepo, circleinfra.NewCircleBaseCache(),
		circleinfra.NewCircleStatsCache(), circleinfra.NewJoinedCirclesCache(), emptyCircleSearcher{},
		circleinfra.NewCircleEventPublisher(), privateCache)
	userFacade := userapp.NewUserFacade(newUserService(deps))
	svc.SetUserFacade(&circleUserFacade{delegate: userFacade})
	svc.SetUserEmailReader(&circleUserEmailReader{delegate: userFacade})
	svc.SetInviteEmailRepository(circleinfra.NewInviteEmailRepository(db))

	env := &emailInviteEnv{db: db, svc: svc, vis: circleapp.NewCircleVisibility(circleRepo, memberRepo, privateCache)}
	env.owner = insertInviteUser(t, db, "")
	if err := svc.CreateCircle(t.Context(), env.owner, circleapp.CreateCircleInput{
		Name: "it169-" + sharedomain.NewID().String(), Description: "email invite", CategoryID: sharedomain.NewID(),
		JoinType: circledomain.CircleJoinTypePrivate,
	}); err != nil {
		t.Fatalf("create circle: %v", err)
	}
	var circleID string
	if err := db.Raw("SELECT id::text FROM domains.circle WHERE creator_id = ?", env.owner).Scan(&circleID).Error; err != nil {
		t.Fatalf("find circle: %v", err)
	}
	env.circleID = uuid.MustParse(circleID)
	testkit.CleanupRows(t, "domains.circle_invite_email", "circle_id", env.circleID)
	testkit.CleanupRows(t, "domains.circle_member", "circle_id", env.circleID)
	testkit.CleanupRows(t, "domains.circle", "id", env.circleID)
	testkit.CleanupKeys(t, redispkg.GetCircleStatsKey(env.circleID), redispkg.GetCircleInfoKey(env.circleID),
		redispkg.GetUserJoinedCirclesKey(env.owner))
	return env
}

// insertInviteUser 插入用户（email 为空时随机生成）并注册清理，返回 ID。
func insertInviteUser(t *testing.T, db *gorm.DB, email string) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if email == "" {
		email = id.String() + "@it.qubar.local"
	}
	if err := db.Exec(`INSERT INTO domains.users (id, username, email) VALUES (?, 'it169 user', ?)`, id, email).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	testkit.CleanupRows(t, "domains.users", "id", id)
	testkit.CleanupKeys(t, redispkg.GetUserInfoKey(id), redispkg.GetUserJoinedCirclesKey(id))
	return id
}

func TestEmailInvite_EndToEnd(t *testing.T) {
	t.Parallel()
	env := setupEmailInvite(t)
	ctx := t.Context()
	local := "it169-" + sharedomain.NewID().String()
	stored := local + "@example.test"
	invitee := insertInviteUser(t, env.db, stored)
	other := insertInviteUser(t, env.db, "")
	unregistered := "it169-" + sharedomain.NewID().String() + "@example.test"

	res, err := env.svc.AddInviteEmails(ctx, env.owner, env.circleID,
		[]string{"  " + strings.ToUpper(local) + "@Example.TEST ", unregistered, "not-an-email"})
	if err != nil || len(res.Added) != 2 || len(res.Ignored) != 1 {
		t.Fatalf("add = %+v, %v; want 2 added / 1 ignored", res, err)
	}

	my, err := env.svc.ListMyInvites(ctx, invitee, 1, 20)
	if err != nil || len(my.Data) != 1 || my.Data[0].CircleID != env.circleID || my.Data[0].CircleName == "" {
		t.Fatalf("my invites = %+v, %v", my, err)
	}
	inviteID := my.Data[0].InviteID
	if mine, _ := env.svc.ListMyInvites(ctx, other, 1, 20); len(mine.Data) != 0 {
		t.Errorf("other user sees %d invites", len(mine.Data))
	}
	if _, err := env.svc.RespondInviteEmail(ctx, other, inviteID, true); !circleapp.IsInviteInvalidErr(err) {
		t.Errorf("other user accept err = %v, want invalid", err)
	}

	if visible, _ := env.vis.CanView(ctx, invitee, env.circleID); visible {
		t.Fatal("invitee sees private circle before accepting")
	}
	got, err := env.svc.RespondInviteEmail(ctx, invitee, inviteID, true)
	if err != nil || got.AlreadyMember {
		t.Fatalf("accept = %+v, %v", got, err)
	}
	if visible, _ := env.vis.CanView(ctx, invitee, env.circleID); !visible {
		t.Error("invitee cannot see private circle after accepting")
	}
	again, err := env.svc.RespondInviteEmail(ctx, invitee, inviteID, true)
	if err != nil || !again.AlreadyMember {
		t.Errorf("repeat accept = %+v, %v; want already member", again, err)
	}
	if mine, _ := env.svc.ListMyInvites(ctx, invitee, 1, 20); len(mine.Data) != 0 {
		t.Errorf("accepted invite still listed: %+v", mine.Data)
	}
	var status int16
	env.db.Raw("SELECT status FROM domains.circle_member WHERE circle_id = ? AND user_id = ?", env.circleID, invitee).Scan(&status)
	if status != circledomain.MemberStatusNormal {
		t.Errorf("member status = %d, want 1", status)
	}
}

func TestEmailInvite_BannedAndReject(t *testing.T) {
	t.Parallel()
	env := setupEmailInvite(t)
	ctx := t.Context()
	bannedEmail, rejectEmail := sharedomain.NewID().String()+"@it.qubar.local", sharedomain.NewID().String()+"@it.qubar.local"
	banned, rejecter := insertInviteUser(t, env.db, bannedEmail), insertInviteUser(t, env.db, rejectEmail)
	if err := env.db.Create(&circledomain.CircleMember{
		ID: sharedomain.NewID(), CircleID: env.circleID, UserID: banned,
		Role: circledomain.MemberRoleMember, Status: circledomain.MemberStatusBanned,
	}).Error; err != nil {
		t.Fatalf("seed banned member: %v", err)
	}
	if _, err := env.svc.AddInviteEmails(ctx, env.owner, env.circleID, []string{bannedEmail, rejectEmail}); err != nil {
		t.Fatalf("add: %v", err)
	}
	inviteOf := func(u uuid.UUID) uuid.UUID {
		my, err := env.svc.ListMyInvites(ctx, u, 1, 20)
		if err != nil || len(my.Data) != 1 {
			t.Fatalf("my invites for %s = %+v, %v", u, my, err)
		}
		return my.Data[0].InviteID
	}

	if _, err := env.svc.RespondInviteEmail(ctx, banned, inviteOf(banned), true); !circleapp.IsBannedFromCircleErr(err) {
		t.Errorf("banned accept err = %v", err)
	}
	inviteOf(banned) // 仍是待接受

	rid := inviteOf(rejecter)
	if _, err := env.svc.RespondInviteEmail(ctx, rejecter, rid, false); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if mine, _ := env.svc.ListMyInvites(ctx, rejecter, 1, 20); len(mine.Data) != 0 {
		t.Error("rejected invite still listed")
	}
	// 拒绝后再次添加 → 重新待接受。
	if res, err := env.svc.AddInviteEmails(ctx, env.owner, env.circleID, []string{rejectEmail}); err != nil || len(res.Added) != 1 {
		t.Errorf("re-add rejected = %+v, %v; want added", res, err)
	}
	inviteOf(rejecter)
}
