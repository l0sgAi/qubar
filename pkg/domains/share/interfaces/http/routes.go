package http

import (
	"interestBar/pkg/domains/share/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 share 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	POST /post/share   分享帖子（需登录；一人一帖只记一次，不可撤销，没有取消接口；#232）
//
// 分享出去的链接落地复用访客可读的 GET /post/detail/:id，不需要新接口。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.ShareService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	g := rg.Group("/post", authCheck)
	{
		g.POST("/share", h.SharePost)
	}
}
