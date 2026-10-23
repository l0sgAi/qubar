// conf_moderation.go：配置段 moderation，聚合进 AppConfig（conf.go）。

package conf

// 帖子 AI 审核默认值（#45 实测 jev-1.13.0 后定的初始值，上线前需用真实帖子重调）。
const (
	DefaultModerationBaseURL               = "https://api.typesafe.ai"
	DefaultModerationModel                 = "jev-1.13.0"
	DefaultModerationTimeoutSec            = 10
	DefaultModerationMaxRetries            = 2
	DefaultModerationContentMaxRunes       = 8000
	DefaultModerationReviewThreshold       = 0.35
	DefaultModerationActionThreshold       = 0.70
	DefaultModerationSeverityBlock         = 1.5
	DefaultModerationLowConfidence         = 0.5
	DefaultModerationLowConfidenceSeverity = 1.0

	// 先发后审 + 举报复核（#153）
	DefaultModerationReviewAction          = ModerationReviewActionPublish
	DefaultModerationReportNotifyThreshold = 1
	DefaultModerationReportDailyLimit      = 20
)

// review_action 取值：AI 判为「人工待审」的帖子怎么处理（#153）。
const (
	ModerationReviewActionPublish = "publish" // 直接发布（先发后审，靠举报复核兜底）
	ModerationReviewActionHold    = "hold"    // 留在审核中，等圈主 / 管理员在 #118 待审队列处理
)

// Moderation 帖子 AI 审核配置（TypeSafe Jev，#45 / #116）。
//
// 数值字段 <=0 时用代码内默认值（项目惯例，report_hide_threshold 例外：0 = 关闭）；fail_open 不写时默认 true。
// 读取时统一走 WithDefaults()，不要直接读原始字段。
// api_key 是平台密钥：只放 git-ignore 的 configs/config.yaml 或环境变量 QUBAR_MODERATION_API_KEY，
// 不得出现在日志和错误信息里。
type Moderation struct {
	Enabled bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`    // 总开关，默认 false；未开启或 api_key 为空时调用返回「未启用」
	APIKey  string `mapstructure:"api_key" json:"api_key" yaml:"api_key"`    // TypeSafe API key（Bearer）
	BaseURL string `mapstructure:"base_url" json:"base_url" yaml:"base_url"` // 默认 https://api.typesafe.ai，只来自平台配置
	// Model 固定版本号（默认 jev-1.13.0），不要用 jev-latest 这类别名：升级模型会改变概率分布，需要重调阈值。
	Model           string `mapstructure:"model" json:"model" yaml:"model"`
	TimeoutSec      int    `mapstructure:"timeout_sec" json:"timeout_sec" yaml:"timeout_sec"`                   // 单次请求超时(秒)，<=0 兜底 10
	MaxRetries      int    `mapstructure:"max_retries" json:"max_retries" yaml:"max_retries"`                   // 超时 / 5xx / 429 / 529 的最大重试次数，<=0 兜底 2
	ContentMaxRunes int    `mapstructure:"content_max_runes" json:"content_max_runes" yaml:"content_max_runes"` // 正文截断长度(字符)，超长保留首尾，<=0 兜底 8000
	// FailOpen 审核服务不可用时是否放行（true = 直接发布，false = 转人工）。不写时默认 true。由 #117 使用。
	FailOpen *bool `mapstructure:"fail_open" json:"fail_open" yaml:"fail_open"`

	// 分流阈值（由 #117 使用，本身不影响审核调用）。
	ReviewThreshold       float64 `mapstructure:"review_threshold" json:"review_threshold" yaml:"review_threshold"`                      // 任一风险概率 >= 此值转人工，<=0 兜底 0.35
	ActionThreshold       float64 `mapstructure:"action_threshold" json:"action_threshold" yaml:"action_threshold"`                      // 风险概率 >= 此值且 severity >= severity_block 才驳回，<=0 兜底 0.70
	SeverityBlock         float64 `mapstructure:"severity_block" json:"severity_block" yaml:"severity_block"`                            // 驳回所需最低 severity(0–3)，<=0 兜底 1.5
	LowConfidence         float64 `mapstructure:"low_confidence" json:"low_confidence" yaml:"low_confidence"`                            // severity 置信度低于此值视为不确定，<=0 兜底 0.5
	LowConfidenceSeverity float64 `mapstructure:"low_confidence_severity" json:"low_confidence_severity" yaml:"low_confidence_severity"` // severity >= 此值且置信度低时转人工，<=0 兜底 1.0

	// 先发后审 + 举报复核（#153）。举报功能与 enabled 无关，始终可用。
	ReviewAction          string `mapstructure:"review_action" json:"review_action" yaml:"review_action"`                               // publish（默认）| hold；空 / 非法值按 publish
	ReportNotifyThreshold int    `mapstructure:"report_notify_threshold" json:"report_notify_threshold" yaml:"report_notify_threshold"` // 待处理举报数达到此值时通知圈主 / 管理员（处理前只通知一次），<=0 兜底 1
	ReportHideThreshold   int    `mapstructure:"report_hide_threshold" json:"report_hide_threshold" yaml:"report_hide_threshold"`       // 待处理举报数达到此值时自动屏蔽（status 4），<=0 = 关闭（默认）
	ReportDailyLimit      int    `mapstructure:"report_daily_limit" json:"report_daily_limit" yaml:"report_daily_limit"`                // 每用户 24 小时内举报上限，<=0 兜底 20
}

// Active 是否真正启用审核：开关打开且配置了 api_key。
func (m Moderation) Active() bool {
	return m.Enabled && m.APIKey != ""
}

// FailOpenEnabled 审核不可用时是否放行，未配置时默认 true。
func (m Moderation) FailOpenEnabled() bool {
	return m.FailOpen == nil || *m.FailOpen
}

// ReviewPublishes AI「人工待审」结果是否直接发布（review_action 未配置 / 非法时为 true）。
func (m Moderation) ReviewPublishes() bool {
	return m.ReviewAction != ModerationReviewActionHold
}

// WithDefaults 返回填好默认值的副本（<=0 / 空字段替换为默认值，fail_open 未配置时置为 true）。
func (m Moderation) WithDefaults() Moderation {
	if m.BaseURL == "" {
		m.BaseURL = DefaultModerationBaseURL
	}
	if m.Model == "" {
		m.Model = DefaultModerationModel
	}
	if m.TimeoutSec <= 0 {
		m.TimeoutSec = DefaultModerationTimeoutSec
	}
	if m.MaxRetries <= 0 {
		m.MaxRetries = DefaultModerationMaxRetries
	}
	if m.ContentMaxRunes <= 0 {
		m.ContentMaxRunes = DefaultModerationContentMaxRunes
	}
	failOpen := m.FailOpenEnabled()
	m.FailOpen = &failOpen
	if m.ReviewThreshold <= 0 {
		m.ReviewThreshold = DefaultModerationReviewThreshold
	}
	if m.ActionThreshold <= 0 {
		m.ActionThreshold = DefaultModerationActionThreshold
	}
	if m.SeverityBlock <= 0 {
		m.SeverityBlock = DefaultModerationSeverityBlock
	}
	if m.LowConfidence <= 0 {
		m.LowConfidence = DefaultModerationLowConfidence
	}
	if m.LowConfidenceSeverity <= 0 {
		m.LowConfidenceSeverity = DefaultModerationLowConfidenceSeverity
	}
	if m.ReviewAction != ModerationReviewActionHold {
		m.ReviewAction = DefaultModerationReviewAction
	}
	if m.ReportNotifyThreshold <= 0 {
		m.ReportNotifyThreshold = DefaultModerationReportNotifyThreshold
	}
	if m.ReportHideThreshold < 0 {
		m.ReportHideThreshold = 0
	}
	if m.ReportDailyLimit <= 0 {
		m.ReportDailyLimit = DefaultModerationReportDailyLimit
	}
	return m
}
