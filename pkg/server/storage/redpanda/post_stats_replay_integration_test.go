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

// assertPostStats 检查持久化的四项计数，返回统计行版本，用于判断重放是否产生 UPDATE。
func assertPostStats(t *testing.T, db *gorm.DB, id uuid.UUID, views, comments, likes, collects int) string {
	t.Helper()
	var got [4]int
	var xmin string
	err := db.Raw(`SELECT view_count, comment_count, like_count, collect_count, xmin::text
		FROM domains.post_stats WHERE post_id = ?`, id).Row().Scan(&got[0], &got[1], &got[2], &got[3], &xmin)
	if err != nil {
		t.Fatalf("read post_stats for %s: %v", id, err)
	}
	if want := [4]int{views, comments, likes, collects}; got != want {
		t.Fatalf("post %s: got counts %v, want %v (view/comment/like/collect)", id, got, want)
	}
	return xmin
}

// 模拟切换后已有持久化计数的帖子；主表旧列保留初始值。
func newPostWithStats(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := newCommentCountPost(t, db)
	if err := db.Exec(`INSERT INTO domains.post_stats (post_id, view_count, comment_count, like_count, collect_count)
		VALUES (?, 11, 2, 3, 1)`, id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPostStats_LikeReplay(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	id := newPostWithStats(t, db)
	testkit.CleanupRows(t, "domains.post_like", "post_id", id)
	userID := sharedomain.NewID()
	like := func() {
		t.Helper()
		if err := redpanda.PersistPostLikesForTest(userID, []uuid.UUID{id}); err != nil {
			t.Fatal(err)
		}
	}

	like()
	before := assertPostStats(t, db, id, 11, 2, 4, 1)
	like()
	if after := assertPostStats(t, db, id, 11, 2, 4, 1); after != before {
		t.Fatalf("replayed like rewrote post_stats: xmin %s -> %s", before, after)
	}
}

func TestPostStats_CollectDeltas(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	id := newPostWithStats(t, db)
	apply := func(delta int64) {
		t.Helper()
		if err := redpanda.UpdatePostCollectsForTest([]uuid.UUID{id}, delta); err != nil {
			t.Fatal(err)
		}
	}

	apply(2)
	assertPostStats(t, db, id, 11, 2, 3, 3)
	apply(-3)
	before := assertPostStats(t, db, id, 11, 2, 3, 0)
	// 收藏数已归零，再次扣减时应保留统计行版本。
	apply(-3)
	if after := assertPostStats(t, db, id, 11, 2, 3, 0); after != before {
		t.Fatalf("unchanged collect count rewrote post_stats: xmin %s -> %s", before, after)
	}
}

// 帖子统计 topic 也支持收藏增量，同一条扣减消息重投时遵循相同的零值下限。
func TestPostStats_CollectDeltaOnStatsTopic(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redpanda(t)
	id := newPostWithStats(t, db)
	if err := redpanda.PublishPostCollectCount(id, -3); err != nil {
		t.Fatal(err)
	}
	msg := postStatsMessages(t, id, 1)[0]
	committer := &recordingCommitter{}
	c := redpanda.NewPostStatsConsumerForTest(committer, nil)
	c.Add(msg)
	c.Flush()
	before := assertPostStats(t, db, id, 11, 2, 3, 0)
	c.Add(msg)
	c.Flush()
	if after := assertPostStats(t, db, id, 11, 2, 3, 0); after != before {
		t.Fatalf("replayed collect decrement rewrote post_stats: xmin %s -> %s", before, after)
	}
	if committer.count() != 2 {
		t.Fatalf("commits = %d, want 2", committer.count())
	}
}
