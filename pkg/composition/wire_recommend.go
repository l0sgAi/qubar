// Package composition 的 wire_recommend.go：recommend 领域装配（Service 构造 + 路由注册）。
package composition

import (
	circleapp "interestBar/pkg/domains/circle/application"
	postapp "interestBar/pkg/domains/post/application"
	recommendapp "interestBar/pkg/domains/recommend/application"
	recommendinfra "interestBar/pkg/domains/recommend/infrastructure"
	recommendhttp "interestBar/pkg/domains/recommend/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// newRecommendService 构造 RecommendService。
//
// searcher/seed/feed 为 recommend 同域 infra（直构，走全局 ES/Redis 客户端）；
// circle/postMeta/hydrator/checker 为跨域桥接器（包 post/circle service）。
func newRecommendService(postSvc postapp.PostService, circleSvc circleapp.CircleService) recommendapp.RecommendService {
	return recommendapp.NewRecommendService(
		recommendinfra.NewHomeFeedSearcher(),        // HomeFeedSearcher
		&recommendCircleLookup{delegate: circleSvc}, // CircleLookup
		&recommendPostMetaReader{delegate: postSvc}, // PostMetaReader
		recommendinfra.NewSeedReader(),              // SeedReader
		&recommendPostHydrator{delegate: postSvc},   // PostHydrator
		&postInteractionChecker{delegate: postSvc},  // InteractionChecker（缓存 + DB 回源）
		recommendinfra.NewFeedCache(),               // FeedCache
		recommendinfra.NewInterestCircleCache(),     // InterestCircleCache
	)
}

// registerRecommend 装配 recommend 领域。
func registerRecommend(root routing.RouterGroup, svc recommendapp.RecommendService, authCheck, optionalCheck routing.HandlerFunc) {
	recommendhttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
