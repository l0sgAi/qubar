package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"interestBar/pkg/domains/follow/application"
	"interestBar/pkg/domains/follow/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/appctx/hertzadapter"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route/param"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

const (
	testViewer = "018f0000-0000-7000-8000-000000000001"
	testTarget = "018f0000-0000-7000-8000-000000000002"
)

// fakeSvc 记录入参并返回预置结果。
type fakeSvc struct {
	err                  error
	gotViewer, gotTarget uuid.UUID
	calls                int
	// 列表 / 批量关系接口的入参（list_handler_test.go）
	gotCursor  string
	gotSize    int
	gotTargets []uuid.UUID
}

func (f *fakeSvc) record(viewer, target uuid.UUID) {
	f.calls++
	f.gotViewer, f.gotTarget = viewer, target
}

func (f *fakeSvc) Follow(_ context.Context, follower, followee uuid.UUID) (*application.RelationVO, error) {
	f.record(follower, followee)
	return &application.RelationVO{Relation: domain.RelationFollowing}, f.err
}

func (f *fakeSvc) Unfollow(_ context.Context, follower, followee uuid.UUID) (*application.RelationVO, error) {
	f.record(follower, followee)
	return &application.RelationVO{Relation: domain.RelationNone}, f.err
}

func (f *fakeSvc) GetStats(_ context.Context, viewer, target uuid.UUID) (*application.StatsVO, error) {
	f.record(viewer, target)
	return &application.StatsVO{UserID: target.String(), FollowingCount: 3, FollowerCount: 7, Relation: domain.RelationMutual}, f.err
}

// newCtx 构造带路径参数 :user_id 的 AppContext；loginID 为空表示访客。
func newCtx(loginID, target string) (appctx.AppContext, *app.RequestContext) {
	rc := &app.RequestContext{}
	rc.Params = append(rc.Params, param.Param{Key: "user_id", Value: target})
	ac := hertzadapter.New(context.Background(), rc)
	if loginID != "" {
		ac.SetLoginID(loginID)
	}
	return ac, rc
}

func TestFollowUnfollow_AuthParamsAndErrors(t *testing.T) {
	limitErr := fmt.Errorf("%w: you can follow at most 2000 users", domain.ErrFollowLimitReached)
	for name, call := range map[string]func(*Handler, appctx.AppContext){
		"follow":   (*Handler).Follow,
		"unfollow": (*Handler).Unfollow,
	} {
		t.Run(name, func(t *testing.T) {
			// 访客 401，不触达 service
			svc := &fakeSvc{}
			ac, rc := newCtx("", testTarget)
			call(NewHandler(svc), ac)
			if rc.Response.StatusCode() != http.StatusUnauthorized || svc.calls != 0 {
				t.Errorf("anonymous: status=%d calls=%d, want 401 / 0", rc.Response.StatusCode(), svc.calls)
			}

			for _, bad := range []string{"not-a-uuid", uuid.Nil.String()} {
				ac, rc = newCtx(testViewer, bad)
				call(NewHandler(svc), ac)
				if rc.Response.StatusCode() != http.StatusBadRequest || svc.calls != 0 {
					t.Errorf("target %q: status=%d calls=%d, want 400 / 0", bad, rc.Response.StatusCode(), svc.calls)
				}
			}

			for _, tc := range []struct {
				err  error
				want int
				body string
			}{
				{nil, http.StatusOK, `"relation":`},
				{domain.ErrCannotFollowSelf, http.StatusBadRequest, "Cannot follow yourself"},
				{domain.ErrUserNotFound, http.StatusNotFound, "No such user"},
				{limitErr, http.StatusBadRequest, "at most 2000 users"},
				{errors.New("boom"), http.StatusInternalServerError, ""},
			} {
				svc = &fakeSvc{err: tc.err}
				ac, rc = newCtx(testViewer, testTarget)
				call(NewHandler(svc), ac)
				if rc.Response.StatusCode() != tc.want || !strings.Contains(string(rc.Response.Body()), tc.body) {
					t.Errorf("err=%v: status=%d body=%s, want %d containing %q",
						tc.err, rc.Response.StatusCode(), rc.Response.Body(), tc.want, tc.body)
				}
				if svc.gotViewer.String() != testViewer || svc.gotTarget.String() != testTarget {
					t.Errorf("service got viewer=%s target=%s", svc.gotViewer, svc.gotTarget)
				}
			}
		})
	}
}

func TestGetStats_GuestAllowedAndErrors(t *testing.T) {
	// 访客：200，viewer 为 uuid.Nil
	svc := &fakeSvc{}
	ac, rc := newCtx("", testTarget)
	NewHandler(svc).GetStats(ac)
	body := string(rc.Response.Body())
	if rc.Response.StatusCode() != http.StatusOK || svc.gotViewer != uuid.Nil {
		t.Fatalf("guest: status=%d viewer=%s, want 200 / nil viewer", rc.Response.StatusCode(), svc.gotViewer)
	}
	for _, field := range []string{`"following_count":3`, `"follower_count":7`, `"relation":3`} {
		if !strings.Contains(body, field) {
			t.Errorf("body %s missing %s", body, field)
		}
	}

	// 登录：viewer 透传
	svc = &fakeSvc{}
	ac, _ = newCtx(testViewer, testTarget)
	NewHandler(svc).GetStats(ac)
	if svc.gotViewer.String() != testViewer {
		t.Errorf("logged-in viewer = %s, want %s", svc.gotViewer, testViewer)
	}

	ac, rc = newCtx("", "bad")
	NewHandler(&fakeSvc{}).GetStats(ac)
	if rc.Response.StatusCode() != http.StatusBadRequest {
		t.Errorf("bad id status = %d, want 400", rc.Response.StatusCode())
	}

	ac, rc = newCtx("", testTarget)
	NewHandler(&fakeSvc{err: domain.ErrUserNotFound}).GetStats(ac)
	if rc.Response.StatusCode() != http.StatusNotFound {
		t.Errorf("missing user status = %d, want 404", rc.Response.StatusCode())
	}
}
