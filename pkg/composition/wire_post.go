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
	// 分享数缓存（#233）：详情 / 列表 share_count 读 Redis post:sharecnt，miss 回源 PG 并回种。
	svc.SetShareCountCache(postinfra.NewPostShareCountCache())
	// AI 审核（#117）：始终注入，是否生效由 moderation.enabled + api_key 决定（未启用时发帖直接发布）。
	svc.SetModeration(postinfra.NewJevModerator(conf.Config.Moderation),
		postinfra.NewPostModerationLogRepository(deps.DB.Get()), conf.Config.Moderation)
	// 举报复核（#153）：与审核开关无关，始终可用。
	svc.SetReportRepository(postinfra.NewPostReportRepository(deps.DB.Get()))
	return svc
}

// registerPost 装配 post 领域。
func registerPost(root routing.RouterGroup, svc postapp.PostService, authCheck, optionalCheck routing.HandlerFunc) {
	posthttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
