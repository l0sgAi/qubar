//go:build integration

// comment_count_integration_test.go 评论数改批量落库（#130）的写入侧端到端集成测试：
// 生产装配的 CommentService.CreateComment 并发发评论 → Redis post:stats 实时 +1（详情读路径在落库前即正确）、
// 不再同步 UPDATE domains.post、每条评论发布一条帖子统计事件。消费 / 落库侧见 redpanda 包的集成测试。
package composition

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/conf"
	commentapp "interestBar/pkg/domains/comment/application"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

func TestCreateComment_CountsInCacheAndPublishesWithoutSyncUpdate(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	ctx := t.Context()

	deps := NewDeps()
	postSvc := newPostService(deps)
	postSvc.SetUserFacade(anyUserFacade{})
	commentSvc := newCommentService(deps)
	commentSvc.SetPostLookup(&commentPostLookup{delegate: postSvc})

	postID := newTestPost(t, db)
	testkit.CleanupRows(t, "domains.comment", "post_id", postID)

	const comments = 12
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for range comments {
		wg.Go(func() {
			_, err := commentSvc.CreateComment(ctx, sharedomain.NewID(), commentapp.CreateCommentInput{
				PostID: postID, Content: "并发评论",
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	// 读路径：落库前详情的 comment_count 已是实时值（Redis 真值，与 like_count 同一口径）。
	detail, err := postSvc.GetPostDetail(ctx, uuid.Nil, postID)
	if err != nil {
		t.Fatalf("GetPostDetail: %v", err)
	}
	if detail.CommentCount != comments {
		t.Fatalf("detail comment_count = %d, want %d before flush", detail.CommentCount, comments)
	}

	// 写路径不再同步 UPDATE post：落库交给帖子统计消费者。
	var dbCount int
	if err := db.Raw("SELECT comment_count FROM domains.post WHERE id = ?", postID).Scan(&dbCount).Error; err != nil {
		t.Fatalf("read comment_count: %v", err)
	}
	if dbCount != 0 {
		t.Fatalf("post.comment_count = %d, want 0 (no synchronous UPDATE)", dbCount)
	}

	// 每条评论一条帖子统计事件。
	if got := countCommentCountEvents(t, postID, comments); got != comments {
		t.Fatalf("post_comment_count events = %d, want %d", got, comments)
	}
}

// countCommentCountEvents 从本进程专用的帖子统计 topic（单分区）开头读取 postID 的 post_comment_count 事件，读满 want 条或超时为止。
func countCommentCountEvents(t *testing.T, postID uuid.UUID, want int) int {
	t.Helper()
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   conf.Config.Redpanda.Brokers,
		Topic:     conf.Config.Redpanda.PostTopic,
		Partition: 0,
		MaxBytes:  10e6,
	})
	defer func() { _ = r.Close() }()

	readCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	key := postID.String()
	got := 0
	for got < want {
		m, err := r.ReadMessage(readCtx)
		if err != nil {
			return got
		}
		if string(m.Key) != key {
			continue
		}
		var msg redpanda.PostStatisticsMessage
		if err := json.Unmarshal(m.Value, &msg); err != nil {
			t.Fatalf("unmarshal post stats message: %v", err)
		}
		if msg.Type == redpanda.StatisticsTypePostComment && msg.PostID == postID {
			got++
		}
	}
	return got
}
