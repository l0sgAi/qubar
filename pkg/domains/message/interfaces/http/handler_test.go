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
	gotClient  string
	gotCursor  string
	gotSize    int
}

func (f *fakeSvc) Send(ctx context.Context, sender, receiver uuid.UUID, content string) (*application.SendVO, error) {
	return f.SendWithClientMsgID(ctx, sender, receiver, content, "")
}

func (f *fakeSvc) SendWithClientMsgID(_ context.Context, sender, receiver uuid.UUID, content, clientMsgID string) (*application.SendVO, error) {
	f.calls++
	f.gotUser, f.gotTarget, f.gotContent, f.gotClient = sender, receiver, content, clientMsgID
	if f.err != nil {
		return nil, f.err
	}
	remaining := 4
	return &application.SendVO{
		MessageVO:      application.MessageVO{ID: "m1", ConversationID: testConv, Content: content},
		RemainingCount: &remaining,
	}, nil
}

func (f *fakeSvc) SetSendRateLimiter(domain.SendRateLimiter, func() domain.SendRateLimits) {}

func (f *fakeSvc) ListConversations(_ context.Context, user uuid.UUID, cursor string, size int) (*application.ConversationListVO, error) {
	f.calls++
	f.gotUser, f.gotCursor, f.gotSize = user, cursor, size
	return &application.ConversationListVO{Items: []application.ConversationItemVO{}}, f.err
}

func (f *fakeSvc) GetConversationWithUser(_ context.Context, user, peer uuid.UUID) (*application.PeerConversationVO, error) {
	f.calls++
	f.gotUser, f.gotTarget = user, peer
	if f.err != nil {
		return nil, f.err
	}
	return &application.PeerConversationVO{}, nil
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

func (f *fakeSvc) GetUnreadTotal(_ context.Context, user uuid.UUID) (int64, error) {
	f.calls++
	f.gotUser = user
	if f.err != nil {
		return 0, f.err
	}
	return 7, nil
}

func (f *fakeSvc) SetUnreadCache(domain.UnreadCache) {}

func (f *fakeSvc) SetUnreadNotifier(application.UnreadNotifier) {}

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
	rateMin := fmt.Errorf("%w: at most 20 messages per minute", domain.ErrSendRateLimited)
	newConv := fmt.Errorf("%w: at most 20 new conversations per day", domain.ErrNewConversationLimited)
	badClientID := fmt.Errorf("%w: at most 64 characters", domain.ErrInvalidClientMsgID)
	for _, tc := range []struct {
		err  error
		want int
		body string
	}{
		{nil, http.StatusOK, `"conversation_id":"` + testConv},
		{nil, http.StatusOK, `"remaining_count":4`},
		{domain.ErrCannotMessageSelf, http.StatusBadRequest, "Cannot send message to yourself"},
		{domain.ErrEmptyContent, http.StatusBadRequest, "empty"},
		{tooLong, http.StatusBadRequest, "at most 2000 characters"},
		{domain.ErrRecipientNotFound, http.StatusNotFound, "No such user"},
		{domain.ErrRecipientUnavailable, http.StatusForbidden, "cannot receive private messages"},
		{domain.ErrSendLimitReached, http.StatusForbidden, `"message":"dm limit reached"`},
		{domain.ErrSenderUnavailable, http.StatusForbidden, "Your account cannot send private messages"},
		{rateMin, http.StatusTooManyRequests, `"message":"message rate limit exceeded: at most 20 messages per minute"`},
		{newConv, http.StatusTooManyRequests, `"message":"new conversation limit exceeded: at most 20 new conversations per day"`},
		{badClientID, http.StatusBadRequest, "invalid client_msg_id: at most 64 characters"},
		{errors.New("boom"), http.StatusInternalServerError, ""},
	} {
		svc = &fakeSvc{err: tc.err}
		ac, rc = newCtx(testUser, "", "", okBody)
		NewHandler(svc).Send(ac)
		assertStatus(t, rc, tc.want, tc.body)
		if svc.gotUser.String() != testUser || svc.gotTarget.String() != testPeer || svc.gotContent != "hi" || svc.gotClient != "" {
			t.Errorf("service got user=%s receiver=%s content=%q client=%q", svc.gotUser, svc.gotTarget, svc.gotContent, svc.gotClient)
		}
	}
}

// client_msg_id（#248）原样透传给 service（校验在 application 层），响应结构不变。
func TestSend_ClientMsgIDPassthrough(t *testing.T) {
	svc := &fakeSvc{}
	body := fmt.Sprintf(`{"receiver_id":%q,"content":"hi","client_msg_id":"c-1_A"}`, testPeer)
	ac, rc := newCtx(testUser, "", "", body)
	NewHandler(svc).Send(ac)
	assertStatus(t, rc, http.StatusOK, `"remaining_count":4`)
	if svc.gotClient != "c-1_A" {
		t.Errorf("client_msg_id = %q, want c-1_A", svc.gotClient)
	}
	if strings.Contains(string(rc.Response.Body()), "client_msg_id") {
		t.Errorf("response should not echo client_msg_id: %s", rc.Response.Body())
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

func TestGetConversationWithUser_ParamsAndErrors(t *testing.T) {
	// withUser 构造 GET /message/conversations/with/:user_id 的上下文。
	withUser := func(loginID, peer string) (appctx.AppContext, *app.RequestContext) {
		ac, rc := newCtx(loginID, "", "", "")
		rc.Params = append(rc.Params, param.Param{Key: "user_id", Value: peer})
		return ac, rc
	}

	svc := &fakeSvc{}
	ac, rc := withUser("", testPeer)
	NewHandler(svc).GetConversationWithUser(ac)
	assertStatus(t, rc, http.StatusUnauthorized, "")
	for _, bad := range []string{"bad", uuid.Nil.String(), ""} {
		ac, rc = withUser(testUser, bad)
		NewHandler(svc).GetConversationWithUser(ac)
		assertStatus(t, rc, http.StatusBadRequest, "Invalid user id")
	}
	if svc.calls != 0 {
		t.Fatalf("service called %d times for invalid requests", svc.calls)
	}

	for _, tc := range []struct {
		err  error
		want int
		body string
	}{
		// 没有会话：成功，conversation 为 null。
		{nil, http.StatusOK, `"conversation":null`},
		{domain.ErrConversationWithSelf, http.StatusBadRequest, "Cannot have a conversation with yourself"},
		{errors.New("boom"), http.StatusInternalServerError, ""},
	} {
		svc = &fakeSvc{err: tc.err}
		ac, rc = withUser(testUser, testPeer)
		NewHandler(svc).GetConversationWithUser(ac)
		assertStatus(t, rc, tc.want, tc.body)
		if svc.gotUser.String() != testUser || svc.gotTarget.String() != testPeer {
			t.Errorf("service got user=%s peer=%s", svc.gotUser, svc.gotTarget)
		}
	}
}

func TestGetUnreadTotal(t *testing.T) {
	svc := &fakeSvc{}
	ac, rc := newCtx("", "", "", "")
	NewHandler(svc).GetUnreadTotal(ac)
	assertStatus(t, rc, http.StatusUnauthorized, "")
	if svc.calls != 0 {
		t.Fatal("service should not be called without login")
	}

	ac, rc = newCtx(testUser, "", "", "")
	NewHandler(svc).GetUnreadTotal(ac)
	assertStatus(t, rc, http.StatusOK, `"unread_count":7`)
	if svc.gotUser.String() != testUser {
		t.Errorf("service got user=%s", svc.gotUser)
	}

	svc = &fakeSvc{err: errors.New("boom")}
	ac, rc = newCtx(testUser, "", "", "")
	NewHandler(svc).GetUnreadTotal(ac)
	assertStatus(t, rc, http.StatusInternalServerError, "")
}
