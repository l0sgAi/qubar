package http

import (
	"interestBar/pkg/domains/comment/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 comment 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	POST /comment/create       发评论/回复（需登录）
//	GET  /comment/list         获取顶层评论列表（访客可读；登录时回填 is_liked）
//	GET  /comment/replies      获取楼层内回复列表（访客可读；登录时回填 is_liked）
//	GET  /comment/locate       评论定位（通知点击直达；访客可读）
//	GET  /comment/detail/:id   获取单条评论详情（访客可读；登录时回填 is_liked）
//
// 读端点 handler 已用 requireUserIDAllowAnon，service 已 best-effort（liked 为空 map 时
// IsLiked 自然为 false）。访客可读端点挂 optionalCheck；写操作挂 authCheck。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CommentService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// 访客可读：评论列表/回复/定位/详情。
	pub := rg.Group("/comment", optionalCheck)
	{
		pub.GET("/list", h.GetComments)
		pub.GET("/replies", h.GetReplies)
		pub.GET("/locate", h.LocateComment)
		pub.GET("/detail/:id", h.GetCommentDetail)
	}

	// 需登录：发评论。
	priv := rg.Group("/comment", authCheck)
	{
		priv.POST("/create", h.CreateComment)
	}
}
