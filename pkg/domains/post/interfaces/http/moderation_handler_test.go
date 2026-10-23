// 人工审核（#118）与举报复核（#153）handler 单元测试：错误 → HTTP 状态映射（400 / 403 / 404 / 409 / 429）与成功路径。
package http

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/post/application"
	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/appctx/hertzadapter"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
)

var (
	testAdminID   = uuid.MustParse("0192a0d0-0000-7000-8000-000000000020")
	testReviewing = uuid.MustParse("0192a0d0-0000-7000-8000-000000000030")
)

// fakeReviewRepo 一篇本圈审核中的帖子；ResolveReview 条件更新。
type fakeReviewRepo struct {
	domain.PostRepository

	post *domain.Post
}

func (r *fakeReviewRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Post, error) {
	if id != r.post.ID {
		return nil, domain.ErrPostNotFound
	}
	cp := *r.post
	return &cp, nil
}

func (r *fakeReviewRepo) ResolveReview(_ context.Context, id uuid.UUID, status int16, _ *domain.PostModerationLog) (bool, error) {
	if id != r.post.ID || r.post.Status != domain.PostStatusReviewing {
		return false, nil
	}
	r.post.Status = status
	return true, nil
}

func (r *fakeReviewRepo) ListReviewingByCircle(context.Context, uuid.UUID, *domain.PostKeyset, int) ([]domain.Post, error) {
	return nil, nil
}

func (r *fakeReviewRepo) GetMentionUserIDsByPostIDs(context.Context, []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return map[uuid.UUID][]uuid.UUID{}, nil
}

type fakeReviewPublisher struct{ domain.PostEventPublisher }

func (fakeReviewPublisher) PublishReviewResultNotice(context.Context, uuid.UUID, uuid.UUID, bool) error {
	return nil
}

// fakeAdminMember testAdminID 是 testCircleID 的管理员，其他人是普通成员。
type fakeAdminMember struct{}

func (fakeAdminMember) GetMember(_ context.Context, circleID, userID uuid.UUID) (*application.CircleMemberInfo, error) {
	if circleID == testCircleID && userID == testAdminID {
		return &application.CircleMemberInfo{Role: 20, Status: 1}, nil
	}
	return &application.CircleMemberInfo{Role: 10, Status: 1}, nil
}

func newReviewHandler() *Handler {
	repo := &fakeReviewRepo{post: &domain.Post{ID: testReviewing, CircleID: testCircleID, Status: domain.PostStatusReviewing}}
	svc := application.NewPostService(repo, nil, nil, nil, nil, fakeReviewPublisher{})
	svc.SetMemberChecker(fakeAdminMember{})
	svc.SetPostCountPort(&fakeCount{})
	return NewHandler(svc)
}

func TestReviewPostHandler_StatusCodes(t *testing.T) {
	h := newReviewHandler()
	body := func(postID uuid.UUID, action string) string {
		return `{"circle_id":"` + testCircleID.String() + `","post_id":"` + postID.String() + `","action":"` + action + `"}`
	}
	cases := []struct {
		name string
		user uuid.UUID
		body string
		want int
	}{
		{"bad json", testAdminID, `{`, http.StatusBadRequest},
		{"bad action", testAdminID, body(testReviewing, "publish"), http.StatusBadRequest},
		{"plain member", testAuthorID, body(testReviewing, "approve"), http.StatusForbidden},
		{"missing post", testAdminID, body(uuid.New(), "approve"), http.StatusNotFound},
		{"approve", testAdminID, body(testReviewing, "approve"), http.StatusOK},
		{"already handled", testAdminID, body(testReviewing, "reject"), http.StatusNotFound},
	}
	for _, tc := range cases { // 顺序执行：approve 之后帖子已不是审核中
		rc := &app.RequestContext{}
		rc.Request.SetMethod(http.MethodPost)
		rc.Request.SetBody([]byte(tc.body))
		rc.Request.Header.SetContentTypeBytes([]byte("application/json"))
		ac := hertzadapter.New(context.Background(), rc)
		ac.SetLoginID(tc.user.String())

		h.ReviewPost(ac)

		if got := rc.Response.StatusCode(); got != tc.want {
			t.Fatalf("%s: status = %d, want %d; body=%s", tc.name, got, tc.want, rc.Response.Body())
		}
	}
}

func TestListReviewQueueHandler_StatusCodes(t *testing.T) {
	h := newReviewHandler()
	cases := []struct {
		name  string
		user  uuid.UUID
		query string
		want  int
	}{
		{"missing circle", testAdminID, "", http.StatusBadRequest},
		{"bad cursor", testAdminID, "circle_id=" + testCircleID.String() + "&cursor=%21%21", http.StatusBadRequest},
		{"plain member", testAuthorID, "circle_id=" + testCircleID.String(), http.StatusForbidden},
		{"admin", testAdminID, "circle_id=" + testCircleID.String() + "&size=5", http.StatusOK},
	}
	for _, tc := range cases {
		rc := &app.RequestContext{}
		rc.Request.SetMethod(http.MethodGet)
		rc.Request.SetRequestURI("/circle/moderation/posts?" + tc.query)
		ac := hertzadapter.New(context.Background(), rc)
		ac.SetLoginID(tc.user.String())

		h.ListReviewQueue(ac)

		if got := rc.Response.StatusCode(); got != tc.want {
			t.Fatalf("%s: status = %d, want %d; body=%s", tc.name, got, tc.want, rc.Response.Body())
		}
	}
}

// ===== 举报与复核（#153）=====

var testPublished = uuid.MustParse("0192a0d0-0000-7000-8000-000000000040")

// fakeHandlerReportRepo 内存举报仓储：唯一 (post, reporter)，复核关闭全部待处理举报。
type fakeHandlerReportRepo struct {
	domain.PostReportRepository

	reports []*domain.PostReport
}

func (r *fakeHandlerReportRepo) Create(_ context.Context, rp *domain.PostReport, _ domain.ReportThresholds) (domain.ReportOutcome, error) {
	for _, x := range r.reports {
		if x.PostID == rp.PostID && x.ReporterID == rp.ReporterID {
			return domain.ReportOutcome{}, domain.ErrPostReportDuplicate
		}
	}
	rp.CreateTime = time.Now()
	r.reports = append(r.reports, rp)
	return domain.ReportOutcome{Pending: len(r.reports)}, nil
}

func (r *fakeHandlerReportRepo) CountByReporterSince(_ context.Context, reporterID uuid.UUID, _ time.Time) (int64, error) {
	var n int64
	for _, x := range r.reports {
		if x.ReporterID == reporterID {
			n++
		}
	}
	return n, nil
}

func (r *fakeHandlerReportRepo) Resolve(_ context.Context, postID, _ uuid.UUID, _ bool, _ *domain.PostModerationLog) (int, bool, error) {
	closed := 0
	for _, x := range r.reports {
		if x.PostID == postID && x.Status == domain.PostReportPending {
			x.Status = domain.PostReportRemoved
			closed++
		}
	}
	return closed, closed > 0, nil
}

func (fakeReviewPublisher) PublishPostRemovedNotice(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

// fakeTwoPostRepo 一篇本圈已发布帖子（作者 testAuthorID）+ 一篇审核中帖子。
type fakeTwoPostRepo struct {
	fakeReviewRepo

	published *domain.Post
}

func (r *fakeTwoPostRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Post, error) {
	if id == r.published.ID {
		cp := *r.published
		return &cp, nil
	}
	return r.fakeReviewRepo.GetByID(ctx, id)
}

func newReportHandler(dailyLimit int) *Handler {
	repo := &fakeTwoPostRepo{
		fakeReviewRepo: fakeReviewRepo{post: &domain.Post{ID: testReviewing, CircleID: testCircleID, Status: domain.PostStatusReviewing}},
		published:      &domain.Post{ID: testPublished, CircleID: testCircleID, UserID: testAuthorID, Status: domain.PostStatusPublished},
	}
	svc := application.NewPostService(repo, nil, nil, nil, nil, fakeReviewPublisher{})
	svc.SetMemberChecker(fakeAdminMember{})
	svc.SetModeration(nil, nil, conf.Moderation{ReportDailyLimit: dailyLimit})
	svc.SetReportRepository(&fakeHandlerReportRepo{})
	return NewHandler(svc)
}

func doJSON(t *testing.T, call func(c appctx.AppContext), user uuid.UUID, body string) (int, string) {
	t.Helper()
	rc := &app.RequestContext{}
	rc.Request.SetMethod(http.MethodPost)
	rc.Request.SetBody([]byte(body))
	rc.Request.Header.SetContentTypeBytes([]byte("application/json"))
	ac := hertzadapter.New(context.Background(), rc)
	ac.SetLoginID(user.String())
	call(ac)
	return rc.Response.StatusCode(), string(rc.Response.Body())
}

func TestReportPostHandler_StatusCodes(t *testing.T) {
	h := newReportHandler(1)
	reporter, other := uuid.New(), uuid.New()
	body := func(postID uuid.UUID, reason int) string {
		return `{"post_id":"` + postID.String() + `","reason_type":` + strconv.Itoa(reason) + `,"reason":"广告"}`
	}
	cases := []struct {
		name string
		user uuid.UUID
		body string
		want int
	}{
		{"bad json", reporter, `{`, http.StatusBadRequest},
		{"bad reason", reporter, body(testPublished, 9), http.StatusBadRequest},
		{"own post", testAuthorID, body(testPublished, 1), http.StatusBadRequest},
		{"missing post", reporter, body(uuid.New(), 1), http.StatusNotFound},
		{"reviewing post", reporter, body(testReviewing, 1), http.StatusNotFound},
		{"report", reporter, body(testPublished, 1), http.StatusOK},
		{"another reporter", other, body(testPublished, 2), http.StatusOK},
		{"daily limit", reporter, body(testPublished, 1), http.StatusTooManyRequests},
	}
	for _, tc := range cases { // 顺序执行：后面的用例依赖前面已写入的举报
		if got, b := doJSON(t, func(c appctx.AppContext) { h.ReportPost(c) }, tc.user, tc.body); got != tc.want {
			t.Fatalf("%s: status = %d, want %d; body=%s", tc.name, got, tc.want, b)
		}
	}
}

func TestReportPostHandler_Duplicate(t *testing.T) {
	h := newReportHandler(5)
	reporter := uuid.New()
	body := `{"post_id":"` + testPublished.String() + `","reason_type":1}`
	for i, want := range []int{http.StatusOK, http.StatusConflict} {
		if got, b := doJSON(t, func(c appctx.AppContext) { h.ReportPost(c) }, reporter, body); got != want {
			t.Fatalf("attempt %d: status = %d, want %d; body=%s", i, got, want, b)
		}
	}
}

func TestResolveReportHandler_StatusCodes(t *testing.T) {
	h := newReportHandler(1)
	if got, b := doJSON(t, func(c appctx.AppContext) { h.ReportPost(c) }, uuid.New(),
		`{"post_id":"`+testPublished.String()+`","reason_type":1}`); got != http.StatusOK {
		t.Fatalf("seed report status = %d; body=%s", got, b)
	}
	body := func(action string) string {
		return `{"circle_id":"` + testCircleID.String() + `","post_id":"` + testPublished.String() + `","action":"` + action + `"}`
	}
	cases := []struct {
		name string
		user uuid.UUID
		body string
		want int
	}{
		{"bad json", testAdminID, `{`, http.StatusBadRequest},
		{"bad action", testAdminID, body("approve"), http.StatusBadRequest},
		{"plain member", testAuthorID, body("remove"), http.StatusForbidden},
		{"remove", testAdminID, body("remove"), http.StatusOK},
		{"already handled", testAdminID, body("dismiss"), http.StatusNotFound},
	}
	for _, tc := range cases {
		if got, b := doJSON(t, func(c appctx.AppContext) { h.ResolveReport(c) }, tc.user, tc.body); got != tc.want {
			t.Fatalf("%s: status = %d, want %d; body=%s", tc.name, got, tc.want, b)
		}
	}
}
