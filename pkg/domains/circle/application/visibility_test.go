package application

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
)

// visRepo 只实现 ListPrivateIDs（其余方法未用到，调用即 panic）。
type visRepo struct {
	domain.CircleRepository
	ids   []uuid.UUID
	calls int
}

func (r *visRepo) ListPrivateIDs(context.Context) ([]uuid.UUID, error) {
	r.calls++
	return r.ids, nil
}

// visMembers 只实现 ListJoinedMemberships：joined 为查看者以正常 / 禁言身份加入的圈子。
type visMembers struct {
	domain.MemberRepository
	joined map[uuid.UUID]bool
}

func (m *visMembers) ListJoinedMemberships(_ context.Context, _ uuid.UUID, circleIDs []uuid.UUID) (map[uuid.UUID]domain.CircleMember, error) {
	out := map[uuid.UUID]domain.CircleMember{}
	for _, id := range circleIDs {
		if m.joined[id] {
			out[id] = domain.CircleMember{CircleID: id, Status: domain.MemberStatusNormal}
		}
	}
	return out, nil
}

type memPrivateCache struct {
	ids []uuid.UUID
	ok  bool
}

func (c *memPrivateCache) Get(context.Context) ([]uuid.UUID, bool, error) { return c.ids, c.ok, nil }
func (c *memPrivateCache) Set(_ context.Context, ids []uuid.UUID) error {
	c.ids, c.ok = ids, true
	return nil
}
func (c *memPrivateCache) Delete(context.Context) error {
	c.ids, c.ok = nil, false
	return nil
}

func TestCircleVisibility(t *testing.T) {
	ctx := context.Background()
	joinedPrivate, otherPrivate, public := uuid.New(), uuid.New(), uuid.New()
	viewer := uuid.New()
	repo := &visRepo{ids: []uuid.UUID{joinedPrivate, otherPrivate}}
	cache := &memPrivateCache{}
	v := newCircleVisibility(repo, &visMembers{joined: map[uuid.UUID]bool{joinedPrivate: true}}, cache)

	for _, tc := range []struct {
		name           string
		viewer, circle uuid.UUID
		want           bool
	}{
		{"public circle, guest", uuid.Nil, public, true},
		{"no circle (draft)", uuid.Nil, uuid.Nil, true},
		{"private circle, guest", uuid.Nil, joinedPrivate, false},
		{"private circle, member", viewer, joinedPrivate, true},
		{"private circle, non-member", viewer, otherPrivate, false},
	} {
		got, err := v.CanView(ctx, tc.viewer, tc.circle)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v err %v, want %v", tc.name, got, err, tc.want)
		}
	}

	hidden, err := v.HiddenCircleIDs(ctx, viewer)
	if err != nil || !slices.Equal(hidden, []uuid.UUID{otherPrivate}) {
		t.Fatalf("hidden for member: %v %v", hidden, err)
	}
	hidden, err = v.HiddenCircleIDs(ctx, uuid.Nil)
	if err != nil || len(hidden) != 2 {
		t.Fatalf("hidden for guest: %v %v", hidden, err)
	}
	if repo.calls != 1 {
		t.Fatalf("private ids should be cached after first load, repo calls = %d", repo.calls)
	}

	v.invalidatePrivateIDs(ctx)
	if _, err := v.HiddenCircleIDs(ctx, uuid.Nil); err != nil || repo.calls != 2 {
		t.Fatalf("invalidate must force reload: calls=%d err=%v", repo.calls, err)
	}
}

// 私密圈精简详情只输出 id / 名称 / 头像 / join_type / 成员数（+ restricted 标志）。
func TestCircleRestrictedViewFields(t *testing.T) {
	vo := &CircleDetailVO{
		ID: uuid.New(), Name: "n", AvatarURL: "a", Description: "secret", Rule: "secret",
		CreatorID: uuid.New(), JoinType: domain.CircleJoinTypePrivate, MemberCount: 3, Restricted: true,
	}
	raw, err := json.Marshal(vo.RestrictedView())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{"avatar_url", "id", "join_type", "member_count", "name", "restricted"}
	if !slices.Equal(keys, want) {
		t.Fatalf("restricted view keys = %v, want %v", keys, want)
	}
}

// activeSearcher 记录 SearchActive 收到的排除集合，返回空榜单。
type activeSearcher struct {
	CircleSearcher
	excluded []uuid.UUID
}

func (s *activeSearcher) SearchActive(_ context.Context, size, offset int, exclude []uuid.UUID) (*RawActiveCircleResult, error) {
	s.excluded = exclude
	return &RawActiveCircleResult{}, nil
}

// 近期活跃圈子在聚合阶段排除全部私密圈（不论查看者是否成员）。
func TestListActiveCircles_ExcludesPrivateCircles(t *testing.T) {
	private := uuid.New()
	searcher := &activeSearcher{}
	svc := NewCircleService(&visRepo{ids: []uuid.UUID{private}}, &visMembers{}, nil, nil, nil, searcher, nil, nil)
	if _, err := svc.ListActiveCircles(context.Background(), 20, 0); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(searcher.excluded, []uuid.UUID{private}) {
		t.Fatalf("excluded = %v, want private circle", searcher.excluded)
	}
}
