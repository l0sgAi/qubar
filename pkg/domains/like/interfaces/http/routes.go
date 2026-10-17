package http

import (
	"interestBar/pkg/domains/like/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 like 领域的路由挂到给定的路由组根上。
//
// 路由清单（与旧 routers.go 中 /like 组一致）：
//
//	POST /like/toggle   点赞/取消点赞（需登录）
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.LikeService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	l := rg.Group("/like", authCheck)
	{
		l.POST("/toggle", h.ToggleLike)
	}
}
