package http

import (
	"interestBar/pkg/domains/auth/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 auth 领域的路由挂到给定的路由组根上。
//
// 路由清单（与旧 routers.go 中 /auth 组一致）：
//
//	公开（无需登录）：
//	  GET  /auth/google/login       OAuth 登录跳转
//	  GET  /auth/google/callback    OAuth 回调（302 透传 code 到前端）
//	  POST /auth/google/callback    前端用一次性 code 换 token
//	  GET  /auth/github/login
//	  GET  /auth/github/callback
//	  POST /auth/github/callback
//	  GET  /auth/azure/login
//	  GET  /auth/azure/callback
//	  POST /auth/azure/callback
//	  POST /auth/register/send-code 发送验证码
//	  POST /auth/register/verify    校验验证码
//	  POST /auth/register/complete  完成注册
//	  POST /auth/login              邮箱密码登录
//	  POST /auth/password/send-code 发送找回密码验证码
//	  POST /auth/password/verify    校验找回密码验证码
//	  POST /auth/password/reset     重置密码
//
//	需要登录：
//	  POST /auth/logout             注销当前 token
//
// 注意：旧 routers.go 把 /auth/me (GetCurrentUser) 也放在这里，
// 但它实际返回的是 user 领域的数据。为了职责清晰，/auth/me 由 user 领域
// 注册到 /user/get（行为等价）。如果前端依赖 /auth/me，可在 user 领域
// 再加一条同名路由——但当前选择"语义归位"：会话查询统一走 /user/*。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.AuthService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	auth := rg.Group("/auth")
	{
		// 公开路由 —— OAuth（每个 provider 显式注册，与旧 routers.go 一致）
		for _, p := range []string{"google", "github", "azure"} {
			auth.GET("/"+p+"/login", h.OAuthLogin(p))
			auth.GET("/"+p+"/callback", h.OAuthCallback(p))
			// code 换 token（前端 success 页调用；与 GET 同路径不同方法）
			auth.POST("/"+p+"/callback", h.OAuthExchange(p))
		}
		auth.POST("/register/send-code", h.SendCode)
		auth.POST("/register/verify", h.VerifyCode)
		auth.POST("/register/complete", h.CompleteRegistration)
		auth.POST("/login", h.Login)
		auth.POST("/password/send-code", h.SendPasswordResetCode)
		auth.POST("/password/verify", h.VerifyPasswordResetCode)
		auth.POST("/password/reset", h.ResetPassword)

		// 需要登录：注销。用子组挂中间件。
		logoutGrp := auth.Group("/", authCheck)
		logoutGrp.POST("/logout", h.Logout)
	}
}
