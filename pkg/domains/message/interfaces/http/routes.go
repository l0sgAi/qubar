package http

import (
	"interestBar/pkg/domains/message/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 message（私信）领域的路由挂到给定的路由组根上。全部需要登录。
//
// 路由清单：
//
//	POST   /message/send                                    发送文本私信（首次发送自动建会话）
//	GET    /message/conversations                           我的会话列表（keyset 游标分页）
//	GET    /message/conversations/with/:user_id             我与某个用户的单聊会话（没有则 conversation 为 null，不创建）
//	GET    /message/conversations/:conversation_id/messages 消息历史（新 → 旧，keyset 游标分页；仅会话成员）
//	POST   /message/conversations/:conversation_id/read     标记会话已读
//	DELETE /message/conversations/:conversation_id          删除会话（只对自己隐藏）
//	GET    /message/unread-count                            我的私信未读总数（#241；实时推送走 /notice/stream 的 dm-unread）
func RegisterRoutes(rg routing.RouterGroup, svc application.MessageService, authCheck routing.HandlerFunc) {
	h := NewHandler(svc)

	g := rg.Group("/message", authCheck)
	{
		g.POST("/send", h.Send)
		g.GET("/conversations", h.ListConversations)
		// 静态段 with 与下面的 :conversation_id 同级：hertz 路由静态段优先，会话 ID 是 UUID，不会与 with 冲突。
		g.GET("/conversations/with/:user_id", h.GetConversationWithUser)
		g.GET("/conversations/:conversation_id/messages", h.ListMessages)
		g.POST("/conversations/:conversation_id/read", h.MarkRead)
		g.DELETE("/conversations/:conversation_id", h.DeleteConversation)
		g.GET("/unread-count", h.GetUnreadTotal)
	}
}
