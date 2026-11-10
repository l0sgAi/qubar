//go:build integration

// follow_list_repo_pg_integration_test.go follow 列表仓储对开发栈 PostgreSQL 的集成测试（#218）：
// 关注 / 粉丝两个方向的翻页顺序与 keyset、follow_time 相同时按 id 定序、取关后再关注排到最前、
// 互关列表、批量关系查询。夹具（followEnv / newUsers / follow / unfollow）见 follow_repo_pg_integration_test.go。
package infrastructure_test

import (
	"slices"
	"testing"
	"time"

	"interestBar/pkg/domains/follow/domain"
	"interestBar/pkg/domains/follow/infrastructure"

	"github.com/google/uuid"
)

const noLimit = 2000

func (e *followEnv) lists() domain.ListRepository { return infrastructure.NewListRepository(e.db) }

// setFollowTime 把一条关系的 follow_time 改成指定值（构造确定的顺序 / 相同时间）。
func (e *followEnv) setFollowTime(t *testing.T, follower, followee uuid.UUID, at time.Time) {
	t.Helper()
	res := e.db.WithContext(t.Context()).Exec(
		"UPDATE domains.user_follow SET follow_time = ? WHERE follower_id = ? AND followee_id = ?", at, follower, followee)
	if res.Error != nil || res.RowsAffected != 1 {
		t.Fatalf("set follow_time: rows=%d err=%v", res.RowsAffected, res.Error)
	}
}

// walk 用 keyset 按 pageSize 翻完整个列表，返回每页的对端用户 ID。
func (e *followEnv) walk(t *testing.T, kind domain.ListKind, owner uuid.UUID, pageSize int) [][]uuid.UUID {
	t.Helper()
	var pages [][]uuid.UUID
	var after *domain.FollowEdge
	for range 50 {
		edges, err := e.lists().ListEdges(t.Context(), kind, owner, after, pageSize)
		if err != nil {
			t.Fatalf("ListEdges(%s): %v", kind, err)
		}
		if len(edges) == 0 {
			return pages
		}
		if len(edges) > pageSize {
			t.Fatalf("ListEdges returned %d rows, limit %d", len(edges), pageSize)
		}
		page := make([]uuid.UUID, len(edges))
		for i := range edges {
			page[i] = edges[i].UserID
			if edges[i].ID == uuid.Nil || edges[i].FollowTime.IsZero() {
				t.Fatalf("edge without keyset: %+v", edges[i])
			}
		}
		pages = append(pages, page)
		after = &edges[len(edges)-1]
	}
	t.Fatal("list did not terminate")
	return nil
}

func (e *followEnv) all(t *testing.T, kind domain.ListKind, owner uuid.UUID) []uuid.UUID {
	t.Helper()
	return slices.Concat(e.walk(t, kind, owner, 50)...)
}

// 两个方向都按 follow_time 倒序翻页；keyset 不重不漏；只返回自己列表里的有效关系。
func TestListRepo_PagesBothDirectionsByFollowTimeDesc(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 7)
	owner, others, stranger := u[0], u[1:6], u[6]
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)

	// others[i] 的关注时间随 i 递增：列表里应当 others[4] 在最前
	for i, other := range others {
		e.follow(t, owner, other, noLimit)
		e.setFollowTime(t, owner, other, base.Add(time.Duration(i)*time.Minute))
		e.follow(t, other, owner, noLimit)
		e.setFollowTime(t, other, owner, base.Add(time.Duration(len(others)-i)*time.Minute)) // 粉丝方向顺序相反
	}
	e.follow(t, stranger, others[0], noLimit) // 不属于 owner 的任何一个列表

	wantFollowing := [][]uuid.UUID{{others[4], others[3]}, {others[2], others[1]}, {others[0]}}
	if got := e.walk(t, domain.ListFollowing, owner, 2); !slices.EqualFunc(got, wantFollowing, slices.Equal[[]uuid.UUID]) {
		t.Errorf("following pages = %v, want %v", got, wantFollowing)
	}
	wantFollowers := [][]uuid.UUID{{others[0], others[1]}, {others[2], others[3]}, {others[4]}}
	if got := e.walk(t, domain.ListFollowers, owner, 2); !slices.EqualFunc(got, wantFollowers, slices.Equal[[]uuid.UUID]) {
		t.Errorf("followers pages = %v, want %v", got, wantFollowers)
	}

	// 取关的不在两个方向的列表里
	e.unfollow(t, owner, others[3])
	e.unfollow(t, others[1], owner)
	if got, want := e.all(t, domain.ListFollowing, owner), []uuid.UUID{others[4], others[2], others[1], others[0]}; !slices.Equal(got, want) {
		t.Errorf("following after unfollow = %v, want %v", got, want)
	}
	if got, want := e.all(t, domain.ListFollowers, owner), []uuid.UUID{others[0], others[2], others[3], others[4]}; !slices.Equal(got, want) {
		t.Errorf("followers after unfollow = %v, want %v", got, want)
	}
	if got := e.all(t, domain.ListFollowing, stranger); !slices.Equal(got, []uuid.UUID{others[0]}) {
		t.Errorf("stranger following = %v, want only others[0]", got)
	}
}

// follow_time 相同的行按 id 倒序定序，逐条翻页不重不漏。
func TestListRepo_SameFollowTimeOrdersByID(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 5)
	owner, others := u[0], u[1:]
	same := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	for _, other := range others {
		e.follow(t, owner, other, noLimit)
		e.setFollowTime(t, owner, other, same)
	}
	// 关系行 id 是 UUIDv7，按创建顺序递增：id 倒序 = 创建顺序倒序
	want := []uuid.UUID{others[3], others[2], others[1], others[0]}
	if got := slices.Concat(e.walk(t, domain.ListFollowing, owner, 1)...); !slices.Equal(got, want) {
		t.Errorf("one-by-one walk = %v, want %v", got, want)
	}
	if got := slices.Concat(e.walk(t, domain.ListFollowing, owner, 3)...); !slices.Equal(got, want) {
		t.Errorf("walk by 3 = %v, want %v", got, want)
	}
}

// 取关后再关注：复用同一行，follow_time 刷新，排到列表最前（两个方向都是）。
func TestListRepo_RefollowMovesToTop(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 4)
	owner, a, b, c := u[0], u[1], u[2], u[3]
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	for i, other := range []uuid.UUID{a, b, c} {
		e.follow(t, owner, other, noLimit)
		e.setFollowTime(t, owner, other, base.Add(time.Duration(i)*time.Minute))
	}
	// c 的粉丝：a、b、owner（owner 最晚）
	e.follow(t, a, c, noLimit)
	e.follow(t, b, c, noLimit)
	e.setFollowTime(t, a, c, base)
	e.setFollowTime(t, b, c, base.Add(time.Minute))

	if got, want := e.all(t, domain.ListFollowing, owner), []uuid.UUID{c, b, a}; !slices.Equal(got, want) {
		t.Fatalf("following before refollow = %v, want %v", got, want)
	}
	rowID := e.rows(t, owner, a)[0].ID

	e.unfollow(t, owner, a)
	if got, want := e.all(t, domain.ListFollowing, owner), []uuid.UUID{c, b}; !slices.Equal(got, want) {
		t.Fatalf("following after unfollow = %v, want %v", got, want)
	}
	e.follow(t, owner, a, noLimit)
	edges, err := e.lists().ListEdges(t.Context(), domain.ListFollowing, owner, nil, 10)
	if err != nil || len(edges) != 3 {
		t.Fatalf("ListEdges: %d rows err=%v", len(edges), err)
	}
	if edges[0].UserID != a || edges[1].UserID != c || edges[2].UserID != b {
		t.Errorf("following after refollow = %v, want [a c b]", []uuid.UUID{edges[0].UserID, edges[1].UserID, edges[2].UserID})
	}
	if edges[0].ID != rowID || !edges[0].FollowTime.After(base.Add(2*time.Minute)) {
		t.Errorf("refollow edge = %+v, want reused row %s with a refreshed follow_time", edges[0], rowID)
	}

	// 粉丝方向：a 取关再关注 c 后排到 c 的粉丝列表最前
	if got, want := e.all(t, domain.ListFollowers, c), []uuid.UUID{owner, b, a}; !slices.Equal(got, want) {
		t.Fatalf("followers before refollow = %v, want %v", got, want)
	}
	e.unfollow(t, a, c)
	e.follow(t, a, c, noLimit)
	if got, want := e.all(t, domain.ListFollowers, c), []uuid.UUID{a, owner, b}; !slices.Equal(got, want) {
		t.Errorf("followers after refollow = %v, want %v", got, want)
	}
}

// 互关列表 = 我关注的 ∩ 关注我的，按我关注对方的时间倒序；任一方向取关即退出。
func TestListRepo_MutualList(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 7)
	owner, a, b, c, d, fan, e2 := u[0], u[1], u[2], u[3], u[4], u[5], u[6]
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	for i, other := range []uuid.UUID{a, b, c, d, e2} {
		e.follow(t, owner, other, noLimit)
		e.setFollowTime(t, owner, other, base.Add(time.Duration(i)*time.Minute))
	}
	// 回关顺序故意与 owner 的关注顺序不同：排序只看 owner 关注对方的时间
	for _, other := range []uuid.UUID{e2, c, a, d} {
		e.follow(t, other, owner, noLimit)
	}
	e.follow(t, fan, owner, noLimit) // 只有单向：粉丝
	e.unfollow(t, d, owner)          // 对方取关：退出互关

	if got, want := e.walk(t, domain.ListMutual, owner, 2), [][]uuid.UUID{{e2, c}, {a}}; !slices.EqualFunc(got, want, slices.Equal[[]uuid.UUID]) {
		t.Errorf("mutual pages = %v, want %v", got, want)
	}
	// 对端视角：a 的互关只有 owner
	if got := e.all(t, domain.ListMutual, a); !slices.Equal(got, []uuid.UUID{owner}) {
		t.Errorf("mutual of a = %v, want [owner]", got)
	}
	e.unfollow(t, owner, c) // 我取关：退出互关
	if got, want := e.all(t, domain.ListMutual, owner), []uuid.UUID{e2, a}; !slices.Equal(got, want) {
		t.Errorf("mutual after unfollow = %v, want %v", got, want)
	}
	if got := e.all(t, domain.ListMutual, fan); len(got) != 0 {
		t.Errorf("mutual of a one-way follower = %v, want empty", got)
	}
}

// 批量关系：一次查询得到四种关系；已取关、陌生人、自己都不在结果里（视为 0）。
func TestListRepo_GetRelationsBatch(t *testing.T) {
	t.Parallel()
	e := newFollowEnv(t)
	u := e.newUsers(t, 7)
	viewer, following, followedBy, mutual, none, ex, bystander := u[0], u[1], u[2], u[3], u[4], u[5], u[6]
	e.follow(t, viewer, following, noLimit)
	e.follow(t, followedBy, viewer, noLimit)
	e.follow(t, viewer, mutual, noLimit)
	e.follow(t, mutual, viewer, noLimit)
	e.follow(t, viewer, ex, noLimit)
	e.unfollow(t, viewer, ex)
	e.follow(t, bystander, following, noLimit) // 与 viewer 无关的关系不能混进来

	got, err := e.lists().GetRelations(t.Context(), viewer, []uuid.UUID{following, followedBy, mutual, none, ex, viewer, bystander})
	if err != nil {
		t.Fatalf("GetRelations: %v", err)
	}
	want := map[uuid.UUID]domain.Relation{
		following:  domain.RelationFollowing,
		followedBy: domain.RelationFollowedBy,
		mutual:     domain.RelationMutual,
	}
	if len(got) != len(want) {
		t.Errorf("relations = %v, want exactly %v", got, want)
	}
	for id, rel := range want {
		if got[id] != rel {
			t.Errorf("relation with %s = %d, want %d", id, got[id], rel)
		}
		// 与单条查询一致
		if single, err := e.relations.GetRelation(t.Context(), viewer, id); err != nil || single != rel {
			t.Errorf("GetRelation(%s) = %d err=%v, want %d", id, single, err, rel)
		}
	}

	// 只返回请求里的目标
	if got, err = e.lists().GetRelations(t.Context(), viewer, []uuid.UUID{none, mutual}); err != nil || len(got) != 1 || got[mutual] != domain.RelationMutual {
		t.Errorf("subset = %v err=%v, want only mutual", got, err)
	}
	if got, err = e.lists().GetRelations(t.Context(), viewer, nil); err != nil || len(got) != 0 {
		t.Errorf("empty targets = %v err=%v, want empty map", got, err)
	}
}
