package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"interestBar/pkg/domains/follow/application"
	"interestBar/pkg/domains/follow/domain"
	"interestBar/pkg/shared/appctx"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
)

// 列表 / 批量关系相关的 fakeSvc 方法：记录入参（gotCursor / gotSize / gotTargets），返回预置错误。

func (f *fakeSvc) list(viewer, target uuid.UUID, cursor string, size int) (*application.FollowListVO, error) {
	f.record(viewer, target)
	f.gotCursor, f.gotSize = cursor, size
	return &application.FollowListVO{
		Items:      []application.FollowListItemVO{{User: application.UserBrief{ID: testTarget, Username: "bob"}, Relation: domain.RelationFollowing}},
		HasMore:    true,
		NextCursor: "next",
	}, f.err
}

func (f *fakeSvc) ListFollowing(_ context.Context, viewer, target uuid.UUID, cursor string, size int) (*application.FollowListVO, error) {
	return f.list(viewer, target, cursor, size)
}

func (f *fakeSvc) ListFollowers(_ context.Context, viewer, target uuid.UUID, cursor string, size int) (*application.FollowListVO, error) {
	return f.list(viewer, target, cursor, size)
}

func (f *fakeSvc) ListMutual(_ context.Context, userID uuid.UUID, cursor string, size int) (*application.FollowListVO, error) {
	return f.list(userID, userID, cursor, size)
}

func (f *fakeSvc) GetRelations(_ context.Context, viewer uuid.UUID, targets []uuid.UUID) (*application.RelationsVO, error) {
	f.record(viewer, uuid.Nil)
	f.gotTargets = targets
	out := &application.RelationsVO{Relations: map[string]domain.Relation{}}
	for _, id := range targets {
		out.Relations[id.String()] = domain.RelationFollowedBy
	}
	return out, f.err
}

// newListCtx 在 newCtx 的基础上设置请求 URI（query）与 JSON body。
func newListCtx(loginID, target, query, body string) (appctx.AppContext, *app.RequestContext) {
	ac, rc := newCtx(loginID, target)
	rc.Request.SetRequestURI("/follow/x?" + query)
	if body != "" {
		rc.Request.SetMethod(http.MethodPost)
		rc.Request.Header.SetContentTypeBytes([]byte("application/json"))
		rc.Request.SetBody([]byte(body))
	}
	return ac, rc
}

func respStatus(rc *app.RequestContext) int  { return rc.Response.StatusCode() }
func respBody(rc *app.RequestContext) string { return string(rc.Response.Body()) }

func TestFollowLists_GuestReadableParamsAndErrors(t *testing.T) {
	for name, call := range map[string]func(*Handler, appctx.AppContext){
		"following": (*Handler).ListFollowing,
		"followers": (*Handler).ListFollowers,
	} {
		t.Run(name, func(t *testing.T) {
			// 访客可读：viewer 为 uuid.Nil，cursor / size 透传
			svc := &fakeSvc{}
			ac, resp := newListCtx("", testTarget, "cursor=abc&size=7", "")
			call(NewHandler(svc), ac)
			if respStatus(resp) != http.StatusOK || svc.gotViewer != uuid.Nil || svc.gotTarget.String() != testTarget ||
				svc.gotCursor != "abc" || svc.gotSize != 7 {
				t.Fatalf("guest: status=%d viewer=%s target=%s cursor=%q size=%d", respStatus(resp), svc.gotViewer, svc.gotTarget, svc.gotCursor, svc.gotSize)
			}
			for _, field := range []string{`"items":[`, `"username":"bob"`, `"follow_time":`, `"relation":1`, `"has_more":true`, `"next_cursor":"next"`} {
				if !strings.Contains(respBody(resp), field) {
					t.Errorf("body %s missing %s", respBody(resp), field)
				}
			}

			// 登录：viewer 透传；不带 query 时 cursor 为空、size 为 0（由 service 取默认值）
			svc = &fakeSvc{}
			ac, _ = newListCtx(testViewer, testTarget, "", "")
			call(NewHandler(svc), ac)
			if svc.gotViewer.String() != testViewer || svc.gotCursor != "" || svc.gotSize != 0 {
				t.Errorf("logged in: viewer=%s cursor=%q size=%d", svc.gotViewer, svc.gotCursor, svc.gotSize)
			}

			for label, tc := range map[string]struct{ target, query string }{
				"bad user id": {"nope", ""},
				"nil user id": {uuid.Nil.String(), ""},
				"bad size":    {testTarget, "size=abc"},
			} {
				svc = &fakeSvc{}
				ac, resp = newListCtx("", tc.target, tc.query, "")
				call(NewHandler(svc), ac)
				if respStatus(resp) != http.StatusBadRequest || svc.calls != 0 {
					t.Errorf("%s: status=%d calls=%d, want 400 / 0", label, respStatus(resp), svc.calls)
				}
			}

			for _, tc := range []struct {
				err  error
				want int
				body string
			}{
				{fmt.Errorf("%w: scope mismatch", domain.ErrInvalidCursor), http.StatusBadRequest, "Invalid cursor"},
				{domain.ErrFollowListPrivate, http.StatusForbidden, "lists are private"},
				{domain.ErrUserNotFound, http.StatusNotFound, "No such user"},
				{errors.New("boom"), http.StatusInternalServerError, ""},
			} {
				ac, resp = newListCtx("", testTarget, "", "")
				call(NewHandler(&fakeSvc{err: tc.err}), ac)
				if respStatus(resp) != tc.want || !strings.Contains(respBody(resp), tc.body) {
					t.Errorf("err=%v: status=%d body=%s, want %d containing %q", tc.err, respStatus(resp), respBody(resp), tc.want, tc.body)
				}
				if strings.Contains(respBody(resp), "scope mismatch") {
					t.Errorf("cursor failure reason leaked to client: %s", respBody(resp))
				}
			}
		})
	}
}

func TestListMutual_RequiresLoginAndUsesCurrentUser(t *testing.T) {
	svc := &fakeSvc{}
	ac, resp := newListCtx("", "", "", "")
	NewHandler(svc).ListMutual(ac)
	if respStatus(resp) != http.StatusUnauthorized || svc.calls != 0 {
		t.Fatalf("guest: status=%d calls=%d, want 401 / 0", respStatus(resp), svc.calls)
	}

	ac, resp = newListCtx(testViewer, "", "cursor=c1&size=50", "")
	NewHandler(svc).ListMutual(ac)
	if respStatus(resp) != http.StatusOK || svc.gotViewer.String() != testViewer || svc.gotCursor != "c1" || svc.gotSize != 50 {
		t.Errorf("logged in: status=%d user=%s cursor=%q size=%d", respStatus(resp), svc.gotViewer, svc.gotCursor, svc.gotSize)
	}

	ac, resp = newListCtx(testViewer, "", "cursor=bad", "")
	NewHandler(&fakeSvc{err: domain.ErrInvalidCursor}).ListMutual(ac)
	if respStatus(resp) != http.StatusBadRequest {
		t.Errorf("invalid cursor: status=%d, want 400", respStatus(resp))
	}
}

func TestGetRelations_AuthBodyAndErrors(t *testing.T) {
	reqBody := fmt.Sprintf(`{"user_ids":[%q,%q]}`, testTarget, testViewer)

	svc := &fakeSvc{}
	ac, resp := newListCtx("", "", "", reqBody)
	NewHandler(svc).GetRelations(ac)
	if respStatus(resp) != http.StatusUnauthorized || svc.calls != 0 {
		t.Fatalf("guest: status=%d calls=%d, want 401 / 0", respStatus(resp), svc.calls)
	}

	ac, resp = newListCtx(testViewer, "", "", reqBody)
	NewHandler(svc).GetRelations(ac)
	if respStatus(resp) != http.StatusOK || svc.gotViewer.String() != testViewer ||
		len(svc.gotTargets) != 2 || svc.gotTargets[0].String() != testTarget {
		t.Fatalf("ok: status=%d viewer=%s targets=%v", respStatus(resp), svc.gotViewer, svc.gotTargets)
	}
	if want := fmt.Sprintf(`"relations":{%q:2`, testViewer); !strings.Contains(respBody(resp), want) {
		t.Errorf("body %s missing %s", respBody(resp), want)
	}

	// 空数组 / 不传 user_ids：照常调用，返回空 map
	for _, empty := range []string{`{}`, `{"user_ids":[]}`} {
		svc = &fakeSvc{}
		ac, resp = newListCtx(testViewer, "", "", empty)
		NewHandler(svc).GetRelations(ac)
		if respStatus(resp) != http.StatusOK || !strings.Contains(respBody(resp), `"relations":{}`) {
			t.Errorf("body %s: status=%d resp=%s, want 200 with empty relations", empty, respStatus(resp), respBody(resp))
		}
	}

	for label, bad := range map[string]string{
		"not json":     `{"user_ids":`,
		"wrong type":   `{"user_ids":"x"}`,
		"invalid uuid": fmt.Sprintf(`{"user_ids":[%q,"nope"]}`, testTarget),
	} {
		svc = &fakeSvc{}
		ac, resp = newListCtx(testViewer, "", "", bad)
		NewHandler(svc).GetRelations(ac)
		if respStatus(resp) != http.StatusBadRequest || svc.calls != 0 {
			t.Errorf("%s: status=%d calls=%d, want 400 / 0", label, respStatus(resp), svc.calls)
		}
	}

	tooMany := fmt.Errorf("%w: at most 100 per request", domain.ErrTooManyTargets)
	ac, resp = newListCtx(testViewer, "", "", reqBody)
	NewHandler(&fakeSvc{err: tooMany}).GetRelations(ac)
	if respStatus(resp) != http.StatusBadRequest || !strings.Contains(respBody(resp), "at most 100") {
		t.Errorf("too many: status=%d body=%s, want 400 with the limit", respStatus(resp), respBody(resp))
	}
}
