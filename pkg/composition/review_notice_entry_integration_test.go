//go:build integration

// review_notice_entry_integration_test.go 审核类通知跳转圈子管理（#156）端到端集成测试（make test-integration）：
// 真实 PG + Redpanda 通知 consumer + notice 列表（post Facade 桥接回填 circle_id）。
//
// 覆盖：GET /circle/moderation/summary 两个计数与待审 / 被举报列表条数一致（已删除 / 已下架 / 他圈不计）；
// 同一帖子两轮举报后，第二轮的举报通知换新 id 排在接收人通知列表第一条，且带 circle_id。
package composition

import (
	"testing"
	"time"

	noticeapp "interestBar/pkg/domains/notice/application"
	noticedomain "interestBar/pkg/domains/notice/domain"
	noticeinfra "interestBar/pkg/domains/notice/infrastructure"
	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// 待审：2 篇审核中计入，已删除的审核中帖子不计；被举报：已发布 + 已屏蔽计入，已删除 / 已下架（举报已关闭）/ 他圈不计。
func TestModerationSummary_MatchesLists(t *testing.T) {
	t.Parallel()
	env := setupReport(t, reportCfg(1, 0, 20))
	c := newReviewCircle(t, env.db)
	other := newReviewCircle(t, env.db)

	insertPost(t, env.db, c, postdomain.PostStatusReviewing)
	insertPost(t, env.db, c, postdomain.PostStatusReviewing)
	goneReviewing, _ := insertPost(t, env.db, c, postdomain.PostStatusReviewing)
	insertPost(t, env.db, other, postdomain.PostStatusReviewing)

	reported := func(circle reviewCircle) uuid.UUID {
		id, _ := insertPost(t, env.db, circle, postdomain.PostStatusPublished)
		if err := report(t, env.svc, sharedomain.NewID(), id); err != nil {
			t.Fatalf("report: %v", err)
		}
		return id
	}
	reported(c)            // 已发布，计入
	blocked := reported(c) // 已屏蔽，计入
	deleted := reported(c) // 删帖，不计
	removed := reported(c) // 下架（举报已关闭），不计
	reported(other)        // 他圈，不计
	for id, sql := range map[uuid.UUID]string{
		goneReviewing: `UPDATE domains.post SET deleted = 1 WHERE id = ?`,
		blocked:       `UPDATE domains.post SET status = 4 WHERE id = ?`,
		deleted:       `UPDATE domains.post SET deleted = 1 WHERE id = ?`,
	} {
		if err := env.db.Exec(sql, id).Error; err != nil {
			t.Fatalf("update post: %v", err)
		}
	}
	if _, err := env.svc.ResolveReport(t.Context(), c.owner, postapp.ResolveReportInput{
		CircleID: c.id, PostID: removed, Action: postapp.ReportRemove,
	}); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got, err := env.svc.GetModerationSummary(t.Context(), c.admin, c.id)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	queue, err := env.svc.ListReviewQueue(t.Context(), c.admin, c.id, 100, "")
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	list, err := env.svc.ListReportedPosts(t.Context(), c.admin, c.id, 100, "")
	if err != nil {
		t.Fatalf("reported list: %v", err)
	}
	if got.ReviewPending != 2 || got.ReportPending != 2 ||
		got.ReviewPending != int64(len(queue.Posts)) || got.ReportPending != int64(len(list.Posts)) {
		t.Fatalf("summary = %+v, queue = %d, reported = %d; want 2 / 2 matching lists", got, len(queue.Posts), len(list.Posts))
	}

	for name, op := range map[string]uuid.UUID{"member": c.member, "muted admin": c.mutedA, "other owner": other.owner} {
		if _, err := env.svc.GetModerationSummary(t.Context(), op, c.id); !postapp.IsNotCircleModeratorErr(err) {
			t.Fatalf("%s summary err = %v, want forbidden", name, err)
		}
	}
}

// 帖子 A 第一轮举报 → 帖子 B 举报（B 的通知更新）→ A 复核保留 → A 第二轮举报：
// 圈主列表第一条是 A 的通知（新 id、未读、仍只有一行），两条都带 circle_id，举报人不暴露。
func TestReviewNotice_SecondRoundOnTop(t *testing.T) {
	t.Parallel()
	env := setupReport(t, reportCfg(1, 0, 20))
	c := newReviewCircle(t, env.db)
	postA, _ := insertPost(t, env.db, c, postdomain.PostStatusPublished)
	postB, _ := insertPost(t, env.db, c, postdomain.PostStatusPublished)

	noticeSvc := noticeapp.NewNoticeService(noticeinfra.NewNotificationRepository(env.db), noticeinfra.NewNoticeUnreadCache())
	noticeSvc.SetPostFacade(&noticePostFacade{delegate: env.svc})
	ownerList := func() []noticeapp.NoticeItem {
		t.Helper()
		res, err := noticeSvc.ListNotifications(t.Context(), c.owner, []int16{noticedomain.NoticeTypePostReported}, 20, "")
		if err != nil {
			t.Fatalf("list notices: %v", err)
		}
		return res.Notices
	}

	if err := report(t, env.svc, sharedomain.NewID(), postA); err != nil {
		t.Fatalf("round 1 report A: %v", err)
	}
	waitNotices(t, env.db, postA, noticedomain.NoticeTypePostReported, 2)
	if err := report(t, env.svc, sharedomain.NewID(), postB); err != nil {
		t.Fatalf("report B: %v", err)
	}
	waitNotices(t, env.db, postB, noticedomain.NoticeTypePostReported, 2)

	round1 := ownerList()
	if len(round1) != 2 || round1[0].PostID != postB.String() || round1[1].PostID != postA.String() {
		t.Fatalf("round 1 list = %+v, want [B, A]", round1)
	}
	oldA := round1[1].ID
	if err := noticeSvc.MarkAllRead(t.Context(), c.owner); err != nil {
		t.Fatalf("mark all read: %v", err)
	}

	if _, err := env.svc.ResolveReport(t.Context(), c.admin, postapp.ResolveReportInput{
		CircleID: c.id, PostID: postA, Action: postapp.ReportDismiss,
	}); err != nil {
		t.Fatalf("dismiss A: %v", err)
	}
	if err := report(t, env.svc, sharedomain.NewID(), postA); err != nil {
		t.Fatalf("round 2 report A: %v", err)
	}

	var round2 []noticeapp.NoticeItem
	testkit.Eventually(t, reviewNoticeWait, 200*time.Millisecond, "round 2 notice on top", func() bool {
		round2 = ownerList()
		return len(round2) > 0 && round2[0].PostID == postA.String()
	})
	if len(round2) != 2 || round2[1].PostID != postB.String() {
		t.Fatalf("round 2 list = %+v, want [A, B] without duplicate rows", round2)
	}
	top := round2[0]
	if top.ID == oldA || top.IsRead || top.Actor != nil {
		t.Fatalf("round 2 top = %+v, want new id (old %s), unread, hidden actor", top, oldA)
	}
	for _, n := range round2 {
		if n.CircleID != c.id.String() {
			t.Fatalf("notice %+v circle_id = %q, want %s", n, n.CircleID, c.id)
		}
	}
}
