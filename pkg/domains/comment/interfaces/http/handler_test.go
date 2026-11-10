// Package http 的评论列表 handler 单元测试（#209 游标 v2）。
//
// handler 接真实 CommentService（仓储为 fake）：仓储对「不属于该帖 / 该楼或排序不一致」的游标返回
// 包装 ErrInvalidCursor 的错误，列表与回复接口都必须回 HTTP 400，而不是 500。
// 不启动 hertz server，直接构造 *app.RequestContext（对齐 post 域 handler 测试范式）。
package http

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"interestBar/pkg/domains/comment/application"
	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/appctx/hertzadapter"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	m.Run()
}

var (
	testPostID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000b0")
	testRootID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000a1")
)

// foreignCursor fake 仓储视为「别的帖子 / 楼 / 排序下产出」的游标。
const foreignCursor = "foreign"

// fakeRepo 只实现列表路径用到的方法；游标校验结果由 foreignCursor 模拟。
type fakeRepo struct {
	domain.CommentRepository
}

func (fakeRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Comment, error) {
	if id != testRootID {
		return nil, domain.ErrCommentNotFound
	}
	c := &domain.Comment{PostID: testPostID, Status: domain.CommentStatusNormal}
	c.ID = testRootID
	return c, nil
}

func (fakeRepo) GetRootCommentsByCursor(_ context.Context, _ uuid.UUID, _, _ int, cursor string) ([]domain.Comment, string, bool, error) {
	return listWithCursor(cursor)
}

func (fakeRepo) GetRepliesByCursor(_ context.Context, _ uuid.UUID, _, _ int, cursor string) ([]domain.Comment, string, bool, error) {
	return listWithCursor(cursor)
}

func listWithCursor(cursor string) ([]domain.Comment, string, bool, error) {
	if cursor == foreignCursor {
		return nil, "", false, fmt.Errorf("%w: scope mismatch", domain.ErrInvalidCursor)
	}
	return nil, "", false, nil
}

// get 以匿名访客身份调用一个 GET handler，返回 HTTP 状态码。
func get(t *testing.T, handle func(*Handler, appctx.AppContext), uri string) int {
	t.Helper()
	h := NewHandler(application.NewCommentService(fakeRepo{}, nil, nil))
	rc := &app.RequestContext{}
	rc.Request.SetMethod(http.MethodGet)
	rc.Request.SetRequestURI(uri)
	handle(h, hertzadapter.New(context.Background(), rc))
	return rc.Response.StatusCode()
}

// TestListHandlers_InvalidCursorIs400 游标不属于本次请求的帖子 / 楼 / 排序 → 400；正常游标 / 无游标 → 200。
func TestListHandlers_InvalidCursorIs400(t *testing.T) {
	list := func(h *Handler, c appctx.AppContext) { h.GetComments(c) }
	replies := func(h *Handler, c appctx.AppContext) { h.GetReplies(c) }
	listURI := "/comment/list?post_id=" + testPostID.String()
	repliesURI := "/comment/replies?root_id=" + testRootID.String()

	cases := []struct {
		name   string
		handle func(*Handler, appctx.AppContext)
		uri    string
		want   int
	}{
		{"顶层列表：首页", list, listURI, http.StatusOK},
		{"顶层列表：有效游标", list, listURI + "&cursor=ok", http.StatusOK},
		{"顶层列表：别处的游标", list, listURI + "&cursor=" + foreignCursor, http.StatusBadRequest},
		{"回复列表：首页", replies, repliesURI, http.StatusOK},
		{"回复列表：有效游标", replies, repliesURI + "&sort=1&cursor=ok", http.StatusOK},
		{"回复列表：别处的游标", replies, repliesURI + "&sort=1&cursor=" + foreignCursor, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := get(t, tc.handle, tc.uri); got != tc.want {
				t.Fatalf("GET %s: status = %d, want %d", tc.uri, got, tc.want)
			}
		})
	}
}
