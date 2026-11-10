package http

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"interestBar/pkg/domains/user/application"
	"interestBar/pkg/domains/user/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/appctx/hertzadapter"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

const (
	testUser  = "018f0000-0000-7000-8000-0000000000a1"
	otherUser = "018f0000-0000-7000-8000-0000000000a2"
)

// memSettingRepo 内存版设置仓储。
type memSettingRepo struct {
	rows map[uuid.UUID]int16
	err  error
}

func (r *memSettingRepo) Get(_ context.Context, userID uuid.UUID) (*domain.UserSetting, error) {
	if r.err != nil {
		return nil, r.err
	}
	return &domain.UserSetting{UserID: userID, FollowListVisibility: r.rows[userID]}, nil
}

func (r *memSettingRepo) SetFollowListVisibility(_ context.Context, userID uuid.UUID, v int16) error {
	if r.err != nil {
		return r.err
	}
	r.rows[userID] = v
	return nil
}

func settingCtx(loginID, body string) (appctx.AppContext, *app.RequestContext) {
	rc := &app.RequestContext{}
	if body != "" {
		rc.Request.SetMethod(http.MethodPut)
		rc.Request.Header.SetContentTypeBytes([]byte("application/json"))
		rc.Request.SetBody([]byte(body))
	}
	ac := hertzadapter.New(context.Background(), rc)
	if loginID != "" {
		ac.SetLoginID(loginID)
	}
	return ac, rc
}

func TestSettingHandler_ReadWriteOwnSettingsOnly(t *testing.T) {
	repo := &memSettingRepo{rows: map[uuid.UUID]int16{}}
	h := NewSettingHandler(application.NewSettingService(repo))

	// 未登录：读写都是 401
	for name, call := range map[string]func(appctx.AppContext){"get": h.GetSettings, "update": h.UpdateSettings} {
		ac, rc := settingCtx("", `{"follow_list_visibility":1}`)
		call(ac)
		if rc.Response.StatusCode() != http.StatusUnauthorized {
			t.Errorf("%s anonymous: status=%d, want 401", name, rc.Response.StatusCode())
		}
	}
	if len(repo.rows) != 0 {
		t.Fatalf("anonymous request wrote settings: %v", repo.rows)
	}

	// 默认公开
	ac, rc := settingCtx(testUser, "")
	h.GetSettings(ac)
	if rc.Response.StatusCode() != http.StatusOK || !strings.Contains(string(rc.Response.Body()), `"follow_list_visibility":0`) {
		t.Fatalf("default: status=%d body=%s", rc.Response.StatusCode(), rc.Response.Body())
	}

	// 设为私密：只影响当前登录用户
	ac, rc = settingCtx(testUser, `{"follow_list_visibility":1}`)
	h.UpdateSettings(ac)
	if rc.Response.StatusCode() != http.StatusOK || !strings.Contains(string(rc.Response.Body()), `"follow_list_visibility":1`) {
		t.Fatalf("update: status=%d body=%s", rc.Response.StatusCode(), rc.Response.Body())
	}
	ac, rc = settingCtx(otherUser, "")
	h.GetSettings(ac)
	if !strings.Contains(string(rc.Response.Body()), `"follow_list_visibility":0`) {
		t.Errorf("other user sees %s, want still public", rc.Response.Body())
	}

	// 非法值 / 空请求 / 类型错误：400，设置不变
	for label, tc := range map[string]struct{ body, msg string }{
		"out of range": {`{"follow_list_visibility":2}`, "must be 0 (public) or 1 (private)"},
		"negative":     {`{"follow_list_visibility":-1}`, "must be 0 (public) or 1 (private)"},
		"null":         {`{"follow_list_visibility":null}`, "At least one setting"},
		"empty":        {`{}`, "At least one setting"},
		"wrong type":   {`{"follow_list_visibility":"private"}`, "Invalid request parameters"},
		"not json":     {`{`, "Invalid request parameters"},
	} {
		ac, rc = settingCtx(testUser, tc.body)
		h.UpdateSettings(ac)
		if rc.Response.StatusCode() != http.StatusBadRequest || !strings.Contains(string(rc.Response.Body()), tc.msg) {
			t.Errorf("%s: status=%d body=%s, want 400 containing %q", label, rc.Response.StatusCode(), rc.Response.Body(), tc.msg)
		}
	}
	if repo.rows[uuid.MustParse(testUser)] != domain.FollowListPrivate {
		t.Errorf("setting changed by an invalid request: %v", repo.rows)
	}

	// 仓储错误：500
	repo.err = errors.New("boom")
	for name, call := range map[string]func(appctx.AppContext){"get": h.GetSettings, "update": h.UpdateSettings} {
		ac, rc = settingCtx(testUser, `{"follow_list_visibility":0}`)
		call(ac)
		if rc.Response.StatusCode() != http.StatusInternalServerError {
			t.Errorf("%s repo error: status=%d, want 500", name, rc.Response.StatusCode())
		}
	}
}
