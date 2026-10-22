package middleware

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type clientGoneKey struct{}

// DetachClientCancel 把客户端断开信号从请求 ctx 的取消语义里摘出来（#127）。
//
// server 开启 WithSenseClientDisconnection 后，netpoll 在对端断开（hup）时取消连接 ctx，
// 这是 handler 运行期间唯一能及时感知断开的途径（netpoll 的 CloseCallback 要等 handler 返回才触发）。
// 但该开关是全局的：直接放开会让所有接口在客户端断开时中途取消 DB / Redis / Redpanda 操作
// （如点赞 Lua 已执行、发事件被取消导致计数漂移）。因此本中间件挂在最前：
// 下游一律拿 context.WithoutCancel（与未开启时行为一致），原 ctx.Done() 存进 value，
// 仅需要的 handler（SSE）经 ClientGone 显式订阅。
func DetachClientCancel() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Next(context.WithValue(context.WithoutCancel(ctx), clientGoneKey{}, ctx.Done()))
	}
}

// ClientGone 返回客户端断开信号：连接被对端关闭时关闭。
// 未挂 DetachClientCancel 或 server 未开启断开感知时返回 nil（select 中永不触发）。
func ClientGone(ctx context.Context) <-chan struct{} {
	ch, _ := ctx.Value(clientGoneKey{}).(<-chan struct{})
	return ch
}
