// Package composition 的 wire_discover.go：discover 领域装配（Service 构造 + 路由注册）。
package composition

import (
	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	discoverapp "interestBar/pkg/domains/discover/application"
	discoverinfra "interestBar/pkg/domains/discover/infrastructure"
	discoverhttp "interestBar/pkg/domains/discover/interfaces/http"
	postapp "interestBar/pkg/domains/post/application"
	"interestBar/pkg/shared/routing"
)

// newDiscoverService 构造 DiscoverService。
//
// pool 为 discover 同域 infra（直构，走全局 Redis 客户端）；
// hydrator/checker/circle/seed/joinedCircles 为跨域桥接器（包 post/circle service + redispkg）。
func newDiscoverService(postSvc postapp.PostService, circleRepo circledomain.CircleRepository, circleSvc circleapp.CircleService) discoverapp.DiscoverService {
	return discoverapp.NewDiscoverService(
		discoverinfra.NewDiscoverPoolStore(),             // DiscoverPoolStore
		&discoverPostHydrator{delegate: postSvc},         // PostHydrator（复用 trending/recommend 同款桥接）
		&postInteractionChecker{delegate: postSvc},       // InteractionChecker（缓存 + DB 回源，同 recommend/trending）
		&discoverCircleLookup{repo: circleRepo},          // CircleLookup（复用 trending 同款桥接）
		&discoverSeedReader{},                            // SeedReader（直接调 redispkg，同 recommend infra）
		&discoverJoinedCircleLookup{delegate: circleSvc}, // JoinedCircleLookup（复用 recommend 同款桥接）
	)
}

// registerDiscover 装配 discover 领域。
//
// discover 允许匿名访问（新用户落地页场景）：登录→反气泡个性化，匿名→纯随机退化。
// 故 authCheck 用 OptionalLogin（有 token 解析、无/坏 token 放行），而非全局 RequireLogin。
func registerDiscover(root routing.RouterGroup, svc discoverapp.DiscoverService, _ /*authCheck*/ routing.HandlerFunc) {
	discoverhttp.RegisterRoutes(root, svc, OptionalLoginFn)
}
