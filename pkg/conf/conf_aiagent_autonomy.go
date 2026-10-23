// conf_aiagent_autonomy.go：配置段 aiagent.autonomy（机器人自主行为的平台总开关与上限），
// 作为 AiAgent 的子段聚合进 AppConfig。设计见 docs/agent-auto-reply-design.md。

package conf

// 自主行为配置的兜底默认值（<=0 时使用）。
const (
	DefaultAutonomyScanIntervalSec     = 300 // 自动回复扫描周期 5 分钟
	DefaultAutonomyMaxAgentsPerTarget  = 1   // 同一目标最多 1 个机器人自动回复
	DefaultAutonomyMaxCandidatesPerRun = 20  // 每轮每个机器人每种目标最多取 20 个候选
)

// AiAgentAutonomy 机器人自主行为的平台级配置：总开关 + 上限兜底（每个机器人的
// ai_agent.auto_config 只能在此范围内生效）。enabled=false 时扫描器不启动、已启动的不再扫描。
type AiAgentAutonomy struct {
	Enabled bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"` // 总开关，默认 false
	// ScanIntervalSec 自动回复扫描周期(秒)，<=0 兜底 300
	ScanIntervalSec int `mapstructure:"scan_interval_sec" json:"scan_interval_sec" yaml:"scan_interval_sec"`
	// MaxAgentsPerTarget 同一目标（帖子 / 评论）最多被几个机器人自动回复（软限），<=0 兜底 1
	MaxAgentsPerTarget int `mapstructure:"max_agents_per_target" json:"max_agents_per_target" yaml:"max_agents_per_target"`
	// MaxCandidatesPerRun 每轮每个机器人每种目标（帖子 / 评论）的候选上限（按热度取 top-N），<=0 兜底 20
	MaxCandidatesPerRun int `mapstructure:"max_candidates_per_run" json:"max_candidates_per_run" yaml:"max_candidates_per_run"`
}

// WithDefaults 返回填好默认值的副本（<=0 字段替换为默认值）。
func (a AiAgentAutonomy) WithDefaults() AiAgentAutonomy {
	if a.ScanIntervalSec <= 0 {
		a.ScanIntervalSec = DefaultAutonomyScanIntervalSec
	}
	if a.MaxAgentsPerTarget <= 0 {
		a.MaxAgentsPerTarget = DefaultAutonomyMaxAgentsPerTarget
	}
	if a.MaxCandidatesPerRun <= 0 {
		a.MaxCandidatesPerRun = DefaultAutonomyMaxCandidatesPerRun
	}
	return a
}
