//go:build integration

package redpanda_test

import (
	"testing"

	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// assertPostStatsMirror 同时核对主表和镜像的四个绝对值，返回镜像行版本，供重放后比较。
func assertPostStatsMirror(t *testing.T, db *gorm.DB, id uuid.UUID, views, comments, likes, collects int) string {
	t.Helper()
	var xmin string
	err := db.Raw(`SELECT s.xmin::text FROM domains.post p
		JOIN domains.post_stats s ON s.post_id = p.id
		WHERE p.id = ? AND (p.view_count, p.comment_count, p.like_count, p.collect_count) = (?, ?, ?, ?)
		AND (s.view_count, s.comment_count, s.like_count, s.collect_count) =
			(p.view_count, p.comment_count, p.like_count, p.collect_count)`,
		id, views, comments, likes, collects).Row().Scan(&xmin)
	if err != nil {
		t.Fatalf("post %s: expected both rows to have counts (%d,%d,%d,%d): %v", id, views, comments, likes, collects, err)
	}
	return xmin
}

// 模拟 M1 之后由旧应用创建且已有四项计数、还没有镜像行的帖子。
func newUnmirroredPost(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := newCommentCountPost(t, db)
	if err := db.Exec("UPDATE domains.post SET view_count=11, comment_count=2, like_count=3, collect_count=1 WHERE id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPostStatsMirror_LikeReplay(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	id := newUnmirroredPost(t, db)
	testkit.CleanupRows(t, "domains.post_like", "post_id", id)
	userID := sharedomain.NewID()
	like := func() {
		t.Helper()
		if err := redpanda.PersistPostLikesForTest(userID, []uuid.UUID{id}); err != nil {
			t.Fatal(err)
		}
	}

	like()
	before := assertPostStatsMirror(t, db, id, 11, 2, 4, 1)
	like()
	if after := assertPostStatsMirror(t, db, id, 11, 2, 4, 1); after != before {
		t.Fatalf("replayed like rewrote post_stats: xmin %s -> %s", before, after)
	}
}

func TestPostStatsMirror_CollectDeltas(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	id := newUnmirroredPost(t, db)
	apply := func(delta int64) {
		t.Helper()
		if err := redpanda.UpdatePostCollectsForTest([]uuid.UUID{id}, delta); err != nil {
			t.Fatal(err)
		}
	}

	apply(2)
	assertPostStatsMirror(t, db, id, 11, 2, 3, 3)
	apply(-3)
	before := assertPostStatsMirror(t, db, id, 11, 2, 3, 0)
	// 收藏事件是增量。重复取消在零触底，主表仍会 UPDATE，但镜像的绝对值相同。
	apply(-3)
	if after := assertPostStatsMirror(t, db, id, 11, 2, 3, 0); after != before {
		t.Fatalf("unchanged collect count rewrote post_stats: xmin %s -> %s", before, after)
	}
}
