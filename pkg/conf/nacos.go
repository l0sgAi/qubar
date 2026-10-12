package conf

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/spf13/viper"
)

// errNoBootstrap 表示未找到引导配置文件，调用方应静默回退到本地文件加载，
// 而不是打印告警（这是开发者尚未接入 Nacos 时的默认路径）。
var errNoBootstrap = errors.New("no nacos bootstrap file")

// bootstrapConfig 是本地引导配置，用于告诉应用如何连接 Nacos 以及
// 环境与 {namespace_id, data_id} 的映射。它本身不能来自 Nacos（鸡生蛋问题）。
type bootstrapConfig struct {
	Nacos struct {
		Host      string `mapstructure:"host"`
		Port      uint64 `mapstructure:"port"`
		Username  string `mapstructure:"username"`
		Password  string `mapstructure:"password"`
		Group     string `mapstructure:"group"`
		TimeoutMs uint64 `mapstructure:"timeout_ms"`
		LogDir    string `mapstructure:"log_dir"`
		CacheDir  string `mapstructure:"cache_dir"`
		LogLevel  string `mapstructure:"log_level"`
	} `mapstructure:"nacos"`
	Envs map[string]struct {
		NamespaceID string `mapstructure:"namespace_id"`
		DataID      string `mapstructure:"data_id"`
	} `mapstructure:"envs"`
}

// loadBootstrap 读取本地引导配置文件。
// 返回 (cfg, true, nil) 表示文件存在并解析成功；
// 返回 (nil, false, nil) 表示文件不存在（调用方静默回退）；
// 返回 (nil, false, err) 表示文件存在但解析失败。
func loadBootstrap(path string) (*bootstrapConfig, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return nil, false, fmt.Errorf("read bootstrap: %w", err)
	}
	var b bootstrapConfig
	if err := v.Unmarshal(&b); err != nil {
		return nil, false, fmt.Errorf("parse bootstrap: %w", err)
	}
	return &b, true, nil
}

// currentEnv 解析 APP_ENV 环境变量。默认 "dev"，任何非 "prod" 的值都按 "dev" 处理。
func currentEnv() string {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if env == "" {
		return "dev"
	}
	if env != "dev" && env != "prod" {
		log.Printf("[conf] APP_ENV=%q not recognized (expected dev|prod), treating as dev", env)
		return "dev"
	}
	return env
}

// resolveTarget 根据环境选出 {namespace_id, data_id, group}。
// namespace_id 为空或仍是占位符时返回错误（最常见的误操作：填了命名空间名称而非 UUID）。
func (b *bootstrapConfig) resolveTarget(env string) (namespaceID, dataID, group string, err error) {
	e, ok := b.Envs[env]
	if !ok {
		return "", "", "", fmt.Errorf("no bootstrap entry for env %q", env)
	}
	if strings.TrimSpace(e.NamespaceID) == "" || strings.HasPrefix(e.NamespaceID, "REPLACE-WITH") {
		return "", "", "", fmt.Errorf("env %q namespace_id is not configured (copy the UUID from the Nacos console, not the namespace name)", env)
	}
	if strings.TrimSpace(e.DataID) == "" {
		return "", "", "", fmt.Errorf("env %q data_id is empty", env)
	}
	group = nonEmpty(b.Nacos.Group, "DEFAULT_GROUP")
	return e.NamespaceID, e.DataID, group, nil
}

// buildClient 根据引导配置与命名空间构造 Nacos 配置客户端。
func buildClient(b *bootstrapConfig, namespaceID string) (config_client.IConfigClient, error) {
	port := b.Nacos.Port
	if port == 0 {
		port = 8848
	}
	timeoutMs := b.Nacos.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 5000
	}
	sc := []constant.ServerConfig{
		{
			IpAddr:      b.Nacos.Host,
			Port:        port,
			ContextPath: "/nacos",
		},
	}
	cc := constant.ClientConfig{
		NamespaceId:         namespaceID,
		Username:            b.Nacos.Username,
		Password:            b.Nacos.Password,
		TimeoutMs:           timeoutMs,
		NotLoadCacheAtStart: true, // 关键：避免不可达时返回陈旧的本地缓存，从而掩盖故障
		LogDir:              b.Nacos.LogDir,
		CacheDir:            b.Nacos.CacheDir,
		LogLevel:            nonEmpty(b.Nacos.LogLevel, "warn"),
	}
	client, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  &cc,
		ServerConfigs: sc,
	})
	if err != nil {
		return nil, fmt.Errorf("create nacos client: %w", err)
	}
	return client, nil
}

// feedViper 将 Nacos 返回的原始配置内容(YAML)反序列化进全局 Config。
// 由于 Data ID(如 qubar-dev-conf)没有扩展名，必须显式声明 SetConfigType("yaml")。
// 初始加载与热更新均复用此函数。
func feedViper(content string) error {
	v := viper.New()
	v.SetConfigType("yaml")
	bindEnv(v) // 环境变量优先于 Nacos 内容（#51）
	if err := v.ReadConfig(bytes.NewReader([]byte(content))); err != nil {
		return fmt.Errorf("parse nacos config content: %w", err)
	}
	return v.Unmarshal(&Config)
}

// startListen 注册 Nacos 配置变更监听。配置变更时重新反序列化进全局 Config。
// 注意：DB/Redis/Redpanda 等客户端在启动时已初始化，热更新不会重建这些连接，
// 与原有 fsnotify 文件监听的行为一致——这些字段需重启进程才能生效。
func startListen(client config_client.IConfigClient, namespaceID, dataID, group string) {
	err := client.ListenConfig(vo.ConfigParam{
		DataId: dataID,
		Group:  group,
		OnChange: func(namespace, group, dataId, data string) {
			log.Printf("[conf] nacos config changed: namespace=%s dataId=%s", namespace, dataId)
			if err := feedViper(data); err != nil {
				log.Printf("[conf] nacos reload failed: %v", err)
				return
			}
			log.Printf("[conf] nacos reload applied (runtime-only fields); DB/Redis/Redpanda 等连接不会重建，需重启生效")
		},
	})
	if err != nil {
		log.Printf("[conf] nacos ListenConfig error: %v", err)
	}
}

// initFromNacos 是从 Nacos 加载配置的编排函数。
// 任何失败都以 error 返回，由调用方决定是否回退到本地文件。
// 用 defer recover() 兜底 SDK 在连接阶段可能的 panic，确保能优雅回退。
func initFromNacos(bootstrapPath string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("nacos client panicked: %v", r)
		}
	}()

	boot, ok, lerr := loadBootstrap(bootstrapPath)
	if lerr != nil {
		return lerr
	}
	if !ok {
		return errNoBootstrap
	}

	env := currentEnv()
	namespaceID, dataID, group, terr := boot.resolveTarget(env)
	if terr != nil {
		return terr
	}

	client, cerr := buildClient(boot, namespaceID)
	if cerr != nil {
		return cerr
	}

	content, gerr := client.GetConfig(vo.ConfigParam{DataId: dataID, Group: group})
	if gerr != nil {
		return fmt.Errorf("nacos GetConfig %s/%s: %w", dataID, group, gerr)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("nacos GetConfig %s/%s returned empty content", dataID, group)
	}

	if ferr := feedViper(content); ferr != nil {
		return ferr
	}

	go startListen(client, namespaceID, dataID, group)

	log.Printf("[conf] loaded config from Nacos: env=%s namespace=%s dataId=%s group=%s", env, namespaceID, dataID, group)
	return nil
}

// nonEmpty 返回去除首尾空白后的 val；若为空则返回 fallback。
func nonEmpty(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}
