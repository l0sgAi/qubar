// Package composition 的 wire_storage.go：storage 领域装配（同域依赖直构 + 路由注册）。
package composition

import (
	storageapp "interestBar/pkg/domains/storage/application"
	storageinfra "interestBar/pkg/domains/storage/infrastructure"
	storagehttp "interestBar/pkg/domains/storage/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// registerStorage 装配 storage 领域。
func registerStorage(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	storage := storageinfra.NewObjectStorage()
	svc := storageapp.NewStorageService(storage)
	storagehttp.RegisterRoutes(root, svc, authCheck)
}
