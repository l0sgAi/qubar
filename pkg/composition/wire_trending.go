// Package composition 的 wire_trending.go：trending 领域装配（Service 构造 + 路由注册）。
package composition

import (
	circledomain "interestBar/pkg/domains/circle/domain"
	postapp "interestBar/pkg/domains/post/application"
	trendingapp "interestBar/pkg/domains/trending/application"
	trendinginfra "interestBar/pkg/domains/trending/infrastructure"
	trendinghttp "interestBar/pkg/domains/trending/interfaces/http"
	userapp "interestBar/pkg/domains/user/application"
	"interestBar/pkg/shared/routing"
)

// newTrendingService 构造 TrendingService。
//
// boardStore 为 trending 同域 infra（直构，走全局 Redis 客户端）；
// hydrator/checker/circle/user 为跨域桥接器（包 post/circle/user service）。
func newTrendingService(postSvc postapp.PostService, circleRepo circledomain.CircleRepository, userFacade userapp.UserFacade) trendingapp.TrendingService {
	return trendingapp.NewTrendingService(
		trendinginfra.NewBoardStore(),              // BoardStore
		&trendingPostHydrator{delegate: postSvc},   // PostHydrator
		&postInteractionChecker{delegate: postSvc}, // InteractionChecker（缓存 + DB 回源）
		&trendingCircleLookup{repo: circleRepo},    // CircleLookup
		&trendingUserLookup{delegate: userFacade},  // UserLookup
	)
}

// registerTrending 装配 trending 领域。
func registerTrending(root routing.RouterGroup, svc trendingapp.TrendingService, authCheck, optionalCheck routing.HandlerFunc) {
	trendinghttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
