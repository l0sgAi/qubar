// moderation_report_test.go 先发后审与举报复核（#153）应用层单元测试。
//
// 覆盖：review_action=publish / hold 下 AI 待审结果的去向、审核失败不受 review_action 影响、self_harm 通知圈管；
// 举报的参数 / 帖子状态 / 自己的帖子 / 重复 / 24 小时上限校验与阈值通知；复核鉴权、动作、审计行与作者通知；被举报列表组装。
// 真实 PG 上的唯一约束、通知去重、并发复核与自动屏蔽见 composition 包的集成测试。
package application

import (
	"context"
	"testing"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

type moderatorNotice struct {
	kind        domain.ModeratorNoticeKind
	actor, post uuid.UUID
}

func (p *fakeModerationPublisher) PublishModeratorNotice(_ context.Context, kind domain.ModeratorNoticeKind, actorID, postID uuid.UUID) error {
	p.moderatorNotices = append(p.moderatorNotices, moderatorNotice{kind: kind, actor: actorID, post: postID})
	return nil
}

func (p *fakeModerationPublisher) PublishPostRemovedNotice(_ context.Context, operatorID, postID uuid.UUID) error {
	p.removedNotices = append(p.removedNotices, reviewNotice{operator: operatorID, post: postID})
	return nil
}

// fakeReportRepo 内存举报仓储：按真实语义模拟唯一约束、待处理计数与「每轮只通知一次」。
type fakeReportRepo struct {
	reports    []*domain.PostReport
	thresholds []domain.ReportThresholds
	reported   []domain.ReportedPost
	resolved   []resolveCall
}

type resolveCall struct {
	post, circle uuid.UUID
	remove       bool
	log          *domain.PostModerationLog
}

func (r *fakeReportRepo) Create(_ context.Context, rp *domain.PostReport, t domain.ReportThresholds) (domain.ReportOutcome, error) {
	r.thresholds = append(r.thresholds, t)
	notified := false
	pending := 1
	for _, x := range r.reports {
		if x.PostID != rp.PostID {
			continue
		}
		if x.ReporterID == rp.ReporterID {
			return domain.ReportOutcome{}, domain.ErrPostReportDuplicate
		}
		if x.Status == domain.PostReportPending {
			pending++
			notified = notified || x.Notified == 1
		}
	}
	rp.ID, rp.CreateTime = uuid.New(), time.Now()
	out := domain.ReportOutcome{Pending: pending}
	if !notified && pending >= t.Notify {
		rp.Notified, out.Notify = 1, true
	}
	out.Hidden = t.Hide > 0 && pending >= t.Hide
	r.reports = append(r.reports, rp)
	return out, nil
}

func (r *fakeReportRepo) CountByReporterSince(_ context.Context, reporterID uuid.UUID, since time.Time) (int64, error) {
	var n int64
	for _, x := range r.reports {
		if x.ReporterID == reporterID && !x.CreateTime.Before(since) {
			n++
		}
	}
	return n, nil
}

func (r *fakeReportRepo) ListReportedByCircle(_ context.Context, circleID uuid.UUID, after *domain.PostKeyset, limit int) ([]domain.ReportedPost, error) {
	var out []domain.ReportedPost
	for _, x := range r.reported {
		if x.CircleID != circleID {
			continue
		}
		if after != nil && !reportedBefore(x, after) {
			continue
		}
		out = append(out, x)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// reportedBefore x 是否排在翻页位置 after 之后（首次举报时间, id 倒序）。
func reportedBefore(x domain.ReportedPost, after *domain.PostKeyset) bool {
	if x.FirstReportTime.Equal(after.CreateTime) {
		return x.ID.String() < after.ID.String()
	}
	return x.FirstReportTime.Before(after.CreateTime)
}

func (r *fakeReportRepo) ListPendingByPostIDs(_ context.Context, ids []uuid.UUID, perPost int) (map[uuid.UUID][]domain.PostReport, error) {
	out := make(map[uuid.UUID][]domain.PostReport)
	for _, id := range ids {
		for i := len(r.reports) - 1; i >= 0; i-- {
			if x := r.reports[i]; x.PostID == id && x.Status == domain.PostReportPending && len(out[id]) < perPost {
				out[id] = append(out[id], *x)
			}
		}
	}
	return out, nil
}

func (r *fakeReportRepo) Resolve(_ context.Context, postID, circleID uuid.UUID, remove bool, log *domain.PostModerationLog) (int, bool, error) {
	closed := 0
	for _, x := range r.reports {
		if x.PostID == postID && x.CircleID == circleID && x.Status == domain.PostReportPending {
			x.Status = domain.PostReportDismissed
			if remove {
				x.Status = domain.PostReportRemoved
			}
			closed++
		}
	}
	if closed == 0 {
		return 0, false, nil
	}
	r.resolved = append(r.resolved, resolveCall{post: postID, circle: circleID, remove: remove, log: log})
	return closed, true, nil
}

// newReportFixture 审核开启（帖子先进审核中再由 AI 放行），注入举报仓储与带角色的成员表；返回一篇已发布帖子。
func newReportFixture(t *testing.T, cfg conf.Moderation) (*moderationFixture, *fakeReportRepo, uuid.UUID) {
	t.Helper()
	f := newModerationFixture(cfg)
	f.svc.SetMemberChecker(reviewMembers())
	reports := &fakeReportRepo{}
	f.svc.SetReportRepository(reports)
	id := f.createPost(t)
	f.repo.created[0].Status = domain.PostStatusPublished
	f.repo.created[0].CreateTime = time.Now()
	return f, reports, id
}

func riskAssessment(h domain.Hazard, p, severity float64) *domain.Assessment {
	risks := make(map[domain.Hazard]float64, len(domain.Hazards))
	for _, x := range domain.Hazards {
		risks[x] = 0.01
	}
	risks[h] = p
	return &domain.Assessment{Risks: risks, Severity: severity, SeverityConfidence: 0.9, Model: "jev-test"}
}

// ===== review_action =====

// 默认 publish：AI 待审（生产首条实测 harassment 0.79 / severity 1.08）直接发布，副作用一次，日志 review_publish。
func TestModeratePost_ReviewActionPublish(t *testing.T) {
	f := newModerationFixture(moderationCfg(true, "key", true))
	id := f.createPost(t)
	f.mod.a = riskAssessment(domain.HazardHarassment, 0.79, 1.08)

	if err := f.svc.ModeratePost(context.Background(), id); err != nil {
		t.Fatalf("ModeratePost: %v", err)
	}
	if got := f.repo.created[0].Status; got != domain.PostStatusPublished {
		t.Fatalf("status = %d, want published", got)
	}
	f.assertPublishedOnce(t, id)
	if len(f.log.rows) != 1 || f.log.rows[0].Decision != domain.ModerationReviewPublish || f.log.rows[0].Model != "jev-test" {
		t.Fatalf("want one review_publish log with scores, got %+v", f.log.rows)
	}
	if len(f.pub.moderatorNotices) != 0 {
		t.Fatalf("no self_harm hit, want no moderator notice: %+v", f.pub.moderatorNotices)
	}

	// 重复投递：帖子已发布，不再调审核器、不重复副作用
	if err := f.svc.ModeratePost(context.Background(), id); err != nil || f.mod.calls != 1 {
		t.Fatalf("duplicate: err=%v calls=%d", err, f.mod.calls)
	}
	f.assertPublishedOnce(t, id)
}

// AI 放行的帖子日志仍是 publish，与「待审后自动发布」区分开。
func TestModeratePost_PlainPublishKeepsDecision(t *testing.T) {
	f := newModerationFixture(moderationCfg(true, "key", true))
	id := f.createPost(t)
	f.mod.a = riskAssessment(domain.HazardSpamAd, 0.05, 0.1)

	if err := f.svc.ModeratePost(context.Background(), id); err != nil {
		t.Fatalf("ModeratePost: %v", err)
	}
	if len(f.log.rows) != 1 || f.log.rows[0].Decision != domain.ModerationPublish {
		t.Fatalf("want publish log, got %+v", f.log.rows)
	}
}

// hold：与 #118 行为一致，留在审核中、无副作用。
func TestModeratePost_ReviewActionHold(t *testing.T) {
	cfg := moderationCfg(true, "key", true)
	cfg.ReviewAction = conf.ModerationReviewActionHold
	f := newModerationFixture(cfg)
	id := f.createPost(t)
	f.mod.a = riskAssessment(domain.HazardSelfHarm, 0.9, 2.5)

	if err := f.svc.ModeratePost(context.Background(), id); err != nil {
		t.Fatalf("ModeratePost: %v", err)
	}
	if got := f.repo.created[0].Status; got != domain.PostStatusReviewing {
		t.Fatalf("status = %d, want reviewing", got)
	}
	f.assertNoSideEffects(t)
	if len(f.log.rows) != 1 || f.log.rows[0].Decision != domain.ModerationReview {
		t.Fatalf("want one review log, got %+v", f.log.rows)
	}
	if len(f.pub.moderatorNotices) != 0 {
		t.Fatalf("hold must not send self-harm notice: %+v", f.pub.moderatorNotices)
	}
}

// 驳回规则不受 review_action 影响。
func TestModeratePost_RejectUnchanged(t *testing.T) {
	f := newModerationFixture(moderationCfg(true, "key", true))
	id := f.createPost(t)
	f.mod.a = riskAssessment(domain.HazardIllegal, 0.92, 2.2)

	if err := f.svc.ModeratePost(context.Background(), id); err != nil {
		t.Fatalf("ModeratePost: %v", err)
	}
	if got := f.repo.created[0].Status; got != domain.PostStatusRejected || f.log.rows[0].Decision != domain.ModerationReject {
		t.Fatalf("status=%d decision=%s, want rejected", got, f.log.rows[0].Decision)
	}
	f.assertNoSideEffects(t)
}

// self_harm >= 转人工阈值：照常发布，同时通知圈管（actor 为作者）。
func TestModeratePost_SelfHarmNotifiesModerators(t *testing.T) {
	f := newModerationFixture(moderationCfg(true, "key", true))
	id := f.createPost(t)
	f.mod.a = riskAssessment(domain.HazardSelfHarm, 0.9, 2.5)

	if err := f.svc.ModeratePost(context.Background(), id); err != nil {
		t.Fatalf("ModeratePost: %v", err)
	}
	f.assertPublishedOnce(t, id)
	n := f.pub.moderatorNotices
	if len(n) != 1 || n[0].kind != domain.ModeratorNoticeSelfHarm || n[0].actor != mentionActor || n[0].post != id {
		t.Fatalf("moderator notices = %+v", n)
	}
}

// 审核失败：fail_open=true 放行（decision=fail_open）；fail_open=false 的转人工不受 review_action=publish 影响。
func TestModeratePost_UnavailableIgnoresReviewAction(t *testing.T) {
	for _, failOpen := range []bool{true, false} {
		f := newModerationFixture(moderationCfg(true, "key", failOpen))
		id := f.createPost(t)
		f.mod.err = domain.ErrModerationUnavailable

		if err := f.svc.ModeratePost(context.Background(), id); err != nil {
			t.Fatalf("fail_open=%v: %v", failOpen, err)
		}
		wantStatus, wantDecision := int16(domain.PostStatusReviewing), domain.ModerationReview
		if failOpen {
			wantStatus, wantDecision = domain.PostStatusPublished, domain.ModerationFailOpen
		}
		if got := f.repo.created[0].Status; got != wantStatus {
			t.Fatalf("fail_open=%v: status = %d, want %d", failOpen, got, wantStatus)
		}
		if len(f.log.rows) != 1 || f.log.rows[0].Decision != wantDecision || f.log.rows[0].ErrorMsg == nil {
			t.Fatalf("fail_open=%v: logs = %+v", failOpen, f.log.rows)
		}
		if failOpen {
			f.assertPublishedOnce(t, id)
		} else {
			f.assertNoSideEffects(t)
		}
	}
}

// ===== 举报 =====

func TestReportPost_Validation(t *testing.T) {
	f, reports, id := newReportFixture(t, moderationCfg(true, "key", true))
	ctx := context.Background()

	for _, rt := range []int16{0, 9, -1} {
		if err := f.svc.ReportPost(ctx, reviewMember, ReportPostInput{PostID: id, ReasonType: rt}); !IsInvalidReportReasonErr(err) {
			t.Fatalf("reason_type %d err = %v", rt, err)
		}
	}
	if err := f.svc.ReportPost(ctx, reviewMember, ReportPostInput{PostID: uuid.New(), ReasonType: 1}); !IsReportPostNotFoundErr(err) {
		t.Fatalf("missing post err = %v", err)
	}
	if err := f.svc.ReportPost(ctx, mentionActor, ReportPostInput{PostID: id, ReasonType: 1}); !IsCannotReportOwnPostErr(err) {
		t.Fatalf("own post err = %v", err)
	}
	for _, st := range []int16{domain.PostStatusReviewing, domain.PostStatusRejected, domain.PostStatusBlocked} {
		f.repo.created[0].Status = st
		if err := f.svc.ReportPost(ctx, reviewMember, ReportPostInput{PostID: id, ReasonType: 1}); !IsReportPostNotFoundErr(err) {
			t.Fatalf("status %d err = %v", st, err)
		}
	}
	if len(reports.reports) != 0 || len(f.pub.moderatorNotices) != 0 {
		t.Fatalf("rejected reports must not be written: %+v", reports.reports)
	}
}

// 默认阈值 1：第一条举报通知圈管（actor 为空、不暴露举报人），本轮后续举报不再通知；处理后新一轮重新通知。
func TestReportPost_NotifyOncePerRound(t *testing.T) {
	f, reports, id := newReportFixture(t, moderationCfg(false, "", true)) // 举报与审核开关无关
	ctx := context.Background()

	if err := f.svc.ReportPost(ctx, reviewMember, ReportPostInput{PostID: id, ReasonType: domain.ReportReasonSpamAd, Reason: "  加微信  "}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if err := f.svc.ReportPost(ctx, reviewStrang, ReportPostInput{PostID: id, ReasonType: domain.ReportReasonOther}); err != nil {
		t.Fatalf("second report: %v", err)
	}
	n := f.pub.moderatorNotices
	if len(n) != 1 || n[0].kind != domain.ModeratorNoticeReported || n[0].actor != uuid.Nil || n[0].post != id {
		t.Fatalf("moderator notices = %+v, want exactly one report notice", n)
	}
	r := reports.reports[0]
	if r.CircleID != postCircleID || r.ReporterID != reviewMember || r.Reason != "加微信" || r.ReasonType != domain.ReportReasonSpamAd {
		t.Fatalf("report row = %+v", r)
	}
	if th := reports.thresholds[0]; th.Notify != 1 || th.Hide != 0 {
		t.Fatalf("default thresholds = %+v, want notify 1 / hide off", th)
	}

	if _, err := f.svc.ResolveReport(ctx, reviewAdmin, ResolveReportInput{CircleID: postCircleID, PostID: id, Action: ReportDismiss}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if err := f.svc.ReportPost(ctx, reviewOwner, ReportPostInput{PostID: id, ReasonType: domain.ReportReasonSexual}); err != nil {
		t.Fatalf("report after dismiss: %v", err)
	}
	if len(f.pub.moderatorNotices) != 2 {
		t.Fatalf("new round must notify again: %+v", f.pub.moderatorNotices)
	}
}

func TestReportPost_DuplicateAndDailyLimit(t *testing.T) {
	cfg := moderationCfg(true, "key", true)
	cfg.ReportDailyLimit = 2
	cfg.ReportNotifyThreshold, cfg.ReportHideThreshold = 3, 5
	f, reports, id := newReportFixture(t, cfg)
	ctx := context.Background()

	if err := f.svc.ReportPost(ctx, reviewMember, ReportPostInput{PostID: id, ReasonType: 3}); err != nil {
		t.Fatalf("report: %v", err)
	}
	if err := f.svc.ReportPost(ctx, reviewMember, ReportPostInput{PostID: id, ReasonType: 4}); !IsAlreadyReportedErr(err) {
		t.Fatalf("duplicate err = %v", err)
	}
	if th := reports.thresholds[0]; th.Notify != 3 || th.Hide != 5 {
		t.Fatalf("thresholds = %+v, want configured 3 / 5", th)
	}
	if len(f.pub.moderatorNotices) != 0 {
		t.Fatalf("below threshold must not notify: %+v", f.pub.moderatorNotices)
	}

	// 24 小时外的举报不计入上限；窗口内已有 2 条（含上面那条）→ 429
	reports.reports = append(reports.reports,
		&domain.PostReport{PostID: uuid.New(), ReporterID: reviewMember, CreateTime: time.Now().Add(-time.Hour)},
		&domain.PostReport{PostID: uuid.New(), ReporterID: reviewMember, CreateTime: time.Now().Add(-25 * time.Hour)},
	)
	other := &domain.Post{ID: uuid.New(), CircleID: postCircleID, UserID: reviewOwner, Status: domain.PostStatusPublished}
	f.repo.created = append(f.repo.created, other)
	if err := f.svc.ReportPost(ctx, reviewMember, ReportPostInput{PostID: other.ID, ReasonType: 1}); !IsReportLimitExceededErr(err) {
		t.Fatalf("limit err = %v", err)
	}
	if err := f.svc.ReportPost(ctx, reviewStrang, ReportPostInput{PostID: other.ID, ReasonType: 1}); err != nil {
		t.Fatalf("other user is not limited: %v", err)
	}
}

// ===== 复核 =====

func TestResolveReport_RemoveAndDismiss(t *testing.T) {
	f, reports, id := newReportFixture(t, moderationCfg(true, "key", true))
	ctx := context.Background()
	if err := f.svc.ReportPost(ctx, reviewMember, ReportPostInput{PostID: id, ReasonType: 1}); err != nil {
		t.Fatalf("report: %v", err)
	}

	res, err := f.svc.ResolveReport(ctx, reviewAdmin, ResolveReportInput{
		CircleID: postCircleID, PostID: id, Action: ReportRemove, Reason: " 广告 ",
	})
	if err != nil || res.Status != domain.PostStatusRejected || res.ClosedReports != 1 {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	c := reports.resolved[0]
	if !c.remove || c.circle != postCircleID || c.log.Decision != domain.ModerationReportRemove ||
		c.log.Source != domain.ModerationSourceManual || c.log.OperatorID == nil || *c.log.OperatorID != reviewAdmin ||
		c.log.Reason == nil || *c.log.Reason != "广告" {
		t.Fatalf("resolve call = %+v / log %+v", c, c.log)
	}
	if n := f.pub.removedNotices; len(n) != 1 || n[0].operator != reviewAdmin || n[0].post != id {
		t.Fatalf("removed notices = %+v", n)
	}

	// 已处理：无待处理举报 → 404，不再通知
	if _, err := f.svc.ResolveReport(ctx, reviewOwner, ResolveReportInput{CircleID: postCircleID, PostID: id, Action: ReportDismiss}); !IsReportedPostNotFoundErr(err) {
		t.Fatalf("second resolve err = %v", err)
	}

	// dismiss：保留帖子，不通知作者
	if err := f.svc.ReportPost(ctx, reviewStrang, ReportPostInput{PostID: id, ReasonType: 2}); err != nil {
		t.Fatalf("report again: %v", err)
	}
	res, err = f.svc.ResolveReport(ctx, reviewOwner, ResolveReportInput{CircleID: postCircleID, PostID: id, Action: ReportDismiss})
	if err != nil || res.Status != domain.PostStatusPublished || reports.resolved[1].log.Decision != domain.ModerationReportDismiss ||
		reports.resolved[1].log.Reason != nil {
		t.Fatalf("dismiss = %+v, %v", res, err)
	}
	if len(f.pub.removedNotices) != 1 {
		t.Fatalf("dismiss must not notify the author: %+v", f.pub.removedNotices)
	}
}

func TestResolveReport_ForbiddenAndBadInput(t *testing.T) {
	f, reports, id := newReportFixture(t, moderationCfg(true, "key", true))
	ctx := context.Background()
	if err := f.svc.ReportPost(ctx, reviewStrang, ReportPostInput{PostID: id, ReasonType: 1}); err != nil {
		t.Fatalf("report: %v", err)
	}
	for name, op := range map[string]uuid.UUID{"member": reviewMember, "muted admin": reviewMuted, "stranger": reviewStrang, "author": mentionActor} {
		if _, err := f.svc.ResolveReport(ctx, op, ResolveReportInput{CircleID: postCircleID, PostID: id, Action: ReportRemove}); !IsNotCircleModeratorErr(err) {
			t.Fatalf("%s resolve err = %v", name, err)
		}
		if _, err := f.svc.ListReportedPosts(ctx, op, postCircleID, 20, ""); !IsNotCircleModeratorErr(err) {
			t.Fatalf("%s list err = %v", name, err)
		}
	}
	for _, action := range []ReportAction{"", "approve", "REMOVE"} {
		if _, err := f.svc.ResolveReport(ctx, reviewOwner, ResolveReportInput{CircleID: postCircleID, PostID: id, Action: action}); !IsInvalidReportActionErr(err) {
			t.Fatalf("action %q err = %v", action, err)
		}
	}
	if _, err := f.svc.ResolveReport(ctx, reviewOwner, ResolveReportInput{PostID: id, Action: ReportRemove}); !IsCircleIDRequiredErr(err) {
		t.Fatalf("missing circle err = %v", err)
	}
	f.repo.created[0].CircleID = otherCircleID
	if _, err := f.svc.ResolveReport(ctx, reviewOwner, ResolveReportInput{CircleID: postCircleID, PostID: id, Action: ReportRemove}); !IsReportedPostNotFoundErr(err) {
		t.Fatalf("other circle post err = %v", err)
	}
	if len(reports.resolved) != 0 || len(f.pub.removedNotices) != 0 {
		t.Fatal("rejected requests must not resolve reports")
	}
}

func TestListReportedPosts_AssemblesReports(t *testing.T) {
	f, reports, id := newReportFixture(t, moderationCfg(true, "key", true))
	ctx := context.Background()
	for i, op := range []uuid.UUID{reviewMember, reviewStrang} {
		if err := f.svc.ReportPost(ctx, op, ReportPostInput{PostID: id, ReasonType: int16(i + 1), Reason: "理由"}); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	f.log.rows = append(f.log.rows, &domain.PostModerationLog{
		PostID: id, Decision: domain.ModerationReviewPublish, Source: domain.ModerationSourceAI,
		Hazards: domain.HazardScores{domain.HazardHarassment: 0.79}, Model: "jev-test",
	})
	base := time.Now()
	older := &domain.Post{ID: uuid.New(), CircleID: postCircleID, UserID: reviewOwner, Status: domain.PostStatusBlocked}
	reports.reported = []domain.ReportedPost{
		{Post: *f.repo.created[0], ReportCount: 2, FirstReportTime: base},
		{Post: *older, ReportCount: 5, FirstReportTime: base.Add(-time.Hour)},
	}

	page1, err := f.svc.ListReportedPosts(ctx, reviewAdmin, postCircleID, 1, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Posts) != 1 || page1.Cursor == "" {
		t.Fatalf("page1 = %+v", page1)
	}
	p := page1.Posts[0]
	if p.ID != id || p.ReportCount != 2 || len(p.Reports) != 2 || p.Reports[0].ReasonType != 2 || p.Reports[0].Reason != "理由" {
		t.Fatalf("item = %+v", p)
	}
	if p.AIVerdict == nil || p.AIVerdict.Decision != "review_publish" || len(p.AIVerdict.HitHazards) != 1 {
		t.Fatalf("verdict = %+v", p.AIVerdict)
	}

	page2, err := f.svc.ListReportedPosts(ctx, reviewOwner, postCircleID, 1, page1.Cursor)
	if err != nil || len(page2.Posts) != 1 || page2.Posts[0].ID != older.ID || page2.Cursor != "" ||
		page2.Posts[0].Status != domain.PostStatusBlocked || page2.Posts[0].Reports == nil {
		t.Fatalf("page2 = %+v, %v", page2, err)
	}
	if _, err := f.svc.ListReportedPosts(ctx, reviewAdmin, postCircleID, 1, "!!!"); !IsInvalidReviewCursorErr(err) {
		t.Fatalf("bad cursor err = %v", err)
	}
}
