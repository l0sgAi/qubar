//go:build integration

// private_circle_list_integration_test.go 私密圈内容隔离（#170）列表 / 检索部分的集成测试（make test-integration）：
// 真实 PG + Redis（私密圈集合、joined ZSET）。集成栈不含 ES，ES 检索用记录入参的桩：断言 post 域把
// 「查看者不可见的私密圈」作为 must_not 传给检索层（查询体本身由 elasticsearch 包单测覆盖）；
// PG 回源（「我的收藏」）与「TA 加入的圈子」按查看者真实过滤。
package composition

import (
	"context"
	"slices"
	"sync"
	"testing"

	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	collectapp "interestBar/pkg/domains/collect/application"
	postapp "interestBar/pkg/domains/post/application"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// recordingPostSearcher 记录每次检索收到的 excludeCircleIDs，返回空结果。
type recordingPostSearcher struct {
	mu       sync.Mutex
	excludes [][]uuid.UUID
}

func (r *recordingPostSearcher) record(ids []uuid.UUID) *postapp.RawPostSearchResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.excludes = append(r.excludes, ids)
	return &postapp.RawPostSearchResult{}
}

func (r *recordingPostSearcher) last() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.excludes[len(r.excludes)-1]
}

func (r *recordingPostSearcher) Search(_ context.Context, _ string, _ uuid.UUID, ex []uuid.UUID, _ int, _ []interface{}) (*postapp.RawPostSearchResult, error) {
	return r.record(ex), nil
}

func (r *recordingPostSearcher) SearchMy(context.Context, uuid.UUID, string, int, []interface{}) (*postapp.RawPostSearchResult, error) {
	return r.record(nil), nil
}

func (r *recordingPostSearcher) SearchByUser(_ context.Context, _ uuid.UUID, _ string, ex []uuid.UUID, _ int, _ []interface{}) (*postapp.RawPostSearchResult, error) {
	return r.record(ex), nil
}

func (r *recordingPostSearcher) SearchByIDs(_ context.Context, _, ex []uuid.UUID, _ int) (*postapp.RawPostSearchResult, error) {
	return r.record(ex), nil
}

func (r *recordingPostSearcher) SearchByIDsAndKeyword(_ context.Context, _ []uuid.UUID, _ string, ex []uuid.UUID, _, _ int) (*postapp.RawPostSearchResult, error) {
	return r.record(ex), nil
}

// 帖子搜索 / TA 的帖子 / 按 ID 回填（history、推荐、热榜、发现共用）都把查看者不可见的私密圈交给检索层排除。
func TestPrivateCircle_SearchExcludesHiddenCircles(t *testing.T) {
	t.Parallel()
	env := setupVisibility(t)
	c := newVisCircle(t, env, circledomain.CircleJoinTypePrivate)
	ctx := t.Context()

	rec := &recordingPostSearcher{}
	svc := postapp.NewPostService(postinfra.NewPostRepository(env.db), postinfra.NewPostStatsCache(),
		postinfra.NewPostLikeCache(), postinfra.NewPostCollectCache(), rec, postinfra.NewPostEventPublisher())
	svc.SetCircleVisibility(env.visibility)

	calls := []struct {
		name string
		call func(viewer uuid.UUID) error
	}{
		{"search", func(v uuid.UUID) error { _, err := svc.SearchPosts(ctx, v, "", uuid.Nil, 10, nil); return err }},
		{"user posts", func(v uuid.UUID) error { _, err := svc.GetUserPosts(ctx, v, c.owner, "", 10, nil); return err }},
		{"by ids", func(v uuid.UUID) error { _, err := svc.SearchPostsByIDs(ctx, v, []uuid.UUID{c.postID}); return err }},
		{"by ids + keyword", func(v uuid.UUID) error {
			_, _, err := svc.SearchPostsByIDsAndKeyword(ctx, v, []uuid.UUID{c.postID}, "私密", 10, 0)
			return err
		}},
	}
	for _, v := range c.viewers() {
		for _, call := range calls {
			if err := call.call(v.id); err != nil {
				t.Fatalf("%s/%s: %v", v.name, call.name, err)
			}
			if excluded := slices.Contains(rec.last(), c.id); excluded == v.visible {
				t.Fatalf("%s/%s: circle excluded=%v, want %v", v.name, call.name, excluded, !v.visible)
			}
		}
	}
}

// 「我的收藏」走 PG 回源：被移出私密圈后收藏记录保留，但列表不再展示该帖；成员可见。
func TestPrivateCircle_CollectListHidesAfterRemoval(t *testing.T) {
	t.Parallel()
	env := setupVisibility(t)
	c := newVisCircle(t, env, circledomain.CircleJoinTypePrivate)
	ctx := t.Context()
	env.collectSvc.SetPostFetcher(&collectPostFetcher{delegate: env.postSvc})
	testkit.CleanupKeys(t, redispkg.GetUserPostCollectListKey(c.member), redispkg.GetUserPostCollectLoadedKey(c.member))

	if _, err := env.collectSvc.Toggle(ctx, c.member, collectapp.ToggleInput{PostID: c.postID, Action: "collect"}); err != nil {
		t.Fatalf("member collect: %v", err)
	}
	listed := func() bool {
		t.Helper()
		res, err := env.collectSvc.ListCollectedPosts(ctx, c.member, "", 20, "")
		if err != nil {
			t.Fatalf("list collected: %v", err)
		}
		return slices.ContainsFunc(res.Posts, func(p postapp.PostListItem) bool { return p.ID == c.postID })
	}
	if !listed() {
		t.Fatal("member must see collected private post")
	}

	if err := env.db.Exec(`UPDATE domains.circle_member SET status = 3 WHERE circle_id = ? AND user_id = ?`, c.id, c.member).Error; err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if listed() {
		t.Fatal("removed member must not see the private post in collect list")
	}
	assertRowCount(t, env.db, `SELECT count(*) FROM domains.post_collect WHERE post_id = ? AND user_id = ? AND deleted = 0`, 1, c.postID, c.member)
}

// 「TA 加入的圈子」：私密圈只在查看者本人也是该圈成员时展示。
func TestPrivateCircle_UserCirclesHidesPrivateFromNonMembers(t *testing.T) {
	t.Parallel()
	env := setupVisibility(t)
	c := newVisCircle(t, env, circledomain.CircleJoinTypePrivate)
	ctx := t.Context()
	testkit.CleanupKeys(t, redispkg.GetUserJoinedCirclesKey(c.member))

	for _, v := range c.viewers() {
		res, err := env.circleSvc.GetUserCircles(ctx, v.id, c.member, "", 0, 100, "")
		if err != nil {
			t.Fatalf("%s: user circles: %v", v.name, err)
		}
		shown := slices.ContainsFunc(res.Circles, func(d circleapp.MyCircleDoc) bool { return d.ID == c.id.String() })
		if shown != v.visible {
			t.Fatalf("%s: private circle shown=%v, want %v", v.name, shown, v.visible)
		}
	}
}
