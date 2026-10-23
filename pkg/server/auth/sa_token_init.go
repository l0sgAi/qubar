package auth

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	sahertz "github.com/sa-tokens/sa-token-go/integrations/hertz"
	"github.com/sa-tokens/sa-token-go/storage/redis"
)

// InitSaToken 初始化 Sa-Token-Go 框架。
//
// gin→hertz 迁移：从 integrations/gin（v0.1.7）切换到 integrations/hertz（v0.2.2）。
// 顶层函数 DefaultConfig/NewManager/SetManager/GetManager 签名在 gin/hertz 集成包里一致。
// stputil API（IsLogin/GetLoginID/Login/Logout/GetSession）在 v0.2.x 无 breaking change。
// 请求鉴权（composition.RequireLogin / OptionalLogin）的校验与续期节流见 composition.authenticate。
func InitSaToken() error {
	// 创建 Redis 存储：用结构化配置而非 URL。
	// 旧实现拼 "redis://<password>@host" 会被 go-redis ParseURL 解析成「用户名=密码、密码为空」，
	// 设了 requirepass 的 Redis 上不发 AUTH → Ping 报 NOAUTH；密码含 @ / : 等字符还会解析失败。
	storage, err := redis.NewStorageFromConfig(&redis.Config{
		Host:     conf.Config.Redis.Host,
		Port:     conf.Config.Redis.Port,
		Password: conf.Config.Redis.Password,
		Database: conf.Config.Redis.D,
		PoolSize: conf.Config.Redis.PoolSize,
	})
	if err != nil {
		return fmt.Errorf("failed to create redis storage: %w", err)
	}

	// 使用配置文件中的 Sa-Token 配置
	config := sahertz.DefaultConfig()

	// 如果配置文件中有 Sa-Token 配置,则使用配置文件的值
	if conf.Config.SaToken.TokenName != "" {
		config.TokenName = conf.Config.SaToken.TokenName
	}
	if conf.Config.SaToken.Timeout > 0 {
		config.Timeout = int64(conf.Config.SaToken.Timeout)
	} else {
		config.Timeout = 259200 // 默认3天
	}
	if conf.Config.SaToken.ActiveTimeout > 0 {
		config.ActiveTimeout = int64(conf.Config.SaToken.ActiveTimeout)
	} else {
		config.ActiveTimeout = 1800 // 默认30分钟
	}
	config.IsConcurrent = conf.Config.SaToken.IsConcurrent
	config.IsShare = conf.Config.SaToken.IsShare
	// 终端上限由 auth 领域的会话索引（auth:sessions:<uid>）控制（sa_token.max_login_count）。
	// 关掉 sa-token 自带的上限：它每次登录都对 account:<uid>:* 做一次全库 SCAN。
	config.MaxLoginCount = -1
	// 续期节流（#131）：sa-token 只在剩余有效期 <= MaxRefresh 时续期。默认 MaxRefresh 是 30 天的一半，
	// 比 3 天的 Timeout 还大，等于每次校验都续期（GET + SET KEEPTTL + 3 次 EXPIRE + session GET）。
	// 取 Timeout - renew_interval：同一 token 距上次登录 / 续期满 renew_interval 秒才续一次，
	// 判断只用校验时本来就要查的 TTL，不新增 Redis 操作。
	// 不用 sa-token 的 RenewInterval：它每次校验都要多 EXISTS 一个 renew 标记 key。
	config.MaxRefresh = RenewMaxRefresh(config.Timeout, int64(conf.Config.SaToken.RenewInterval))
	config.IsLog = true

	// 创建 Sa-Token 管理器
	manager := sahertz.NewManager(storage, config)

	// 设置全局管理器
	sahertz.SetManager(manager)

	logger.Log.Info("Sa-Token initialized successfully")
	logger.Log.Info(fmt.Sprintf("Token timeout: %d seconds, renew when remaining <= %d seconds", config.Timeout, config.MaxRefresh))
	logger.Log.Info(fmt.Sprintf("Token name: %s", config.TokenName))

	return nil
}

// defaultRenewInterval sa_token.renew_interval 未配置（<=0）时的续期最小间隔（秒）。
const defaultRenewInterval = 600

// RenewMaxRefresh 由 token 有效期与续期最小间隔（秒）算出 sa-token 的 MaxRefresh 续期阈值。
//
//   - timeout <= 0（永不过期）：无需续期，返回 -1（不限制，sa-token 也不会续期）；
//   - interval <= 0：回落 defaultRenewInterval；
//   - interval >= timeout：阈值退化为 timeout / 2（与 sa-token 对 MaxRefresh > Timeout 的修正一致），
//     避免阈值 <= 0 导致永不续期。
func RenewMaxRefresh(timeout, interval int64) int64 {
	if timeout <= 0 {
		return -1
	}
	if interval <= 0 {
		interval = defaultRenewInterval
	}
	if interval >= timeout {
		return max(timeout/2, 1)
	}
	return timeout - interval
}

// CloseSaToken 关闭 Sa-Token 连接
func CloseSaToken() error {
	if sahertz.GetManager() != nil && sahertz.GetManager().GetStorage() != nil {
		return sahertz.GetManager().GetStorage().(interface {
			Close() error
		}).Close()
	}
	return nil
}
