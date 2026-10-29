//go:build integration

// circle_join_integration_test.go 加入 / 移出状态机对开发栈 PostgreSQL + Redis 的集成测试（#168）：
// POST /circle/join 按 join_type × 现有成员状态逐条覆盖（含三种绕过审核 / 邀请的回归场景）、
// 计数副作用（Redis member_count、计数事件、joined ZSET）、并发加入只成功一次、
// 移出后公开圈可再加入而私密圈不行。service 用真实 PG 仓储 + Redis 缓存，事件发布用内存记录器。
package infrastructure_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/domains/circle/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// recordingPublisher 记录成员计数事件（代替 Redpanda）。
type recordingPublisher struct {
	mu     sync.Mutex
	deltas []int64
}

func (p *recordingPublisher) PublishMemberCount(_ context.Context, _ uuid.UUID, delta int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deltas = append(p.deltas, delta)
	return nil
}

func (p *recordingPublisher) PublishPostCount(context.Context, uuid.UUID) error { return nil }

func (p *recordingPublisher) sum() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var s int64
	for _, d := range p.deltas {
		s += d
	}
	return s
}

type joinFixture struct {
	svc       application.CircleService
	members   domain.MemberRepository
	stats     domain.CircleStatsCache
	joined    domain.JoinedCirclesCache
	publisher *recordingPublisher
	circleID  uuid.UUID
	ownerID   uuid.UUID
}

// newJoinFixture 建一个指定 join_type 的圈子（圈主随机），注册圈子 / 成员行与 Redis 键清理。
func newJoinFixture(t *testing.T, joinType int16) *joinFixture {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	ctx := t.Context()

	c := newCircle(t, "")
	if err := infrastructure.NewCircleRepository(db).Create(ctx, c); err != nil {
		t.Fatalf("create circle: %v", err)
	}
	// join_type=0 是零值会走列默认值，统一显式写回（含存量 1 的兼容场景）。
	if err := db.Exec("UPDATE domains.circle SET join_type = ?, member_count = 1 WHERE id = ?", joinType, c.ID).Error; err != nil {
		t.Fatalf("set join_type: %v", err)
	}
	testkit.CleanupKeys(t, redispkg.GetCircleStatsKey(c.ID), redispkg.GetCircleInfoKey(c.ID))

	f := &joinFixture{
		members:   infrastructure.NewMemberRepository(db),
		stats:     infrastructure.NewCircleStatsCache(),
		joined:    infrastructure.NewJoinedCirclesCache(),
		publisher: &recordingPublisher{},
		circleID:  c.ID,
		ownerID:   c.CreatorID,
	}
	testkit.CleanupKeys(t, redispkg.GetUserJoinedCirclesKey(f.ownerID))
	f.svc = application.NewCircleService(
		infrastructure.NewCircleRepository(db), f.members,
		infrastructure.NewCircleBaseCache(), f.stats, f.joined, nil, f.publisher, infrastructure.NewPrivateCircleCache(),
	)
	return f
}

// user 随机用户，注册其 joined ZSET 清理并预热（Rebuild 空集不建键，用占位成员让 Exists=true）。
func (f *joinFixture) user(t *testing.T) uuid.UUID {
	t.Helper()
	u := sharedomain.NewID()
	key := redispkg.GetUserJoinedCirclesKey(u)
	testkit.CleanupKeys(t, key)
	if err := f.joined.Rebuild(t.Context(), u, []domain.JoinedMember{{CircleID: sharedomain.NewID(), ScoreMs: 1}}); err != nil {
		t.Fatalf("warm joined zset: %v", err)
	}
	return u
}

// seed 直接写一条成员行（status=0 是零值，INSERT 会被 gorm default:1 替换，显式写回）。
func (f *joinFixture) seed(t *testing.T, userID uuid.UUID, role, status int16) {
	t.Helper()
	db := testkit.PG(t)
	m := domain.CircleMember{ID: sharedomain.NewID(), CircleID: f.circleID, UserID: userID, Role: role, Status: status}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := db.Exec("UPDATE domains.circle_member SET status = ? WHERE id = ?", status, m.ID).Error; err != nil {
		t.Fatalf("seed member status: %v", err)
	}
}

// status 成员当前状态；无记录返回 -1。
func (f *joinFixture) status(t *testing.T, userID uuid.UUID) int16 {
	t.Helper()
	m, err := f.members.GetMember(t.Context(), f.circleID, userID)
	if errors.Is(err, domain.ErrMemberNotFound) {
		return -1
	}
	if err != nil {
		t.Fatalf("get member: %v", err)
	}
	return m.Status
}

func (f *joinFixture) memberCount(t *testing.T) int {
	t.Helper()
	s, err := f.stats.GetStats(t.Context(), f.circleID)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if s == nil {
		return 1 // 统计 Hash 未建：等于建圈时的 member_count
	}
	return s.MemberCount
}

func (f *joinFixture) inJoined(t *testing.T, userID uuid.UUID) bool {
	t.Helper()
	_, err := redispkg.Client.ZScore(t.Context(), redispkg.GetUserJoinedCirclesKey(userID), f.circleID.String()).Result()
	return err == nil
}

const noRecord int16 = -1

// TestJoinCircle_StateMachine POST /circle/join 的完整状态机：join_type ∈ {0, 1(存量), 2} × 现有状态。
// 公开圈：无记录 / 已退出（含存量待审）→ 正常；非公开圈：无记录 / 已退出 / 待审一律私密错误且不改数据。
// 「bypass#」用例是 #168 的三种绕过回归：待审核自助通过、被拒绝（审核拒绝落 4）重进、退出 / 解黑后回私密圈。
func TestJoinCircle_StateMachine(t *testing.T) {
	t.Parallel()
	type tc struct {
		name     string
		joinType int16
		from     int16 // noRecord = 无成员记录
		wantErr  func(error) bool
		want     int16 // 期望的最终状态（noRecord = 仍无记录）
	}
	cases := []tc{
		{"public/new", domain.CircleJoinTypeDirect, noRecord, nil, domain.MemberStatusNormal},
		{"public/left rejoins", domain.CircleJoinTypeDirect, domain.MemberStatusLeft, nil, domain.MemberStatusNormal},
		{"public/legacy pending treated as left", domain.CircleJoinTypeDirect, domain.MemberStatusPending, nil, domain.MemberStatusNormal},
		{"public/normal", domain.CircleJoinTypeDirect, domain.MemberStatusNormal, application.IsAlreadyMemberErr, domain.MemberStatusNormal},
		{"public/muted", domain.CircleJoinTypeDirect, domain.MemberStatusMuted, application.IsAlreadyMemberErr, domain.MemberStatusMuted},
		{"public/banned", domain.CircleJoinTypeDirect, domain.MemberStatusBanned, application.IsBannedFromCircleErr, domain.MemberStatusBanned},

		{"private/new", domain.CircleJoinTypePrivate, noRecord, application.IsPrivateCircleErr, noRecord},
		{"private/normal", domain.CircleJoinTypePrivate, domain.MemberStatusNormal, application.IsAlreadyMemberErr, domain.MemberStatusNormal},
		{"private/muted", domain.CircleJoinTypePrivate, domain.MemberStatusMuted, application.IsAlreadyMemberErr, domain.MemberStatusMuted},
		{"private/banned", domain.CircleJoinTypePrivate, domain.MemberStatusBanned, application.IsBannedFromCircleErr, domain.MemberStatusBanned},
		{"bypass#1 private/pending cannot self-approve", domain.CircleJoinTypePrivate, domain.MemberStatusPending, application.IsPrivateCircleErr, domain.MemberStatusPending},
		{"bypass#2+3 private/left cannot rejoin without invite", domain.CircleJoinTypePrivate, domain.MemberStatusLeft, application.IsPrivateCircleErr, domain.MemberStatusLeft},

		// 存量 join_type=1（迁移前 / 迁移窗口内）按非公开处理，不再产生待审核记录。
		{"legacy approval/new", domain.CircleJoinTypeApproval, noRecord, application.IsPrivateCircleErr, noRecord},
		{"bypass#1 legacy approval/pending cannot self-approve", domain.CircleJoinTypeApproval, domain.MemberStatusPending, application.IsPrivateCircleErr, domain.MemberStatusPending},
		{"bypass#2 legacy approval/rejected cannot rejoin", domain.CircleJoinTypeApproval, domain.MemberStatusLeft, application.IsPrivateCircleErr, domain.MemberStatusLeft},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newJoinFixture(t, c.joinType)
			u := f.user(t)
			if c.from != noRecord {
				f.seed(t, u, domain.MemberRoleMember, c.from)
			}

			err := f.svc.JoinCircle(t.Context(), u, f.circleID)
			if got := f.status(t, u); got != c.want {
				t.Errorf("status after join = %d, want %d", got, c.want)
			}

			if c.wantErr != nil {
				if !c.wantErr(err) {
					t.Fatalf("err = %v, want rejection", err)
				}
				if f.memberCount(t) != 1 || f.publisher.sum() != 0 || f.inJoined(t, u) {
					t.Errorf("rejected join had side effects: count=%d events=%d inJoined=%v",
						f.memberCount(t), f.publisher.sum(), f.inJoined(t, u))
				}
				return
			}
			if err != nil {
				t.Fatalf("JoinCircle: %v", err)
			}
			if f.memberCount(t) != 2 || f.publisher.sum() != 1 || !f.inJoined(t, u) {
				t.Errorf("join side effects: count=%d events=%d inJoined=%v, want 2 / +1 / true",
					f.memberCount(t), f.publisher.sum(), f.inJoined(t, u))
			}
		})
	}
}

// TestJoinCircle_ConcurrentJoinCountsOnce 并发重复加入（首次插入撞唯一索引 / 已退出 CAS 回圈）只成功一次，
// 其余返回已是成员，计数只 +1。
func TestJoinCircle_ConcurrentJoinCountsOnce(t *testing.T) {
	t.Parallel()
	for _, from := range []int16{noRecord, domain.MemberStatusLeft} {
		f := newJoinFixture(t, domain.CircleJoinTypeDirect)
		u := f.user(t)
		if from != noRecord {
			f.seed(t, u, domain.MemberRoleMember, from)
		}

		var ok, already atomic.Int32
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := f.svc.JoinCircle(context.WithoutCancel(t.Context()), u, f.circleID)
				switch {
				case err == nil:
					ok.Add(1)
				case application.IsAlreadyMemberErr(err):
					already.Add(1)
				default:
					t.Errorf("from %d: unexpected err %v", from, err)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 || already.Load() != 7 {
			t.Errorf("from %d: ok=%d already=%d, want 1 / 7", from, ok.Load(), already.Load())
		}
		if f.publisher.sum() != 1 || f.memberCount(t) != 2 {
			t.Errorf("from %d: events=%d count=%d, want +1 / 2", from, f.publisher.sum(), f.memberCount(t))
		}
	}
}

// TestRemoveMember_RejoinByJoinType 移出：正常 → 已退出，计数 -1、移出 ZSET；
// 之后公开圈可直接重新加入，私密圈仍需邀请（不改数据）。
func TestRemoveMember_RejoinByJoinType(t *testing.T) {
	t.Parallel()
	for _, jt := range []int16{domain.CircleJoinTypeDirect, domain.CircleJoinTypePrivate} {
		f := newJoinFixture(t, jt)
		u := f.user(t)
		f.seed(t, u, domain.MemberRoleMember, domain.MemberStatusNormal)
		if err := f.joined.Add(t.Context(), u, f.circleID, 1); err != nil {
			t.Fatalf("seed joined zset: %v", err)
		}

		if err := f.svc.RemoveMember(t.Context(), f.ownerID, f.circleID, u); err != nil {
			t.Fatalf("join_type=%d: RemoveMember: %v", jt, err)
		}
		if got := f.status(t, u); got != domain.MemberStatusLeft {
			t.Errorf("join_type=%d: status = %d, want 4", jt, got)
		}
		if f.memberCount(t) != 0 || f.publisher.sum() != -1 || f.inJoined(t, u) {
			t.Errorf("join_type=%d: count=%d events=%d inJoined=%v, want 0 / -1 / false",
				jt, f.memberCount(t), f.publisher.sum(), f.inJoined(t, u))
		}

		err := f.svc.JoinCircle(t.Context(), u, f.circleID)
		if jt == domain.CircleJoinTypeDirect {
			if err != nil || f.status(t, u) != domain.MemberStatusNormal {
				t.Errorf("public rejoin: err=%v status=%d, want ok / 1", err, f.status(t, u))
			}
			continue
		}
		if !application.IsPrivateCircleErr(err) || f.status(t, u) != domain.MemberStatusLeft {
			t.Errorf("private rejoin: err=%v status=%d, want private error / 4", err, f.status(t, u))
		}
	}
}

// TestRemoveMember_Permissions 管理员不能移出管理员 / 圈主，圈主可移出管理员；被拒不改状态。
func TestRemoveMember_Permissions(t *testing.T) {
	t.Parallel()
	f := newJoinFixture(t, domain.CircleJoinTypeDirect)
	admin, admin2 := f.user(t), f.user(t)
	f.seed(t, admin, domain.MemberRoleAdmin, domain.MemberStatusNormal)
	f.seed(t, admin2, domain.MemberRoleAdmin, domain.MemberStatusNormal)
	ctx := t.Context()

	if err := f.svc.RemoveMember(ctx, admin, f.circleID, admin2); !application.IsCannotManageTargetErr(err) {
		t.Errorf("admin removes admin: err = %v, want cannot manage", err)
	}
	if err := f.svc.RemoveMember(ctx, admin, f.circleID, f.ownerID); !application.IsCannotManageTargetErr(err) {
		t.Errorf("admin removes owner: err = %v, want cannot manage", err)
	}
	if err := f.svc.RemoveMember(ctx, f.ownerID, f.circleID, f.ownerID); !application.IsCannotManageTargetErr(err) {
		t.Errorf("owner removes self: err = %v, want cannot manage", err)
	}
	if f.status(t, admin2) != domain.MemberStatusNormal || f.status(t, f.ownerID) != domain.MemberStatusNormal {
		t.Fatal("rejected removals changed member status")
	}
	if err := f.svc.RemoveMember(ctx, f.ownerID, f.circleID, admin2); err != nil {
		t.Fatalf("owner removes admin: %v", err)
	}
	if got := f.status(t, admin2); got != domain.MemberStatusLeft {
		t.Errorf("removed admin status = %d, want 4", got)
	}
}

// TestUnbanThenJoinPrivate_StillNeedsInvite 回归 bypass#3：私密圈拉黑 → 解黑（落 4）→ 自助加入仍被拒。
func TestUnbanThenJoinPrivate_StillNeedsInvite(t *testing.T) {
	t.Parallel()
	f := newJoinFixture(t, domain.CircleJoinTypePrivate)
	u := f.user(t)
	f.seed(t, u, domain.MemberRoleMember, domain.MemberStatusNormal)
	ctx := t.Context()

	if err := f.svc.BanMember(ctx, f.ownerID, f.circleID, u); err != nil {
		t.Fatalf("ban: %v", err)
	}
	if err := f.svc.UnbanMember(ctx, f.ownerID, f.circleID, u); err != nil {
		t.Fatalf("unban: %v", err)
	}
	if err := f.svc.JoinCircle(ctx, u, f.circleID); !application.IsPrivateCircleErr(err) {
		t.Fatalf("join after unban: err = %v, want private error", err)
	}
	if got := f.status(t, u); got != domain.MemberStatusLeft {
		t.Errorf("status = %d, want 4", got)
	}
}
