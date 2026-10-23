// Package composition 的 wire_post.go：post 领域装配（Service 构造 + 路由注册）。
package composition

import (
	"interestBar/pkg/conf"
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
	svc := postapp.NewPostService(repo, statsCache, likeCache, collectCache, searcher, publisher)
	// AI 审核（#117）：始终注入，是否生效由 moderation.enabled + api_key 决定（未启用时发帖直接发布）。
	svc.SetModeration(postinfra.NewJevModerator(conf.Config.Moderation),
		postinfra.NewPostModerationLogRepository(deps.DB.Get()), conf.Config.Moderation)
	return svc
}

// registerPost 装配 post 领域。
func registerPost(root routing.RouterGroup, svc postapp.PostService, authCheck, optionalCheck routing.HandlerFunc) {
	posthttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
