// Package composition 的 wire_category.go：category 领域装配（同域依赖直构 + 路由注册）。
package composition

import (
	categoryapp "interestBar/pkg/domains/category/application"
	categoryinfra "interestBar/pkg/domains/category/infrastructure"
	categoryhttp "interestBar/pkg/domains/category/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// registerCategory 装配 category 领域。
func registerCategory(root routing.RouterGroup, deps *Deps, authCheck, optionalCheck routing.HandlerFunc) {
	repo := categoryinfra.NewCategoryRepository(deps.DB.Get())
	cache := categoryinfra.NewCategoryCache()
	svc := categoryapp.NewCategoryService(repo, cache)
	categoryhttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
