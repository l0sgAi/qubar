// Package infrastructure 提供 auth 领域基础设施层实现。
//
// 包括：
//   - saTokenSessionImpl：封装 stputil 调用（登录/登出/会话写入）
//   - sessionIndexRedis：用户在线终端索引（auth:sessions:<uid>）
//   - verificationStoreRedis：基于 pkg/server/storage/redis 的验证码存储
//   - emailSenderImpl：基于 pkg/util/email 的邮件发送
//   - OAuth provider 适配器（google/github/azure）
package infrastructure

import (
	"errors"
	"fmt"

	"interestBar/pkg/domains/auth/domain"

	"github.com/sa-tokens/sa-token-go/stputil"
)

// SessionKeyForUser session 中存储用户信息的 key（与旧 utils.SessionKeyForUser 一致）。
const SessionKeyForUser = "user_info"

// saTokenSessionImpl 基于 stputil 的 SaTokenSession 实现。
type saTokenSessionImpl struct{}

// NewSaTokenSession 构造一个基于 stputil 的 SaTokenSession。
func NewSaTokenSession() domain.SaTokenSession {
	return &saTokenSessionImpl{}
}

// Login 调用 stputil.Login 为 loginID 在指定设备上登录。
func (s *saTokenSessionImpl) Login(loginID, device string) (string, error) {
	return stputil.Login(loginID, device)
}

// LogoutByToken 按 token 注销登录。
func (s *saTokenSessionImpl) LogoutByToken(token string) error {
	return stputil.LogoutByToken(token)
}

// Logout 注销 loginID 在指定设备上的登录。
func (s *saTokenSessionImpl) Logout(loginID, device string) error {
	return stputil.Logout(loginID, device)
}

// Kickout 踢下线 loginID 的全部会话（不区分设备）。
// 用于找回密码成功后强制吊销旧 token。
//
// 不能直接调 stputil.Kickout(loginID)：不传 device 时 sa-token 只踢 "default" 设备，
// 而登录只用 web / mobile。这里用 GetTokenValueList（SCAN 账号映射 <prefix>account:<loginID>:*）
// 列出全部 token 后逐个 KickoutByToken，不依赖枚举 device 名。
// 单个 token 失败时继续踢其余的，最后用 errors.Join 汇总返回；没有任何会话时返回 nil。
func (s *saTokenSessionImpl) Kickout(loginID string) error {
	tokens, err := stputil.GetTokenValueList(loginID)
	if err != nil {
		return fmt.Errorf("list tokens of %s: %w", loginID, err)
	}
	var errs []error
	for _, token := range tokens {
		if err := stputil.KickoutByToken(token); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("kickout %d of %d tokens failed: %w", len(errs), len(tokens), errors.Join(errs...))
	}
	return nil
}

// KickoutByToken 踢下线单个 token（sa-token 把它标记为 KICK_OUT 并保留原 TTL）。
func (s *saTokenSessionImpl) KickoutByToken(token string) error {
	return stputil.KickoutByToken(token)
}

// IsTokenAlive 判断 token 是否仍有效。
//
// 用 GetTokenInfo 而不是 IsLogin：IsLogin 会触发异步自动续期，核对其他终端的 token 时不应顺带延长它们的有效期。
// GetTokenInfo 对不存在（过期 / 已注销）的 token 返回 key not found，对被踢 / 被顶的 token 返回状态错误。
func (s *saTokenSessionImpl) IsTokenAlive(token string) bool {
	if token == "" {
		return false
	}
	info, err := stputil.GetTokenInfo(token)
	return err == nil && info != nil
}

// SetSessionUser 把用户信息写入 loginID 的 sa-token 会话。
//
// 与旧 utils.SetUserToSession 行为一致：session.Set(SessionKeyForUser, user)。
func (s *saTokenSessionImpl) SetSessionUser(loginID string, user domain.SessionUser) error {
	session, err := stputil.GetSession(loginID)
	if err != nil {
		return err
	}
	return session.Set(SessionKeyForUser, user)
}

// GetTokenTimeout 返回 token 剩余有效期（秒）。
func (s *saTokenSessionImpl) GetTokenTimeout(token string) (int64, error) {
	return stputil.GetTokenTimeout(token)
}
