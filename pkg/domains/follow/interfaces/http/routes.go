package http

import (
	"interestBar/pkg/domains/follow/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 follow 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET    /follow/stats/:user_id   关注数 / 粉丝数 / 关系状态（访客可读）
//	POST   /follow/:user_id         关注（需登录，幂等）
//	DELETE /follow/:user_id         取关（需登录，幂等）
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.FollowService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// 访客可读：登录时回填 relation，匿名时 relation=0。
	pub := rg.Group("/follow", optionalCheck)
	{
		pub.GET("/stats/:user_id", h.GetStats)
	}

	// 需登录。
	priv := rg.Group("/follow", authCheck)
	{
		priv.POST("/:user_id", h.Follow)
		priv.DELETE("/:user_id", h.Unfollow)
	}
}
