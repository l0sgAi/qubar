package http

import (
	"interestBar/pkg/domains/user/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 user 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET /user/get          获取当前会话用户（需登录）
//	PUT /user/update       修改资料（需登录）
//	GET /user/search       搜索用户（访客可读，handler 不读 userID）
//	GET /user/detail/:id   获取指定用户详情（访客可读，handler 不读 userID）
//
// 访客可读端点（search/detail）挂 optionalCheck；个人资料读写（get/update）挂 authCheck。
//
// 注意：旧 routers.go 中 /auth/logout 和 /auth/me 这两条路由虽然调用的
// 是 UserController.Logout / GetCurrentUser，但语义上属于"会话/鉴权"
// 而非"用户资料"，因此随 auth 领域一起迁移（见 pkg/domains/auth）。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.UserService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// 访客可读：搜索/详情（公开主页场景）。handler 不读登录 userID，对匿名自然降级。
	pub := rg.Group("/user", optionalCheck)
	{
		pub.GET("/search", h.SearchUsers)
		pub.GET("/detail/:id", h.GetUserDetail)
	}

	// 需登录：当前用户资料读写。
	priv := rg.Group("/user", authCheck)
	{
		priv.GET("/get", h.GetCurrentUser)
		priv.PUT("/update", h.UpdateProfile)
	}
}
