// Package composition 的 wire_like.go：like 领域装配（Service 构造 + 路由注册）。
package composition

import (
	likeapp "interestBar/pkg/domains/like/application"
	likeinfra "interestBar/pkg/domains/like/infrastructure"
	likehttp "interestBar/pkg/domains/like/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// newLikeService 构造 LikeService。
//
// post/comment 跨领域依赖通过 setter 注入（见 RegisterDomainRoutes）。
func newLikeService(deps *Deps) likeapp.LikeService {
	postCache := likeinfra.NewPostLikeCache()
	commentCache := likeinfra.NewCommentLikeCache()
	publisher := likeinfra.NewLikeEventPublisher()
	return likeapp.NewLikeService(postCache, commentCache, publisher)
}

// registerLike 装配 like 领域。
func registerLike(root routing.RouterGroup, svc likeapp.LikeService, authCheck routing.HandlerFunc) {
	likehttp.RegisterRoutes(root, svc, authCheck)
}
