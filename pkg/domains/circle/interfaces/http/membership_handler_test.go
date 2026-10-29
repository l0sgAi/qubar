// Package http 的加入 / 移出 handler 单元测试（#168）。
//
// 覆盖：POST /circle/manage/remove 入参透传与错误映射（403 / 404 / 409）、
// POST /circle/join 私密圈 403 与成功文案、join_type=1 的 400、
// 入圈审核路由已下线（/circle/manage/review 不再注册）。
package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/appctx/hertzadapter"
	"interestBar/pkg/shared/routing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
)

// fakeSvcForMembership 仅实现 JoinCircle / RemoveMember 的最小 CircleService fake。
type fakeSvcForMembership struct {
	application.CircleService

	err         error
	removeCalls int
	gotOperator uuid.UUID
	gotCircle   uuid.UUID
	gotTarget   uuid.UUID
}

func (f *fakeSvcForMembership) RemoveMember(_ context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	f.removeCalls++
	f.gotOperator, f.gotCircle, f.gotTarget = operatorID, circleID, targetUserID
	return f.err
}

func (f *fakeSvcForMembership) JoinCircle(context.Context, uuid.UUID, uuid.UUID) error { return f.err }

func (f *fakeSvcForMembership) UpdateCircleProfile(context.Context, uuid.UUID, uuid.UUID, application.UpdateCircleProfileInput) error {
	return f.err
}

const (
	testOperator = "0192a0d0-0000-7000-8000-000000000001"
	testCircle   = "0192a0d0-0000-7000-8000-0000000000c1"
	testTarget   = "0192a0d0-0000-7000-8000-0000000000d1"
)

// newJSONCtx 构造带 JSON body 的已登录 AppContext。
func newJSONCtx(body string) (appctx.AppContext, *app.RequestContext) {
	rc := &app.RequestContext{}
	rc.Request.Header.SetContentTypeBytes([]byte("application/json"))
	rc.Request.SetBody([]byte(body))
	ac := hertzadapter.New(context.Background(), rc)
	ac.SetLoginID(testOperator)
	return ac, rc
}

func TestManageRemove_PassThrough(t *testing.T) {
	svc := &fakeSvcForMembership{}
	ac, rc := newJSONCtx(`{"circle_id":"` + testCircle + `","target_user_id":"` + testTarget + `"}`)
	NewHandler(svc).ManageRemove(ac)

	if rc.Response.StatusCode() != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rc.Response.StatusCode(), rc.Response.Body())
	}
	if svc.gotOperator.String() != testOperator || svc.gotCircle.String() != testCircle || svc.gotTarget.String() != testTarget {
		t.Fatalf("service got op=%s circle=%s target=%s", svc.gotOperator, svc.gotCircle, svc.gotTarget)
	}
}

func TestManageRemove_BadBody(t *testing.T) {
	svc := &fakeSvcForMembership{}
	ac, rc := newJSONCtx(`{"circle_id":"not-a-uuid"}`)
	NewHandler(svc).ManageRemove(ac)

	if rc.Response.StatusCode() != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rc.Response.StatusCode())
	}
	if svc.removeCalls != 0 {
		t.Fatal("service must not be called on bind failure")
	}
}

func TestManageRemove_ErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"target not member", domain.ErrMemberNotFound, http.StatusNotFound},
		{"state conflict", domain.ErrMemberStateConflict, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ac, rc := newJSONCtx(`{"circle_id":"` + testCircle + `","target_user_id":"` + testTarget + `"}`)
			NewHandler(&fakeSvcForMembership{err: tc.err}).ManageRemove(ac)
			if rc.Response.StatusCode() != tc.want {
				t.Fatalf("status = %d, want %d", rc.Response.StatusCode(), tc.want)
			}
		})
	}
}

// TestJoinCircle_Responses 成功文案与私密圈 403。错误由真实 service 产出（fake 仓储让 JoinCircle 走到
// mapJoinLeaveError），避免在 http 包伪造 application 的私有哨兵。
func TestJoinCircle_Responses(t *testing.T) {
	ok := &fakeSvcForMembership{}
	ac, rc := newJSONCtx(`{"circle_id":"` + testCircle + `"}`)
	NewHandler(ok).JoinCircle(ac)
	if rc.Response.StatusCode() != http.StatusOK || !strings.Contains(string(rc.Response.Body()), "Successfully joined") {
		t.Fatalf("success: status=%d body=%s", rc.Response.StatusCode(), rc.Response.Body())
	}

	private := &fakeSvcForMembership{err: privateCircleErr(t)}
	ac, rc = newJSONCtx(`{"circle_id":"` + testCircle + `"}`)
	NewHandler(private).JoinCircle(ac)
	if rc.Response.StatusCode() != http.StatusForbidden {
		t.Fatalf("private: status = %d, want 403", rc.Response.StatusCode())
	}
}

func TestUpdateCircle_JoinTypeApproval400(t *testing.T) {
	svc := &fakeSvcForMembership{err: invalidJoinTypeErr(t)}
	ac, rc := newJSONCtx(`{"circle_id":"` + testCircle + `","join_type":1}`)
	NewHandler(svc).UpdateCircleProfile(ac)
	if rc.Response.StatusCode() != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rc.Response.StatusCode())
	}
	if !strings.Contains(string(rc.Response.Body()), "0 (public) or 2 (private)") {
		t.Fatalf("body = %s, want updated join_type message", rc.Response.Body())
	}
}

// privateCircleErr / invalidJoinTypeErr 从真实 service 取 application 层私有错误。
func privateCircleErr(t *testing.T) error {
	t.Helper()
	circle := &domain.Circle{ID: uuid.MustParse(testCircle), JoinType: domain.CircleJoinTypePrivate, Status: domain.CircleStatusNormal}
	svc := application.NewCircleService(stubCircleRepo{c: circle}, stubMemberRepo{}, nil, nil, nil, nil, nil)
	err := svc.JoinCircle(context.Background(), uuid.MustParse(testOperator), circle.ID)
	if !application.IsPrivateCircleErr(err) {
		t.Fatalf("setup: err = %v, want private circle error", err)
	}
	return err
}

func invalidJoinTypeErr(t *testing.T) error {
	t.Helper()
	svc := application.NewCircleService(nil, nil, nil, nil, nil, nil, nil)
	err := svc.CreateCircle(context.Background(), uuid.MustParse(testOperator), application.CreateCircleInput{JoinType: domain.CircleJoinTypeApproval})
	if !application.IsInvalidJoinTypeErr(err) {
		t.Fatalf("setup: err = %v, want invalid join_type", err)
	}
	return err
}

type stubCircleRepo struct {
	domain.CircleRepository
	c *domain.Circle
}

func (r stubCircleRepo) GetByID(context.Context, uuid.UUID) (*domain.Circle, error) { return r.c, nil }

type stubMemberRepo struct{ domain.MemberRepository }

func (stubMemberRepo) JoinCircle(context.Context, uuid.UUID, uuid.UUID, int16) (*domain.CircleMember, error) {
	return nil, errors.New("this circle is private and requires invitation")
}

// TestRoutes_ReviewRemoved 入圈审核路由已删除，移出路由已注册。
func TestRoutes_ReviewRemoved(t *testing.T) {
	rg := &recordingGroup{}
	RegisterRoutes(rg, &fakeSvcForMembership{}, nil, nil)

	if rg.has("POST", "/circle/manage/review") {
		t.Error("/circle/manage/review must no longer be registered")
	}
	if !rg.has("POST", "/circle/manage/remove") {
		t.Error("/circle/manage/remove must be registered")
	}
}

// recordingGroup 记录注册路由的 RouterGroup（只关心 method + 完整路径）。
type recordingGroup struct {
	prefix string
	routes *[]string
}

func (g *recordingGroup) add(method, path string) {
	if g.routes == nil {
		g.routes = &[]string{}
	}
	*g.routes = append(*g.routes, method+" "+g.prefix+path)
}

func (g *recordingGroup) has(method, path string) bool {
	if g.routes == nil {
		return false
	}
	for _, r := range *g.routes {
		if r == method+" "+path {
			return true
		}
	}
	return false
}

func (g *recordingGroup) Group(prefix string, _ ...routing.HandlerFunc) routing.RouterGroup {
	if g.routes == nil {
		g.routes = &[]string{}
	}
	return &recordingGroup{prefix: g.prefix + prefix, routes: g.routes}
}

func (g *recordingGroup) GET(p string, _ ...routing.HandlerFunc) {
	g.add("GET", p)
}
func (g *recordingGroup) POST(p string, _ ...routing.HandlerFunc) {
	g.add("POST", p)
}
func (g *recordingGroup) PUT(p string, _ ...routing.HandlerFunc) {
	g.add("PUT", p)
}
func (g *recordingGroup) DELETE(p string, _ ...routing.HandlerFunc) {
	g.add("DELETE", p)
}
