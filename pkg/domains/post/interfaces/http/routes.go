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
//	GET  /circle/moderation/posts   圈内待审帖子列表（需登录；admin+，service 校验；#118）
//	POST /circle/moderation/review  人工审核通过 / 驳回（需登录；admin+，service 校验；#118）
//	POST /post/report               举报帖子（需登录；#153）
//	GET  /circle/moderation/reports          圈内被举报帖子列表（需登录；admin+，service 校验；#153）
//	POST /circle/moderation/reports/resolve  举报复核 保留 / 下架（需登录；admin+，service 校验；#153）
//	GET  /circle/moderation/summary          圈内待审 / 被举报帖子数（需登录；admin+，service 校验；#156）
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

	// 需登录：发帖/我的帖子/举报。
	priv := rg.Group("/post", authCheck)
	{
		priv.POST("/create", h.CreatePost)
		priv.GET("/my", h.GetMyPosts)
		priv.POST("/report", h.ReportPost)
	}

	// 需登录：圈主 / 管理员人工审核（帖子数据归 post 域，路径与 /circle/agent/* 一样挂在圈子管理下）。
	mod := rg.Group("/circle/moderation", authCheck)
	{
		mod.GET("/posts", h.ListReviewQueue)
		mod.POST("/review", h.ReviewPost)
		mod.GET("/reports", h.ListReportedPosts)
		mod.POST("/reports/resolve", h.ResolveReport)
		mod.GET("/summary", h.GetModerationSummary)
	}
}
