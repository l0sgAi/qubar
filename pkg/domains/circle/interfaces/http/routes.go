package http

import (
	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 circle 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	POST /circle/create       创建圈子（需登录）
//	GET  /circle/list         搜索圈子列表（访客可读）
//	GET  /circle/active       近期活跃圈子列表（访客可读）
//	GET  /circle/random       随机圈子列表（访客可读，侧栏推荐）
//	GET  /circle/detail/:id   获取圈子详情（访客可读；登录时回填 is_joined）
//	GET  /circle/my           我加入的圈子列表（需登录）
//	GET  /circle/user         任意用户加入圈子列表（访客可读）
//	POST /circle/join         加入圈子（需登录）
//	POST /circle/leave        退出圈子（需登录）
//	GET  /circle/posts        圈内帖子列表（访客可读）
//	GET  /circle/members      管理端成员列表（需登录；admin+，service 校验）
//	GET  /circle/manage/list  我可管理的圈子列表（需登录；查询即权限过滤 owner/admin）
//	POST /circle/manage/role     任免管理员（需登录；owner）
//	POST /circle/manage/transfer 转让圈主（需登录；owner）
//	POST /circle/manage/mute     禁言（需登录；admin+）
//	POST /circle/manage/unmute   解除禁言（需登录；admin+）
//	POST /circle/manage/ban      拉黑/踢出（需登录；admin+）
//	POST /circle/manage/unban    解除拉黑（需登录；admin+）
//	POST /circle/manage/remove   移出成员（需登录；admin+，不拉黑）
//	PUT  /circle/update          编辑圈子资料（需登录；分字段权限 owner/admin）
//
// 访客可读端点（list/active/random/detail/user/posts）挂 optionalCheck；写操作与个人列表挂 authCheck。
// 管理端点全部挂 authCheck，角色/层级权限矩阵在 service 层校验（见 application/manage.go）。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CircleService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// 访客可读：搜索/活跃/随机/详情/指定用户圈子/圈内帖。
	pub := rg.Group("/circle", optionalCheck)
	{
		pub.GET("/list", h.GetCircles)
		pub.GET("/active", h.GetActiveCircles)
		pub.GET("/random", h.GetRandomCircles)
		pub.GET("/detail/:id", h.GetCircleDetail)
		pub.GET("/user", h.GetUserCircles)
		pub.GET("/posts", h.GetCirclePosts)
	}

	// 需登录：建圈/我的圈子/加圈/退圈 + 圈子管理（成员列表/角色/禁言/拉黑/移出/资料编辑）。
	priv := rg.Group("/circle", authCheck)
	{
		priv.POST("/create", h.CreateCircle)
		priv.GET("/my", h.GetMyCircles)
		priv.POST("/join", h.JoinCircle)
		priv.POST("/leave", h.LeaveCircle)

		priv.GET("/members", h.GetCircleMembers)
		priv.GET("/manage/list", h.ListManagedCircles)
		priv.POST("/manage/role", h.ManageRole)
		priv.POST("/manage/transfer", h.ManageTransfer)
		priv.POST("/manage/mute", h.ManageMute)
		priv.POST("/manage/unmute", h.ManageUnmute)
		priv.POST("/manage/ban", h.ManageBan)
		priv.POST("/manage/unban", h.ManageUnban)
		priv.POST("/manage/remove", h.ManageRemove)
		priv.PUT("/update", h.UpdateCircleProfile)
	}
}
