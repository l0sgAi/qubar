// 人工审核 handler（#118）单元测试：错误 → HTTP 状态映射（400 / 403 / 404）与成功路径。
package http

import (
	"context"
	"net/http"
	"testing"

	"interestBar/pkg/domains/post/application"
	"interestBar/pkg/domains/post/domain"
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
