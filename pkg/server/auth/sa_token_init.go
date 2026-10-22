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
// stputil API（IsLogin/GetLoginID/Login/Logout/GetSession）在 v0.2.x 无 breaking change，
// composition.RequireLogin 鉴权逻辑零改动。
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
	config.IsLog = true

	// 创建 Sa-Token 管理器
	manager := sahertz.NewManager(storage, config)

	// 设置全局管理器
	sahertz.SetManager(manager)

	logger.Log.Info("Sa-Token initialized successfully")
	logger.Log.Info(fmt.Sprintf("Token timeout: %d seconds", config.Timeout))
	logger.Log.Info(fmt.Sprintf("Token name: %s", config.TokenName))

	return nil
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
