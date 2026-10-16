//go:build integration

// like_pipeline_integration_test.go 用生产装配（newXxxService + facade 桥接器）对开发栈做端到端集成测试
// （make test-integration）：缓存回源（read-through）与点赞 Write-Behind 全链路
// LikeService.Toggle → Redis Lua → Redpanda like_events → 点赞消费者 → PostgreSQL。
package composition

import (
	"sync"
	"testing"
	"time"

	commentdomain "interestBar/pkg/domains/comment/domain"
	commentinfra "interestBar/pkg/domains/comment/infrastructure"
	likeapp "interestBar/pkg/domains/like/application"
	postdomain "interestBar/pkg/domains/post/domain"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// newTestPost 用 post 仓储插一条已发布帖子，测试结束删除帖子及其点赞流水与相关缓存。
func newTestPost(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	p := &postdomain.Post{
		CircleID: sharedomain.NewID(),
		UserID:   sharedomain.NewID(),
		Title:    "集成测试帖子",
		Status:   postdomain.PostStatusPublished,
	}
	if err := postinfra.NewPostRepository(db).Create(t.Context(), p); err != nil {
		t.Fatalf("create post: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", p.ID)
	testkit.CleanupRows(t, "domains.post_like", "post_id", p.ID)
	testkit.CleanupKeys(t, redispkg.GetPostStatsKey(p.ID), redispkg.GetPostHotCapKey(p.ID))
	return p.ID
}

func newTestUser(t *testing.T) uuid.UUID {
	t.Helper()
	u := sharedomain.NewID()
	testkit.CleanupKeys(t, redispkg.GetUserPostLikeListKey(u), redispkg.GetUserCommentLikeListKey(u))
	return u
}

// 点赞状态 ZSET 有 TTL 与容量上限，miss ≠ 未赞：必须回源 DB，并把确认的已赞回填 ZSET。
func TestPostService_IsLikedReadThrough(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	ctx := t.Context()
	postSvc := newPostService(NewDeps())

	postID := newTestPost(t, db)
	liker, stranger := newTestUser(t), newTestUser(t)
	if err := db.Exec("INSERT INTO domains.post_like (id, user_id, post_id, deleted) VALUES (?, ?, ?, 0)",
		sharedomain.NewID(), liker, postID).Error; err != nil {
		t.Fatalf("insert post_like: %v", err)
	}

	zsetKey := redispkg.GetUserPostLikeListKey(liker)
	if n := redispkg.Client.Exists(ctx, zsetKey).Val(); n != 0 {
		t.Fatalf("precondition: like ZSET should be empty")
	}
	liked, err := postSvc.IsLikedByUser(ctx, liker, postID)
	if err != nil || !liked {
		t.Fatalf("IsLikedByUser(liker) = %v, %v; want true from DB", liked, err)
	}
	if _, err := redispkg.Client.ZScore(ctx, zsetKey, postID.String()).Result(); err != nil {
		t.Fatalf("liked post was not backfilled into %s: %v", zsetKey, err)
	}

	liked, err = postSvc.IsLikedByUser(ctx, stranger, postID)
	if err != nil || liked {
		t.Fatalf("IsLikedByUser(stranger) = %v, %v; want false", liked, err)
	}
	if n := redispkg.Client.Exists(ctx, redispkg.GetUserPostLikeListKey(stranger)).Val(); n != 0 {
		t.Fatal("not-liked state must not be cached (only confirmed likes are backfilled)")
	}
}

// 统计 Hash 缺失时从 DB 恢复；已存在时不覆盖（避免冲掉并发 HINCRBY）。
func TestPostService_RestoreStatsFromDB(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	ctx := t.Context()
	postSvc := newPostService(NewDeps())

	postID := newTestPost(t, db)
	if err := db.Exec("UPDATE domains.post SET like_count = 7, comment_count = 3, collect_count = 2, view_count = 11 WHERE id = ?",
		postID).Error; err != nil {
		t.Fatalf("set counters: %v", err)
	}

	if err := postSvc.RestoreStats(ctx, postID); err != nil {
		t.Fatalf("RestoreStats: %v", err)
	}
	stats, err := redispkg.GetPostStatistics(postID)
	if err != nil || stats == nil {
		t.Fatalf("GetPostStatistics = %v, %v", stats, err)
	}
	want := redispkg.PostStatistics{ViewCount: 11, CommentCount: 3, LikeCount: 7, CollectCount: 2}
	if *stats != want {
		t.Fatalf("restored stats = %+v, want %+v", *stats, want)
	}

	// 缓存已存在：DB 变化不应覆盖 Redis 真值
	if err := redispkg.Client.HIncrBy(ctx, redispkg.GetPostStatsKey(postID), "like_count", 1).Err(); err != nil {
		t.Fatalf("HINCRBY: %v", err)
	}
	if err := db.Exec("UPDATE domains.post SET like_count = 0 WHERE id = ?", postID).Error; err != nil {
		t.Fatalf("reset like_count: %v", err)
	}
	if err := postSvc.RestoreStats(ctx, postID); err != nil {
		t.Fatalf("RestoreStats (exists): %v", err)
	}
	if stats, _ := redispkg.GetPostStatistics(postID); stats == nil || stats.LikeCount != 8 {
		t.Fatalf("existing stats overwritten: %+v, want like_count 8", stats)
	}
}

var likeConsumerOnce sync.Once

// newLikePipeline 按 RegisterDomainRoutes 的方式装配 LikeService，并启动本进程专用 topic 上的点赞消费者
// （testkit.Main 结束时排干并停止）。
func newLikePipeline(t *testing.T) likeapp.LikeService {
	t.Helper()
	testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	likeConsumerOnce.Do(func() {
		if err := redpanda.StartLikeEventConsumer(); err != nil {
			t.Fatalf("start like consumer: %v", err)
		}
	})

	deps := NewDeps()
	postSvc := newPostService(deps)
	commentSvc := newCommentService(deps)
	likeSvc := newLikeService(deps)
	likeSvc.SetPostTarget(&likePostTarget{delegate: postSvc})
	likeSvc.SetCommentTarget(&likeCommentTarget{delegate: commentSvc})
	return likeSvc
}

const pipelineTimeout = 45 * time.Second // 含首次加入 consumer group 的再均衡

// postLikeState 读 PG 中的点赞流水（-1 = 无行）与帖子 like_count。
func postLikeState(t *testing.T, db *gorm.DB, userID, postID uuid.UUID) (rowDeleted, likeCount int) {
	t.Helper()
	rowDeleted = -1
	var rows []int
	if err := db.Raw("SELECT deleted FROM domains.post_like WHERE user_id = ? AND post_id = ?", userID, postID).
		Scan(&rows).Error; err != nil {
		t.Fatalf("read post_like: %v", err)
	}
	if len(rows) > 0 {
		rowDeleted = rows[0]
	}
	if err := db.Raw("SELECT like_count FROM domains.post WHERE id = ?", postID).Scan(&likeCount).Error; err != nil {
		t.Fatalf("read like_count: %v", err)
	}
	return rowDeleted, likeCount
}

func TestLikePipeline_PostLikeFlushedToPG(t *testing.T) {
	t.Parallel()
	likeSvc := newLikePipeline(t)
	db := testkit.PG(t)
	ctx := t.Context()

	postID := newTestPost(t, db)
	userA, userB := newTestUser(t), newTestUser(t)

	for _, u := range []uuid.UUID{userA, userB} {
		res, err := likeSvc.Toggle(ctx, u, likeapp.ToggleInput{Type: "post", TargetID: postID, Action: "like"})
		if err != nil || !res.IsLiked {
			t.Fatalf("Toggle(like) = %+v, %v", res, err)
		}
	}
	// 重复点赞：Redis 已处于期望态，不发事件、不改计数
	if _, err := likeSvc.Toggle(ctx, userA, likeapp.ToggleInput{Type: "post", TargetID: postID, Action: "like"}); err != nil {
		t.Fatalf("Toggle(like again): %v", err)
	}
	if stats, _ := redispkg.GetPostStatistics(postID); stats == nil || stats.LikeCount != 2 {
		t.Fatalf("redis like_count = %+v, want 2", stats)
	}

	testkit.Eventually(t, pipelineTimeout, 200*time.Millisecond, "two likes persisted to PG", func() bool {
		a, count := postLikeState(t, db, userA, postID)
		b, _ := postLikeState(t, db, userB, postID)
		return a == 0 && b == 0 && count == 2
	})

	// 切换（空 action）= 取消：行 1→0 迁移推导 like_count -1
	res, err := likeSvc.Toggle(ctx, userA, likeapp.ToggleInput{Type: "post", TargetID: postID})
	if err != nil || res.IsLiked {
		t.Fatalf("Toggle(toggle off) = %+v, %v", res, err)
	}
	testkit.Eventually(t, pipelineTimeout, 200*time.Millisecond, "unlike persisted to PG", func() bool {
		a, count := postLikeState(t, db, userA, postID)
		return a == 1 && count == 1
	})
}

func TestLikePipeline_CommentLikeFlushedToPG(t *testing.T) {
	t.Parallel()
	likeSvc := newLikePipeline(t)
	db := testkit.PG(t)
	ctx := t.Context()

	postID := newTestPost(t, db)
	c := &commentdomain.Comment{PostID: postID, UserID: sharedomain.NewID(), Content: "集成测试评论", ExtraData: []byte("{}")}
	if err := commentinfra.NewCommentRepository(db).Create(ctx, c); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	testkit.CleanupRows(t, "domains.comment", "id", c.ID)
	testkit.CleanupRows(t, "domains.comment_like", "comment_id", c.ID)
	testkit.CleanupKeys(t, redispkg.GetCommentStatsKey(c.ID))
	user := newTestUser(t)

	res, err := likeSvc.Toggle(ctx, user, likeapp.ToggleInput{Type: "comment", TargetID: c.ID})
	if err != nil || !res.IsLiked {
		t.Fatalf("Toggle(comment) = %+v, %v", res, err)
	}

	testkit.Eventually(t, pipelineTimeout, 200*time.Millisecond, "comment like persisted to PG", func() bool {
		var row struct {
			Deleted int
			PostID  *uuid.UUID
		}
		if err := db.Raw("SELECT deleted, post_id FROM domains.comment_like WHERE user_id = ? AND comment_id = ?", user, c.ID).
			Scan(&row).Error; err != nil {
			t.Fatalf("read comment_like: %v", err)
		}
		var likeCount int
		if err := db.Raw("SELECT like_count FROM domains.comment WHERE id = ?", c.ID).Scan(&likeCount).Error; err != nil {
			t.Fatalf("read comment like_count: %v", err)
		}
		return row.PostID != nil && *row.PostID == postID && row.Deleted == 0 && likeCount == 1
	})
}
