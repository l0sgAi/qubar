// Package composition 的 wire_circle.go：circle 领域装配（Service 构造 + 路由注册）。
package composition

import (
	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	circleinfra "interestBar/pkg/domains/circle/infrastructure"
	circlehttp "interestBar/pkg/domains/circle/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// newCircleService 构造 CircleService 并返回其依赖的 repo（供桥接器使用）。
func newCircleService(deps *Deps) (circleapp.CircleService, circledomain.CircleRepository, circledomain.MemberRepository) {
	circleRepo := circleinfra.NewCircleRepository(deps.DB.Get())
	memberRepo := circleinfra.NewMemberRepository(deps.DB.Get())
	baseCache := circleinfra.NewCircleBaseCache()
	statsCache := circleinfra.NewCircleStatsCache()
	joinedCache := circleinfra.NewJoinedCirclesCache()
	searcher := circleinfra.NewCircleSearcher()
	publisher := circleinfra.NewCircleEventPublisher()
	privateCache := circleinfra.NewPrivateCircleCache()

	svc := circleapp.NewCircleService(
		circleRepo, memberRepo, baseCache, statsCache, joinedCache, searcher, publisher, privateCache,
	)
	return svc, circleRepo, memberRepo
}

// registerCircle 装配 circle 领域。
func registerCircle(root routing.RouterGroup, svc circleapp.CircleService, authCheck, optionalCheck routing.HandlerFunc) {
	circlehttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}
