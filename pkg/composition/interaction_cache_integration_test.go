//go:build integration

// interaction_cache_integration_test.go 点赞 / 收藏状态"完整加载"负缓存（#129）的端到端集成测试（make test-integration）：
// 生产装配的 PostService / CommentService / LikeService + 开发栈 PG / Redis / Redpanda。
//
// "不回源"的判定方式：完整加载后直接往 PG 插一条缓存不知道的有效点赞，批查仍返回 false，
// 说明批查没有再查 PG（真实写路径都会先经过 Redis Lua，不会出现这种 PG 独有行）。
package composition

import (
	"testing"

	commentdomain "interestBar/pkg/domains/comment/domain"
	commentinfra "interestBar/pkg/domains/comment/infrastructure"
	likeapp "interestBar/pkg/domains/like/application"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// newInteractionUser 随机用户，注册其点赞 / 收藏 ZSET 与完整加载标记的清理。
func newInteractionUser(t *testing.T) uuid.UUID {
	t.Helper()
	u := newTestUser(t)
	testkit.CleanupKeys(t,
		redispkg.GetUserPostLikeLoadedKey(u),
		redispkg.GetUserPostCollectListKey(u), redispkg.GetUserPostCollectLoadedKey(u),
		redispkg.GetUserCommentLikeLoadedKey(u),
	)
	return u
}

func insertInteraction(t *testing.T, db *gorm.DB, table, column string, userID, targetID uuid.UUID) {
	t.Helper()
	if err := db.Exec("INSERT INTO domains."+table+" (id, user_id, "+column+", deleted) VALUES (?, ?, ?, 0)",
		sharedomain.NewID(), userID, targetID).Error; err != nil {
		t.Fatalf("insert %s: %v", table, err)
	}
}

func loadedMarker(t *testing.T, key string) string {
	t.Helper()
	v, err := redispkg.Client.Get(t.Context(), key).Result()
	if err != nil {
		t.Fatalf("GET %s: %v", key, err)
	}
	return v
}

func TestPostInteractions_FullLoadNegativeCache(t *testing.T) {
	t.Parallel()
	likeSvc := newLikePipeline(t)
	db := testkit.PG(t)
	ctx := t.Context()
	postSvc := newPostService(NewDeps())

	liked, collected, other := newTestPost(t, db), newTestPost(t, db), newTestPost(t, db)
	for _, p := range []uuid.UUID{liked, collected, other} {
		testkit.CleanupRows(t, "domains.post_collect", "post_id", p)
	}
	user := newInteractionUser(t)
	insertInteraction(t, db, "post_like", "post_id", user, liked)
	insertInteraction(t, db, "post_collect", "post_id", user, collected)
	all := []uuid.UUID{liked, collected, other}

	// 1. 冷启动：ZSET 与标记都不存在 → 全量装载，结果正确并写入完整加载标记
	likedMap, collectedMap, err := postSvc.BatchCheckInteractions(ctx, user, all)
	if err != nil {
		t.Fatalf("BatchCheckInteractions: %v", err)
	}
	if !likedMap[liked] || likedMap[collected] || likedMap[other] {
		t.Fatalf("cold liked = %v", likedMap)
	}
	if !collectedMap[collected] || collectedMap[liked] || collectedMap[other] {
		t.Fatalf("cold collected = %v", collectedMap)
	}
	likeZset, likeLoaded := redispkg.GetUserPostLikeListKey(user), redispkg.GetUserPostLikeLoadedKey(user)
	for _, key := range []string{likeLoaded, redispkg.GetUserPostCollectLoadedKey(user)} {
		if v := loadedMarker(t, key); v != "1" {
			t.Fatalf("%s = %q, want complete marker", key, v)
		}
	}
	if _, err := redispkg.Client.ZScore(ctx, likeZset, liked.String()).Result(); err != nil {
		t.Fatalf("liked post not loaded into %s: %v", likeZset, err)
	}
	// 标记与 ZSET 同一时刻过期：标记 TTL 不得长于 ZSET
	if zt, lt := redispkg.Client.PTTL(ctx, likeZset).Val(), redispkg.Client.PTTL(ctx, likeLoaded).Val(); lt <= 0 || lt > zt {
		t.Fatalf("PTTL zset=%v marker=%v, want 0 < marker <= zset", zt, lt)
	}

	// 2. 标记命中不回源：PG 里多出缓存不知道的点赞 / 收藏，批查仍为 false
	insertInteraction(t, db, "post_like", "post_id", user, other)
	insertInteraction(t, db, "post_collect", "post_id", user, other)
	likedMap, collectedMap, _ = postSvc.BatchCheckInteractions(ctx, user, all)
	if likedMap[other] || collectedMap[other] || !likedMap[liked] || !collectedMap[collected] {
		t.Fatalf("with complete marker must not query PG: liked=%v collected=%v", likedMap, collectedMap)
	}

	// 3. 点赞后立即可见（写路径同样信任完整加载的 ZSET，不依赖 PG 落库）
	if res, err := likeSvc.Toggle(ctx, user, likeapp.ToggleInput{Type: "post", TargetID: collected, Action: "like"}); err != nil || !res.IsLiked {
		t.Fatalf("Toggle(like) = %+v, %v", res, err)
	}
	if likedMap, _, _ = postSvc.BatchCheckInteractions(ctx, user, all); !likedMap[collected] {
		t.Fatalf("liked post not visible right after like: %v", likedMap)
	}
	if ok, err := postSvc.IsLikedByUser(ctx, user, collected); err != nil || !ok {
		t.Fatalf("IsLikedByUser after like = %v, %v", ok, err)
	}

	// 4. 取消后为 false（PG 行可能尚未落库，批查不能被它影响）
	if res, err := likeSvc.Toggle(ctx, user, likeapp.ToggleInput{Type: "post", TargetID: liked, Action: "unlike"}); err != nil || res.IsLiked {
		t.Fatalf("Toggle(unlike) = %+v, %v", res, err)
	}
	if likedMap, _, _ = postSvc.BatchCheckInteractions(ctx, user, all); likedMap[liked] {
		t.Fatalf("unliked post still liked: %v", likedMap)
	}
	if ok, err := postSvc.IsLikedByUser(ctx, user, liked); err != nil || ok {
		t.Fatalf("IsLikedByUser after unlike = %v, %v", ok, err)
	}

	// 收藏设值脚本同样维护 ZSET：取消收藏后立即为 false，标记仍完整
	if _, err := redispkg.SetPostCollect(ctx, user, collected, false); err != nil {
		t.Fatalf("SetPostCollect(false): %v", err)
	}
	if _, collectedMap, _ = postSvc.BatchCheckInteractions(ctx, user, all); collectedMap[collected] {
		t.Fatalf("uncollected post still collected: %v", collectedMap)
	}
	if v := loadedMarker(t, redispkg.GetUserPostCollectLoadedKey(user)); v != "1" {
		t.Fatalf("collect marker = %q after uncollect, want complete", v)
	}

	// 5. 标记过期 / 被删 → 回到冷启动：重新从 PG 全量装载（此时能看到第 2 步插入的行）
	redispkg.Client.Del(ctx, likeLoaded, likeZset)
	if likedMap, _, _ = postSvc.BatchCheckInteractions(ctx, user, []uuid.UUID{other}); !likedMap[other] {
		t.Fatalf("after marker expiry must reload from PG: %v", likedMap)
	}
}

func TestCommentLiked_FullLoadNegativeCache(t *testing.T) {
	t.Parallel()
	likeSvc := newLikePipeline(t)
	db := testkit.PG(t)
	ctx := t.Context()
	commentSvc := newCommentService(NewDeps())

	postID := newTestPost(t, db)
	repo := commentinfra.NewCommentRepository(db)
	var ids []uuid.UUID
	for range 3 {
		c := &commentdomain.Comment{PostID: postID, UserID: sharedomain.NewID(), Content: "集成测试评论", ExtraData: []byte("{}")}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("create comment: %v", err)
		}
		testkit.CleanupRows(t, "domains.comment", "id", c.ID)
		testkit.CleanupRows(t, "domains.comment_like", "comment_id", c.ID)
		testkit.CleanupKeys(t, redispkg.GetCommentStatsKey(c.ID))
		ids = append(ids, c.ID)
	}
	likedID, plainID, laterID := ids[0], ids[1], ids[2]
	user := newInteractionUser(t)
	insertInteraction(t, db, "comment_like", "comment_id", user, likedID)

	likedOf := func() map[uuid.UUID]bool {
		t.Helper()
		res, err := commentSvc.GetRootComments(ctx, user, postID, 1, "")
		if err != nil {
			t.Fatalf("GetRootComments: %v", err)
		}
		out := make(map[uuid.UUID]bool, len(res.Items))
		for _, vo := range res.Items {
			out[vo.ID] = vo.Liked
		}
		return out
	}

	// 冷启动全量装载
	if got := likedOf(); !got[likedID] || got[plainID] || got[laterID] {
		t.Fatalf("cold liked = %v", got)
	}
	if v := loadedMarker(t, redispkg.GetUserCommentLikeLoadedKey(user)); v != "1" {
		t.Fatalf("comment like marker = %q, want complete", v)
	}

	// 标记命中不回源
	insertInteraction(t, db, "comment_like", "comment_id", user, laterID)
	if got := likedOf(); got[laterID] {
		t.Fatalf("with complete marker must not query PG: %v", got)
	}

	// 点赞立即可见 / 取消后为 false
	if res, err := likeSvc.Toggle(ctx, user, likeapp.ToggleInput{Type: "comment", TargetID: plainID, Action: "like"}); err != nil || !res.IsLiked {
		t.Fatalf("Toggle(like comment) = %+v, %v", res, err)
	}
	if res, err := likeSvc.Toggle(ctx, user, likeapp.ToggleInput{Type: "comment", TargetID: likedID, Action: "unlike"}); err != nil || res.IsLiked {
		t.Fatalf("Toggle(unlike comment) = %+v, %v", res, err)
	}
	if got := likedOf(); !got[plainID] || got[likedID] {
		t.Fatalf("after like/unlike = %v", got)
	}
}
