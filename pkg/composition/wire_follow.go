// Package composition 的 wire_follow.go：follow 领域装配（Service 构造 + 路由注册）。
package composition

import (
	"interestBar/pkg/conf"
	followapp "interestBar/pkg/domains/follow/application"
	followdomain "interestBar/pkg/domains/follow/domain"
	followinfra "interestBar/pkg/domains/follow/infrastructure"
	followhttp "interestBar/pkg/domains/follow/interfaces/http"
	userapp "interestBar/pkg/domains/user/application"
	"interestBar/pkg/shared/routing"
)

// newFollowService 构造 FollowService。
//
// user 是只读跨域依赖（校验目标用户存在且正常、列表项的简要信息、列表隐私开关），直接构造注入；
// 关注上限每次从 conf.Config 读取，配置热更新后立即生效；
// 事件发布器（被关注通知，#219）是同域依赖，用 Redpanda 实现。
func newFollowService(deps *Deps, userFacade userapp.UserFacade, userSettings userapp.SettingService) followapp.FollowService {
	return newFollowServiceWithEvents(deps, userFacade, userSettings, followinfra.NewFollowEventPublisher())
}

// newFollowServiceWithEvents 与 newFollowService 相同，只是事件发布器由调用方给定
// （集成测试里不关心通知的用例传空实现，见 follow_integration_test.go）。
func newFollowServiceWithEvents(
	deps *Deps, userFacade userapp.UserFacade, userSettings userapp.SettingService, events followdomain.FollowEventPublisher,
) followapp.FollowService {
	db := deps.DB.Get()
	return followapp.NewFollowService(
		followinfra.NewRelationRepository(db),
		followinfra.NewStatRepository(db),
		&followUserChecker{delegate: userFacade},
		func() int { return conf.Config.Follow.MaxFollowingOrDefault() },
		followinfra.NewFolloweeIDRepository(db),
		followapp.ListDeps{
			Repo:       followinfra.NewListRepository(db),
			Users:      &followUserBriefReader{delegate: userFacade},
			Visibility: &followListVisibilityReader{delegate: userSettings},
		},
		events,
	)
}

// registerFollow 装配 follow 领域。
func registerFollow(root routing.RouterGroup, svc followapp.FollowService, authCheck, optionalCheck routing.HandlerFunc) {
	followhttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
