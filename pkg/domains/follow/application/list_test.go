package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
)

type listKey struct {
	kind  domain.ListKind
	owner uuid.UUID
}

// fakeListRepo 内存版列表仓储：每个列表的关系按 (FollowTime DESC, ID DESC) 排好，keyset 语义与 PG 实现一致。
type fakeListRepo struct {
	edges     map[listKey][]domain.FollowEdge
	relations map[uuid.UUID]domain.Relation

	listErr, relErr error
	limits          []int
	relCalls        int
	relTargets      []uuid.UUID
}

func (r *fakeListRepo) ListEdges(
	_ context.Context, kind domain.ListKind, owner uuid.UUID, after *domain.FollowEdge, limit int,
) ([]domain.FollowEdge, error) {
	r.limits = append(r.limits, limit)
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []domain.FollowEdge
	for _, e := range r.edges[listKey{kind, owner}] {
		if after != nil && !edgeBefore(e, *after) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, e)
	}
	return out, nil
}

// edgeBefore e 是否排在 after 之后（即 (follow_time, id) 更小）。
func edgeBefore(e, after domain.FollowEdge) bool {
	if !e.FollowTime.Equal(after.FollowTime) {
		return e.FollowTime.Before(after.FollowTime)
	}
	return e.ID.String() < after.ID.String()
}

func (r *fakeListRepo) GetRelations(_ context.Context, _ uuid.UUID, targets []uuid.UUID) (map[uuid.UUID]domain.Relation, error) {
	r.relCalls++
	r.relTargets = targets
	if r.relErr != nil {
		return nil, r.relErr
	}
	out := map[uuid.UUID]domain.Relation{}
	for _, id := range targets {
		if rel, ok := r.relations[id]; ok {
			out[id] = rel
		}
	}
	return out, nil
}

// fakeBriefs missing 里的用户视为已禁用 / 已删除（不出现在返回里）。
type fakeBriefs struct {
	missing map[uuid.UUID]bool
	err     error
	calls   int
}

func (b *fakeBriefs) GetBriefs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]UserBrief, error) {
	b.calls++
	if b.err != nil {
		return nil, b.err
	}
	out := map[uuid.UUID]UserBrief{}
	for _, id := range ids {
		if !b.missing[id] {
			out[id] = UserBrief{ID: id.String(), Username: "u-" + id.String()[:8]}
		}
	}
	return out, nil
}

type fakeVisibility struct {
	private map[uuid.UUID]bool
	err     error
	calls   int
}

func (v *fakeVisibility) IsFollowListPrivate(_ context.Context, id uuid.UUID) (bool, error) {
	v.calls++
	return v.private[id], v.err
}

type listEnv struct {
	svc        FollowService
	repo       *fakeListRepo
	briefs     *fakeBriefs
	visibility *fakeVisibility
	users      *fakeUsers
}

func newListEnv() *listEnv {
	e := &listEnv{
		repo:       &fakeListRepo{edges: map[listKey][]domain.FollowEdge{}, relations: map[uuid.UUID]domain.Relation{}},
		briefs:     &fakeBriefs{missing: map[uuid.UUID]bool{}},
		visibility: &fakeVisibility{private: map[uuid.UUID]bool{}},
		users:      &fakeUsers{missing: map[uuid.UUID]bool{}},
	}
	rel := newFakeRepo()
	e.svc = NewFollowService(rel, rel, e.users, func() int { return 2000 }, nil,
		ListDeps{Repo: e.repo, Users: e.briefs, Visibility: e.visibility}, &fakeEvents{})
	return e
}

// seed 给 owner 的 kind 列表放 n 条关系，返回按列表顺序（最新在前）排列的对端用户 ID。
func (e *listEnv) seed(kind domain.ListKind, owner uuid.UUID, n int) []uuid.UUID {
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	edges := make([]domain.FollowEdge, n)
	for i := range edges {
		edges[i] = domain.FollowEdge{ID: uuid.New(), UserID: uuid.New(), FollowTime: base.Add(-time.Duration(i) * time.Minute)}
	}
	sort.SliceStable(edges, func(i, j int) bool { return edgeBefore(edges[j], edges[i]) })
	e.repo.edges[listKey{kind, owner}] = edges
	ids := make([]uuid.UUID, n)
	for i := range edges {
		ids[i] = edges[i].UserID
	}
	return ids
}

func itemIDs(vo *FollowListVO) []string {
	ids := make([]string, len(vo.Items))
	for i := range vo.Items {
		ids[i] = vo.Items[i].User.ID
	}
	return ids
}

func wantIDs(ids ...uuid.UUID) []string {
	out := make([]string, len(ids))
	for i := range ids {
		out[i] = ids[i].String()
	}
	return out
}

func TestList_PagesInOrderWithCursor(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []domain.ListKind{domain.ListFollowing, domain.ListFollowers, domain.ListMutual} {
		t.Run(string(kind), func(t *testing.T) {
			e := newListEnv()
			owner := uuid.New()
			ids := e.seed(kind, owner, 5)
			list := func(cursor string) *FollowListVO {
				t.Helper()
				var vo *FollowListVO
				var err error
				switch kind {
				case domain.ListFollowing:
					vo, err = e.svc.ListFollowing(ctx, uuid.Nil, owner, cursor, 2)
				case domain.ListFollowers:
					vo, err = e.svc.ListFollowers(ctx, uuid.Nil, owner, cursor, 2)
				default:
					vo, err = e.svc.ListMutual(ctx, owner, cursor, 2)
				}
				if err != nil {
					t.Fatalf("list(%q): %v", cursor, err)
				}
				return vo
			}

			p1 := list("")
			if fmt.Sprint(itemIDs(p1)) != fmt.Sprint(wantIDs(ids[0], ids[1])) || !p1.HasMore || p1.NextCursor == "" {
				t.Fatalf("page 1 = %v has_more=%v cursor=%q", itemIDs(p1), p1.HasMore, p1.NextCursor)
			}
			p2 := list(p1.NextCursor)
			if fmt.Sprint(itemIDs(p2)) != fmt.Sprint(wantIDs(ids[2], ids[3])) || !p2.HasMore {
				t.Fatalf("page 2 = %v has_more=%v", itemIDs(p2), p2.HasMore)
			}
			p3 := list(p2.NextCursor)
			if fmt.Sprint(itemIDs(p3)) != fmt.Sprint(wantIDs(ids[4])) || p3.HasMore || p3.NextCursor != "" {
				t.Fatalf("page 3 = %v has_more=%v cursor=%q, want last page", itemIDs(p3), p3.HasMore, p3.NextCursor)
			}
			if p1.Items[0].FollowTime.IsZero() {
				t.Error("follow_time must be filled")
			}
		})
	}
}

func TestList_EmptyAndExactPage(t *testing.T) {
	ctx := context.Background()
	e := newListEnv()
	owner := uuid.New()
	vo, err := e.svc.ListFollowing(ctx, uuid.Nil, owner, "", 10)
	if err != nil || vo.Items == nil || len(vo.Items) != 0 || vo.HasMore || vo.NextCursor != "" {
		t.Fatalf("empty list: vo=%+v err=%v, want empty non-nil items", vo, err)
	}
	if e.briefs.calls != 0 || e.repo.relCalls != 0 {
		t.Error("empty list must not hit briefs / relations")
	}
	// 恰好一页：没有下一页
	e.seed(domain.ListFollowing, owner, 3)
	if vo, err = e.svc.ListFollowing(ctx, uuid.Nil, owner, "", 3); err != nil || len(vo.Items) != 3 || vo.HasMore {
		t.Fatalf("exact page: vo=%+v err=%v", vo, err)
	}
}

func TestList_SizeNormalization(t *testing.T) {
	for size, want := range map[int]int{0: 20, -3: 20, 1: 1, 50: 50, 51: 50, 1000: 50} {
		e := newListEnv()
		if _, err := e.svc.ListFollowing(context.Background(), uuid.Nil, uuid.New(), "", size); err != nil {
			t.Fatal(err)
		}
		// 仓储每次多取一条用来判断 has_more
		if len(e.repo.limits) != 1 || e.repo.limits[0] != want+1 {
			t.Errorf("size %d: repo limits = %v, want [%d]", size, e.repo.limits, want+1)
		}
	}
}

func TestList_InvalidCursor(t *testing.T) {
	ctx := context.Background()
	e := newListEnv()
	owner, other := uuid.New(), uuid.New()
	e.seed(domain.ListFollowing, owner, 3)
	e.seed(domain.ListFollowers, owner, 3)
	e.seed(domain.ListFollowing, other, 3)
	p1, err := e.svc.ListFollowing(ctx, uuid.Nil, owner, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	valid := p1.NextCursor
	raw := func(json string) string { return base64.RawURLEncoding.EncodeToString([]byte(json)) }
	edgeID := uuid.New().String()

	for name, cursor := range map[string]string{
		"not base64":      "%%%",
		"not json":        raw("hello"),
		"wrong type":      raw(`{"v":"1"}`),
		"unknown version": raw(fmt.Sprintf(`{"v":2,"list":"following","scope_id":%q,"follow_time":1,"id":%q}`, owner, edgeID)),
		"no version":      raw(fmt.Sprintf(`{"list":"following","scope_id":%q,"follow_time":1,"id":%q}`, owner, edgeID)),
		"missing id":      raw(fmt.Sprintf(`{"v":1,"list":"following","scope_id":%q,"follow_time":1}`, owner)),
		"missing time":    raw(fmt.Sprintf(`{"v":1,"list":"following","scope_id":%q,"id":%q}`, owner, edgeID)),
		"bad scope":       raw(fmt.Sprintf(`{"v":1,"list":"following","scope_id":"x","follow_time":1,"id":%q}`, edgeID)),
	} {
		if _, err := e.svc.ListFollowing(ctx, uuid.Nil, owner, cursor, 1); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Errorf("%s: err = %v, want ErrInvalidCursor", name, err)
		}
	}
	// 作用域：别人的列表、同一个人的另一种列表都不能用
	if _, err := e.svc.ListFollowing(ctx, uuid.Nil, other, valid, 1); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Errorf("cursor on another user's list: err = %v, want ErrInvalidCursor", err)
	}
	if _, err := e.svc.ListFollowers(ctx, uuid.Nil, owner, valid, 1); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Errorf("following cursor on followers list: err = %v, want ErrInvalidCursor", err)
	}
	if _, err := e.svc.ListMutual(ctx, owner, valid, 1); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Errorf("following cursor on mutual list: err = %v, want ErrInvalidCursor", err)
	}
	if _, err := e.svc.ListFollowing(ctx, uuid.Nil, owner, valid, 1); err != nil {
		t.Errorf("valid cursor rejected: %v", err)
	}
}

func TestList_PrivacyAndMissingOwner(t *testing.T) {
	ctx := context.Background()
	e := newListEnv()
	owner, other := uuid.New(), uuid.New()
	e.seed(domain.ListFollowing, owner, 2)
	e.seed(domain.ListFollowers, owner, 2)
	e.seed(domain.ListMutual, owner, 1)
	e.visibility.private[owner] = true

	for name, list := range map[string]func(context.Context, uuid.UUID, uuid.UUID, string, int) (*FollowListVO, error){
		"following": e.svc.ListFollowing,
		"followers": e.svc.ListFollowers,
	} {
		for who, viewer := range map[string]uuid.UUID{"guest": uuid.Nil, "other": other} {
			if _, err := list(ctx, viewer, owner, "", 10); !errors.Is(err, domain.ErrFollowListPrivate) {
				t.Errorf("%s by %s: err = %v, want ErrFollowListPrivate", name, who, err)
			}
		}
		if vo, err := list(ctx, owner, owner, "", 10); err != nil || len(vo.Items) != 2 {
			t.Errorf("%s by owner: vo=%+v err=%v, want 2 items", name, vo, err)
		}
	}
	if len(e.repo.limits) != 2 {
		t.Errorf("repo list calls = %d, want 2 (denied requests must not read the list)", len(e.repo.limits))
	}
	// 本人看自己的列表不读隐私开关；互关列表不受开关影响
	calls := e.visibility.calls
	if vo, err := e.svc.ListMutual(ctx, owner, "", 10); err != nil || len(vo.Items) != 1 {
		t.Errorf("mutual with private lists: vo=%+v err=%v, want 1 item", vo, err)
	}
	if _, err := e.svc.ListFollowing(ctx, owner, owner, "", 10); err != nil || e.visibility.calls != calls {
		t.Errorf("owner path: err=%v visibility calls %d -> %d, want unchanged", err, calls, e.visibility.calls)
	}

	// stats：计数与 relation 不受影响，list_visible 反映开关
	for who, tc := range map[string]struct {
		viewer uuid.UUID
		want   bool
	}{"guest": {uuid.Nil, false}, "other": {other, false}, "owner": {owner, true}} {
		vo, err := e.svc.GetStats(ctx, tc.viewer, owner)
		if err != nil || vo.ListVisible != tc.want {
			t.Errorf("stats by %s: vo=%+v err=%v, want list_visible=%v", who, vo, err, tc.want)
		}
	}
	e.visibility.private[owner] = false
	if vo, err := e.svc.GetStats(ctx, uuid.Nil, owner); err != nil || !vo.ListVisible {
		t.Errorf("stats of public user: vo=%+v err=%v, want list_visible=true", vo, err)
	}

	// 目标用户不存在 / 已禁用 / 已删除：404 优先于隐私判断
	e.users.missing[owner] = true
	if _, err := e.svc.ListFollowers(ctx, other, owner, "", 10); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("missing owner: err = %v, want ErrUserNotFound", err)
	}
}

func TestList_RelationsBatchedPerPage(t *testing.T) {
	ctx := context.Background()
	e := newListEnv()
	owner, viewer := uuid.New(), uuid.New()
	ids := e.seed(domain.ListFollowers, owner, 4)
	e.repo.relations[ids[0]] = domain.RelationMutual
	e.repo.relations[ids[2]] = domain.RelationFollowedBy

	vo, err := e.svc.ListFollowers(ctx, viewer, owner, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if e.repo.relCalls != 1 || len(e.repo.relTargets) != 4 {
		t.Fatalf("relation queries = %d with %d targets, want 1 batch of 4", e.repo.relCalls, len(e.repo.relTargets))
	}
	got := []domain.Relation{vo.Items[0].Relation, vo.Items[1].Relation, vo.Items[2].Relation, vo.Items[3].Relation}
	if fmt.Sprint(got) != fmt.Sprint([]domain.Relation{3, 0, 2, 0}) {
		t.Errorf("relations = %v, want [3 0 2 0]", got)
	}

	// 访客：不查关系，全部为 0
	e.repo.relCalls = 0
	if vo, err = e.svc.ListFollowers(ctx, uuid.Nil, owner, "", 10); err != nil {
		t.Fatal(err)
	}
	if e.repo.relCalls != 0 || vo.Items[0].Relation != domain.RelationNone {
		t.Errorf("guest: relation queries=%d first relation=%d, want 0 / 0", e.repo.relCalls, vo.Items[0].Relation)
	}
}

func TestList_SkipsInactiveUsersAndTopsUp(t *testing.T) {
	ctx := context.Background()
	e := newListEnv()
	owner, viewer := uuid.New(), uuid.New()
	ids := e.seed(domain.ListFollowing, owner, 7)
	e.briefs.missing[ids[1]] = true
	e.briefs.missing[ids[2]] = true

	// 第一轮取 3 条只剩 1 条有效，接着往后补满一页
	p1, err := e.svc.ListFollowing(ctx, viewer, owner, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(itemIDs(p1)) != fmt.Sprint(wantIDs(ids[0], ids[3], ids[4])) || !p1.HasMore {
		t.Fatalf("page 1 = %v has_more=%v, want [0 3 4] with more", itemIDs(p1), p1.HasMore)
	}
	if e.repo.relCalls != 1 || len(e.repo.relTargets) != 3 {
		t.Errorf("relation queries = %d with %d targets, want 1 batch of the 3 returned users", e.repo.relCalls, len(e.repo.relTargets))
	}
	p2, err := e.svc.ListFollowing(ctx, viewer, owner, p1.NextCursor, 3)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(itemIDs(p2)) != fmt.Sprint(wantIDs(ids[5], ids[6])) || p2.HasMore || p2.NextCursor != "" {
		t.Fatalf("page 2 = %v has_more=%v, want [5 6] and end", itemIDs(p2), p2.HasMore)
	}

	// 连续失效用户超过轮数上限：这一页可以不满，但 has_more / next_cursor 仍然能翻到后面
	e = newListEnv()
	ids = e.seed(domain.ListFollowing, owner, 5)
	for _, id := range ids[:4] {
		e.briefs.missing[id] = true
	}
	p1, err = e.svc.ListFollowing(ctx, uuid.Nil, owner, "", 1)
	if err != nil || len(p1.Items) != 0 || !p1.HasMore || len(e.repo.limits) != maxListRounds {
		t.Fatalf("capped rounds: vo=%+v err=%v rounds=%d", p1, err, len(e.repo.limits))
	}
	p2, err = e.svc.ListFollowing(ctx, uuid.Nil, owner, p1.NextCursor, 1)
	if err != nil || fmt.Sprint(itemIDs(p2)) != fmt.Sprint(wantIDs(ids[4])) || p2.HasMore {
		t.Fatalf("next page: vo=%+v err=%v, want the only active user", p2, err)
	}
}

func TestList_DependencyErrors(t *testing.T) {
	ctx := context.Background()
	owner, viewer := uuid.New(), uuid.New()
	for name, breakIt := range map[string]func(*listEnv){
		"user check": func(e *listEnv) { e.users.err = errBoom },
		"visibility": func(e *listEnv) { e.visibility.err = errBoom },
		"list repo":  func(e *listEnv) { e.repo.listErr = errBoom },
		"briefs":     func(e *listEnv) { e.briefs.err = errBoom },
		"relations":  func(e *listEnv) { e.repo.relErr = errBoom },
	} {
		e := newListEnv()
		e.seed(domain.ListFollowing, owner, 2)
		breakIt(e)
		if _, err := e.svc.ListFollowing(ctx, viewer, owner, "", 10); !errors.Is(err, errBoom) {
			t.Errorf("%s: err = %v, want boom", name, err)
		}
	}
	e := newListEnv()
	e.visibility.err = errBoom
	if _, err := e.svc.GetStats(ctx, viewer, owner); !errors.Is(err, errBoom) {
		t.Errorf("stats with visibility error: err = %v, want boom", err)
	}
}

func TestGetRelations(t *testing.T) {
	ctx := context.Background()
	e := newListEnv()
	viewer, a, b := uuid.New(), uuid.New(), uuid.New()
	e.repo.relations[a] = domain.RelationFollowing

	vo, err := e.svc.GetRelations(ctx, viewer, []uuid.UUID{a, b, viewer, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(vo.Relations) != 3 || vo.Relations[a.String()] != domain.RelationFollowing ||
		vo.Relations[b.String()] != domain.RelationNone || vo.Relations[viewer.String()] != domain.RelationNone {
		t.Errorf("relations = %v, want a=1 b=0 self=0", vo.Relations)
	}
	if vo, err = e.svc.GetRelations(ctx, viewer, nil); err != nil || vo.Relations == nil || len(vo.Relations) != 0 {
		t.Errorf("empty input: vo=%+v err=%v, want empty map", vo, err)
	}

	ids := make([]uuid.UUID, MaxRelationTargets)
	for i := range ids {
		ids[i] = uuid.New()
	}
	e.repo.relCalls = 0
	if _, err = e.svc.GetRelations(ctx, viewer, ids); err != nil || e.repo.relCalls != 1 {
		t.Errorf("%d ids: err=%v queries=%d, want ok with 1 query", len(ids), err, e.repo.relCalls)
	}
	if _, err = e.svc.GetRelations(ctx, viewer, append(ids, uuid.New())); !errors.Is(err, domain.ErrTooManyTargets) || e.repo.relCalls != 1 {
		t.Errorf("%d ids: err=%v queries=%d, want ErrTooManyTargets without a query", len(ids)+1, err, e.repo.relCalls)
	}
	e.repo.relErr = errBoom
	if _, err = e.svc.GetRelations(ctx, viewer, ids[:1]); !errors.Is(err, errBoom) {
		t.Errorf("repo error: err = %v, want boom", err)
	}
}

func TestListCursor_RoundTripKeepsMicroseconds(t *testing.T) {
	owner := uuid.New()
	edge := domain.FollowEdge{ID: uuid.New(), FollowTime: time.Date(2026, 11, 10, 9, 8, 7, 123456000, time.UTC)}
	got, err := parseListCursor(encodeListCursor(domain.ListMutual, owner, edge), domain.ListMutual, owner)
	if err != nil || got.ID != edge.ID || !got.FollowTime.Equal(edge.FollowTime) {
		t.Fatalf("round trip = %+v err=%v, want %+v", got, err, edge)
	}
	if first, err := parseListCursor("", domain.ListMutual, owner); first != nil || err != nil {
		t.Errorf("empty cursor = %+v, %v; want nil, nil", first, err)
	}
}
