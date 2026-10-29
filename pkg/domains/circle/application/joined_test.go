// Package application 的「我加入的圈子」列表单元测试（GetMyCircles / GetUserCircles）。
//
// 覆盖：slug / role / 成员状态回填、他人列表不含状态、禁言过期按正常返回、
// 一页一次成员查询（无 N+1）、ZSET 残留剔除、role 参数校验、角色筛选翻页
// （浏览 + 关键字，批内截断不丢不重）、rank / 角色游标不可混用。
// ZSET / ES / 成员仓储均为内存 fake，语义对齐真实实现。
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
)

// fakeJoinedCache 内存 ZSET：members 按 score 倒序取 rank。
type fakeJoinedCache struct {
	domain.JoinedCirclesCache
	members map[uuid.UUID][]domain.JoinedMember // userID → 成员
	warm    map[uuid.UUID]bool
}

func newFakeJoinedCache() *fakeJoinedCache {
	return &fakeJoinedCache{members: map[uuid.UUID][]domain.JoinedMember{}, warm: map[uuid.UUID]bool{}}
}

func (c *fakeJoinedCache) sorted(userID uuid.UUID) []domain.JoinedMember {
	ms := slices.Clone(c.members[userID])
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].ScoreMs > ms[j].ScoreMs })
	return ms
}

func (c *fakeJoinedCache) PageByRank(_ context.Context, userID uuid.UUID, start, limit int64) ([]uuid.UUID, error) {
	ms := c.sorted(userID)
	var ids []uuid.UUID
	for i := start; i < start+limit && i < int64(len(ms)); i++ {
		ids = append(ids, ms[i].CircleID)
	}
	return ids, nil
}

func (c *fakeJoinedCache) Card(_ context.Context, userID uuid.UUID) (int64, error) {
	return int64(len(c.members[userID])), nil
}

func (c *fakeJoinedCache) Exists(_ context.Context, userID uuid.UUID) (bool, error) {
	return c.warm[userID], nil
}

func (c *fakeJoinedCache) Rebuild(_ context.Context, userID uuid.UUID, members []domain.JoinedMember) error {
	c.members[userID] = slices.Clone(members)
	c.warm[userID] = len(members) > 0
	return nil
}

// fakeMySearcher 内存 ES：只返回 docs 中存在的圈子（模拟 status/deleted 过滤），
// keyword 按 name 子串匹配，排序 id 倒序（与 SearchMyCircles 浏览排序一致）。
type fakeMySearcher struct {
	CircleSearcher
	docs  map[uuid.UUID]MyCircleDoc
	calls int
}

func (s *fakeMySearcher) SearchMy(_ context.Context, circleIDs []uuid.UUID, keyword string, size int, _ []interface{}) (*MyCircleSearchResult, error) {
	s.calls++
	out := []MyCircleDoc{}
	for _, id := range circleIDs {
		d, ok := s.docs[id]
		if !ok || (keyword != "" && !strings.Contains(d.Name, keyword)) {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > size {
		out = out[:size]
	}
	return &MyCircleSearchResult{Circles: out, Total: int64(len(out)), Size: size}, nil
}

// fakeJoinedMemberRepo 内存 circle_member（单用户视角足够）。
type fakeJoinedMemberRepo struct {
	domain.MemberRepository
	rows            []domain.CircleMember
	membershipCalls int
}

func (r *fakeJoinedMemberRepo) joined(userID uuid.UUID) []domain.CircleMember {
	var out []domain.CircleMember
	for _, m := range r.rows {
		if m.UserID == userID && slices.Contains(domain.JoinedMemberStatuses, m.Status) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreateTime.Equal(out[j].CreateTime) {
			return out[i].CreateTime.After(out[j].CreateTime)
		}
		return out[i].CircleID.String() > out[j].CircleID.String()
	})
	return out
}

func (r *fakeJoinedMemberRepo) ListJoinedWithScore(_ context.Context, userID uuid.UUID, _ int) ([]domain.JoinedMember, error) {
	var out []domain.JoinedMember
	for _, m := range r.joined(userID) {
		out = append(out, domain.JoinedMember{CircleID: m.CircleID, ScoreMs: m.CreateTime.UnixMilli()})
	}
	return out, nil
}

func (r *fakeJoinedMemberRepo) ListJoinedMemberships(_ context.Context, userID uuid.UUID, circleIDs []uuid.UUID) (map[uuid.UUID]domain.CircleMember, error) {
	r.membershipCalls++
	out := map[uuid.UUID]domain.CircleMember{}
	for _, m := range r.joined(userID) {
		if slices.Contains(circleIDs, m.CircleID) {
			out[m.CircleID] = m
		}
	}
	return out, nil
}

func (r *fakeJoinedMemberRepo) ListJoinedByRole(_ context.Context, userID uuid.UUID, role int16, after *domain.JoinedKey, limit int) ([]domain.CircleMember, error) {
	var out []domain.CircleMember
	for _, m := range r.joined(userID) {
		if m.Role != role {
			continue
		}
		if after != nil && !keysetBefore(m, *after) {
			continue
		}
		out = append(out, m)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// keysetBefore (create_time, circle_id) < after（与 PG 行值比较语义一致）。
func keysetBefore(m domain.CircleMember, after domain.JoinedKey) bool {
	if !m.CreateTime.Equal(after.CreateTime) {
		return m.CreateTime.Before(after.CreateTime)
	}
	return m.CircleID.String() < after.CircleID.String()
}

func (r *fakeJoinedMemberRepo) CountJoinedByRole(_ context.Context, userID uuid.UUID, role int16) (int64, error) {
	var n int64
	for _, m := range r.joined(userID) {
		if m.Role == role {
			n++
		}
	}
	return n, nil
}

// joinedFixture 一个用户 + 若干圈子（ES 文档 + 成员行）。
type joinedFixture struct {
	user     uuid.UUID
	repo     *fakeJoinedMemberRepo
	searcher *fakeMySearcher
	cache    *fakeJoinedCache
	svc      *circleServiceImpl
	base     time.Time
	n        int
}

func newJoinedFixture() *joinedFixture {
	f := &joinedFixture{
		user:     uuid.New(),
		repo:     &fakeJoinedMemberRepo{},
		searcher: &fakeMySearcher{docs: map[uuid.UUID]MyCircleDoc{}},
		cache:    newFakeJoinedCache(),
		base:     time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	f.svc = NewCircleService(nil, f.repo, nil, nil, f.cache, f.searcher, nil, nil).(*circleServiceImpl)
	return f
}

// add 加入一个圈子（加入时间按调用顺序递增），inES=false 模拟 ES 过滤掉的圈子。
func (f *joinedFixture) add(name string, role, status int16, muteEnd *time.Time, inES bool) uuid.UUID {
	f.n++
	id := uuid.New()
	f.repo.rows = append(f.repo.rows, domain.CircleMember{
		ID: uuid.New(), CircleID: id, UserID: f.user, Role: role, Status: status,
		MuteEndTime: muteEnd, CreateTime: f.base.Add(time.Duration(f.n) * time.Minute),
	})
	if inES {
		f.searcher.docs[id] = MyCircleDoc{ID: id.String(), Name: name, Slug: "s-" + name, MemberCount: 3}
	}
	return id
}

func ptrTime(t time.Time) *time.Time { return &t }

func docByID(docs []MyCircleDoc, id uuid.UUID) (MyCircleDoc, bool) {
	for _, d := range docs {
		if d.ID == id.String() {
			return d, true
		}
	}
	return MyCircleDoc{}, false
}

// TestGetMyCircles_FillsMembership 本人列表回填 slug / role / 成员状态；
// 禁言生效返回截止时间，过期禁言按正常且不带截止时间；一页只查一次成员表。
func TestGetMyCircles_FillsMembership(t *testing.T) {
	f := newJoinedFixture()
	owner := f.add("owner", domain.MemberRoleOwner, domain.MemberStatusNormal, nil, true)
	future := time.Now().Add(2 * time.Hour)
	muted := f.add("muted", domain.MemberRoleMember, domain.MemberStatusMuted, ptrTime(future), true)
	expired := f.add("expired", domain.MemberRoleAdmin, domain.MemberStatusMuted, ptrTime(time.Now().Add(-time.Hour)), true)

	res, err := f.svc.GetMyCircles(context.Background(), f.user, "", 0, 20, "")
	if err != nil {
		t.Fatalf("GetMyCircles: %v", err)
	}
	if len(res.Circles) != 3 || res.Total != 3 {
		t.Fatalf("got %d circles total=%d, want 3/3", len(res.Circles), res.Total)
	}
	if f.repo.membershipCalls != 1 {
		t.Fatalf("membership queries = %d, want 1 (no N+1)", f.repo.membershipCalls)
	}

	d, _ := docByID(res.Circles, owner)
	if d.Slug != "s-owner" || d.Role != domain.MemberRoleOwner || d.MemberStatus != domain.MemberStatusNormal || d.MuteEndTime != nil {
		t.Errorf("owner doc = %+v", d)
	}
	d, _ = docByID(res.Circles, muted)
	if d.Role != domain.MemberRoleMember || d.MemberStatus != domain.MemberStatusMuted || d.MuteEndTime == nil || !d.MuteEndTime.Equal(future) {
		t.Errorf("muted doc = %+v", d)
	}
	d, _ = docByID(res.Circles, expired)
	if d.Role != domain.MemberRoleAdmin || d.MemberStatus != domain.MemberStatusNormal || d.MuteEndTime != nil {
		t.Errorf("expired-mute doc should read as normal without mute_end_time, got %+v", d)
	}
}

// TestGetUserCircles_NoMemberStatus 他人列表回填 role，但 member_status / mute_end_time 不出现在 JSON。
func TestGetUserCircles_NoMemberStatus(t *testing.T) {
	f := newJoinedFixture()
	f.add("muted", domain.MemberRoleAdmin, domain.MemberStatusMuted, ptrTime(time.Now().Add(time.Hour)), true)

	res, err := f.svc.GetUserCircles(context.Background(), f.user, "", 0, 20, "")
	if err != nil {
		t.Fatalf("GetUserCircles: %v", err)
	}
	if len(res.Circles) != 1 || res.Circles[0].Role != domain.MemberRoleAdmin {
		t.Fatalf("circles = %+v", res.Circles)
	}
	b, _ := json.Marshal(res.Circles[0])
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"member_status", "mute_end_time"} {
		if _, ok := m[k]; ok {
			t.Errorf("GET /circle/user item must not contain %q: %s", k, b)
		}
	}
	if m["role"].(float64) != float64(domain.MemberRoleAdmin) || m["slug"] != "s-muted" {
		t.Errorf("role/slug missing: %s", b)
	}
}

// TestGetMyCircles_MutedSurvivesRebuild ZSET 重建口径含禁言：缓存 miss 后禁言圈子仍在列表。
func TestGetMyCircles_MutedSurvivesRebuild(t *testing.T) {
	f := newJoinedFixture()
	muted := f.add("muted", domain.MemberRoleMember, domain.MemberStatusMuted, ptrTime(time.Now().Add(time.Hour)), true)
	f.add("left", domain.MemberRoleMember, domain.MemberStatusLeft, nil, true)
	f.add("banned", domain.MemberRoleMember, domain.MemberStatusBanned, nil, true)
	f.add("pending", domain.MemberRoleMember, domain.MemberStatusPending, nil, true)

	res, err := f.svc.GetMyCircles(context.Background(), f.user, "", 0, 20, "")
	if err != nil {
		t.Fatalf("GetMyCircles: %v", err)
	}
	if len(res.Circles) != 1 || res.Circles[0].ID != muted.String() || res.Total != 1 {
		t.Fatalf("want only the muted circle, got %+v total=%d", res.Circles, res.Total)
	}
}

// TestGetMyCircles_DropsStaleZSetEntry ZSET 残留（成员关系已不在已加入口径）的圈子被剔除。
func TestGetMyCircles_DropsStaleZSetEntry(t *testing.T) {
	f := newJoinedFixture()
	keep := f.add("keep", domain.MemberRoleMember, domain.MemberStatusNormal, nil, true)
	stale := f.add("stale", domain.MemberRoleMember, domain.MemberStatusNormal, nil, true)
	if _, err := f.svc.GetMyCircles(context.Background(), f.user, "", 0, 20, ""); err != nil {
		t.Fatalf("warm: %v", err)
	}
	f.repo.rows[1].Status = domain.MemberStatusLeft // 退出但 ZSET 未移除

	res, err := f.svc.GetMyCircles(context.Background(), f.user, "", 0, 20, "")
	if err != nil {
		t.Fatalf("GetMyCircles: %v", err)
	}
	if _, ok := docByID(res.Circles, stale); ok {
		t.Fatalf("stale circle should be dropped: %+v", res.Circles)
	}
	if _, ok := docByID(res.Circles, keep); !ok {
		t.Fatalf("kept circle missing: %+v", res.Circles)
	}
}

// TestJoinedCircles_InvalidRole role 只接受 0/10/20/30。
func TestJoinedCircles_InvalidRole(t *testing.T) {
	f := newJoinedFixture()
	for _, role := range []int16{-1, 1, 15, 31, 40} {
		if _, err := f.svc.GetMyCircles(context.Background(), f.user, "", role, 20, ""); !IsInvalidJoinedRoleFilterErr(err) {
			t.Errorf("GetMyCircles role=%d err = %v, want invalid role filter", role, err)
		}
		if _, err := f.svc.GetUserCircles(context.Background(), f.user, "", role, 20, ""); !IsInvalidJoinedRoleFilterErr(err) {
			t.Errorf("GetUserCircles role=%d err = %v, want invalid role filter", role, err)
		}
	}
}

// collectPages 用 search_after 翻页直到结束，返回全部 ID（按返回顺序）与每页结果。
func collectPages(t *testing.T, f *joinedFixture, keyword string, role int16, size int) ([]string, []*MyCircleSearchResult) {
	t.Helper()
	var ids []string
	var pages []*MyCircleSearchResult
	cursor := ""
	for i := 0; i < 100; i++ {
		res, err := f.svc.GetMyCircles(context.Background(), f.user, keyword, role, size, cursor)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		if len(res.Circles) > size {
			t.Fatalf("page %d has %d items > size %d", i, len(res.Circles), size)
		}
		pages = append(pages, res)
		for _, d := range res.Circles {
			ids = append(ids, d.ID)
		}
		if res.SearchAfter == "" {
			return ids, pages
		}
		cursor = res.SearchAfter
	}
	t.Fatal("pagination did not terminate")
	return nil, nil
}

// TestGetMyCircles_RoleFilterPaging 角色筛选浏览：只返回该角色、按加入时间倒序、
// 翻页不丢不重、total 为该角色加入数、ES 过滤掉的圈子不出现。
func TestGetMyCircles_RoleFilterPaging(t *testing.T) {
	f := newJoinedFixture()
	var want []string
	for i := 0; i < 11; i++ {
		id := f.add(fmt.Sprintf("m%d", i), domain.MemberRoleMember, domain.MemberStatusNormal, nil, i != 4)
		if i != 4 {
			want = append(want, id.String())
		}
		f.add(fmt.Sprintf("a%d", i), domain.MemberRoleAdmin, domain.MemberStatusNormal, nil, true)
	}
	f.add("left", domain.MemberRoleMember, domain.MemberStatusLeft, nil, true)
	slices.Reverse(want) // 加入时间倒序

	got, pages := collectPages(t, f, "", domain.MemberRoleMember, 4)
	if !slices.Equal(got, want) {
		t.Fatalf("role paging ids =\n%v\nwant\n%v", got, want)
	}
	for _, p := range pages {
		if p.Total != 11 {
			t.Fatalf("total = %d, want 11", p.Total)
		}
		for _, d := range p.Circles {
			if d.Role != domain.MemberRoleMember || d.MemberStatus != domain.MemberStatusNormal {
				t.Fatalf("unexpected item %+v", d)
			}
		}
	}
	if len(pages) != 3 {
		t.Fatalf("pages = %d, want 3 (4+4+3 rows, no trailing empty page)", len(pages))
	}
}

// TestGetMyCircles_RoleFilterKeywordPaging 角色筛选 + 关键字：批内命中超过 size 时截断，
// 游标落在最后返回的命中上，翻页不丢不重；total 恒 0。
func TestGetMyCircles_RoleFilterKeywordPaging(t *testing.T) {
	f := newJoinedFixture()
	var want []string
	for i := 0; i < 9; i++ {
		name := fmt.Sprintf("other%d", i)
		if i%3 != 0 {
			name = fmt.Sprintf("hit%d", i)
		}
		id := f.add(name, domain.MemberRoleOwner, domain.MemberStatusNormal, nil, true)
		if i%3 != 0 {
			want = append(want, id.String())
		}
	}
	f.add("hit-member", domain.MemberRoleMember, domain.MemberStatusNormal, nil, true)
	slices.Reverse(want)

	got, pages := collectPages(t, f, "hit", domain.MemberRoleOwner, 4)
	if !slices.Equal(got, want) {
		t.Fatalf("role+keyword ids =\n%v\nwant\n%v", got, want)
	}
	for _, p := range pages {
		if p.Total != 0 || p.Truncated {
			t.Fatalf("keyword page total=%d truncated=%v, want 0/false", p.Total, p.Truncated)
		}
	}
}

// TestGetMyCircles_NoRoleUnchanged 不带 role 时仍走 ZSET rank 游标，total = ZSET 基数。
func TestGetMyCircles_NoRoleUnchanged(t *testing.T) {
	f := newJoinedFixture()
	for i := 0; i < 5; i++ {
		f.add(fmt.Sprintf("c%d", i), domain.MemberRoleMember, domain.MemberStatusNormal, nil, true)
	}
	res, err := f.svc.GetMyCircles(context.Background(), f.user, "", 0, 2, "")
	if err != nil {
		t.Fatalf("GetMyCircles: %v", err)
	}
	if res.Total != 5 || len(res.Circles) != 2 || res.SearchAfter != encodeJoinedCursor(2) {
		t.Fatalf("got total=%d len=%d cursor=%q", res.Total, len(res.Circles), res.SearchAfter)
	}
}

// TestJoinedCircles_CursorKindMismatch rank 游标与角色游标不可混用，换角色也判非法。
func TestJoinedCircles_CursorKindMismatch(t *testing.T) {
	f := newJoinedFixture()
	roleCursor := encodeJoinedRoleKey(domain.MemberRoleAdmin, domain.JoinedKey{CreateTime: f.base, CircleID: uuid.New()})
	cases := []struct {
		name   string
		role   int16
		cursor string
	}{
		{"rank cursor with role", domain.MemberRoleAdmin, encodeJoinedCursor(20)},
		{"role cursor without role", 0, roleCursor},
		{"role cursor with other role", domain.MemberRoleOwner, roleCursor},
		{"garbage", 0, "!!!"},
		{"unknown kind", 0, "eyJrIjoieCJ9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.svc.GetMyCircles(context.Background(), f.user, "", tc.role, 20, tc.cursor); !IsInvalidCursorErr(err) {
				t.Fatalf("err = %v, want invalid cursor", err)
			}
		})
	}
	if _, err := f.svc.GetMyCircles(context.Background(), f.user, "", domain.MemberRoleAdmin, 20, roleCursor); err != nil {
		t.Fatalf("matching role cursor: %v", err)
	}
}

// TestDecodeJoinedCursor_RoundTrip 两种游标编解码往返（角色游标保留微秒精度）。
func TestDecodeJoinedCursor_RoundTrip(t *testing.T) {
	c, err := decodeJoinedCursor(encodeJoinedCursor(40))
	if err != nil || c.rank != 40 || c.role != 0 || c.key != nil {
		t.Fatalf("rank cursor = %+v, %v", c, err)
	}
	k := domain.JoinedKey{CreateTime: time.UnixMicro(1_790_000_000_123_456), CircleID: uuid.New()}
	c, err = decodeJoinedCursor(encodeJoinedRoleKey(domain.MemberRoleMember, k))
	if err != nil || c.role != domain.MemberRoleMember || c.key == nil ||
		!c.key.CreateTime.Equal(k.CreateTime) || c.key.CircleID != k.CircleID {
		t.Fatalf("role cursor = %+v, %v", c, err)
	}
}
