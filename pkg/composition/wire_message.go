// Package composition 的 wire_message.go：message（私信）领域装配（Service 构造 + 路由注册）。
package composition

import (
	messageapp "interestBar/pkg/domains/message/application"
	messageinfra "interestBar/pkg/domains/message/infrastructure"
	messagehttp "interestBar/pkg/domains/message/interfaces/http"
	noticeapp "interestBar/pkg/domains/notice/application"
	userapp "interestBar/pkg/domains/user/application"
	"interestBar/pkg/shared/routing"
)

// newMessageService 构造 MessageService。
//
// user 是只读跨域依赖（接收方状态校验、会话列表对方简要信息），构造时直接注入。
func newMessageService(deps *Deps, userSvc userapp.UserService, userFacade userapp.UserFacade) messageapp.MessageService {
	svc := messageapp.NewMessageService(
		messageinfra.NewRepository(deps.DB.Get()),
		&messageRecipientChecker{delegate: userSvc},
		&messageUserBriefReader{delegate: userFacade},
	)
	// 私信未读总数 Redis 缓存（#241）。
	svc.SetUnreadCache(messageinfra.NewUnreadCache())
	return svc
}

// wireMessageStream 把私信未读接入 SSE 推流 hub（#241，设计 docs/design/sse-notification-design.md §八）：
// hub 的 dm-unread 通道读 MessageService.GetUnreadTotal（与 GET /message/unread-count 同源），
// message 写路径提交后经 messageStreamNotifier 标脏 / 推新消息摘要。hub 为 nil 时不接入。
func wireMessageStream(svc messageapp.MessageService, hub noticeapp.StreamHub) {
	if hub == nil {
		return
	}
	hub.SetChannelCountReader(noticeapp.ChannelDMUnread, svc.GetUnreadTotal)
	svc.SetUnreadNotifier(&messageStreamNotifier{hub: hub})
}

// registerMessage 装配 message 领域。
func registerMessage(root routing.RouterGroup, svc messageapp.MessageService, authCheck routing.HandlerFunc) {
	messagehttp.RegisterRoutes(root, svc, authCheck)
}
