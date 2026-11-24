// Package composition 的 wire_share.go：share 领域装配（Service 构造 + 路由注册，#232）。
package composition

import (
	shareapp "interestBar/pkg/domains/share/application"
	shareinfra "interestBar/pkg/domains/share/infrastructure"
	sharehttp "interestBar/pkg/domains/share/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// newShareService 构造 ShareService。post 跨领域依赖通过 setter 注入（见 RegisterDomainRoutes）。
func newShareService(deps *Deps) shareapp.ShareService {
	repo := shareinfra.NewPostShareRepository(deps.DB.Get())
	cache := shareinfra.NewShareCountCache()
	publisher := shareinfra.NewShareEventPublisher()
	return shareapp.NewShareService(repo, cache, publisher)
}

// registerShare 装配 share 领域。
func registerShare(root routing.RouterGroup, svc shareapp.ShareService, authCheck routing.HandlerFunc) {
	sharehttp.RegisterRoutes(root, svc, authCheck)
}
