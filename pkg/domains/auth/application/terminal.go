package application

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/auth/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TerminalInput 登录终端信息（邮箱登录、注册完成、OAuth 换 token 三个入口共用）。
type TerminalInput struct {
	Device    string // 终端类型 web / mobile，空值 / 非法值按 web
	DeviceID  string // 客户端持久化的终端 UUID；非法 / 缺省时服务端生成，并在登录响应里返回
	IP        string // 客户端 IP（只写入会话索引的元数据）
	UserAgent string // User-Agent（截断后写入会话索引的元数据）
}

const (
	// defaultMaxLoginCount sa_token.max_login_count 未配置（<=0）时的终端上限。
	defaultMaxLoginCount = 10
	// defaultTokenTimeout sa_token.timeout 未配置（<=0）时的 token 有效期（秒），与 sa_token_init.go 一致。
	defaultTokenTimeout = 259200
	// maxUserAgentBytes 会话索引里 UA 摘要的最大字节数。
	maxUserAgentBytes = 256
)

// resolveTerminal 返回 (终端类型, device_id)：类型按 resolveDevice 归一，device_id 不合法时生成新的。
func resolveTerminal(in TerminalInput) (deviceType, deviceID string) {
	deviceID = normalizeDeviceID(in.DeviceID)
	if deviceID == "" {
		deviceID = uuid.NewString()
	}
	return resolveDevice(in.Device), deviceID
}

// normalizeDeviceID 只接受 8-4-4-4-12 形式的标准 UUID，统一为小写；其他输入返回空串（当作没传）。
//
// uuid.Parse 还接受 "urn:uuid:…"、"{…}"、32 位无连字符等写法，前者带 ":" 会破坏 OAuth state 的分隔，
// 所以先卡长度 36。全零 UUID 也拒绝：客户端默认值误传时会让所有终端挤进同一个 device 槽位互相顶掉。
func normalizeDeviceID(s string) string {
	if len(s) != 36 {
		return ""
	}
	u, err := uuid.Parse(s)
	if err != nil || u == uuid.Nil {
		return ""
	}
	return u.String()
}

// maxLoginCount 返回同一账号同时在线的终端上限（sa_token.max_login_count，<=0 回落默认值）。
func maxLoginCount() int {
	if n := conf.Config.SaToken.MaxLoginCount; n > 0 {
		return n
	}
	return defaultMaxLoginCount
}

// sessionIndexTTL 返回会话索引的 TTL：与 token 有效期（sa_token.timeout）一致。
func sessionIndexTTL() time.Duration {
	timeout := conf.Config.SaToken.Timeout
	if timeout <= 0 {
		timeout = defaultTokenTimeout
	}
	return time.Duration(timeout) * time.Second
}

// truncateUserAgent 把 UA 截断到 maxUserAgentBytes 字节以内，不切断多字节字符。
func truncateUserAgent(ua string) string {
	if len(ua) <= maxUserAgentBytes {
		return ua
	}
	cut := maxUserAgentBytes
	for cut > 0 && !utf8.RuneStart(ua[cut]) {
		cut--
	}
	return ua[:cut]
}

// issueToken 在终端 in 上为 user 登录，返回 token 与该终端的 device_id。
//
//  1. sa-token device = "<type>:<device_id>"：先注销这个终端自己的旧 token（同一终端重新登录只替换自己），
//     其他终端的 token 不受影响；
//  2. 登录 + 写会话用户信息；
//  3. 登记会话索引并执行终端上限（best-effort，失败只记日志，不阻断登录）。
func (s *authServiceImpl) issueToken(ctx context.Context, user *domain.LoginUser, in TerminalInput) (token, deviceID string, err error) {
	deviceType, deviceID := resolveTerminal(in)
	device := domain.TerminalDevice(deviceType, deviceID)

	_ = s.session.Logout(user.ID, device)

	token, err = s.session.Login(user.ID, device)
	if err != nil {
		return "", "", err
	}
	if err := s.session.SetSessionUser(user.ID, user.ToSessionUser()); err != nil {
		return "", "", err
	}

	s.trackSession(ctx, user.ID, token, domain.TerminalMeta{
		Type:      deviceType,
		DeviceID:  deviceID,
		LoginTime: time.Now().Unix(),
		IP:        in.IP,
		UA:        truncateUserAgent(in.UserAgent),
	})
	return token, deviceID, nil
}

// trackSession 把新 token 登记进会话索引，清理已失效的 member，并在超出终端上限时注销最早登录的终端。
//
// 上限是软限：两个终端并发登录时可能短暂多出一个，下次登录时会收敛。
// 被注销的终端用 KickoutByToken，再请求时得到"被踢下线"状态而不是"未登录"。
func (s *authServiceImpl) trackSession(ctx context.Context, loginID, token string, meta domain.TerminalMeta) {
	if err := s.index.Add(ctx, loginID, token, meta, sessionIndexTTL()); err != nil {
		logger.Log.Warn("session index add failed", zap.String("user_id", loginID), zap.Error(err))
		return
	}
	tokens, err := s.index.List(ctx, loginID)
	if err != nil {
		logger.Log.Warn("session index list failed", zap.String("user_id", loginID), zap.Error(err))
		return
	}

	// 其余 token 按登录时间从早到晚：已失效的（过期 / 注销 / 被踢 / 同终端被替换）直接移出索引
	var alive, remove []string
	for _, tk := range tokens {
		if tk == token {
			continue
		}
		if s.session.IsTokenAlive(tk) {
			alive = append(alive, tk)
		} else {
			remove = append(remove, tk)
		}
	}

	// 新 token 占一个名额，超出部分从最早登录的开始注销
	if over := len(alive) + 1 - maxLoginCount(); over > 0 {
		for _, tk := range alive[:over] {
			if err := s.session.KickoutByToken(tk); err != nil {
				// 留在索引里：仍有效就由下次登录重试，已失效就由下次登录清理
				logger.Log.Warn("kickout oldest terminal failed", zap.String("user_id", loginID), zap.Error(err))
				continue
			}
			remove = append(remove, tk)
		}
	}

	if err := s.index.Remove(ctx, loginID, remove...); err != nil {
		logger.Log.Warn("session index remove failed", zap.String("user_id", loginID), zap.Error(err))
	}
}

// kickoutAllTerminals 注销 loginID 的全部终端（重置密码后调用）。
//
//  1. 遍历会话索引，逐个 KickoutByToken（跳过已失效的）；
//  2. session.Kickout 兜底：SCAN 账号映射 account:<uid>:*，踢掉索引里没有的 token——
//     上线前按 web / mobile 签发的旧 token、自动续期后比索引 TTL 活得久的 token、索引写入失败漏登记的 token；
//  3. 清空索引。
//
// 某一步失败时继续执行后面的步骤，最后汇总返回错误。
func (s *authServiceImpl) kickoutAllTerminals(ctx context.Context, loginID string) error {
	var errs []error

	tokens, err := s.index.List(ctx, loginID)
	if err != nil {
		errs = append(errs, fmt.Errorf("list session index: %w", err))
	}
	for _, tk := range tokens {
		if !s.session.IsTokenAlive(tk) {
			continue
		}
		if err := s.session.KickoutByToken(tk); err != nil {
			errs = append(errs, err)
		}
	}

	if err := s.session.Kickout(loginID); err != nil {
		errs = append(errs, err)
	}
	if err := s.index.Clear(ctx, loginID); err != nil {
		errs = append(errs, fmt.Errorf("clear session index: %w", err))
	}
	return errors.Join(errs...)
}
