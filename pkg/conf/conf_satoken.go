// conf_satoken.go：配置段 sa_token，聚合进 AppConfig（conf.go）。

package conf

type SaToken struct {
	TokenName     string `mapstructure:"token_name" json:"token_name" yaml:"token_name"`
	Timeout       int    `mapstructure:"timeout" json:"timeout" yaml:"timeout"`
	ActiveTimeout int    `mapstructure:"active_timeout" json:"active_timeout" yaml:"active_timeout"`
	IsConcurrent  bool   `mapstructure:"is_concurrent" json:"is_concurrent" yaml:"is_concurrent"`
	IsShare       bool   `mapstructure:"is_share" json:"is_share" yaml:"is_share"`
	// MaxLoginCount 同一账号同时在线的终端上限，超出时注销最早登录的终端；<=0 时回落默认 10。
	// 由 auth 领域的会话索引控制，sa-token 自带的 MaxLoginCount 固定关闭（-1）。
	MaxLoginCount int `mapstructure:"max_login_count" json:"max_login_count" yaml:"max_login_count"`
	// RenewInterval 同一 token 两次自动续期的最小间隔（秒）；<=0 时回落默认 600。
	// 通过 sa-token 的 MaxRefresh = timeout - renew_interval 实现（剩余有效期低于阈值才续期），不额外访问 Redis。
	RenewInterval int `mapstructure:"renew_interval" json:"renew_interval" yaml:"renew_interval"`
}
