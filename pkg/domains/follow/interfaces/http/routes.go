package http

import (
	"interestBar/pkg/domains/follow/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 follow 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET    /follow/stats/:user_id       关注数 / 粉丝数 / 关系状态 / 列表是否可见（访客可读）
//	GET    /follow/following/:user_id   关注列表（访客可读；对方设为私密时仅本人）
//	GET    /follow/followers/:user_id   粉丝列表（访客可读；对方设为私密时仅本人）
//	GET    /follow/mutual               我的互关列表（需登录）
//	POST   /follow/relations            批量查询我与一组用户的关系（需登录）
//	POST   /follow/:user_id             关注（需登录，幂等）
//	DELETE /follow/:user_id             取关（需登录，幂等）
//
// POST /follow/relations 与 POST /follow/:user_id 同层：静态段优先于参数段，且 "relations" 不是合法 UUID，不会互相影响。
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
		pub.GET("/following/:user_id", h.ListFollowing)
		pub.GET("/followers/:user_id", h.ListFollowers)
	}

	// 需登录。
	priv := rg.Group("/follow", authCheck)
	{
		priv.GET("/mutual", h.ListMutual)
		priv.POST("/relations", h.GetRelations)
		priv.POST("/:user_id", h.Follow)
		priv.DELETE("/:user_id", h.Unfollow)
	}
}
