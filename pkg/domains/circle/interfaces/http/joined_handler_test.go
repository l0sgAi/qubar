// Package http 的「我加入的圈子」列表 handler 单元测试：role 参数绑定与校验。
package http

import (
	"context"
	"net/http"
	"testing"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// TestMain 绑定失败路径会记 Error 日志，注入 nop logger（单测环境 logger.Log 为 nil）。
func TestMain(m *testing.M) {
	logger.InitLogger()
	m.Run()
}

// fakeSvcForJoined 记录 GetMyCircles / GetUserCircles 入参的最小 CircleService fake。
type fakeSvcForJoined struct {
	application.CircleService

	called  bool
	gotRole int16
}

func (f *fakeSvcForJoined) GetMyCircles(ctx context.Context, userID uuid.UUID, keyword string, role int16, size int, cursor string) (*application.MyCircleSearchResult, error) {
	f.called, f.gotRole = true, role
	return &application.MyCircleSearchResult{Circles: []application.MyCircleDoc{}, Size: size}, nil
}

func (f *fakeSvcForJoined) GetUserCircles(ctx context.Context, viewerID, targetUserID uuid.UUID, keyword string, role int16, size int, cursor string) (*application.MyCircleSearchResult, error) {
	f.called, f.gotRole = true, role
	return &application.MyCircleSearchResult{Circles: []application.MyCircleDoc{}, Size: size}, nil
}

// TestJoinedCircles_RolePassThrough role 原样透传 service（不传为 0 = 全部）。
func TestJoinedCircles_RolePassThrough(t *testing.T) {
	cases := []struct {
		query string
		want  int16
	}{
		{"", 0},
		{"role=0", 0},
		{"role=30", 30},
		{"role=10&keyword=x", 10},
	}
	for _, tc := range cases {
		for _, ep := range []string{"my", "user"} {
			svc := &fakeSvcForJoined{}
			h := NewHandler(svc)
			q := tc.query
			if ep == "user" {
				q = "user_id=" + uuid.NewString() + "&" + q
			}
			ac, rc := newManageListCtx(q, true)
			if ep == "my" {
				h.GetMyCircles(ac)
			} else {
				h.GetUserCircles(ac)
			}
			if rc.Response.StatusCode() != http.StatusOK || !svc.called || svc.gotRole != tc.want {
				t.Errorf("%s %q: status=%d called=%v role=%d, want 200/true/%d",
					ep, tc.query, rc.Response.StatusCode(), svc.called, svc.gotRole, tc.want)
			}
		}
	}
}

// TestJoinedCircles_InvalidRole400 非数字 role（绑定失败）与越界 role（service 校验）均 400。
func TestJoinedCircles_InvalidRole400(t *testing.T) {
	// 越界值由真实 service 校验（校验先于任何依赖访问，依赖传 nil 即可）。
	realSvc := application.NewCircleService(nil, nil, nil, nil, nil, nil, nil, nil)
	for _, query := range []string{"role=abc", "role=5", "role=-1", "role=40"} {
		for _, ep := range []string{"my", "user"} {
			h := NewHandler(realSvc)
			q := query
			if ep == "user" {
				q = "user_id=" + uuid.NewString() + "&" + q
			}
			ac, rc := newManageListCtx(q, true)
			if ep == "my" {
				h.GetMyCircles(ac)
			} else {
				h.GetUserCircles(ac)
			}
			if rc.Response.StatusCode() != http.StatusBadRequest {
				t.Errorf("%s %q: status = %d, want 400", ep, query, rc.Response.StatusCode())
			}
		}
	}
}
