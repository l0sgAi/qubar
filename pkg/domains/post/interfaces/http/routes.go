package http

import (
	"interestBar/pkg/domains/post/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 post 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	POST /post/create        发帖（需登录）
//	GET  /post/list          搜索帖子列表（访客可读）
//	GET  /post/my            查看自己发的帖（需登录）
//	GET  /post/user/:user_id 查看任意用户发的帖（访客可读，仅已发布）
//	GET  /post/detail/:id    获取帖子详情（访客可读；登录时回填 is_liked/is_collected）
//
// 访客可读端点（list/user/detail）挂 optionalCheck；写操作与个人列表挂 authCheck。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.PostService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// 访客可读：搜索/指定用户帖子/详情。
	pub := rg.Group("/post", optionalCheck)
	{
		pub.GET("/list", h.GetPosts)
		pub.GET("/user/:user_id", h.GetUserPosts)
		pub.GET("/detail/:id", h.GetPostDetail)
	}

	// 需登录：发帖/我的帖子。
	priv := rg.Group("/post", authCheck)
	{
		priv.POST("/create", h.CreatePost)
		priv.GET("/my", h.GetMyPosts)
	}
}
