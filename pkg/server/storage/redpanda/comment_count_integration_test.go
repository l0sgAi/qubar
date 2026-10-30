//go:build integration

// comment_count_integration_test.go 评论数批量落库（#130）的集成测试：事件经真实 Redpanda topic 投递，
// 由生产 spec 的帖子统计消费者聚合、按评论明细重算 comment_count 落库，再校准 Redis post:stats。
// 覆盖并发评论后 flush、评论删除后计数减少、flush 失败重试与重投不重复计数。
package redpanda_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/conf"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// recordingCommitter 记录提交次数（替代 kafka.Reader 的提交能力）。
type recordingCommitter struct {
	mu    sync.Mutex
	calls int
}

func (c *recordingCommitter) CommitMessages(context.Context, ...kafka.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return nil
}

func (c *recordingCommitter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// newCommentCountPost 插一篇帖子，测试结束删除帖子、其评论与统计缓存。
func newCommentCountPost(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post", "id", id)
	testkit.CleanupRows(t, "domains.comment", "post_id", id)
	testkit.CleanupKeys(t, redispkg.GetPostStatsKey(id))
	if err := db.Exec("INSERT INTO domains.post (id, circle_id, user_id) VALUES (?, ?, ?)",
		id, sharedomain.NewID(), sharedomain.NewID()).Error; err != nil {
		t.Fatalf("insert post: %v", err)
	}
	return id
}

// addComment 插一条评论并发布评论数事件（与 CreateComment 的顺序一致：先落评论行，再发事件）。
func addComment(db *gorm.DB, postID uuid.UUID) (uuid.UUID, error) {
	id := sharedomain.NewID()
	if err := db.Exec("INSERT INTO domains.comment (id, post_id, user_id, content) VALUES (?, ?, ?, ?)",
		id, postID, sharedomain.NewID(), "集成测试评论").Error; err != nil {
		return uuid.Nil, err
	}
	return id, redpanda.PublishPostCommentCount(postID, 1)
}

// postStatsMessages 从本进程专用的帖子统计 topic（单分区）开头读取，返回 key 为 postID 的消息，至少 n 条。
func postStatsMessages(t *testing.T, postID uuid.UUID, n int) []kafka.Message {
	t.Helper()
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   conf.Config.Redpanda.Brokers,
		Topic:     conf.Config.Redpanda.PostTopic,
		Partition: 0,
		MaxBytes:  10e6,
	})
	defer func() { _ = r.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	key := postID.String()
	var out []kafka.Message
	for len(out) < n {
		m, err := r.ReadMessage(ctx)
		if err != nil {
			t.Fatalf("read post stats messages: got %d/%d: %v", len(out), n, err)
		}
		if string(m.Key) == key {
			out = append(out, m)
		}
	}
	return out
}

func commentCount(t *testing.T, db *gorm.DB, postID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.Raw("SELECT comment_count FROM domains.post WHERE id = ?", postID).Scan(&n).Error; err != nil {
		t.Fatalf("read comment_count: %v", err)
	}
	return n
}

func TestPostCommentCount_ConcurrentCommentsThenFlush(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)

	postID := newCommentCountPost(t, db)
	// 缓存在评论落库前由 PG 回填，漏掉了窗口内的评论：落库后应被抬回 PG 值。
	if err := redispkg.UpdatePostStatistics(postID, &redispkg.PostStatistics{CommentCount: 1}); err != nil {
		t.Fatal(err)
	}

	const comments = 30
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	record := func(err error) {
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}
	for range comments {
		wg.Go(func() {
			_, err := addComment(db, postID)
			record(err)
		})
	}
	// 同时有热度批量写入同一行（统一取锁顺序，#124）
	wg.Go(func() {
		for range 5 {
			record(redpanda.UpdatePostHotForTest([]uuid.UUID{postID}, 1))
		}
	})
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("concurrent comments: %v", err)
	}

	committer := &recordingCommitter{}
	c := redpanda.NewPostStatsConsumerForTest(committer, nil)
	for _, m := range postStatsMessages(t, postID, comments) {
		c.Add(m)
	}
	c.Flush()

	if got := commentCount(t, db, postID); got != comments {
		t.Fatalf("comment_count = %d, want %d", got, comments)
	}
	if committer.count() != 1 {
		t.Fatalf("commits = %d, want 1 after successful flush", committer.count())
	}
	if stats, err := redispkg.GetPostStatistics(postID); err != nil || stats == nil || stats.CommentCount != comments {
		t.Fatalf("cached stats = %+v, %v; want comment_count %d", stats, err, comments)
	}
}

func TestPostCommentCount_DeleteDecreases(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)

	postID := newCommentCountPost(t, db)
	var ids []uuid.UUID
	for range 3 {
		id, err := addComment(db, postID)
		if err != nil {
			t.Fatalf("add comment: %v", err)
		}
		ids = append(ids, id)
	}
	c := redpanda.NewPostStatsConsumerForTest(&recordingCommitter{}, nil)
	for _, m := range postStatsMessages(t, postID, 3) {
		c.Add(m)
	}
	c.Flush()
	if got := commentCount(t, db, postID); got != 3 {
		t.Fatalf("comment_count = %d, want 3", got)
	}

	// 逻辑删除一条评论并发布变化事件：重算后计数减少
	if err := db.Exec("UPDATE domains.comment SET deleted = 1 WHERE id = ?", ids[0]).Error; err != nil {
		t.Fatalf("soft delete comment: %v", err)
	}
	if err := redpanda.PublishPostCommentCount(postID, -1); err != nil {
		t.Fatalf("publish delete: %v", err)
	}
	msgs := postStatsMessages(t, postID, 4)
	for _, m := range msgs[3:] {
		c.Add(m)
	}
	c.Flush()
	if got := commentCount(t, db, postID); got != 2 {
		t.Fatalf("comment_count after delete = %d, want 2", got)
	}
}

func TestPostCommentCount_RetryAndRedeliveryDoNotDoubleCount(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)

	postID := newCommentCountPost(t, db)
	const comments = 4
	for range comments {
		if _, err := addComment(db, postID); err != nil {
			t.Fatalf("add comment: %v", err)
		}
	}
	msgs := postStatsMessages(t, postID, comments)

	// 第一次 flush：落库已提交，但随后报错（如确认丢失）→ 缓冲合并回、offset 不提交，下次 flush 重试。
	failures := 1
	committer := &recordingCommitter{}
	c := redpanda.NewPostStatsConsumerForTest(committer, func() error {
		if failures > 0 {
			failures--
			return errors.New("injected failure after persist")
		}
		return nil
	})
	for _, m := range msgs {
		c.Add(m)
	}
	c.Flush()
	if committer.count() != 0 {
		t.Fatalf("offsets committed after failed flush")
	}
	if got := commentCount(t, db, postID); got != comments {
		t.Fatalf("comment_count after first (failed) flush = %d, want %d", got, comments)
	}

	c.Flush() // 重试同一批
	if got := commentCount(t, db, postID); got != comments {
		t.Fatalf("comment_count after retry = %d, want %d (no double count)", got, comments)
	}
	if committer.count() != 1 {
		t.Fatalf("commits after retry = %d, want 1", committer.count())
	}

	// 重投（提交前进程被杀 / 再均衡）：同一批消息再处理一次，计数不变，且不写出新的行版本（不产生 WAL / CDC 事件）。
	var xminBefore, xminAfter string
	if err := db.Raw("SELECT xmin::text FROM domains.post WHERE id = ?", postID).Scan(&xminBefore).Error; err != nil {
		t.Fatal(err)
	}
	redelivered := redpanda.NewPostStatsConsumerForTest(&recordingCommitter{}, nil)
	for _, m := range msgs {
		redelivered.Add(m)
	}
	redelivered.Flush()
	if got := commentCount(t, db, postID); got != comments {
		t.Fatalf("comment_count after redelivery = %d, want %d", got, comments)
	}
	if err := db.Raw("SELECT xmin::text FROM domains.post WHERE id = ?", postID).Scan(&xminAfter).Error; err != nil {
		t.Fatal(err)
	}
	if xminAfter != xminBefore {
		t.Fatalf("redelivery rewrote the post row (xmin %s → %s); unchanged recount must be skipped", xminBefore, xminAfter)
	}
}
