package application

import "errors"

// 领域层可识别错误。handler 层据此映射 HTTP 响应。
var (
	errInvalidCredentials            = errors.New("invalid credentials")
	errAccountDisabled               = errors.New("account has been disabled")
	errAccountNotFound               = errors.New("account not found")
	errInvalidEmail                  = errors.New("invalid email format")
	errEmailAlreadyExists            = errors.New("email already registered")
	errRateLimitExceeded             = errors.New("rate limit exceeded")
	errOTPExpired                    = errors.New("verification code expired")
	errInvalidOTP                    = errors.New("invalid verification code")
	errUsernameTooLong               = errors.New("username must be at most 50 characters")
	errPasswordTooShort              = errors.New("password must be at least 6 characters")
	errUnknownOAuthProvider          = errors.New("unknown OAuth provider")
	errFrontendRedirectNotConfigured = errors.New("frontend redirect URL not configured")
	errOAuthProviderUnavailable      = errors.New("oauth provider temporarily unavailable")
	errOAuthInvalidGrant             = errors.New("oauth authorization code invalid or expired")
)

// 错误判断函数（供 handler 层使用）。
func IsInvalidCredentialsErr(err error) bool   { return errors.Is(err, errInvalidCredentials) }
func IsAccountDisabledErr(err error) bool      { return errors.Is(err, errAccountDisabled) }
func IsAccountNotFoundErr(err error) bool      { return errors.Is(err, errAccountNotFound) }
func IsInvalidEmailErr(err error) bool         { return errors.Is(err, errInvalidEmail) }
func IsEmailAlreadyExistsErr(err error) bool   { return errors.Is(err, errEmailAlreadyExists) }
func IsRateLimitExceededErr(err error) bool    { return errors.Is(err, errRateLimitExceeded) }
func IsOTPExpiredErr(err error) bool           { return errors.Is(err, errOTPExpired) }
func IsInvalidOTPErr(err error) bool           { return errors.Is(err, errInvalidOTP) }
func IsUsernameTooLongErr(err error) bool      { return errors.Is(err, errUsernameTooLong) }
func IsPasswordTooShortErr(err error) bool     { return errors.Is(err, errPasswordTooShort) }
func IsUnknownOAuthProviderErr(err error) bool { return errors.Is(err, errUnknownOAuthProvider) }
func IsFrontendRedirectNotConfiguredErr(err error) bool {
	return errors.Is(err, errFrontendRedirectNotConfigured)
}

// IsOAuthProviderUnavailableErr 判断是否为「OAuth 提供方暂不可用」错误
// （换 token / 拉用户信息时的网络超时或连接失败）。handler 层据此返回 503。
func IsOAuthProviderUnavailableErr(err error) bool {
	return errors.Is(err, errOAuthProviderUnavailable)
}

// IsOAuthInvalidGrantErr 判断是否为「授权码无效或已过期」错误
// （code 已被使用/过期/redirect_uri 不匹配）。handler 层据此返回 400。
func IsOAuthInvalidGrantErr(err error) bool {
	return errors.Is(err, errOAuthInvalidGrant)
}
