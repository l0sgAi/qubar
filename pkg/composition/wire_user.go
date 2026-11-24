// Package composition 的 wire_user.go：user 领域装配（Service 构造 + 路由注册）。
package composition

import (
	userapp "interestBar/pkg/domains/user/application"
	userinfra "interestBar/pkg/domains/user/infrastructure"
	userhttp "interestBar/pkg/domains/user/interfaces/http"
	"interestBar/pkg/shared/routing"
)

func newUserService(deps *Deps) userapp.UserService {
	repo := userinfra.NewUserRepository(deps.DB.Get())
	cache := userinfra.NewUserCache()
	searcher := userinfra.NewUserSearcher()
	return userapp.NewUserService(repo, cache, searcher)
}

// registerUser 装配 user 领域。
func registerUser(root routing.RouterGroup, svc userapp.UserService, authCheck, optionalCheck routing.HandlerFunc) {
	userhttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}

// newUserSettingService 构造用户设置服务（个人中心开关类设置；follow 域经桥接读取列表隐私开关）。
func newUserSettingService(deps *Deps) userapp.SettingService {
	return userapp.NewSettingService(userinfra.NewSettingRepository(deps.DB.Get()))
}

// registerUserSetting 注册用户设置路由。
func registerUserSetting(root routing.RouterGroup, svc userapp.SettingService, authCheck routing.HandlerFunc) {
	userhttp.RegisterSettingRoutes(root, svc, authCheck)
}
