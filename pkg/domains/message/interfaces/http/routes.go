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
//	GET    /message/conversations/:conversation_id/messages 消息历史（新 → 旧，keyset 游标分页；仅会话成员）
//	POST   /message/conversations/:conversation_id/read     标记会话已读
//	DELETE /message/conversations/:conversation_id          删除会话（只对自己隐藏）
func RegisterRoutes(rg routing.RouterGroup, svc application.MessageService, authCheck routing.HandlerFunc) {
	h := NewHandler(svc)

	g := rg.Group("/message", authCheck)
	{
		g.POST("/send", h.Send)
		g.GET("/conversations", h.ListConversations)
		g.GET("/conversations/:conversation_id/messages", h.ListMessages)
		g.POST("/conversations/:conversation_id/read", h.MarkRead)
		g.DELETE("/conversations/:conversation_id", h.DeleteConversation)
	}
}
