// Package composition 的 wire_message.go：message（私信）领域装配（Service 构造 + 路由注册）。
package composition

import (
	followapp "interestBar/pkg/domains/follow/application"
	messageapp "interestBar/pkg/domains/message/application"
	messageinfra "interestBar/pkg/domains/message/infrastructure"
	messagehttp "interestBar/pkg/domains/message/interfaces/http"
	userapp "interestBar/pkg/domains/user/application"
	"interestBar/pkg/shared/routing"
)

// newMessageService 构造 MessageService。
//
// user 是只读跨域依赖（接收方状态校验、会话列表对方简要信息），follow 是只读跨域依赖（#239 未关注发送限制），
// 都在构造时直接注入。
func newMessageService(
	deps *Deps, userSvc userapp.UserService, userFacade userapp.UserFacade, followSvc followapp.FollowService,
) messageapp.MessageService {
	return messageapp.NewMessageService(
		messageinfra.NewRepository(deps.DB.Get()),
		&messageRecipientChecker{delegate: userSvc},
		&messageUserBriefReader{delegate: userFacade},
		&messageFollowChecker{delegate: followSvc},
	)
}

// registerMessage 装配 message 领域。
func registerMessage(root routing.RouterGroup, svc messageapp.MessageService, authCheck routing.HandlerFunc) {
	messagehttp.RegisterRoutes(root, svc, authCheck)
}
