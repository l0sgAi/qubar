//go:build integration

// joined_circles_integration_test.go 「我加入的圈子」列表对开发栈 PostgreSQL + Redis 的集成测试（make test-integration）：
// ZSET 重建口径含禁言（缓存 miss 后禁言圈子仍在列表）、角色筛选 keyset 翻页、成员关系批量回填。
// 开发栈无 CDC，ES 圈子文档不随 PG 同步：service 级用例以内存 searcher 代替 ES SearchMy。
package infrastructure_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/domains/circle/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// stubMySearcher 代替 ES：SearchMy 原样返回传入的全部圈子（视为正常、未删除、命中）。
type stubMySearcher struct {
	application.CircleSearcher
}

func (stubMySearcher) SearchMy(_ context.Context, circleIDs []uuid.UUID, _ string, size int, _ []interface{}) (*application.MyCircleSearchResult, error) {
	docs := make([]application.MyCircleDoc, 0, len(circleIDs))
	for _, id := range circleIDs {
		docs = append(docs, application.MyCircleDoc{ID: id.String(), Name: "it-" + id.String()})
	}
	return &application.MyCircleSearchResult{Circles: docs, Total: int64(len(docs)), Size: size}, nil
}

// joinedUser 随机用户，注册其成员行与 joined ZSET 的清理。
func joinedUser(t *testing.T) uuid.UUID {
	t.Helper()
	testkit.Redis(t)
	user := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.circle_member", "user_id", user)
	testkit.CleanupKeys(t, redispkg.GetUserJoinedCirclesKey(user))
	return user
}

// insertMember 直接插入一条成员行（随机圈子 ID，显式 create_time 控制加入顺序）。
func insertMember(t *testing.T, user uuid.UUID, role, status int16, muteEnd *time.Time, joined time.Time) domain.CircleMember {
	t.Helper()
	m := domain.CircleMember{
		ID: sharedomain.NewID(), CircleID: sharedomain.NewID(), UserID: user,
		Role: role, Status: status, MuteEndTime: muteEnd, CreateTime: joined, UpdateTime: joined,
	}
	db := testkit.PG(t)
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("insert member: %v", err)
	}
	// status=0（申请中）是零值，INSERT 会被 gorm default:1 标签替换成 1，这里显式写回。
	if status == domain.MemberStatusPending {
		if err := db.Exec("UPDATE domains.circle_member SET status = ? WHERE id = ?", status, m.ID).Error; err != nil {
			t.Fatalf("set pending status: %v", err)
		}
		m.Status = status
	}
	return m
}

func newJoinedService(t *testing.T) application.CircleService {
	t.Helper()
	db := testkit.PG(t)
	return application.NewCircleService(
		infrastructure.NewCircleRepository(db), infrastructure.NewMemberRepository(db),
		nil, nil, infrastructure.NewJoinedCirclesCache(), stubMySearcher{}, nil,
	)
}

// TestJoinedCircles_MutedSurvivesCacheRebuild 禁言成员的圈子在 ZSET 缓存重建后仍在「我的圈子」，
// 且带 member_status=2 / mute_end_time；拉黑 / 退出 / 申请中不出现。
func TestJoinedCircles_MutedSurvivesCacheRebuild(t *testing.T) {
	t.Parallel()
	user := joinedUser(t)
	svc := newJoinedService(t)
	ctx := t.Context()

	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	muteEnd := time.Now().Add(24 * time.Hour).Truncate(time.Microsecond)
	normal := insertMember(t, user, domain.MemberRoleOwner, domain.MemberStatusNormal, nil, base)
	muted := insertMember(t, user, domain.MemberRoleMember, domain.MemberStatusMuted, &muteEnd, base.Add(time.Minute))
	insertMember(t, user, domain.MemberRoleMember, domain.MemberStatusBanned, nil, base.Add(2*time.Minute))
	insertMember(t, user, domain.MemberRoleMember, domain.MemberStatusLeft, nil, base.Add(3*time.Minute))
	insertMember(t, user, domain.MemberRoleMember, domain.MemberStatusPending, nil, base.Add(4*time.Minute))

	// 先清 ZSET，强制走 ensureJoinedWarm → ListJoinedWithScore 重建。
	if err := redispkg.Client.Del(ctx, redispkg.GetUserJoinedCirclesKey(user)).Err(); err != nil {
		t.Fatalf("del joined key: %v", err)
	}
	res, err := svc.GetMyCircles(ctx, user, "", 0, 20, "")
	if err != nil {
		t.Fatalf("GetMyCircles: %v", err)
	}
	if res.Total != 2 || len(res.Circles) != 2 {
		t.Fatalf("total=%d circles=%+v, want the normal + muted circles", res.Total, res.Circles)
	}
	byID := map[string]application.MyCircleDoc{}
	for _, d := range res.Circles {
		byID[d.ID] = d
	}
	if d := byID[normal.CircleID.String()]; d.Role != domain.MemberRoleOwner || d.MemberStatus != domain.MemberStatusNormal {
		t.Errorf("normal circle = %+v", d)
	}
	d := byID[muted.CircleID.String()]
	if d.MemberStatus != domain.MemberStatusMuted || d.MuteEndTime == nil || !d.MuteEndTime.Equal(muteEnd) {
		t.Errorf("muted circle = %+v, want member_status=2 with mute_end_time", d)
	}

	// ZSET 本身含禁言圈子（推荐召回 ListJoinedCircleIDs 同口径）。
	ids, err := svc.ListJoinedCircleIDs(ctx, user, 50)
	if err != nil {
		t.Fatalf("ListJoinedCircleIDs: %v", err)
	}
	if !slices.Equal(ids, []uuid.UUID{muted.CircleID, normal.CircleID}) {
		t.Errorf("joined ids = %v, want [muted normal]", ids)
	}

	// 他人视角：有 role，无成员状态。
	other, err := svc.GetUserCircles(ctx, user, "", 0, 20, "")
	if err != nil {
		t.Fatalf("GetUserCircles: %v", err)
	}
	for _, d := range other.Circles {
		if d.Role == 0 || d.MemberStatus != 0 || d.MuteEndTime != nil {
			t.Errorf("GetUserCircles item = %+v, want role only", d)
		}
	}
}

// TestJoinedCircles_RoleFilterPaging 角色筛选走 PG keyset：按加入时间倒序翻页，
// 同一加入时刻按 circle_id 倒序决胜，不丢不重；total 为该角色加入数（含禁言，不含退出）。
func TestJoinedCircles_RoleFilterPaging(t *testing.T) {
	t.Parallel()
	user := joinedUser(t)
	svc := newJoinedService(t)
	ctx := t.Context()

	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	var members []domain.CircleMember
	for i := 0; i < 7; i++ {
		joined := base.Add(time.Duration(i) * time.Minute)
		if i >= 4 {
			joined = base.Add(10 * time.Minute) // 3 行同一时刻，检验 circle_id 决胜
		}
		status := int16(domain.MemberStatusNormal)
		if i == 2 {
			status = domain.MemberStatusMuted
		}
		members = append(members, insertMember(t, user, domain.MemberRoleMember, status, nil, joined))
		insertMember(t, user, domain.MemberRoleAdmin, domain.MemberStatusNormal, nil, joined)
	}
	insertMember(t, user, domain.MemberRoleMember, domain.MemberStatusLeft, nil, base.Add(20*time.Minute))

	slices.SortFunc(members, func(a, b domain.CircleMember) int {
		if c := b.CreateTime.Compare(a.CreateTime); c != 0 {
			return c
		}
		return slices.Compare(b.CircleID[:], a.CircleID[:])
	})
	var want []string
	for _, m := range members {
		want = append(want, m.CircleID.String())
	}

	var got []string
	cursor := ""
	pages := 0
	for {
		res, err := svc.GetMyCircles(ctx, user, "", domain.MemberRoleMember, 3, cursor)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		if res.Total != 7 {
			t.Fatalf("total = %d, want 7", res.Total)
		}
		for _, d := range res.Circles {
			if d.Role != domain.MemberRoleMember {
				t.Fatalf("role = %d, want 10", d.Role)
			}
			got = append(got, d.ID)
		}
		if res.SearchAfter == "" {
			break
		}
		cursor = res.SearchAfter
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3 (3+3+1)", pages)
	}
	if !slices.Equal(got, want) {
		t.Errorf("role paging =\n%v\nwant\n%v", got, want)
	}

	// 角色游标不能用于无筛选列表。
	first, err := svc.GetMyCircles(ctx, user, "", domain.MemberRoleMember, 3, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if _, err := svc.GetMyCircles(ctx, user, "", 0, 3, first.SearchAfter); !application.IsInvalidCursorErr(err) {
		t.Errorf("role cursor without role: err = %v, want invalid cursor", err)
	}

	// 角色筛选 + 关键字：同样翻页完整。
	got = got[:0]
	cursor = ""
	for i := 0; ; i++ {
		res, err := svc.GetMyCircles(ctx, user, "it", domain.MemberRoleAdmin, 2, cursor)
		if err != nil {
			t.Fatalf("keyword page %d: %v", i, err)
		}
		for _, d := range res.Circles {
			got = append(got, d.ID)
		}
		if res.SearchAfter == "" || i > 10 {
			break
		}
		cursor = res.SearchAfter
	}
	if len(got) != 7 {
		t.Errorf("role+keyword collected %d admin circles, want 7", len(got))
	}
}

// TestMemberRepo_ListJoinedMemberships 批量回填只返回已加入口径（正常 + 禁言）的成员行。
func TestMemberRepo_ListJoinedMemberships(t *testing.T) {
	t.Parallel()
	user := joinedUser(t)
	repo := infrastructure.NewMemberRepository(testkit.PG(t))

	now := time.Now().Truncate(time.Microsecond)
	muteEnd := now.Add(time.Hour)
	a := insertMember(t, user, domain.MemberRoleAdmin, domain.MemberStatusNormal, nil, now)
	b := insertMember(t, user, domain.MemberRoleMember, domain.MemberStatusMuted, &muteEnd, now)
	c := insertMember(t, user, domain.MemberRoleMember, domain.MemberStatusBanned, nil, now)

	got, err := repo.ListJoinedMemberships(t.Context(), user, []uuid.UUID{a.CircleID, b.CircleID, c.CircleID, sharedomain.NewID()})
	if err != nil {
		t.Fatalf("ListJoinedMemberships: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}
	if got[a.CircleID].Role != domain.MemberRoleAdmin {
		t.Errorf("a = %+v", got[a.CircleID])
	}
	if m := got[b.CircleID]; m.Status != domain.MemberStatusMuted || m.MuteEndTime == nil || !m.MuteEndTime.Equal(muteEnd) {
		t.Errorf("b = %+v", m)
	}
}
