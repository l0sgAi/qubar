package http

import (
	"context"
	"net/http"
	"testing"

	"interestBar/pkg/composition/hertzadapter"
	"interestBar/pkg/domains/message/application"
	"interestBar/pkg/shared/appctx"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/google/uuid"
)

// routeSvc 记录被调用的方法与目标 ID，用来确认请求落到了哪个 handler。
type routeSvc struct {
	fakeSvc
	method string
}

func (r *routeSvc) GetConversationWithUser(ctx context.Context, user, peer uuid.UUID) (*application.PeerConversationVO, error) {
	r.method = "with"
	return r.fakeSvc.GetConversationWithUser(ctx, user, peer)
}

func (r *routeSvc) ListMessages(ctx context.Context, user, conv uuid.UUID, cursor string, size int) (*application.MessageListVO, error) {
	r.method = "messages"
	return r.fakeSvc.ListMessages(ctx, user, conv, cursor, size)
}

// 静态段 /conversations/with/:user_id 与参数段 /conversations/:conversation_id/messages 同级，在真实 hertz 路由上互不抢占。
func TestRoutes_WithUserDoesNotClashWithConversationID(t *testing.T) {
	h := server.Default(server.WithHostPorts("127.0.0.1:0"))
	login := func(c appctx.AppContext) { c.SetLoginID(testUser) }
	svc := &routeSvc{}
	RegisterRoutes(hertzadapter.ForEngine(h), svc, login)

	for _, tc := range []struct {
		path       string
		wantMethod string
		wantTarget string
	}{
		{"/message/conversations/with/" + testPeer, "with", testPeer},
		{"/message/conversations/" + testConv + "/messages", "messages", testConv},
	} {
		svc.method, svc.gotTarget = "", uuid.Nil
		w := ut.PerformRequest(h.Engine, http.MethodGet, tc.path, nil)
		if w.Code != http.StatusOK || svc.method != tc.wantMethod || svc.gotTarget.String() != tc.wantTarget {
			t.Errorf("GET %s: status=%d method=%q target=%s, want 200 / %q / %s",
				tc.path, w.Code, svc.method, svc.gotTarget, tc.wantMethod, tc.wantTarget)
		}
	}
}
