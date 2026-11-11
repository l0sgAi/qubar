package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"interestBar/pkg/domains/message/application"
	"interestBar/pkg/domains/message/domain"
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
	testUser = "018f0000-0000-7000-8000-000000000001"
	testPeer = "018f0000-0000-7000-8000-000000000002"
	testConv = "018f0000-0000-7000-8000-000000000003"
)

// fakeSvc 记录入参并返回预置结果。
type fakeSvc struct {
	err        error
	calls      int
	gotUser    uuid.UUID
	gotTarget  uuid.UUID
	gotContent string
	gotCursor  string
	gotSize    int
}

func (f *fakeSvc) Send(_ context.Context, sender, receiver uuid.UUID, content string) (*application.MessageVO, error) {
	f.calls++
	f.gotUser, f.gotTarget, f.gotContent = sender, receiver, content
	if f.err != nil {
		return nil, f.err
	}
	return &application.MessageVO{ID: "m1", ConversationID: testConv, Content: content}, nil
}

func (f *fakeSvc) ListConversations(_ context.Context, user uuid.UUID, cursor string, size int) (*application.ConversationListVO, error) {
	f.calls++
	f.gotUser, f.gotCursor, f.gotSize = user, cursor, size
	return &application.ConversationListVO{Items: []application.ConversationItemVO{}}, f.err
}

func (f *fakeSvc) MarkRead(_ context.Context, user, conv uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotTarget = user, conv
	return f.err
}

func (f *fakeSvc) DeleteConversation(_ context.Context, user, conv uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotTarget = user, conv
	return f.err
}

func (f *fakeSvc) ListMessages(_ context.Context, user, conv uuid.UUID, cursor string, size int) (*application.MessageListVO, error) {
	f.calls++
	f.gotUser, f.gotTarget, f.gotCursor, f.gotSize = user, conv, cursor, size
	return &application.MessageListVO{Items: []application.MessageVO{}}, f.err
}

// newCtx 构造 AppContext：loginID 为空表示未登录；conv 非空时设路径参数 :conversation_id；
// query 拼到 URI；body 非空时作为 JSON 请求体。
func newCtx(loginID, conv, query, body string) (appctx.AppContext, *app.RequestContext) {
	rc := &app.RequestContext{}
	if conv != "" {
		rc.Params = append(rc.Params, param.Param{Key: "conversation_id", Value: conv})
	}
	rc.Request.SetRequestURI("/message/x?" + query)
	if body != "" {
		rc.Request.Header.SetMethod(http.MethodPost)
		rc.Request.Header.SetContentTypeBytes([]byte("application/json"))
		rc.Request.SetBody([]byte(body))
	}
	ac := hertzadapter.New(context.Background(), rc)
	if loginID != "" {
		ac.SetLoginID(loginID)
	}
	return ac, rc
}

func assertStatus(t *testing.T, rc *app.RequestContext, want int, bodyPart string) {
	t.Helper()
	if rc.Response.StatusCode() != want || !strings.Contains(string(rc.Response.Body()), bodyPart) {
		t.Errorf("status=%d body=%s, want %d containing %q", rc.Response.StatusCode(), rc.Response.Body(), want, bodyPart)
	}
}

func TestSend_AuthBodyAndErrors(t *testing.T) {
	okBody := fmt.Sprintf(`{"receiver_id":%q,"content":"hi"}`, testPeer)

	svc := &fakeSvc{}
	ac, rc := newCtx("", "", "", okBody)
	NewHandler(svc).Send(ac)
	assertStatus(t, rc, http.StatusUnauthorized, "")

	for _, bad := range []string{`{"receiver_id":"nope","content":"hi"}`, `{"content":"hi"}`, `not json`} {
		ac, rc = newCtx(testUser, "", "", bad)
		NewHandler(svc).Send(ac)
		assertStatus(t, rc, http.StatusBadRequest, "")
	}
	if svc.calls != 0 {
		t.Fatalf("service called %d times for invalid requests", svc.calls)
	}

	tooLong := fmt.Errorf("%w: at most 2000 characters", domain.ErrContentTooLong)
	for _, tc := range []struct {
		err  error
		want int
		body string
	}{
		{nil, http.StatusOK, `"conversation_id":"` + testConv},
		{domain.ErrCannotMessageSelf, http.StatusBadRequest, "Cannot send message to yourself"},
		{domain.ErrEmptyContent, http.StatusBadRequest, "empty"},
		{tooLong, http.StatusBadRequest, "at most 2000 characters"},
		{domain.ErrRecipientNotFound, http.StatusNotFound, "No such user"},
		{domain.ErrRecipientUnavailable, http.StatusForbidden, "cannot receive private messages"},
		{errors.New("boom"), http.StatusInternalServerError, ""},
	} {
		svc = &fakeSvc{err: tc.err}
		ac, rc = newCtx(testUser, "", "", okBody)
		NewHandler(svc).Send(ac)
		assertStatus(t, rc, tc.want, tc.body)
		if svc.gotUser.String() != testUser || svc.gotTarget.String() != testPeer || svc.gotContent != "hi" {
			t.Errorf("service got user=%s receiver=%s content=%q", svc.gotUser, svc.gotTarget, svc.gotContent)
		}
	}
}

func TestConversationEndpoints_ParamsAndErrors(t *testing.T) {
	for name, call := range map[string]func(*Handler, appctx.AppContext){
		"messages": (*Handler).ListMessages,
		"read":     (*Handler).MarkRead,
		"delete":   (*Handler).DeleteConversation,
	} {
		t.Run(name, func(t *testing.T) {
			svc := &fakeSvc{}
			ac, rc := newCtx("", testConv, "", "")
			call(NewHandler(svc), ac)
			assertStatus(t, rc, http.StatusUnauthorized, "")

			for _, bad := range []string{"bad", uuid.Nil.String()} {
				ac, rc = newCtx(testUser, bad, "", "")
				call(NewHandler(svc), ac)
				assertStatus(t, rc, http.StatusBadRequest, "Invalid conversation id")
			}
			if svc.calls != 0 {
				t.Fatalf("service called %d times for invalid requests", svc.calls)
			}

			for _, tc := range []struct {
				err  error
				want int
				body string
			}{
				{nil, http.StatusOK, ""},
				{domain.ErrConversationNotFound, http.StatusNotFound, "No such conversation"},
				{domain.ErrInvalidCursor, http.StatusBadRequest, "Invalid cursor"},
				{errors.New("boom"), http.StatusInternalServerError, ""},
			} {
				svc = &fakeSvc{err: tc.err}
				ac, rc = newCtx(testUser, testConv, "cursor=abc&size=5", "")
				call(NewHandler(svc), ac)
				assertStatus(t, rc, tc.want, tc.body)
				if svc.gotUser.String() != testUser || svc.gotTarget.String() != testConv {
					t.Errorf("service got user=%s conv=%s", svc.gotUser, svc.gotTarget)
				}
			}
		})
	}
}

func TestListEndpoints_PassCursorAndSize(t *testing.T) {
	svc := &fakeSvc{}
	ac, rc := newCtx(testUser, "", "cursor=abc&size=5", "")
	NewHandler(svc).ListConversations(ac)
	assertStatus(t, rc, http.StatusOK, `"items":[]`)
	if svc.gotCursor != "abc" || svc.gotSize != 5 || svc.gotUser.String() != testUser {
		t.Errorf("conversations got cursor=%q size=%d user=%s", svc.gotCursor, svc.gotSize, svc.gotUser)
	}

	svc = &fakeSvc{}
	ac, rc = newCtx(testUser, testConv, "cursor=xyz&size=9", "")
	NewHandler(svc).ListMessages(ac)
	assertStatus(t, rc, http.StatusOK, `"items":[]`)
	if svc.gotCursor != "xyz" || svc.gotSize != 9 {
		t.Errorf("messages got cursor=%q size=%d", svc.gotCursor, svc.gotSize)
	}

	ac, rc = newCtx("", "", "", "")
	NewHandler(&fakeSvc{}).ListConversations(ac)
	assertStatus(t, rc, http.StatusUnauthorized, "")
}
