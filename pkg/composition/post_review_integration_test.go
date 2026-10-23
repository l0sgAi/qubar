//go:build integration

// post_review_integration_test.go 帖子人工审核（#118）端到端集成测试（make test-integration）：
// 真实 PG（post / circle_member / post_moderation_log / notification）+ circle 成员仓储桥接鉴权
// + Redpanda 通知事件 → 通知 consumer 落库。
//
// 覆盖：圈主 / 管理员可列表与处理，普通成员 / 禁言管理员 / 非成员 / 他圈管理员 403；
// 帖子不在该圈 / 已处理 404；并发通过 / 驳回只有一方生效（副作用与审计行各一次）；作者收到通过 / 驳回通知。
package composition

import (
	"sync"
	"testing"
	"time"

	circleinfra "interestBar/pkg/domains/circle/infrastructure"
	noticedomain "interestBar/pkg/domains/notice/domain"
	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const reviewNoticeWait = 45 * time.Second // 含消费者组首次 join + flush 间隔

var notificationConsumerOnce sync.Once

// reviewCircle 一个随机圈子及其成员（圈主 / 管理员 / 普通成员 / 禁言中的管理员）。
type reviewCircle struct {
	id                           uuid.UUID
	owner, admin, member, mutedA uuid.UUID
}

func newReviewCircle(t *testing.T, db *gorm.DB) reviewCircle {
	t.Helper()
	c := reviewCircle{
		id: sharedomain.NewID(), owner: sharedomain.NewID(), admin: sharedomain.NewID(),
		member: sharedomain.NewID(), mutedA: sharedomain.NewID(),
	}
	testkit.CleanupRows(t, "domains.circle_member", "circle_id", c.id)
	muteEnd := time.Now().Add(time.Hour)
	for _, m := range []struct {
		user         uuid.UUID
		role, status int16
		muteEnd      *time.Time
	}{
		{c.owner, 30, 1, nil},
		{c.admin, 20, 1, nil},
		{c.member, 10, 1, nil},
		{c.mutedA, 20, 2, &muteEnd},
	} {
		if err := db.Exec(`INSERT INTO domains.circle_member (id, circle_id, user_id, role, status, mute_end_time)
			VALUES (?, ?, ?, ?, ?, ?)`, sharedomain.NewID(), c.id, m.user, m.role, m.status, m.muteEnd).Error; err != nil {
			t.Fatalf("insert circle member: %v", err)
		}
	}
	return c
}

type reviewEnv struct {
	db  *gorm.DB
	svc postapp.PostService
	fx  *sideEffects
}

// setupReview 用真实仓储 / circle 成员仓储桥接 / Redpanda 发布器装配 PostService，发布副作用端口用桩；
// 启动进程内唯一的通知 consumer（把审核结果通知落库）。
func setupReview(t *testing.T) *reviewEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	notificationConsumerOnce.Do(func() {
		if err := redpanda.StartNotificationEventConsumer(); err != nil {
			t.Fatalf("start notification consumer: %v", err)
		}
	})

	fx := &sideEffects{counts: map[uuid.UUID]int{}, trigger: map[uuid.UUID]int{}}
	svc := postapp.NewPostService(postinfra.NewPostRepository(db), nil, nil, nil, nil, postinfra.NewPostEventPublisher())
	svc.SetUserFacade(anyUserFacade{})
	svc.SetMemberChecker(&circleMemberCheckerForPost{memberRepo: circleinfra.NewMemberRepository(db)})
	svc.SetPostCountPort(fx)
	svc.SetAgentTrigger(fx)
	svc.SetModeration(&scriptedModerator{results: map[string]scriptedResult{}, calls: map[string]int{}},
		postinfra.NewPostModerationLogRepository(db), activeModeration(false))
	return &reviewEnv{db: db, svc: svc, fx: fx}
}

// insertReviewingPost 直接落一篇审核中的帖子（带一个 @提及 与一条 AI 待审日志），返回帖子与作者 ID。
func insertReviewingPost(t *testing.T, env *reviewEnv, circleID uuid.UUID) (postID, authorID uuid.UUID) {
	t.Helper()
	repo := postinfra.NewPostRepository(env.db)
	authorID = sharedomain.NewID()
	p := &postdomain.Post{
		CircleID: circleID, UserID: authorID, Type: postdomain.PostTypeTextImage,
		Title: "人工审核-" + sharedomain.NewID().String(), Content: "待审正文",
		MediaExtra: postdomain.MediaExtraJSON{}, Status: postdomain.PostStatusReviewing,
	}
	if err := repo.Create(t.Context(), p); err != nil {
		t.Fatalf("create post: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", p.ID)
	testkit.CleanupRows(t, "domains.post_mention", "post_id", p.ID)
	testkit.CleanupRows(t, "domains.post_moderation_log", "post_id", p.ID)
	testkit.CleanupRows(t, "domains.notification", "post_id", p.ID)
	testkit.CleanupKeys(t, redispkg.GetNoticeUnreadKey(authorID))
	if err := repo.CreateMentions(t.Context(), p.ID, []uuid.UUID{sharedomain.NewID()}); err != nil {
		t.Fatalf("create mentions: %v", err)
	}
	sev, conf := 2.2, 0.9
	if err := postinfra.NewPostModerationLogRepository(env.db).Create(t.Context(), &postdomain.PostModerationLog{
		PostID: p.ID, Decision: postdomain.ModerationReview, Model: "jev-test",
		Hazards:  postdomain.HazardScores{postdomain.HazardSelfHarm: 0.93, postdomain.HazardSexual: 0.01},
		Severity: &sev, SeverityConfidence: &conf,
	}); err != nil {
		t.Fatalf("create AI log: %v", err)
	}
	return p.ID, authorID
}

func manualLogCount(t *testing.T, db *gorm.DB, postID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.Raw(`SELECT count(*) FROM domains.post_moderation_log WHERE post_id = ? AND source = ?`,
		postID, postdomain.ModerationSourceManual).Scan(&n).Error; err != nil {
		t.Fatalf("count manual logs: %v", err)
	}
	return n
}

// waitReviewNotice 等作者收到指定类型的通知，校验 actor 为处理人、snippet 为帖子标题快照。
func waitReviewNotice(t *testing.T, db *gorm.DB, authorID, postID, operatorID uuid.UUID, noticeType int16) {
	t.Helper()
	type row struct {
		ActorID uuid.UUID
		Snippet string
	}
	var rows []row
	testkit.Eventually(t, reviewNoticeWait, 200*time.Millisecond, "review result notice written", func() bool {
		rows = nil
		if err := db.Raw(`SELECT actor_id, snippet FROM domains.notification
			WHERE recipient_id = ? AND post_id = ? AND notice_type = ? AND deleted = 0`,
			authorID, postID, noticeType).Scan(&rows).Error; err != nil {
			t.Fatalf("query notification: %v", err)
		}
		return len(rows) > 0
	})
	if len(rows) != 1 || rows[0].ActorID != operatorID || rows[0].Snippet == "" {
		t.Fatalf("unexpected review notices: %+v", rows)
	}
}

// 圈主 / 管理员能看到本圈待审帖子与 AI 结论；无权限一律 403，不泄露帖子。
func TestPostReview_ListAndPermissions(t *testing.T) {
	t.Parallel()
	env := setupReview(t)
	c := newReviewCircle(t, env.db)
	other := newReviewCircle(t, env.db)
	postID, _ := insertReviewingPost(t, env, c.id)
	ctx := t.Context()

	for _, op := range []uuid.UUID{c.owner, c.admin} {
		res, err := env.svc.ListReviewQueue(ctx, op, c.id, 20, "")
		if err != nil {
			t.Fatalf("list by %s: %v", op, err)
		}
		if len(res.Posts) != 1 || res.Posts[0].ID != postID || res.Cursor != "" {
			t.Fatalf("queue = %+v", res)
		}
		v := res.Posts[0].AIVerdict
		if v == nil || v.Decision != "review" || len(v.HitHazards) != 1 || v.HitHazards[0] != "self_harm" ||
			v.Severity == nil || *v.Severity != 2.2 {
			t.Fatalf("AI verdict = %+v", v)
		}
	}

	forbidden := map[string]uuid.UUID{
		"member": c.member, "muted admin": c.mutedA, "stranger": sharedomain.NewID(), "other circle owner": other.owner,
	}
	for name, op := range forbidden {
		if _, err := env.svc.ListReviewQueue(ctx, op, c.id, 20, ""); !postapp.IsNotCircleModeratorErr(err) {
			t.Fatalf("%s list err = %v, want forbidden", name, err)
		}
		if _, err := env.svc.ReviewPost(ctx, op, postapp.ReviewPostInput{
			CircleID: c.id, PostID: postID, Action: postapp.ReviewApprove,
		}); !postapp.IsNotCircleModeratorErr(err) {
			t.Fatalf("%s review err = %v, want forbidden", name, err)
		}
	}

	// 他圈管理员拿自己的圈子 ID 处理本圈帖子：帖子不在该圈 → 404
	if _, err := env.svc.ReviewPost(ctx, other.owner, postapp.ReviewPostInput{
		CircleID: other.id, PostID: postID, Action: postapp.ReviewApprove,
	}); !postapp.IsReviewPostNotFoundErr(err) {
		t.Fatalf("cross-circle review err = %v, want not found", err)
	}

	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusReviewing {
		t.Fatalf("status = %d, rejected requests must not change it", got)
	}
	if n := manualLogCount(t, env.db, postID); n != 0 {
		t.Fatalf("manual logs = %d, want 0", n)
	}
}

// 通过：status=1，发布副作用一次，人工审计行一条，作者收到通过通知；再处理返回 404。
func TestPostReview_ApproveNotifiesAuthor(t *testing.T) {
	t.Parallel()
	env := setupReview(t)
	c := newReviewCircle(t, env.db)
	postID, authorID := insertReviewingPost(t, env, c.id)

	res, err := env.svc.ReviewPost(t.Context(), c.admin, postapp.ReviewPostInput{
		CircleID: c.id, PostID: postID, Action: postapp.ReviewApprove, Reason: "人工复核无问题",
	})
	if err != nil || res.Status != postdomain.PostStatusPublished {
		t.Fatalf("approve = %+v, %v", res, err)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusPublished {
		t.Fatalf("status = %d, want published", got)
	}
	if count, trigger := env.fx.get(c.id, postID); count != 1 || trigger != 1 {
		t.Fatalf("side effects count=%d trigger=%d, want 1/1", count, trigger)
	}
	if n := manualLogCount(t, env.db, postID); n != 1 {
		t.Fatalf("manual logs = %d, want 1", n)
	}
	if res, err := env.svc.ListReviewQueue(t.Context(), c.owner, c.id, 20, ""); err != nil || len(res.Posts) != 0 {
		t.Fatalf("queue after approve = %+v, %v; want empty", res, err)
	}
	if _, err := env.svc.ReviewPost(t.Context(), c.owner, postapp.ReviewPostInput{
		CircleID: c.id, PostID: postID, Action: postapp.ReviewReject,
	}); !postapp.IsReviewPostNotFoundErr(err) {
		t.Fatalf("second review err = %v, want not found", err)
	}

	waitReviewNotice(t, env.db, authorID, postID, c.admin, noticedomain.NoticeTypePostApproved)
}

// 驳回：status=3，无发布副作用，作者收到驳回通知。
func TestPostReview_RejectNotifiesAuthor(t *testing.T) {
	t.Parallel()
	env := setupReview(t)
	c := newReviewCircle(t, env.db)
	postID, authorID := insertReviewingPost(t, env, c.id)

	if _, err := env.svc.ReviewPost(t.Context(), c.owner, postapp.ReviewPostInput{
		CircleID: c.id, PostID: postID, Action: postapp.ReviewReject, Reason: "站外引流",
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusRejected {
		t.Fatalf("status = %d, want rejected", got)
	}
	if count, trigger := env.fx.get(c.id, postID); count != 0 || trigger != 0 {
		t.Fatalf("reject must not run side effects: count=%d trigger=%d", count, trigger)
	}
	waitReviewNotice(t, env.db, authorID, postID, c.owner, noticedomain.NoticeTypePostRejected)
}

// 圈主与管理员并发通过 / 驳回同一帖子：只有一方生效，其余 404；副作用与审计行都只有一次。
func TestPostReview_ConcurrentActionsOnlyOneWins(t *testing.T) {
	t.Parallel()
	env := setupReview(t)
	c := newReviewCircle(t, env.db)
	postID, _ := insertReviewingPost(t, env, c.id)

	const n = 10
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		winners  []postapp.ReviewAction
		notFound int
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			op, action := c.owner, postapp.ReviewApprove
			if i%2 == 1 {
				op, action = c.admin, postapp.ReviewReject
			}
			_, err := env.svc.ReviewPost(t.Context(), op, postapp.ReviewPostInput{CircleID: c.id, PostID: postID, Action: action})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners = append(winners, action)
			case postapp.IsReviewPostNotFoundErr(err):
				notFound++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if len(winners) != 1 || notFound != n-1 {
		t.Fatalf("winners=%v notFound=%d, want exactly one winner", winners, notFound)
	}
	if got := manualLogCount(t, env.db, postID); got != 1 {
		t.Fatalf("manual logs = %d, want 1", got)
	}
	wantStatus, wantCount := int16(postdomain.PostStatusRejected), 0
	if winners[0] == postapp.ReviewApprove {
		wantStatus, wantCount = postdomain.PostStatusPublished, 1
	}
	if got := postStatus(t, env.db, postID); got != wantStatus {
		t.Fatalf("status = %d, want %d", got, wantStatus)
	}
	if count, trigger := env.fx.get(c.id, postID); count != wantCount || trigger != wantCount {
		t.Fatalf("side effects count=%d trigger=%d, want %d", count, trigger, wantCount)
	}
}

// 人工审核与 AI worker 抢同一帖子：人工先通过后，worker 重投不再改状态、不调审核器。
func TestPostReview_WorkerSkipsAfterManualDecision(t *testing.T) {
	t.Parallel()
	env := setupReview(t)
	c := newReviewCircle(t, env.db)
	postID, _ := insertReviewingPost(t, env, c.id)

	if _, err := env.svc.ReviewPost(t.Context(), c.owner, postapp.ReviewPostInput{
		CircleID: c.id, PostID: postID, Action: postapp.ReviewReject,
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if err := env.svc.ModeratePost(t.Context(), postID); err != nil {
		t.Fatalf("ModeratePost after manual decision: %v", err)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusRejected {
		t.Fatalf("status = %d, worker must not override manual decision", got)
	}
}
