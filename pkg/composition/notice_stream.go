// notice_stream.go GET /notice/stream 的裸 hertz SSE handler
// （设计 docs/design/sse-notification-design.md §四#4，决策 D1 踢最旧 / D2a 心跳复检 / D3a 裸 handler）。
//
// 为何不走 AppContext 路由抽象：SSE 需 hijack response writer 做长连接推送，
// AppContext 无流式写能力，为其加 SSEWriter() 会污染框架无关抽象（单端点不值得）。
// composition 层 import hertz 有先例（hertzadapter/group.go）。
// 业务状态全在 notice application 的 StreamHub，本 handler 只做 auth + IO 泵。
package composition

import (
	"context"
	"encoding/json"
	"time"

	"interestBar/pkg/composition/middleware"
	"interestBar/pkg/conf"
	noticeapp "interestBar/pkg/domains/notice/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx/hertzadapter"
	"interestBar/pkg/shared/httputil"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
	"github.com/google/uuid"
	"github.com/sa-tokens/sa-token-go/stputil"
)

// ServeNoticeStream 构造 GET /notice/stream handler。
//
// 流程：auth（header→query 兜底）→ 注册连接（超限踢该用户最旧连接）→ 连接建立即推全量（首事件带 retry）
// → 泵循环（conn chan 推 unread-count / 25s 心跳 + token 复检 → auth-expired /
// 被踢 → conn-evicted / 客户端断开即退出释放名额）。
// 重连带 Last-Event-ID 一律忽略：连接建立即推当前全量，不补历史。
func ServeNoticeStream(hub noticeapp.StreamHub) app.HandlerFunc {
	cfg := conf.Config.NoticeStream
	heartbeat := time.Duration(cfg.HeartbeatSec) * time.Second
	if heartbeat <= 0 {
		heartbeat = 25 * time.Second
	}
	retry := time.Duration(cfg.RetryMs) * time.Millisecond
	if retry <= 0 {
		retry = 5 * time.Second
	}

	return func(ctx context.Context, c *app.RequestContext) {
		ac := hertzadapter.New(ctx, c)

		// 鉴权：header 优先，query 兜底（浏览器 EventSource 不能自定义 header）。
		// 不走 RequireLogin 包装：失败需直接 401 JSON，不建立 SSE 连接。
		tokenName := conf.Config.SaToken.TokenName
		token := string(c.GetHeader(tokenName))
		if token == "" {
			token = string(c.Query(tokenName))
		}
		if token == "" {
			httputil.Unauthorized(ac, "Token not found")
			return
		}
		if !stputil.IsLogin(token) {
			httputil.Unauthorized(ac, httputil.MsgInvalidToken)
			return
		}
		loginID, err := stputil.GetLoginID(token)
		if err != nil {
			httputil.Unauthorized(ac, httputil.MsgInvalidToken)
			return
		}
		userID, err := uuid.Parse(loginID)
		if err != nil {
			httputil.Unauthorized(ac, httputil.MsgInvalidToken)
			return
		}

		// D1 踢最旧（#127）：超限由 hub 挤掉该用户最旧连接，新连接总能建立；仅 hub 已停止（关停中）失败。
		conn, ok := hub.Add(userID)
		if !ok {
			httputil.ServiceUnavailable(ac)
			return
		}
		defer hub.Remove(userID, conn.ID)
		// 客户端断开信号（netpoll hup，见 middleware.DetachClientCancel）：断开即退出、释放名额，
		// 不再等下一次写失败（最长一个心跳周期）。
		gone := middleware.ClientGone(ctx)

		// 首事件数据在 hijack 前备好，失败仍可返回 JSON 错误。
		first, err := hub.Snapshot(ctx, userID)
		if err != nil {
			logger.Log.Error("Failed to snapshot unread count for notice stream: " + err.Error())
			httputil.InternalError(ac)
			return
		}

		// Nginx 缓冲双保险（hertz sse.NewWriter 不设此头）。
		c.Response.Header.Set("X-Accel-Buffering", "no")
		w := sse.NewWriter(c)

		// 连接建立即推全量；retry 重连建议仅首事件携带。
		if err := writeUnreadEvent(w, first, retry); err != nil {
			return
		}
		lastSent := first.Count

		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-gone:
				return
			case <-conn.Evicted:
				// 被同用户新连接挤掉：通知客户端主动 close（不再自动重连，避免互踢），随后断连。
				_ = w.WriteEvent("", "conn-evicted", nil)
				return
			case ev := <-conn.Events:
				// 与 Snapshot/首事件竞态的重复值兜底去重（hub 内 lastSent 已挡大部分）。
				if ev.Count == lastSent {
					continue
				}
				if err := writeUnreadEvent(w, ev, 0); err != nil {
					return // 客户端断开/假死：defer Remove 释放连接
				}
				lastSent = ev.Count
			case <-ticker.C:
				// 心跳 + token 复检（D2a：覆盖登出/过期/Kickout，≤heartbeat 延迟）。
				if !stputil.IsLogin(token) {
					_ = w.WriteEvent("", "auth-expired", nil)
					return
				}
				if err := w.WriteComment("ping"); err != nil {
					return
				}
			}
		}
	}
}

// writeUnreadEvent 写一条 unread-count 事件。retry>0 时携带重连建议（仅首事件）。
func writeUnreadEvent(w *sse.Writer, ev noticeapp.StreamEvent, retry time.Duration) error {
	data, _ := json.Marshal(struct {
		UnreadCount int64 `json:"unread_count"`
	}{UnreadCount: ev.Count})
	e := &sse.Event{Type: "unread-count"}
	e.SetID(ev.ID)
	e.SetData(data)
	if retry > 0 {
		e.SetRetry(retry)
	}
	return w.Write(e)
}
