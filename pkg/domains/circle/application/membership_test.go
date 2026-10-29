// Package application 的加入 / 移出 / 加入方式校验单元测试（#168）。
//
// 覆盖：JoinCircle 成功时的计数副作用（Redis +1、事件 +1、joined ZSET 写入）与各类拒绝
// 不产生副作用；RemoveMember 权限矩阵（圈主 / 管理员 / 自己 / 圈主目标）、源状态校验与
// 计数副作用（与 BanMember 同链路）；创建 / 编辑拒绝 join_type=1。
// 状态机本身在 memberRepo.JoinCircle（PG），由 infrastructure 集成测试覆盖。
package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
)

// fakeCircleRepo 只放一个圈子的最小 CircleRepository fake。
type fakeCircleRepo struct {
	domain.CircleRepository
	circle  *domain.Circle
	updated *domain.CircleUpdateFields
	created *domain.Circle
}

func (r *fakeCircleRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Circle, error) {
	if r.circle == nil || r.circle.ID != id {
		return nil, domain.ErrCircleNotFound
	}
	c := *r.circle
	return &c, nil
}

func (r *fakeCircleRepo) ExistsByName(context.Context, string) (bool, error) { return false, nil }
func (r *fakeCircleRepo) ExistsBySlug(context.Context, string) (bool, error) { return false, nil }

func (r *fakeCircleRepo) Create(_ context.Context, c *domain.Circle) error {
	c.ID = uuid.New()
	r.created = c
	return nil
}

func (r *fakeCircleRepo) Update(_ context.Context, _ uuid.UUID, f domain.CircleUpdateFields) error {
	r.updated = &f
	return nil
}

// fakeMemberRepo 内存成员表（单圈，按 userID 索引）；JoinCircle 返回预置结果。
type fakeMemberRepo struct {
	domain.MemberRepository
	members map[uuid.UUID]*domain.CircleMember

	joinRet      *domain.CircleMember
	joinErr      error
	joinCalls    int
	gotJoinType  int16
	statusWrites int
}

func (r *fakeMemberRepo) GetMember(_ context.Context, _, userID uuid.UUID) (*domain.CircleMember, error) {
	m, ok := r.members[userID]
	if !ok {
		return nil, domain.ErrMemberNotFound
	}
	c := *m
	return &c, nil
}

func (r *fakeMemberRepo) UpdateMemberStatus(_ context.Context, _, userID uuid.UUID, from, to int16, _ time.Time) error {
	m, ok := r.members[userID]
	if !ok || m.Status != from {
		return domain.ErrMemberStateConflict
	}
	m.Status = to
	r.statusWrites++
	return nil
}

func (r *fakeMemberRepo) JoinCircle(_ context.Context, _, _ uuid.UUID, joinType int16) (*domain.CircleMember, error) {
	r.joinCalls++
	r.gotJoinType = joinType
	return r.joinRet, r.joinErr
}

// fakeStatsCache 记录 member_count 净变化（统计 Hash 视为已存在，不走 DB 恢复）。
type fakeStatsCache struct {
	domain.CircleStatsCache
	memberDelta int
}

func (c *fakeStatsCache) StatsExists(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (c *fakeStatsCache) IncrMemberCount(context.Context, uuid.UUID) error {
	c.memberDelta++
	return nil
}
func (c *fakeStatsCache) DecrMemberCount(context.Context, uuid.UUID) error {
	c.memberDelta--
	return nil
}

// fakePublisher 记录发布的成员计数事件。
type fakePublisher struct {
	domain.CircleEventPublisher
	deltas []int64
}

func (p *fakePublisher) PublishMemberCount(_ context.Context, _ uuid.UUID, delta int64) error {
	p.deltas = append(p.deltas, delta)
	return nil
}

// fakeBaseCache 基础信息缓存（编辑资料后失效用）。
type fakeBaseCache struct{ domain.CircleBaseCache }

func (fakeBaseCache) DeleteBase(context.Context, uuid.UUID) error { return nil }

// Add / Remove 补齐 joined_test.go 的 fakeJoinedCache（加入 / 移出写 ZSET）。
func (c *fakeJoinedCache) Add(_ context.Context, userID, circleID uuid.UUID, scoreMs int64) error {
	c.members[userID] = append(c.members[userID], domain.JoinedMember{CircleID: circleID, ScoreMs: scoreMs})
	return nil
}

func (c *fakeJoinedCache) Remove(_ context.Context, userID, circleID uuid.UUID) error {
	kept := c.members[userID][:0]
	for _, m := range c.members[userID] {
		if m.CircleID != circleID {
			kept = append(kept, m)
		}
	}
	c.members[userID] = kept
	return nil
}

func (c *fakeJoinedCache) has(userID, circleID uuid.UUID) bool {
	for _, m := range c.members[userID] {
		if m.CircleID == circleID {
			return true
		}
	}
	return false
}

type membershipFixture struct {
	svc       *circleServiceImpl
	circle    *domain.Circle
	circles   *fakeCircleRepo
	members   *fakeMemberRepo
	stats     *fakeStatsCache
	publisher *fakePublisher
	joined    *fakeJoinedCache
}

func newMembershipFixture(joinType int16) *membershipFixture {
	circle := &domain.Circle{ID: uuid.New(), JoinType: joinType, Status: domain.CircleStatusNormal}
	f := &membershipFixture{
		circle:    circle,
		circles:   &fakeCircleRepo{circle: circle},
		members:   &fakeMemberRepo{members: map[uuid.UUID]*domain.CircleMember{}},
		stats:     &fakeStatsCache{},
		publisher: &fakePublisher{},
		joined:    newFakeJoinedCache(),
	}
	f.svc = NewCircleService(f.circles, f.members, fakeBaseCache{}, f.stats, f.joined, nil, f.publisher, nil).(*circleServiceImpl)
	return f
}

// add 放入一个成员并把其 joined ZSET 标记为已预热（方便断言增删）。
func (f *membershipFixture) add(role, status int16) uuid.UUID {
	id := uuid.New()
	f.members.members[id] = &domain.CircleMember{CircleID: f.circle.ID, UserID: id, Role: role, Status: status}
	f.joined.warm[id] = true
	if status == domain.MemberStatusNormal || status == domain.MemberStatusMuted {
		f.joined.members[id] = []domain.JoinedMember{{CircleID: f.circle.ID, ScoreMs: 1}}
	}
	return id
}

// ===== JoinCircle =====

func TestJoinCircle_SuccessSideEffects(t *testing.T) {
	f := newMembershipFixture(domain.CircleJoinTypeDirect)
	user := uuid.New()
	f.joined.warm[user] = true
	joinedAt := time.Now().Add(-time.Hour)
	f.members.joinRet = &domain.CircleMember{UserID: user, Status: domain.MemberStatusNormal, CreateTime: joinedAt}

	if err := f.svc.JoinCircle(context.Background(), user, f.circle.ID); err != nil {
		t.Fatalf("JoinCircle: %v", err)
	}
	if f.members.gotJoinType != domain.CircleJoinTypeDirect {
		t.Errorf("repo got join_type=%d, want 0", f.members.gotJoinType)
	}
	if f.stats.memberDelta != 1 || len(f.publisher.deltas) != 1 || f.publisher.deltas[0] != 1 {
		t.Errorf("member delta=%d events=%v, want +1 / [1]", f.stats.memberDelta, f.publisher.deltas)
	}
	ms := f.joined.members[user]
	if len(ms) != 1 || ms[0].CircleID != f.circle.ID || ms[0].ScoreMs != joinedAt.UnixMilli() {
		t.Errorf("joined zset = %+v, want circle with score = member create_time", ms)
	}
}

// TestJoinCircle_RejectionsHaveNoSideEffects 仓储拒绝（已是成员 / 拉黑 / 私密）映射为可识别错误，
// 且不改计数、不发事件、不写 ZSET。
func TestJoinCircle_RejectionsHaveNoSideEffects(t *testing.T) {
	cases := []struct {
		name    string
		repoErr string
		is      func(error) bool
	}{
		{"already member", "user is already a member of this circle", IsAlreadyMemberErr},
		{"banned", "user is banned from this circle", IsBannedFromCircleErr},
		{"private", "this circle is private and requires invitation", IsPrivateCircleErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMembershipFixture(domain.CircleJoinTypePrivate)
			user := uuid.New()
			f.joined.warm[user] = true
			f.members.joinErr = errors.New(tc.repoErr)

			err := f.svc.JoinCircle(context.Background(), user, f.circle.ID)
			if !tc.is(err) {
				t.Fatalf("err = %v, want %s", err, tc.name)
			}
			if f.stats.memberDelta != 0 || len(f.publisher.deltas) != 0 || len(f.joined.members[user]) != 0 {
				t.Errorf("side effects: delta=%d events=%v zset=%v", f.stats.memberDelta, f.publisher.deltas, f.joined.members[user])
			}
		})
	}
}

func TestJoinCircle_CircleNotAvailable(t *testing.T) {
	f := newMembershipFixture(domain.CircleJoinTypeDirect)
	f.circle.Status = domain.CircleStatusBanned

	err := f.svc.JoinCircle(context.Background(), uuid.New(), f.circle.ID)
	if !IsCircleNotAvailableErr(err) {
		t.Fatalf("err = %v, want circle not available", err)
	}
	if f.members.joinCalls != 0 {
		t.Errorf("repo JoinCircle called %d times, want 0", f.members.joinCalls)
	}
}

// ===== RemoveMember =====

// TestRemoveMember_PermissionMatrix 权限与 ban 一致：圈主可移出管理员 / 成员，管理员只能移出成员，
// 不能移出自己和圈主，普通成员 / 非成员无权；被拒时不改状态、无副作用。
func TestRemoveMember_PermissionMatrix(t *testing.T) {
	cases := []struct {
		name       string
		opRole     int16 // 0 = 非成员
		targetRole int16
		self       bool
		wantErr    func(error) bool
	}{
		{"owner removes admin", domain.MemberRoleOwner, domain.MemberRoleAdmin, false, nil},
		{"owner removes member", domain.MemberRoleOwner, domain.MemberRoleMember, false, nil},
		{"admin removes member", domain.MemberRoleAdmin, domain.MemberRoleMember, false, nil},
		{"admin cannot remove admin", domain.MemberRoleAdmin, domain.MemberRoleAdmin, false, IsCannotManageTargetErr},
		{"admin cannot remove owner", domain.MemberRoleAdmin, domain.MemberRoleOwner, false, IsCannotManageTargetErr},
		{"owner cannot remove self", domain.MemberRoleOwner, 0, true, IsCannotManageTargetErr},
		{"admin cannot remove self", domain.MemberRoleAdmin, 0, true, IsCannotManageTargetErr},
		{"member has no permission", domain.MemberRoleMember, domain.MemberRoleMember, false, IsNotCircleAdminErr},
		{"non-member has no permission", 0, domain.MemberRoleMember, false, IsNotCircleAdminErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMembershipFixture(domain.CircleJoinTypeDirect)
			op := uuid.New()
			if tc.opRole != 0 {
				op = f.add(tc.opRole, domain.MemberStatusNormal)
			}
			target := op
			if !tc.self {
				target = f.add(tc.targetRole, domain.MemberStatusNormal)
			}

			err := f.svc.RemoveMember(context.Background(), op, f.circle.ID, target)
			if tc.wantErr != nil {
				if !tc.wantErr(err) {
					t.Fatalf("err = %v, want permission error", err)
				}
				if f.members.statusWrites != 0 || f.stats.memberDelta != 0 || len(f.publisher.deltas) != 0 {
					t.Errorf("rejected removal had side effects: writes=%d delta=%d events=%v",
						f.members.statusWrites, f.stats.memberDelta, f.publisher.deltas)
				}
				return
			}
			if err != nil {
				t.Fatalf("RemoveMember: %v", err)
			}
			if got := f.members.members[target].Status; got != domain.MemberStatusLeft {
				t.Errorf("target status = %d, want 4 (left)", got)
			}
		})
	}
}

// TestRemoveMember_SourceStatus 正常 / 禁言 → 已退出，计数 -1、事件 -1、移出 ZSET；
// 拉黑 / 已退出 → 409；不存在 → ErrMemberNotFound。
func TestRemoveMember_SourceStatus(t *testing.T) {
	for _, from := range []int16{domain.MemberStatusNormal, domain.MemberStatusMuted} {
		f := newMembershipFixture(domain.CircleJoinTypePrivate)
		owner := f.add(domain.MemberRoleOwner, domain.MemberStatusNormal)
		target := f.add(domain.MemberRoleMember, from)

		if err := f.svc.RemoveMember(context.Background(), owner, f.circle.ID, target); err != nil {
			t.Fatalf("from %d: RemoveMember: %v", from, err)
		}
		if got := f.members.members[target].Status; got != domain.MemberStatusLeft {
			t.Errorf("from %d: status = %d, want 4", from, got)
		}
		if f.stats.memberDelta != -1 || len(f.publisher.deltas) != 1 || f.publisher.deltas[0] != -1 {
			t.Errorf("from %d: delta=%d events=%v, want -1 / [-1]", from, f.stats.memberDelta, f.publisher.deltas)
		}
		if f.joined.has(target, f.circle.ID) {
			t.Errorf("from %d: circle still in target's joined zset", from)
		}
	}

	for _, from := range []int16{domain.MemberStatusBanned, domain.MemberStatusLeft} {
		f := newMembershipFixture(domain.CircleJoinTypeDirect)
		owner := f.add(domain.MemberRoleOwner, domain.MemberStatusNormal)
		target := f.add(domain.MemberRoleMember, from)

		err := f.svc.RemoveMember(context.Background(), owner, f.circle.ID, target)
		if !errors.Is(err, domain.ErrMemberStateConflict) {
			t.Errorf("from %d: err = %v, want state conflict", from, err)
		}
		if f.stats.memberDelta != 0 || len(f.publisher.deltas) != 0 {
			t.Errorf("from %d: side effects delta=%d events=%v", from, f.stats.memberDelta, f.publisher.deltas)
		}
	}

	f := newMembershipFixture(domain.CircleJoinTypeDirect)
	owner := f.add(domain.MemberRoleOwner, domain.MemberStatusNormal)
	if err := f.svc.RemoveMember(context.Background(), owner, f.circle.ID, uuid.New()); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("unknown target: err = %v, want ErrMemberNotFound", err)
	}
}

// TestBanMember_StillBans 拉黑与移出共用 evictMember：拉黑仍落 status=3 且计数 -1。
func TestBanMember_StillBans(t *testing.T) {
	f := newMembershipFixture(domain.CircleJoinTypeDirect)
	admin := f.add(domain.MemberRoleAdmin, domain.MemberStatusNormal)
	target := f.add(domain.MemberRoleMember, domain.MemberStatusMuted)

	if err := f.svc.BanMember(context.Background(), admin, f.circle.ID, target); err != nil {
		t.Fatalf("BanMember: %v", err)
	}
	if got := f.members.members[target].Status; got != domain.MemberStatusBanned {
		t.Errorf("status = %d, want 3", got)
	}
	if f.stats.memberDelta != -1 || len(f.publisher.deltas) != 1 {
		t.Errorf("delta=%d events=%v, want -1 / one event", f.stats.memberDelta, f.publisher.deltas)
	}
}

// ===== join_type 校验 =====

func TestCreateCircle_JoinTypeValidation(t *testing.T) {
	for _, tc := range []struct {
		joinType int16
		ok       bool
	}{{0, true}, {1, false}, {2, true}, {3, false}, {-1, false}} {
		f := newMembershipFixture(domain.CircleJoinTypeDirect)
		err := f.svc.CreateCircle(context.Background(), uuid.New(), CreateCircleInput{
			Name: "c", Description: "d", JoinType: tc.joinType,
		})
		if tc.ok && err != nil {
			t.Errorf("join_type=%d: unexpected err %v", tc.joinType, err)
		}
		if !tc.ok && !IsInvalidJoinTypeErr(err) {
			t.Errorf("join_type=%d: err = %v, want invalid join_type", tc.joinType, err)
		}
		if !tc.ok && f.circles.created != nil {
			t.Errorf("join_type=%d: circle created despite invalid join_type", tc.joinType)
		}
	}
}

func TestUpdateCircleProfile_JoinTypeValidation(t *testing.T) {
	for _, tc := range []struct {
		joinType int16
		ok       bool
	}{{0, true}, {1, false}, {2, true}, {3, false}} {
		f := newMembershipFixture(domain.CircleJoinTypeDirect)
		owner := f.add(domain.MemberRoleOwner, domain.MemberStatusNormal)
		jt := tc.joinType

		err := f.svc.UpdateCircleProfile(context.Background(), owner, f.circle.ID, UpdateCircleProfileInput{JoinType: &jt})
		if tc.ok && (err != nil || f.circles.updated == nil || *f.circles.updated.JoinType != jt) {
			t.Errorf("join_type=%d: err=%v updated=%+v", tc.joinType, err, f.circles.updated)
		}
		if !tc.ok && (!IsInvalidJoinTypeErr(err) || f.circles.updated != nil) {
			t.Errorf("join_type=%d: err=%v updated=%+v, want invalid join_type and no write", tc.joinType, err, f.circles.updated)
		}
	}
}
