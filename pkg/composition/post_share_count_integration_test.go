//go:build integration

// post_share_count_integration_test.go 帖子分享数展示（#233）端到端集成测试（make test-integration）：
// 生产装配的 PostService 详情 / 列表（PG 来源的收藏列表、circle 与 ES 来源列表共用的 BatchGetShareCounts）
// 与 ShareService 分享自增读同一个 Redis post:sharecnt，冷启动 / 缓存缺失从 PG post.share_count 回种；
// is_shared 在详情与信息流交互态桥接器里口径一致。ES 来源列表（搜索 / 用户帖子 / 首页 / 热榜 / 发现 / 最近浏览）
// 的组装依赖 ES 文档，开发栈没有 CDC，这里只覆盖它们共用的 known=nil 回源路径。
package composition

import (
	"testing"
	"time"

	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// newShareCountViewer 登录用户：注册详情回显（点赞 / 收藏 ZSET 装载）与浏览去重写入的 key 的清理。
func newShareCountViewer(t *testing.T, postID uuid.UUID) uuid.UUID {
	t.Helper()
	u := sharedomain.NewID()
	testkit.CleanupKeys(t,
		redispkg.GetUserPostLikeListKey(u), redispkg.GetUserPostLikeLoadedKey(u),
		redispkg.GetUserPostCollectListKey(u), redispkg.GetUserPostCollectLoadedKey(u),
		redispkg.GetPostViewDedupeKey(postID, u),
	)
	return u
}

func TestPostShareCount_DetailAndListsConsistent(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	ctx := t.Context()

	postSvc := newPostService(NewDeps())
	shareSvc := newShareTestService(postSvc)
	checker := &postInteractionChecker{delegate: postSvc}
	postID := newTestPost(t, db)
	cleanupShares(t, postID)
	key := redispkg.GetPostShareCountKey(postID)
	setPG := func(n int) {
		t.Helper()
		if err := db.Exec("UPDATE domains.post SET share_count = ? WHERE id = ?", n, postID).Error; err != nil {
			t.Fatalf("set share_count: %v", err)
		}
	}
	// assertAll 详情（访客）、PG 来源列表、circle / ES 来源列表共用的批量接口返回同一个数。
	assertAll := func(step string, want int) {
		t.Helper()
		detail, err := postSvc.GetPostDetail(ctx, uuid.Nil, postID)
		if err != nil || detail.ShareCount != want || detail.IsShared {
			t.Fatalf("%s: detail = %+v, %v; want share_count=%d is_shared=false", step, detail, err, want)
		}
		items, err := postSvc.GetPostsByIDs(ctx, uuid.Nil, []uuid.UUID{postID})
		if err != nil || len(items) != 1 || items[0].ShareCount != want {
			t.Fatalf("%s: GetPostsByIDs = %+v, %v; want share_count=%d", step, items, err, want)
		}
		if got := postSvc.BatchGetShareCounts(ctx, []uuid.UUID{postID}); got[postID] != want {
			t.Fatalf("%s: BatchGetShareCounts = %v; want %d", step, got, want)
		}
	}

	// 1. 冷启动：Redis 无 key，从 PG share_count 恢复并回种
	setPG(5)
	assertAll("cold start", 5)
	if v, err := redispkg.Client.Get(ctx, key).Int(); err != nil || v != 5 {
		t.Fatalf("seeded cache = %d, %v; want 5", v, err)
	}

	// 2. 分享自增后（PG 尚未落库）详情与列表同时看到新值，PG 旧值不会覆盖缓存
	sharer := newShareCountViewer(t, postID)
	res, err := shareSvc.SharePost(ctx, sharer, postID)
	if err != nil || !res.FirstShare || res.ShareCount != 6 {
		t.Fatalf("SharePost = %+v, %v; want first share, count 6", res, err)
	}
	assertAll("after share", 6)

	// 3. is_shared：详情登录态与信息流桥接器一致
	detail, err := postSvc.GetPostDetail(ctx, sharer, postID)
	if err != nil || !detail.IsShared || detail.ShareCount != 6 {
		t.Fatalf("sharer detail = %+v, %v; want is_shared share_count=6", detail, err)
	}
	viewer := newShareCountViewer(t, postID)
	if detail, err = postSvc.GetPostDetail(ctx, viewer, postID); err != nil || detail.IsShared {
		t.Fatalf("viewer detail = %+v, %v; want is_shared=false", detail, err)
	}
	for user, want := range map[uuid.UUID]bool{sharer: true, viewer: false, uuid.Nil: false} {
		_, _, shared, err := checker.BatchCheck(ctx, user, []uuid.UUID{postID})
		if err != nil || shared[postID] != want {
			t.Fatalf("feed is_shared for %v = %v, %v; want %v", user, shared, err, want)
		}
	}
	// 登录态详情会异步自增浏览量：等它写完再结束，避免清理后又写回 key
	testkit.Eventually(t, 5*time.Second, 50*time.Millisecond, "async view count", func() bool {
		return redispkg.Client.Exists(ctx, redispkg.GetPostViewDedupeKey(postID, viewer)).Val() == 1 &&
			redispkg.Client.Exists(ctx, redispkg.GetPostViewDedupeKey(postID, sharer)).Val() == 1
	})

	// 4. 缓存丢失（过期 / 重启）：PG 已落库到 7，各接口一致从 PG 恢复
	setPG(7)
	if err := redispkg.Client.Del(ctx, key).Err(); err != nil {
		t.Fatalf("del key: %v", err)
	}
	if got := postSvc.BatchGetShareCounts(ctx, []uuid.UUID{postID}); got[postID] != 7 { // known=nil：PG 主键回源
		t.Fatalf("BatchGetShareCounts after cache loss = %v; want 7", got)
	}
	assertAll("after cache loss", 7)
}
