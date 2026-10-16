// Package composition 的 wire_collect.go：collect 领域装配（Service 构造 + 路由注册）。
package composition

import (
	collectapp "interestBar/pkg/domains/collect/application"
	collectinfra "interestBar/pkg/domains/collect/infrastructure"
	collecthttp "interestBar/pkg/domains/collect/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// newCollectService 构造 CollectService。
//
// post 跨领域依赖（查询端口 + 组装端口）通过 setter 注入（见 RegisterDomainRoutes）。
func newCollectService(deps *Deps) collectapp.CollectService {
	cache := collectinfra.NewPostCollectCache()
	repo := collectinfra.NewPostCollectRepository(deps.DB.Get())
	publisher := collectinfra.NewCollectEventPublisher()
	return collectapp.NewCollectService(cache, repo, publisher)
}

// registerCollect 装配 collect 领域。
func registerCollect(root routing.RouterGroup, svc collectapp.CollectService, authCheck routing.HandlerFunc) {
	collecthttp.RegisterRoutes(root, svc, authCheck)
}
