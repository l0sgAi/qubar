// conf_message.go：配置段 message（私信），聚合进 AppConfig（conf.go）。

package conf

// message.send_rate 各项未配置（<=0）时的默认值（维护者 2026-11-11 确认，#242）。
const (
	DefaultMessageSendPerMinute          = 20
	DefaultMessageSendPerDay             = 500
	DefaultMessageNewConversationsPerDay = 20
)

// Message 私信配置（message 领域）。设计见 docs/message-design.md 第十二节。
type Message struct {
	SendRate MessageSendRate `mapstructure:"send_rate" json:"send_rate" yaml:"send_rate"`
}

// MessageSendRate 私信发送频率限制（按发送者计，Redis 计数，多实例共享）。
type MessageSendRate struct {
	PerMinute              int `mapstructure:"per_minute" json:"per_minute" yaml:"per_minute"`                                              // 每分钟最多发送条数(<=0 兜底 20)
	PerDay                 int `mapstructure:"per_day" json:"per_day" yaml:"per_day"`                                                       // 每天最多发送条数(<=0 兜底 500)
	NewConversationsPerDay int `mapstructure:"new_conversations_per_day" json:"new_conversations_per_day" yaml:"new_conversations_per_day"` // 每天最多主动开启的新会话数(<=0 兜底 20)
}

// PerMinuteOrDefault 返回每分钟上限；<=0 时回落 DefaultMessageSendPerMinute。
func (r MessageSendRate) PerMinuteOrDefault() int {
	return positiveOr(r.PerMinute, DefaultMessageSendPerMinute)
}

// PerDayOrDefault 返回每天上限；<=0 时回落 DefaultMessageSendPerDay。
func (r MessageSendRate) PerDayOrDefault() int {
	return positiveOr(r.PerDay, DefaultMessageSendPerDay)
}

// NewConversationsPerDayOrDefault 返回每天新会话上限；<=0 时回落 DefaultMessageNewConversationsPerDay。
func (r MessageSendRate) NewConversationsPerDayOrDefault() int {
	return positiveOr(r.NewConversationsPerDay, DefaultMessageNewConversationsPerDay)
}

func positiveOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
