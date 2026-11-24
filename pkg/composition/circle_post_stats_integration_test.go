//go:build integration

// circle_post_stats_integration_test.go 圈内帖子列表计数口径（#250）集成测试（make test-integration）：
// circle 的 PostMediaFetcher 桥接（postMediaFetcherForCircle.BatchGetStats）与 post 域列表（applyStatsOverlay）、
// 帖子详情读同一个 Redis post:stats；未命中时桥接不返回该帖（circle 保留 ES 快照）且不回种。
// GetCirclePosts 本身依赖 ES 文档，开发栈没有 CDC，组装逻辑由 circle application 单测覆盖。
package composition

import (
	"testing"

	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestCirclePostStatsBridge_SameSourceAsPostLists(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	ctx := t.Context()

	postSvc := newPostService(NewDeps())
	bridge := &postMediaFetcherForCircle{delegate: postSvc}
	postID := newTestPost(t, db) // 已注册 post:stats 清理

	// 未命中：不返回该帖，且读路径不回种
	if got := bridge.BatchGetStats(ctx, []uuid.UUID{postID}); len(got) != 0 {
		t.Fatalf("BatchGetStats on miss = %v; want empty", got)
	}
	if exists, err := redispkg.PostStatisticsExists(postID); err != nil || exists {
		t.Fatalf("post:stats exists after miss = %v, %v; want not seeded", exists, err)
	}

	// 命中：桥接返回 Redis 实时值，与 post 域列表 / 详情一致
	want := &redispkg.PostStatistics{ViewCount: 42, CommentCount: 5, LikeCount: 9, CollectCount: 3}
	if err := redispkg.UpdatePostStatistics(postID, want); err != nil {
		t.Fatalf("seed post:stats: %v", err)
	}
	got, ok := bridge.BatchGetStats(ctx, []uuid.UUID{postID})[postID]
	if !ok || got.ViewCount != want.ViewCount || got.CommentCount != want.CommentCount ||
		got.LikeCount != want.LikeCount || got.CollectCount != want.CollectCount {
		t.Fatalf("BatchGetStats = %+v (ok=%v); want %+v", got, ok, want)
	}

	items, err := postSvc.GetPostsByIDs(ctx, uuid.Nil, []uuid.UUID{postID})
	if err != nil || len(items) != 1 {
		t.Fatalf("GetPostsByIDs = %+v, %v", items, err)
	}
	if p := items[0]; p.ViewCount != got.ViewCount || p.CommentCount != got.CommentCount ||
		p.LikeCount != got.LikeCount || p.CollectCount != got.CollectCount {
		t.Fatalf("post list counts = %+v; bridge = %+v", p, got)
	}

	// 详情（访客）可能计一次浏览，只比较赞 / 藏 / 评论
	detail, err := postSvc.GetPostDetail(ctx, uuid.Nil, postID)
	if err != nil {
		t.Fatalf("GetPostDetail: %v", err)
	}
	if detail.LikeCount != got.LikeCount || detail.CollectCount != got.CollectCount || detail.CommentCount != got.CommentCount {
		t.Fatalf("detail counts like/collect/comment = %d/%d/%d; bridge = %+v",
			detail.LikeCount, detail.CollectCount, detail.CommentCount, got)
	}
}
