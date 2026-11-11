// Package composition 的 wire_message.go：message（私信）领域装配（Service 构造 + 路由注册）。
package composition

import (
	"interestBar/pkg/conf"
	followapp "interestBar/pkg/domains/follow/application"
	messageapp "interestBar/pkg/domains/message/application"
	messagedomain "interestBar/pkg/domains/message/domain"
	messageinfra "interestBar/pkg/domains/message/infrastructure"
	messagehttp "interestBar/pkg/domains/message/interfaces/http"
	userapp "interestBar/pkg/domains/user/application"
	"interestBar/pkg/shared/routing"
)

// newMessageService 构造 MessageService。
//
// user 是只读跨域依赖（接收方状态校验、会话列表对方简要信息），follow 是只读跨域依赖（#239 未关注发送限制），
// 都在构造时直接注入。发送频率限制（#242）由 setter 注入。
func newMessageService(
	deps *Deps, userSvc userapp.UserService, userFacade userapp.UserFacade, followSvc followapp.FollowService,
) messageapp.MessageService {
	svc := messageapp.NewMessageService(
		messageinfra.NewRepository(deps.DB.Get()),
		&messageRecipientChecker{delegate: userSvc},
		&messageUserBriefReader{delegate: userFacade},
		&messageFollowChecker{delegate: followSvc},
	)
	// 发送频率限制（#242）：Redis 计数，上限每次发送时读配置 message.send_rate（<=0 兜底默认值）。
	svc.SetSendRateLimiter(messageinfra.NewSendRateLimiter(), messageSendRateLimits)
	return svc
}

// messageSendRateLimits 读取 message.send_rate（热更新即时生效），各项 <=0 时兜底为默认值。
func messageSendRateLimits() messagedomain.SendRateLimits {
	r := conf.Config.Message.SendRate
	return messagedomain.SendRateLimits{
		PerMinute:              r.PerMinuteOrDefault(),
		PerDay:                 r.PerDayOrDefault(),
		NewConversationsPerDay: r.NewConversationsPerDayOrDefault(),
	}
}

// registerMessage 装配 message 领域。
func registerMessage(root routing.RouterGroup, svc messageapp.MessageService, authCheck routing.HandlerFunc) {
	messagehttp.RegisterRoutes(root, svc, authCheck)
}
