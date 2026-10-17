// Package composition 的 wire_auth.go：auth 领域装配（同域依赖直构 + user 会话存储桥接 + 路由注册）。
package composition

import (
	authapp "interestBar/pkg/domains/auth/application"
	authinfra "interestBar/pkg/domains/auth/infrastructure"
	authhttp "interestBar/pkg/domains/auth/interfaces/http"
	"interestBar/pkg/shared/routing"
)

// registerAuth 装配 auth 领域。
func registerAuth(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	session := authinfra.NewSaTokenSession()
	userStore := NewUserSessionStore(deps.DB.Get())
	verify := authinfra.NewVerificationStore()
	email := authinfra.NewEmailSender()
	oauthReg := authinfra.NewOAuthProviderRegistry()
	pwdReset := authinfra.NewPasswordResetStore()
	svc := authapp.NewAuthService(session, userStore, verify, email, oauthReg, pwdReset)
	authhttp.RegisterRoutes(root, svc, authCheck)
}
