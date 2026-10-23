// conf_aiagent.go：配置段 aiagent，聚合进 AppConfig（conf.go）。

package conf

// AiAgent AI 机器人回复执行链路配置（设计见 docs/agent-reply-design.md）。
// 各字段 <=0 时用代码内默认值（30s / 4000 字符 / 3）。
type AiAgent struct {
	TimeoutSec       int `mapstructure:"timeout_sec" json:"timeout_sec" yaml:"timeout_sec"`                   // LLM 单次调用超时(秒)，默认 30
	MaxContentChars  int `mapstructure:"max_content_chars" json:"max_content_chars" yaml:"max_content_chars"` // Prompt 各段(title/summary/评论)截断长度，默认 4000
	ReplyConcurrency int `mapstructure:"reply_concurrency" json:"reply_concurrency" yaml:"reply_concurrency"` // 关键词触发异步执行并发上限，默认 3
	// AllowPrivateBaseURL 允许 base_url 使用 http 及内网/环回地址（#48 防 SSRF 开关）。
	// 默认 false；仅本地开发或内网自建模型网关时开启，开启后圈主可让服务端访问内网。
	AllowPrivateBaseURL bool `mapstructure:"allow_private_base_url" json:"allow_private_base_url" yaml:"allow_private_base_url"`
	// Autonomy 自主行为（自动回复高热度帖子 / 评论）的平台总开关与上限，见 conf_aiagent_autonomy.go。
	Autonomy AiAgentAutonomy `mapstructure:"autonomy" json:"autonomy" yaml:"autonomy"`
}
