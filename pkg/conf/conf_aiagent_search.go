// conf_aiagent_search.go：配置段 aiagent.search，嵌在 AiAgent（conf_aiagent.go）。

package conf

// AiAgentSearch 机器人网络搜索热点来源配置（#100）。平台统一出 key，按机器人限制每日次数。
// api_key 为空时搜索功能关闭（启动不报错，调用返回"未启用"）。
// api_key 只放 configs/config.yaml（已 git-ignore）或环境变量 QUBAR_AIAGENT_SEARCH_API_KEY，绝不入库。
// 数值字段 <=0 时用代码内默认值（见 aiagent/infrastructure/hot_topic_source.go）。
type AiAgentSearch struct {
	Provider       string `mapstructure:"provider" json:"provider" yaml:"provider"`                      // 服务商：bocha（当前唯一实现）
	Endpoint       string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`                      // 搜索 API 地址，空 = 服务商默认地址
	APIKey         string `mapstructure:"api_key" json:"-" yaml:"api_key"`                               // 平台 key，不序列化到 JSON
	TimeoutSec     int    `mapstructure:"timeout_sec" json:"timeout_sec" yaml:"timeout_sec"`             // 单次调用超时(秒)，默认 10
	MaxResults     int    `mapstructure:"max_results" json:"max_results" yaml:"max_results"`             // 默认返回条数，默认 10，上限 50
	FreshnessHours int    `mapstructure:"freshness_hours" json:"freshness_hours" yaml:"freshness_hours"` // 默认时效(小时)，默认 24
	DailyQuota     int    `mapstructure:"daily_quota" json:"daily_quota" yaml:"daily_quota"`             // 每个机器人每日搜索次数，默认 20
}
