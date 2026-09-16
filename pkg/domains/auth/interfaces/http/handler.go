// Package http 提供 auth 领域的 HTTP 入站适配器（handler + 路由注册）。
package http

import (
	"net/http"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/auth/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
)

// Handler 处理 auth 相关的 HTTP 请求。
type Handler struct {
	svc application.AuthService
}

// NewHandler 构造一个 auth Handler。
func NewHandler(svc application.AuthService) *Handler {
	return &Handler{svc: svc}
}

// loginReq 邮箱密码登录请求。
type loginReq struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Device   string `json:"device"`
}

// Login POST /auth/login
func (h *Handler) Login(c appctx.AppContext) {
	var req loginReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, httputil.MsgMissingParameter)
		return
	}

	result, err := h.svc.Login(c, application.LoginInput{
		Email:    req.Email,
		Password: req.Password,
		Device:   req.Device,
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}

	// 与旧 controller 一致：直接用 c.JSON 返回带 message 的结构
	// （而不是 httputil.Success），保持前端解析兼容。
	c.JSON(http.StatusOK, map[string]interface{}{
		"code":    httputil.CodeSuccess,
		"message": "Login successful",
		"data":    result,
	})
}

// sendCodeReq 发送验证码请求。
type sendCodeReq struct {
	Email string `json:"email" binding:"required"`
	Lang  string `json:"lang"`
}

// SendCode POST /auth/register/send-code
func (h *Handler) SendCode(c appctx.AppContext) {
	var req sendCodeReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, httputil.MsgMissingParameter)
		return
	}

	if err := h.svc.SendCode(c, application.SendCodeInput{Email: req.Email, Lang: req.Lang}); err != nil {
		writeAuthError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// verifyCodeReq 校验验证码请求。
type verifyCodeReq struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

// VerifyCode POST /auth/register/verify
func (h *Handler) VerifyCode(c appctx.AppContext) {
	var req verifyCodeReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, httputil.MsgMissingParameter)
		return
	}

	if err := h.svc.VerifyCode(c, application.VerifyCodeInput{Email: req.Email, Code: req.Code}); err != nil {
		writeAuthError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// completeReq 完成注册请求。
type completeReq struct {
	Email    string `json:"email" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Device   string `json:"device"`
}

// CompleteRegistration POST /auth/register/complete
func (h *Handler) CompleteRegistration(c appctx.AppContext) {
	var req completeReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, httputil.MsgMissingParameter)
		return
	}

	result, err := h.svc.Register(c, application.RegisterInput{
		Email:    req.Email,
		Username: req.Username,
		Password: req.Password,
		Device:   req.Device,
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"code":    httputil.CodeSuccess,
		"message": "Registration successful",
		"data":    result,
	})
}

// sendPwdCodeReq 发送找回密码验证码请求。
type sendPwdCodeReq struct {
	Email string `json:"email" binding:"required"`
	Lang  string `json:"lang"`
}

// SendPasswordResetCode POST /auth/password/send-code
func (h *Handler) SendPasswordResetCode(c appctx.AppContext) {
	var req sendPwdCodeReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, httputil.MsgMissingParameter)
		return
	}

	if err := h.svc.SendPasswordResetCode(c, application.SendCodeInput{Email: req.Email, Lang: req.Lang}); err != nil {
		writeAuthError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// verifyPwdCodeReq 校验找回密码验证码请求。
type verifyPwdCodeReq struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

// VerifyPasswordResetCode POST /auth/password/verify
func (h *Handler) VerifyPasswordResetCode(c appctx.AppContext) {
	var req verifyPwdCodeReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, httputil.MsgMissingParameter)
		return
	}

	if err := h.svc.VerifyPasswordResetCode(c, application.VerifyCodeInput{Email: req.Email, Code: req.Code}); err != nil {
		writeAuthError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// resetPasswordReq 重置密码请求。
type resetPasswordReq struct {
	Email       string `json:"email" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ResetPassword POST /auth/password/reset
func (h *Handler) ResetPassword(c appctx.AppContext) {
	var req resetPasswordReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, httputil.MsgMissingParameter)
		return
	}

	if err := h.svc.ResetPassword(c, application.ResetPasswordInput{
		Email:       req.Email,
		NewPassword: req.NewPassword,
	}); err != nil {
		writeAuthError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Password reset successful", nil)
}

// OAuthLogin GET /auth/<provider>/login
//
// 生成跳转到 OAuth provider 的 URL 并 307 重定向。
// provider 通过闭包绑定（与旧 oauthCtrl.Login("google") 一致），
// 不从路径参数读取——这样路由可以用静态路径，避免 gin 的参数路由冲突。
func (h *Handler) OAuthLogin(provider string) func(c appctx.AppContext) {
	return func(c appctx.AppContext) {
		device := c.Query("device")
		out, err := h.svc.OAuthLoginURL(c, provider, device)
		if err != nil {
			writeAuthError(c, err)
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, out.RedirectURL)
	}
}

// OAuthCallback GET /auth/<provider>/callback
//
// 处理 OAuth provider 回调：不消耗 code，302 透传到前端 success 页
// （URL 携带一次性 code + device），由前端 POST 换 token。
// token 不出现在 URL 中（安全：避免泄漏到浏览器历史/服务端日志）。
func (h *Handler) OAuthCallback(provider string) func(c appctx.AppContext) {
	return func(c appctx.AppContext) {
		code := c.Query("code")
		state := c.Query("state")
		device := application.ParseOAuthState(state)

		if code == "" {
			httputil.BadRequest(c, "Code not found")
			return
		}

		redirectURL, err := h.svc.OAuthCallback(c, provider, code, device)
		if err != nil {
			writeAuthError(c, err)
			return
		}
		c.Redirect(http.StatusFound, redirectURL)
	}
}

// oauthExchangeReq POST /auth/<provider>/callback 请求体。
type oauthExchangeReq struct {
	Code   string `json:"code"`
	Device string `json:"device"` // 可选，缺省 web
}

// OAuthExchange POST /auth/<provider>/callback
//
// 前端 success 页拿 URL 中的一次性 code 调本接口换 token。
// 成功返回 {token, expire, email, user}；code 无效/过期返回 400。
func (h *Handler) OAuthExchange(provider string) func(c appctx.AppContext) {
	return func(c appctx.AppContext) {
		var req oauthExchangeReq
		if err := c.BindJSON(&req); err != nil || req.Code == "" {
			httputil.BadRequest(c, httputil.MsgMissingParameter)
			return
		}

		result, err := h.svc.OAuthExchange(c, provider, req.Code, req.Device)
		if err != nil {
			writeAuthError(c, err)
			return
		}
		httputil.Success(c, result)
	}
}

// Logout POST /auth/logout —— 按 token 注销。
func (h *Handler) Logout(c appctx.AppContext) {
	tokenName := conf.Config.SaToken.TokenName
	token := c.Header(tokenName)
	if token == "" {
		httputil.Unauthorized(c, "Token not found")
		return
	}

	if err := h.svc.LogoutByToken(c, token); err != nil {
		httputil.InternalError(c, "Failed to logout")
		return
	}
	httputil.SuccessWithMessage(c, "Logout successful", nil)
}

// writeAuthError 把 service 层的可识别错误映射到 HTTP 响应。
func writeAuthError(c appctx.AppContext, err error) {
	switch {
	case application.IsInvalidCredentialsErr(err):
		httputil.Unauthorized(c, httputil.MsgInvalidCredentials)
	case application.IsAccountDisabledErr(err):
		httputil.Forbidden(c, httputil.MsgAccountDisabled)
	case application.IsAccountNotFoundErr(err):
		httputil.NotFound(c, "Account not found")
	case application.IsInvalidEmailErr(err):
		httputil.BadRequest(c, httputil.MsgInvalidEmail)
	case application.IsEmailAlreadyExistsErr(err):
		httputil.Conflict(c, httputil.MsgEmailAlreadyExists)
	case application.IsRateLimitExceededErr(err):
		httputil.TooManyRequests(c, httputil.MsgRateLimitExceeded)
	case application.IsOTPExpiredErr(err):
		// 与旧 controller.VerifyCode 一致：用 BadRequest code
		httputil.ErrorWithMessage(c, httputil.CodeBadRequest, httputil.MsgOTPExpired)
	case application.IsInvalidOTPErr(err):
		httputil.BadRequest(c, httputil.MsgInvalidOTP)
	case application.IsUsernameTooLongErr(err):
		httputil.BadRequest(c, "Username must be at most 50 characters")
	case application.IsPasswordTooShortErr(err):
		httputil.BadRequest(c, "Password must be at least 6 characters")
	case application.IsUnknownOAuthProviderErr(err):
		httputil.BadRequest(c, "Unknown OAuth provider")
	case application.IsFrontendRedirectNotConfiguredErr(err):
		httputil.InternalError(c, "Frontend redirect URL not configured")
	case application.IsOAuthProviderUnavailableErr(err):
		httputil.ServiceUnavailable(c, "Third-party login is temporarily unavailable, please try again later")
	case application.IsOAuthInvalidGrantErr(err):
		httputil.BadRequest(c, "Authorization code invalid or expired, please try again")
	default:
		logger.Log.Error("auth service error: " + err.Error())
		httputil.InternalError(c, "Internal error")
	}
}
