// Package composition 的 wire_follow.go：follow 领域装配（Service 构造 + 路由注册）。
package composition

import (
	"interestBar/pkg/conf"
	followapp "interestBar/pkg/domains/follow/application"
	followinfra "interestBar/pkg/domains/follow/infrastructure"
	followhttp "interestBar/pkg/domains/follow/interfaces/http"
	userapp "interestBar/pkg/domains/user/application"
	"interestBar/pkg/shared/routing"
)

// newFollowService 构造 FollowService。
//
// user 是只读跨域依赖（校验目标用户存在且正常），直接构造注入；
// 关注上限每次从 conf.Config 读取，配置热更新后立即生效。
func newFollowService(deps *Deps, userFacade userapp.UserFacade) followapp.FollowService {
	db := deps.DB.Get()
	return followapp.NewFollowService(
		followinfra.NewRelationRepository(db),
		followinfra.NewStatRepository(db),
		&followUserChecker{delegate: userFacade},
		func() int { return conf.Config.Follow.MaxFollowingOrDefault() },
		followinfra.NewFolloweeIDRepository(db),
	)
}

// registerFollow 装配 follow 领域。
func registerFollow(root routing.RouterGroup, svc followapp.FollowService, authCheck, optionalCheck routing.HandlerFunc) {
	followhttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
