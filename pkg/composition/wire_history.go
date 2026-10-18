// Package composition 的 wire_history.go：history 领域装配（Service 构造 + 路由注册）。
package composition

import (
	historyapp "interestBar/pkg/domains/history/application"
	historyinfra "interestBar/pkg/domains/history/infrastructure"
	historyhttp "interestBar/pkg/domains/history/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// newHistoryService 构造 HistoryService。
//
// post 跨领域依赖（ES 帖子组装端口）通过 setter 注入（见 RegisterDomainRoutes）。
func newHistoryService(deps *Deps) historyapp.HistoryService {
	cache := historyinfra.NewPostHistoryCache()
	repo := historyinfra.NewPostHistoryRepository(deps.DB.Get())
	publisher := historyinfra.NewHistoryEventPublisher()
	return historyapp.NewHistoryService(cache, repo, publisher)
}

// registerHistory 装配 history 领域。
func registerHistory(root routing.RouterGroup, svc historyapp.HistoryService, authCheck routing.HandlerFunc) {
	historyhttp.RegisterRoutes(root, svc, authCheck)
}
