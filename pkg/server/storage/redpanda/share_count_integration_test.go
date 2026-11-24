//go:build integration

// share_count_integration_test.go 分享数批量落库（#232）的集成测试：「有新分享」标记经真实 Redpanda topic 投递，
// 由生产 spec 的帖子统计消费者聚合、按 domains.post_share 行数重算 share_count，再校准 Redis post:sharecnt。
// 覆盖落库后计数正确、缓存只升不降、重投 / 重复标记不重复计数、值不变时不写行（不产生 CDC 事件）。
package redpanda_test

import (
	"testing"

	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func shareCount(t *testing.T, db *gorm.DB, postID uuid.UUID) (n int, xmin string) {
	t.Helper()
	var row struct {
		ShareCount int
		Xmin       string
	}
	if err := db.Raw("SELECT share_count, xmin::text AS xmin FROM domains.post WHERE id = ?", postID).Scan(&row).Error; err != nil {
		t.Fatalf("read share_count: %v", err)
	}
	return row.ShareCount, row.Xmin
}

func TestPostShareCount_RecountFromRowsAndReconcileCache(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)

	postID := newCommentCountPost(t, db)
	testkit.CleanupRows(t, "domains.post_share", "post_id", postID)
	testkit.CleanupKeys(t, redispkg.GetPostShareCountKey(postID))

	// 缓存在并发首次分享交错时回种偏低：落库后应抬回 PG 值。
	if _, err := redispkg.SeedPostShareCount(t.Context(), postID, 1); err != nil {
		t.Fatal(err)
	}

	const shares = 5
	for range shares {
		if err := db.Exec("INSERT INTO domains.post_share (id, user_id, post_id) VALUES (?, ?, ?)",
			sharedomain.NewID(), sharedomain.NewID(), postID).Error; err != nil {
			t.Fatalf("insert post_share: %v", err)
		}
		if err := redpanda.PublishPostShareCount(postID); err != nil {
			t.Fatalf("publish share mark: %v", err)
		}
	}
	// 多余的标记（如重试重发）不影响结果：落库按行数重算，不累加 value
	if err := redpanda.PublishPostShareCount(postID); err != nil {
		t.Fatal(err)
	}

	committer := &recordingCommitter{}
	c := redpanda.NewPostStatsConsumerForTest(committer, nil)
	msgs := postStatsMessages(t, postID, shares+1)
	for _, m := range msgs {
		c.Add(m)
	}
	c.Flush()

	got, xmin := shareCount(t, db, postID)
	if got != shares {
		t.Fatalf("share_count = %d, want %d", got, shares)
	}
	if committer.count() != 1 {
		t.Fatalf("commits = %d, want 1", committer.count())
	}
	if v, ok, err := redispkg.GetPostShareCount(t.Context(), postID); err != nil || !ok || v != shares {
		t.Fatalf("cached share count = %d, %v, %v; want raised to %d", v, ok, err, shares)
	}

	// 重投同一批标记：重算结果不变，不写行（xmin 不变即没有新的行版本，也就没有 WAL / CDC 事件）
	for _, m := range msgs {
		c.Add(m)
	}
	c.Flush()
	if again, xmin2 := shareCount(t, db, postID); again != shares || xmin2 != xmin {
		t.Fatalf("after redelivery share_count=%d xmin=%s, want %d and unchanged xmin %s", again, xmin2, shares, xmin)
	}

	// 缓存高于 PG（含尚未落库的分享）时不降
	if err := redispkg.Client.Set(t.Context(), redispkg.GetPostShareCountKey(postID), shares+3, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := redpanda.PublishPostShareCount(postID); err != nil {
		t.Fatal(err)
	}
	for _, m := range postStatsMessages(t, postID, shares+2)[shares+1:] {
		c.Add(m)
	}
	c.Flush()
	if v, _, _ := redispkg.GetPostShareCount(t.Context(), postID); v != shares+3 {
		t.Fatalf("cached share count = %d, want %d kept (only raise)", v, shares+3)
	}
}
