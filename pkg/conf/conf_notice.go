// conf_notice.go：配置段 notice / notice_stream，聚合进 AppConfig（conf.go）。

package conf

// Notice 消息中心配置。设计见 docs/notice-design.md。
type Notice struct {
	MentionMax int `mapstructure:"mention_max" json:"mention_max" yaml:"mention_max"` // 单条内容 @ 提及用户上限(<=0 兜底 10)
}

// NoticeStream SSE 未读数推流配置。设计见 docs/design/sse-notification-design.md §五。
// 数值字段 <=0 时用代码内默认值（项目惯例）。
type NoticeStream struct {
	Enabled         bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`                                  // false 时不注册路由（灰度开关）
	HeartbeatSec    int  `mapstructure:"heartbeat_sec" json:"heartbeat_sec" yaml:"heartbeat_sec"`                // 心跳+token复检周期(秒)，<=0 兜底 25
	MaxConnsPerUser int  `mapstructure:"max_conns_per_user" json:"max_conns_per_user" yaml:"max_conns_per_user"` // 每用户连接上限，<=0 兜底 5
	CoalesceMs      int  `mapstructure:"coalesce_ms" json:"coalesce_ms" yaml:"coalesce_ms"`                      // 合并推送窗口(毫秒)，<=0 兜底 1000
	RetryMs         int  `mapstructure:"retry_ms" json:"retry_ms" yaml:"retry_ms"`                               // 写给客户端的重连建议(毫秒)，<=0 兜底 5000
}
