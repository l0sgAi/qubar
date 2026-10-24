//go:build integration

// post_report_integration_test.go 先发后审与举报复核（#153）端到端集成测试（make test-integration）：
// 真实 PG（post / post_report / circle_member / post_moderation_log / notification）+ circle 成员仓储桥接鉴权
// + Redpanda 通知事件 → 通知 consumer 按圈子扇出给圈主 / 管理员。
//
// 覆盖：举报唯一约束、每轮只通知一次（并发举报）、24 小时上限、圈管通知扇出（不含普通成员 / 禁言管理员 / 作者）、
// 并发复核只有一方生效、下架通知作者、自动屏蔽（不进 #118 待审队列，dismiss 恢复）、AI 待审帖子 self_harm 通知圈管。
package composition

import (
	"errors"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/conf"
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

type reportEnv struct {
	db  *gorm.DB
	svc postapp.PostService
	mod *scriptedModerator
	fx  *sideEffects
}

// setupReport 用真实仓储（帖子 / 举报 / 审核日志）、circle 成员仓储桥接与 Redpanda 发布器装配 PostService，
// 审核器与发布副作用端口用桩、不发审核事件；启动进程内唯一的通知 consumer。
func setupReport(t *testing.T, cfg conf.Moderation) *reportEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	startNotificationConsumer(t)

	env := &reportEnv{
		db:  db,
		mod: &scriptedModerator{results: map[string]scriptedResult{}, calls: map[string]int{}},
		fx:  &sideEffects{counts: map[uuid.UUID]int{}, trigger: map[uuid.UUID]int{}},
	}
	env.svc = newDirectModerationService(db, env.mod, env.fx, cfg)
	env.svc.SetMemberChecker(&circleMemberCheckerForPost{memberRepo: circleinfra.NewMemberRepository(db)})
	env.svc.SetReportRepository(postinfra.NewPostReportRepository(db))
	return env
}

// startNotificationConsumer 启动进程内唯一的通知 consumer（与人工审核测试共用）。
func startNotificationConsumer(t *testing.T) {
	t.Helper()
	notificationConsumerOnce.Do(func() {
		if err := redpanda.StartNotificationEventConsumer(); err != nil {
			t.Fatalf("start notification consumer: %v", err)
		}
	})
}

// insertPost 直接落一篇帖子（不发事件），注册帖子 / 举报 / 审计 / 通知清理；返回帖子与作者 ID。
func insertPost(t *testing.T, db *gorm.DB, c reviewCircle, status int16) (postID, authorID uuid.UUID) {
	t.Helper()
	authorID = sharedomain.NewID()
	p := &postdomain.Post{
		CircleID: c.id, UserID: authorID, Type: postdomain.PostTypeTextImage,
		Title: "举报复核-" + sharedomain.NewID().String(), Content: "正文",
		MediaExtra: postdomain.MediaExtraJSON{}, Status: status,
	}
	if err := postinfra.NewPostRepository(db).Create(t.Context(), p); err != nil {
		t.Fatalf("create post: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", p.ID)
	testkit.CleanupRows(t, "domains.post_mention", "post_id", p.ID)
	testkit.CleanupRows(t, "domains.post_report", "post_id", p.ID)
	testkit.CleanupRows(t, "domains.post_moderation_log", "post_id", p.ID)
	testkit.CleanupRows(t, "domains.notification", "post_id", p.ID)
	testkit.CleanupKeys(t, redispkg.GetNoticeUnreadKey(authorID), redispkg.GetNoticeUnreadKey(c.owner),
		redispkg.GetNoticeUnreadKey(c.admin), redispkg.GetNoticeUnreadKey(c.member), redispkg.GetNoticeUnreadKey(c.mutedA))
	return p.ID, authorID
}

func reportCfg(notify, hide, limit int) conf.Moderation {
	cfg := activeModeration(true)
	cfg.ReportNotifyThreshold, cfg.ReportHideThreshold, cfg.ReportDailyLimit = notify, hide, limit
	return cfg
}

func report(t *testing.T, svc postapp.PostService, reporter, postID uuid.UUID) error {
	t.Helper()
	return svc.ReportPost(t.Context(), reporter, postapp.ReportPostInput{
		PostID: postID, ReasonType: postdomain.ReportReasonSpamAd, Reason: "站外引流",
	})
}

type reportCounts struct {
	Total, Pending, Notified int
}

func countReports(t *testing.T, db *gorm.DB, postID uuid.UUID) reportCounts {
	t.Helper()
	var c reportCounts
	if err := db.Raw(`SELECT count(*) AS total, count(*) FILTER (WHERE status = 0) AS pending,
			count(*) FILTER (WHERE notified = 1) AS notified
		FROM domains.post_report WHERE post_id = ?`, postID).Scan(&c).Error; err != nil {
		t.Fatalf("count reports: %v", err)
	}
	return c
}

type noticeRow struct {
	RecipientID uuid.UUID
	ActorID     uuid.UUID
	Snippet     string
}

// waitNotices 等帖子上指定类型的通知达到 want 条，返回全部行（之后再多等一个 flush 周期确认没有多余接收人）。
func waitNotices(t *testing.T, db *gorm.DB, postID uuid.UUID, noticeType int16, want int) []noticeRow {
	t.Helper()
	var rows []noticeRow
	query := func() {
		rows = nil
		if err := db.Raw(`SELECT recipient_id, actor_id, snippet FROM domains.notification
			WHERE post_id = ? AND notice_type = ? AND deleted = 0`, postID, noticeType).Scan(&rows).Error; err != nil {
			t.Fatalf("query notifications: %v", err)
		}
	}
	testkit.Eventually(t, reviewNoticeWait, 200*time.Millisecond, "notifications written", func() bool {
		query()
		return len(rows) >= want
	})
	time.Sleep(6 * time.Second) // 一个 flush 周期（默认 5s）：多出来的接收人也会落库
	query()
	return rows
}

func recipients(rows []noticeRow) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		out[r.RecipientID] = true
	}
	return out
}

// 举报唯一约束 + 默认阈值 1：首条举报通知圈主与管理员（actor 为空、snippet 为标题），普通成员 / 禁言管理员 / 作者收不到；
// 同一用户再举报 409，表里只有一行。
func TestPostReport_UniqueAndNotifyModerators(t *testing.T) {
	t.Parallel()
	env := setupReport(t, activeModeration(true))
	c := newReviewCircle(t, env.db)
	postID, authorID := insertPost(t, env.db, c, postdomain.PostStatusPublished)

	if err := report(t, env.svc, c.member, postID); err != nil {
		t.Fatalf("report: %v", err)
	}
	if err := report(t, env.svc, c.member, postID); !postapp.IsAlreadyReportedErr(err) {
		t.Fatalf("duplicate err = %v, want already reported", err)
	}
	// 仓储层：唯一约束兜底（绕过应用层校验直接写）
	_, err := postinfra.NewPostReportRepository(env.db).Create(t.Context(), &postdomain.PostReport{
		PostID: postID, CircleID: c.id, ReporterID: c.member, ReasonType: 1,
	}, postdomain.ReportThresholds{Notify: 1})
	if !errors.Is(err, postdomain.ErrPostReportDuplicate) {
		t.Fatalf("repo duplicate err = %v", err)
	}
	if got := countReports(t, env.db, postID); got != (reportCounts{Total: 1, Pending: 1, Notified: 1}) {
		t.Fatalf("reports = %+v", got)
	}
	if err := report(t, env.svc, authorID, postID); !postapp.IsCannotReportOwnPostErr(err) {
		t.Fatalf("own post err = %v", err)
	}

	rows := waitNotices(t, env.db, postID, noticedomain.NoticeTypePostReported, 2)
	got := recipients(rows)
	if len(rows) != 2 || !got[c.owner] || !got[c.admin] {
		t.Fatalf("report notices = %+v, want owner + admin only", rows)
	}
	for _, r := range rows {
		if r.ActorID != uuid.Nil || r.Snippet == "" {
			t.Fatalf("report notice must hide reporter and snapshot title: %+v", r)
		}
	}
}

// 阈值 3，10 人并发举报：只有一条举报标记为已通知；dismiss 后新一轮再达到阈值，再通知一次。
func TestPostReport_ConcurrentReportsNotifyOncePerRound(t *testing.T) {
	t.Parallel()
	env := setupReport(t, reportCfg(3, 0, 20))
	c := newReviewCircle(t, env.db)
	postID, _ := insertPost(t, env.db, c, postdomain.PostStatusPublished)

	const n = 10
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := report(t, env.svc, sharedomain.NewID(), postID); err != nil {
				t.Errorf("report: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := countReports(t, env.db, postID); got != (reportCounts{Total: n, Pending: n, Notified: 1}) {
		t.Fatalf("after concurrent reports = %+v, want exactly one notified", got)
	}

	res, err := env.svc.ResolveReport(t.Context(), c.admin, postapp.ResolveReportInput{
		CircleID: c.id, PostID: postID, Action: postapp.ReportDismiss,
	})
	if err != nil || res.ClosedReports != n || res.Status != postdomain.PostStatusPublished {
		t.Fatalf("dismiss = %+v, %v", res, err)
	}
	for i := range 3 {
		if err := report(t, env.svc, sharedomain.NewID(), postID); err != nil {
			t.Fatalf("round 2 report %d: %v", i, err)
		}
		want := reportCounts{Total: n + i + 1, Pending: i + 1, Notified: 1}
		if i == 2 {
			want.Notified = 2
		}
		if got := countReports(t, env.db, postID); got != want {
			t.Fatalf("round 2 after %d reports = %+v, want %+v", i+1, got, want)
		}
	}
}

// 24 小时上限：第 3 次举报（不同帖子）429，其他用户不受影响。
func TestPostReport_DailyLimit(t *testing.T) {
	t.Parallel()
	env := setupReport(t, reportCfg(1, 0, 2))
	c := newReviewCircle(t, env.db)
	reporter := sharedomain.NewID()
	var last uuid.UUID
	for i := range 3 {
		postID, _ := insertPost(t, env.db, c, postdomain.PostStatusPublished)
		last = postID
		err := report(t, env.svc, reporter, postID)
		if i < 2 && err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
		if i == 2 && !postapp.IsReportLimitExceededErr(err) {
			t.Fatalf("third report err = %v, want limit exceeded", err)
		}
	}
	if err := report(t, env.svc, sharedomain.NewID(), last); err != nil {
		t.Fatalf("other reporter: %v", err)
	}
}

// 圈主与管理员并发 dismiss / remove：只有一方生效，其余 404；审计行一条，举报全部关闭，状态与胜出动作一致。
func TestPostReport_ConcurrentResolveOnlyOneWins(t *testing.T) {
	t.Parallel()
	env := setupReport(t, activeModeration(true))
	c := newReviewCircle(t, env.db)
	postID, _ := insertPost(t, env.db, c, postdomain.PostStatusPublished)
	for range 2 {
		if err := report(t, env.svc, sharedomain.NewID(), postID); err != nil {
			t.Fatalf("report: %v", err)
		}
	}

	const n = 10
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		winners  []postapp.ReportAction
		notFound int
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			op, action := c.owner, postapp.ReportDismiss
			if i%2 == 1 {
				op, action = c.admin, postapp.ReportRemove
			}
			_, err := env.svc.ResolveReport(t.Context(), op, postapp.ResolveReportInput{CircleID: c.id, PostID: postID, Action: action})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners = append(winners, action)
			case postapp.IsReportedPostNotFoundErr(err):
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
	if got := countReports(t, env.db, postID); got.Pending != 0 || got.Total != 2 {
		t.Fatalf("reports = %+v, want all closed", got)
	}
	want, reportStatus := int16(postdomain.PostStatusPublished), postdomain.PostReportDismissed
	if winners[0] == postapp.ReportRemove {
		want, reportStatus = postdomain.PostStatusRejected, postdomain.PostReportRemoved
	}
	if got := postStatus(t, env.db, postID); got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var closed int
	if err := env.db.Raw(`SELECT count(*) FROM domains.post_report WHERE post_id = ? AND status = ? AND handler_id IS NOT NULL
		AND handle_time IS NOT NULL`, postID, reportStatus).Scan(&closed).Error; err != nil || closed != 2 {
		t.Fatalf("closed reports with handler = %d, %v; want 2", closed, err)
	}
}

// remove：帖子下架为 3、审计 report_remove、作者收到下架通知（不暴露处理人）；之后不能再举报。
func TestPostReport_RemoveNotifiesAuthor(t *testing.T) {
	t.Parallel()
	env := setupReport(t, activeModeration(true))
	c := newReviewCircle(t, env.db)
	other := newReviewCircle(t, env.db)
	postID, authorID := insertPost(t, env.db, c, postdomain.PostStatusPublished)
	if err := report(t, env.svc, c.member, postID); err != nil {
		t.Fatalf("report: %v", err)
	}

	// 他圈圈主拿自己的圈子处理：403（不是该圈管理员）/ 404（帖子不在该圈）
	if _, err := env.svc.ResolveReport(t.Context(), other.owner, postapp.ResolveReportInput{
		CircleID: c.id, PostID: postID, Action: postapp.ReportRemove,
	}); !postapp.IsNotCircleModeratorErr(err) {
		t.Fatalf("other circle owner err = %v", err)
	}
	if _, err := env.svc.ResolveReport(t.Context(), other.owner, postapp.ResolveReportInput{
		CircleID: other.id, PostID: postID, Action: postapp.ReportRemove,
	}); !postapp.IsReportedPostNotFoundErr(err) {
		t.Fatalf("cross-circle err = %v", err)
	}

	res, err := env.svc.ResolveReport(t.Context(), c.owner, postapp.ResolveReportInput{
		CircleID: c.id, PostID: postID, Action: postapp.ReportRemove, Reason: "广告",
	})
	if err != nil || res.Status != postdomain.PostStatusRejected || res.ClosedReports != 1 {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusRejected {
		t.Fatalf("status = %d, want rejected", got)
	}
	var decision string
	if err := env.db.Raw(`SELECT decision FROM domains.post_moderation_log WHERE post_id = ? AND source = ? AND operator_id = ?`,
		postID, postdomain.ModerationSourceManual, c.owner).Scan(&decision).Error; err != nil || decision != string(postdomain.ModerationReportRemove) {
		t.Fatalf("manual log decision = %q, %v", decision, err)
	}
	if err := report(t, env.svc, c.admin, postID); !postapp.IsReportPostNotFoundErr(err) {
		t.Fatalf("report removed post err = %v", err)
	}

	waitReviewNotice(t, env.db, authorID, postID, c.owner, noticedomain.NoticeTypePostRemoved)
}

// 自动屏蔽（阈值 2）：第 2 条举报把帖子转为屏蔽（4），不进 #118 待审队列，在被举报列表里；再举报 404；dismiss 恢复为已发布。
func TestPostReport_AutoHideAndDismissRestores(t *testing.T) {
	t.Parallel()
	env := setupReport(t, reportCfg(1, 2, 20))
	c := newReviewCircle(t, env.db)
	postID, _ := insertPost(t, env.db, c, postdomain.PostStatusPublished)

	if err := report(t, env.svc, sharedomain.NewID(), postID); err != nil {
		t.Fatalf("report 1: %v", err)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusPublished {
		t.Fatalf("status after 1 report = %d, want published", got)
	}
	if err := report(t, env.svc, sharedomain.NewID(), postID); err != nil {
		t.Fatalf("report 2: %v", err)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusBlocked {
		t.Fatalf("status after 2 reports = %d, want blocked", got)
	}
	if err := report(t, env.svc, sharedomain.NewID(), postID); !postapp.IsReportPostNotFoundErr(err) {
		t.Fatalf("report hidden post err = %v", err)
	}

	queue, err := env.svc.ListReviewQueue(t.Context(), c.owner, c.id, 20, "")
	if err != nil || len(queue.Posts) != 0 {
		t.Fatalf("#118 queue = %+v, %v; hidden post must not be there", queue, err)
	}
	list, err := env.svc.ListReportedPosts(t.Context(), c.admin, c.id, 20, "")
	if err != nil || len(list.Posts) != 1 {
		t.Fatalf("reported list = %+v, %v", list, err)
	}
	if p := list.Posts[0]; p.ID != postID || p.Status != postdomain.PostStatusBlocked || p.ReportCount != 2 ||
		len(p.Reports) != 2 || p.Reports[0].Reason != "站外引流" {
		t.Fatalf("reported item = %+v", p)
	}
	if _, err := env.svc.ListReportedPosts(t.Context(), c.member, c.id, 20, ""); !postapp.IsNotCircleModeratorErr(err) {
		t.Fatalf("member list err = %v", err)
	}

	res, err := env.svc.ResolveReport(t.Context(), c.owner, postapp.ResolveReportInput{
		CircleID: c.id, PostID: postID, Action: postapp.ReportDismiss,
	})
	if err != nil || res.Status != postdomain.PostStatusPublished || res.ClosedReports != 2 {
		t.Fatalf("dismiss = %+v, %v", res, err)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusPublished {
		t.Fatalf("status after dismiss = %d, want published", got)
	}
	if list, err := env.svc.ListReportedPosts(t.Context(), c.admin, c.id, 20, ""); err != nil || len(list.Posts) != 0 {
		t.Fatalf("reported list after dismiss = %+v, %v", list, err)
	}
}

// 被举报列表翻页：首次举报时间倒序，游标稳定。
func TestPostReport_ListPaginates(t *testing.T) {
	t.Parallel()
	env := setupReport(t, activeModeration(true))
	c := newReviewCircle(t, env.db)
	var ids []uuid.UUID
	for range 3 {
		postID, _ := insertPost(t, env.db, c, postdomain.PostStatusPublished)
		if err := report(t, env.svc, sharedomain.NewID(), postID); err != nil {
			t.Fatalf("report: %v", err)
		}
		ids = append(ids, postID)
	}
	page1, err := env.svc.ListReportedPosts(t.Context(), c.owner, c.id, 2, "")
	if err != nil || len(page1.Posts) != 2 || page1.Cursor == "" || page1.Posts[0].ID != ids[2] || page1.Posts[1].ID != ids[1] {
		t.Fatalf("page1 = %+v, %v", page1, err)
	}
	page2, err := env.svc.ListReportedPosts(t.Context(), c.owner, c.id, 2, page1.Cursor)
	if err != nil || len(page2.Posts) != 1 || page2.Cursor != "" || page2.Posts[0].ID != ids[0] {
		t.Fatalf("page2 = %+v, %v", page2, err)
	}
}

// AI 待审（self_harm）+ review_action=publish：帖子发布（副作用一次），审计 review_publish，圈主 / 管理员收到疑似自伤通知（actor 为作者）。
func TestModeration_SelfHarmPublishesAndNotifiesModerators(t *testing.T) {
	t.Parallel()
	env := setupReport(t, activeModeration(true))
	c := newReviewCircle(t, env.db)
	postID, authorID := insertPost(t, env.db, c, postdomain.PostStatusReviewing)
	var title string
	if err := env.db.Raw("SELECT title FROM domains.post WHERE id = ?", postID).Scan(&title).Error; err != nil {
		t.Fatalf("query title: %v", err)
	}
	env.mod.set(title, assessment(postdomain.HazardSelfHarm, 0.93, 2.2, 0.9), nil)

	if err := env.svc.ModeratePost(t.Context(), postID); err != nil {
		t.Fatalf("ModeratePost: %v", err)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusPublished {
		t.Fatalf("status = %d, want published", got)
	}
	rows := moderationLogs(t, env.db, postID)
	if len(rows) != 1 || rows[0].Decision != string(postdomain.ModerationReviewPublish) {
		t.Fatalf("logs = %+v, want one review_publish", rows)
	}
	if count, _ := env.fx.get(c.id, postID); count != 1 {
		t.Fatalf("post count incr = %d, want 1", count)
	}

	notices := waitNotices(t, env.db, postID, noticedomain.NoticeTypePostSelfHarm, 2)
	got := recipients(notices)
	if len(notices) != 2 || !got[c.owner] || !got[c.admin] {
		t.Fatalf("self-harm notices = %+v, want owner + admin only", notices)
	}
	for _, n := range notices {
		if n.ActorID != authorID || n.Snippet != title {
			t.Fatalf("self-harm notice = %+v", n)
		}
	}
}
