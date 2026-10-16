// Package composition 的 wire_post.go：post 领域装配（Service 构造 + 路由注册）。
package composition

import (
	postapp "interestBar/pkg/domains/post/application"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	posthttp "interestBar/pkg/domains/post/interfaces/http"
	"interestBar/pkg/shared/routing"
)

func newPostService(deps *Deps) postapp.PostService {
	repo := postinfra.NewPostRepository(deps.DB.Get())
	statsCache := postinfra.NewPostStatsCache()
	likeCache := postinfra.NewPostLikeCache()
	collectCache := postinfra.NewPostCollectCache()
	searcher := postinfra.NewPostSearcher()
	publisher := postinfra.NewPostEventPublisher()
	return postapp.NewPostService(repo, statsCache, likeCache, collectCache, searcher, publisher)
}

// registerPost 装配 post 领域。
func registerPost(root routing.RouterGroup, svc postapp.PostService, authCheck, optionalCheck routing.HandlerFunc) {
	posthttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
