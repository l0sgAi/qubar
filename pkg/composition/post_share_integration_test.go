//go:build integration

// post_share_integration_test.go 帖子分享（#232）写入侧端到端集成测试（make test-integration）：
// 生产装配的 ShareService.SharePost（post 桥接可分享校验）→ post_share 去重插入 → 分享事件（同步 ack）
// → 帖子统计「有新分享」标记 + 帖子热度增量 → Redis post:sharecnt 读路径计数。
// 覆盖首次 / 重复分享、同一用户并发只计一次、不同用户并发累加、不同步 UPDATE post、私密圈成员可分享 / 非成员 404、
// 未发布帖子 404。消费 / 落库侧（share_count 按 post_share 重算）见 redpanda 包的 share_count_integration_test.go。
package composition

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"interestBar/pkg/conf"
	circledomain "interestBar/pkg/domains/circle/domain"
	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"
	shareapp "interestBar/pkg/domains/share/application"
	sharedom "interestBar/pkg/domains/share/domain"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// cleanupShares 注册分享记录与分享数缓存的清理。
func cleanupShares(t *testing.T, postID uuid.UUID) {
	t.Helper()
	testkit.CleanupRows(t, "domains.post_share", "post_id", postID)
	testkit.CleanupKeys(t, redispkg.GetPostShareCountKey(postID))
}

// readKeyed 从本进程专用 topic（单分区）开头读取 key == postID 的消息：读满 want 条后再等 settle，
// 用来断言「恰好 want 条」（多出来的也会读到并返回）。
func readKeyed(t *testing.T, topic string, postID uuid.UUID, want int, settle time.Duration) []kafka.Message {
	t.Helper()
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   conf.Config.Redpanda.Brokers,
		Topic:     topic,
		Partition: 0,
		MaxBytes:  10e6,
	})
	defer func() { _ = r.Close() }()

	key := postID.String()
	var out []kafka.Message
	read := func(timeout time.Duration) bool {
		ctx, cancel := context.WithTimeout(t.Context(), timeout)
		defer cancel()
		m, err := r.ReadMessage(ctx)
		if err != nil {
			return false
		}
		if string(m.Key) == key {
			out = append(out, m)
		}
		return true
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(out) < want && time.Now().Before(deadline) {
		if !read(time.Until(deadline)) {
			break
		}
	}
	for read(settle) {
	}
	return out
}

// newShareTestService 按 RegisterDomainRoutes 的方式装配 ShareService（post 桥接可分享校验）。
func newShareTestService(postSvc postapp.PostService) shareapp.ShareService {
	svc := newShareService(NewDeps())
	svc.SetPostTarget(&sharePostTarget{delegate: postSvc})
	return svc
}

func TestSharePost_FirstRepeatConcurrentAndEvents(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	ctx := t.Context()

	postSvc := newPostService(NewDeps())
	shareSvc := newShareTestService(postSvc)

	postID := newTestPost(t, db)
	cleanupShares(t, postID)
	var post struct {
		UserID   uuid.UUID
		CircleID uuid.UUID
	}
	if err := db.Raw("SELECT user_id, circle_id FROM domains.post WHERE id = ?", postID).Scan(&post).Error; err != nil {
		t.Fatalf("load post: %v", err)
	}

	share := func(user uuid.UUID, wantFirst bool, wantCount int64) {
		t.Helper()
		got, err := shareSvc.SharePost(ctx, user, postID)
		if err != nil || got.FirstShare != wantFirst || got.ShareCount != wantCount {
			t.Fatalf("SharePost = %+v, %v; want first_share=%v share_count=%d", got, err, wantFirst, wantCount)
		}
	}
	a, b := sharedomain.NewID(), sharedomain.NewID()
	share(a, true, 1)
	share(a, false, 1) // 重复分享：幂等成功、不重复计数
	share(a, false, 1)
	share(b, true, 2)

	// 并发：同一用户 10 个请求只计一次；另外 5 个用户同时各分享一次
	same := sharedomain.NewID()
	var firstSame atomic.Int32
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	record := func(err error) {
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}
	for range 10 {
		wg.Go(func() {
			got, err := shareSvc.SharePost(ctx, same, postID)
			record(err)
			if err == nil && got.FirstShare {
				firstSame.Add(1)
			}
		})
	}
	for range 5 {
		wg.Go(func() {
			got, err := shareSvc.SharePost(ctx, sharedomain.NewID(), postID)
			record(err)
			if err == nil && !got.FirstShare {
				record(errors.New("distinct user share reported first_share=false"))
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("concurrent shares: %v", err)
	}
	if firstSame.Load() != 1 {
		t.Fatalf("same-user concurrent first_share = %d, want 1", firstSame.Load())
	}

	const total = 8 // a, b, same, 5 个并发用户
	share(a, false, total)

	assertRowCount(t, db, `SELECT count(*) FROM domains.post_share WHERE post_id = ?`, total, postID)
	var dbShareCount int
	if err := db.Raw("SELECT share_count FROM domains.post WHERE id = ?", postID).Scan(&dbShareCount).Error; err != nil {
		t.Fatalf("read share_count: %v", err)
	}
	if dbShareCount != 0 {
		t.Fatalf("post.share_count = %d, want 0 (no synchronous UPDATE; persisted by post stats consumer)", dbShareCount)
	}

	// 分享事件：每个首次分享恰好一条，share_id 对应 post_share 行，带作者与圈子。
	var shareIDs []uuid.UUID
	if err := db.Raw("SELECT id FROM domains.post_share WHERE post_id = ?", postID).Scan(&shareIDs).Error; err != nil {
		t.Fatalf("load share ids: %v", err)
	}
	events := readKeyed(t, conf.Config.Redpanda.ShareEventTopic, postID, total, 2*time.Second)
	if len(events) != total {
		t.Fatalf("share events = %d, want %d", len(events), total)
	}
	seen := map[uuid.UUID]bool{}
	for _, m := range events {
		var ev redpanda.ShareEventMessage
		if err := json.Unmarshal(m.Value, &ev); err != nil {
			t.Fatalf("unmarshal share event: %v", err)
		}
		if ev.Type != redpanda.ShareEventType || ev.PostID != postID || ev.AuthorID != post.UserID ||
			ev.CircleID != post.CircleID || ev.UserID == uuid.Nil || ev.Ts == 0 {
			t.Fatalf("share event = %+v", ev)
		}
		seen[ev.ShareID] = true
	}
	for _, id := range shareIDs {
		if !seen[id] {
			t.Fatalf("post_share row %s has no share event", id)
		}
	}

	// 帖子统计「有新分享」标记：每个首次分享一条（消费者按 post_share 重算，条数不影响结果）。
	marks := 0
	for _, m := range readKeyed(t, conf.Config.Redpanda.PostTopic, postID, total, time.Second) {
		var msg redpanda.PostStatisticsMessage
		if err := json.Unmarshal(m.Value, &msg); err == nil && msg.Type == redpanda.StatisticsTypePostShare {
			marks++
		}
	}
	if marks != total {
		t.Fatalf("post_share_count marks = %d, want %d", marks, total)
	}

	// 热度：每个首次分享 +hot.weight.post_share，重复分享不加。
	var hot int64
	hotMsgs := readKeyed(t, conf.Config.Redpanda.PostHotTopic, postID, total, time.Second)
	for _, m := range hotMsgs {
		var msg redpanda.PostHotMessage
		if err := json.Unmarshal(m.Value, &msg); err != nil {
			t.Fatalf("unmarshal post hot: %v", err)
		}
		hot += msg.Delta
	}
	if want := int64(total * conf.Config.Hot.Weight.PostShare); len(hotMsgs) != total || hot != want {
		t.Fatalf("post hot = %d over %d messages, want %d over %d", hot, len(hotMsgs), want, total)
	}
}

// 缓存冷启动：已有分享记录（如缓存过期）时，下一次首次分享按记录数回种，不从 0 起算。
func TestSharePost_ColdCacheSeedsFromRows(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	ctx := t.Context()

	postSvc := newPostService(NewDeps())
	shareSvc := newShareTestService(postSvc)
	postID := newTestPost(t, db)
	cleanupShares(t, postID)

	for range 3 {
		if err := db.Exec("INSERT INTO domains.post_share (id, user_id, post_id) VALUES (?, ?, ?)",
			sharedomain.NewID(), sharedomain.NewID(), postID).Error; err != nil {
			t.Fatalf("insert post_share: %v", err)
		}
	}
	got, err := shareSvc.SharePost(ctx, sharedomain.NewID(), postID)
	if err != nil || !got.FirstShare || got.ShareCount != 4 {
		t.Fatalf("SharePost on cold cache = %+v, %v; want first share, share_count 4", got, err)
	}
}

// 不可分享：未发布（审核中）/ 已删除 / 不存在的帖子 404，不写记录。
func TestSharePost_UnpublishedOrMissingNotFound(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	ctx := t.Context()

	postSvc := newPostService(NewDeps())
	shareSvc := newShareTestService(postSvc)

	reviewing := newTestPost(t, db)
	deleted := newTestPost(t, db)
	cleanupShares(t, reviewing)
	cleanupShares(t, deleted)
	if err := db.Exec("UPDATE domains.post SET status = ? WHERE id = ?", postdomain.PostStatusReviewing, reviewing).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE domains.post SET deleted = 1 WHERE id = ?", deleted).Error; err != nil {
		t.Fatal(err)
	}

	user := sharedomain.NewID()
	for name, id := range map[string]uuid.UUID{"reviewing": reviewing, "deleted": deleted, "missing": sharedomain.NewID()} {
		if _, err := shareSvc.SharePost(ctx, user, id); !errors.Is(err, sharedom.ErrPostNotFound) {
			t.Fatalf("%s: err = %v, want ErrPostNotFound", name, err)
		}
	}
	assertRowCount(t, db, `SELECT count(*) FROM domains.post_share WHERE user_id = ?`, 0, user)
}

// 私密圈：可分享范围与 CanViewPost 一致——正常 / 禁言成员可分享；非成员、已移出、已退出成员 404。
func TestSharePost_PrivateCircleVisibility(t *testing.T) {
	t.Parallel()
	env := setupVisibility(t)
	c := newVisCircle(t, env, circledomain.CircleJoinTypePrivate)
	cleanupShares(t, c.postID)
	ctx := t.Context()

	shareSvc := newShareTestService(env.postSvc)

	for _, v := range c.viewers() {
		if v.id == uuid.Nil {
			continue // 分享接口要求登录
		}
		t.Run(v.name, func(t *testing.T) {
			got, err := shareSvc.SharePost(ctx, v.id, c.postID)
			if v.visible {
				if err != nil || !got.FirstShare {
					t.Fatalf("member share = %+v, %v; want first share", got, err)
				}
				return
			}
			if !errors.Is(err, sharedom.ErrPostNotFound) {
				t.Fatalf("non-member share err = %v, want ErrPostNotFound", err)
			}
		})
	}
	assertRowCount(t, env.db, `SELECT count(*) FROM domains.post_share WHERE post_id = ?`, 2, c.postID)
}
