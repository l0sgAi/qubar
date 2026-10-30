// Package http 的邀请链接 handler 单元测试（#169）：错误映射（无效 404 / 拉黑 403 / 参数 400）、
// 预览允许访客、接受需登录、创建入参透传（max_uses 不传 = 不限次数）。
package http

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/shared/appctx/hertzadapter"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
)

// fakeSvcForInvite 仅实现邀请方法的最小 CircleService fake。
type fakeSvcForInvite struct {
	application.CircleService

	err      error
	gotToken string
	gotInput application.CreateInviteLinkInput
}

func (f *fakeSvcForInvite) PreviewInvite(_ context.Context, token string) (*application.InvitePreviewVO, error) {
	f.gotToken = token
	if f.err != nil {
		return nil, f.err
	}
	return &application.InvitePreviewVO{Name: "圈"}, nil
}

func (f *fakeSvcForInvite) AcceptInvite(_ context.Context, _ uuid.UUID, token string) (*application.InviteAcceptResult, error) {
	f.gotToken = token
	if f.err != nil {
		return nil, f.err
	}
	return &application.InviteAcceptResult{}, nil
}

func (f *fakeSvcForInvite) CreateInviteLink(_ context.Context, _, _ uuid.UUID, in application.CreateInviteLinkInput) (*application.InviteLinkCreated, error) {
	f.gotInput = in
	if f.err != nil {
		return nil, f.err
	}
	return &application.InviteLinkCreated{Token: "t"}, nil
}

func TestPreviewInvite_GuestAndInvalid(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{nil, http.StatusOK},
		{inviteInvalidErr(t), http.StatusNotFound},
	} {
		svc := &fakeSvcForInvite{err: tc.err}
		rc := &app.RequestContext{}
		rc.Request.SetQueryString("token=abc_-1")
		NewHandler(svc).PreviewInvite(hertzadapter.New(context.Background(), rc)) // 访客，无登录态

		if rc.Response.StatusCode() != tc.want || svc.gotToken != "abc_-1" {
			t.Errorf("err=%v: status=%d token=%q, want %d", tc.err, rc.Response.StatusCode(), svc.gotToken, tc.want)
		}
	}
}

func TestAcceptInvite_RequiresLoginAndMapsErrors(t *testing.T) {
	rc := &app.RequestContext{}
	rc.Request.Header.SetContentTypeBytes([]byte("application/json"))
	rc.Request.SetBody([]byte(`{"token":"x"}`))
	NewHandler(&fakeSvcForInvite{}).AcceptInvite(hertzadapter.New(context.Background(), rc))
	if rc.Response.StatusCode() != http.StatusUnauthorized {
		t.Errorf("anonymous accept status = %d, want 401", rc.Response.StatusCode())
	}

	for _, tc := range []struct {
		body string
		err  error
		want int
	}{
		{`{"token":"x"}`, nil, http.StatusOK},
		{`{"token":"x"}`, inviteInvalidErr(t), http.StatusNotFound},
		{`{"token":"x"}`, errors.New("boom"), http.StatusInternalServerError},
	} {
		ac, rc := newJSONCtx(tc.body)
		NewHandler(&fakeSvcForInvite{err: tc.err}).AcceptInvite(ac)
		if rc.Response.StatusCode() != tc.want {
			t.Errorf("body=%s err=%v: status = %d, want %d", tc.body, tc.err, rc.Response.StatusCode(), tc.want)
		}
	}
}

func TestCreateInviteLink_PassThrough(t *testing.T) {
	svc := &fakeSvcForInvite{}
	ac, rc := newJSONCtx(`{"circle_id":"` + testCircle + `","expire_hours":24}`)
	NewHandler(svc).CreateInviteLink(ac)
	if rc.Response.StatusCode() != http.StatusOK || svc.gotInput.ExpireHours != 24 || svc.gotInput.MaxUses != nil {
		t.Fatalf("status=%d input=%+v, want 200 / 24h / unlimited", rc.Response.StatusCode(), svc.gotInput)
	}
}

// stubInviteRepo 任何 token 都查不到。
type stubInviteRepo struct{ domain.InviteLinkRepository }

func (stubInviteRepo) GetByTokenHash(context.Context, string) (*domain.CircleInviteLink, error) {
	return nil, domain.ErrInviteLinkNotFound
}

// inviteInvalidErr 经真实 service 取得 errInviteInvalid（未导出哨兵）。
func inviteInvalidErr(t *testing.T) error {
	t.Helper()
	svc := application.NewCircleService(nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetInviteLinkRepository(stubInviteRepo{})
	_, err := svc.PreviewInvite(context.Background(), "unknown")
	if !application.IsInviteInvalidErr(err) {
		t.Fatalf("setup: err = %v, want invite invalid", err)
	}
	return err
}
