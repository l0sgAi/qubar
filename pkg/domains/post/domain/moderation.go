package domain

import (
	"context"
	"errors"
	"time"
)

// ContentModerator 帖子内容 AI 审核端口（#45 / #116），由 infrastructure 的 TypeSafe Jev 适配器实现。
//
// 只负责「问模型、拿概率」，不做分流决策：放行 / 转人工 / 驳回由调用方按 moderation 阈值判断（#117）。
type ContentModerator interface {
	// Assess 评估一篇帖子。错误均包装下方哨兵之一，调用方用 errors.Is 区分：
	//   ErrModerationDisabled    — 未启用（开关关闭或未配置 api_key），不应告警
	//   ErrModerationUnavailable — 超时 / 5xx / 429 / 529 重试耗尽，或 ctx 取消，按 fail_open 处理
	//   ErrModerationAuth        — 401 / 403，api_key 无效，需要告警
	//   ErrModerationRejected    — 400 / 413 / 415 / 422 等请求被拒，属于调用方 bug，需要告警
	//   ErrModerationBadResponse — 200 但响应缺字段或数值越界
	Assess(ctx context.Context, in ModerationInput) (*Assessment, error)
}

// ModerationInput 审核输入。圈子信息帮助模型理解语境（如「同人」圈的虚构暴力）。
type ModerationInput struct {
	CircleName     string
	CircleCategory string
	Title          string
	Content        string
}

// Hazard 风险类别，同时是发给 Jev 的 noul 题目名。
type Hazard string

const (
	HazardSexual         Hazard = "sexual"          // 色情低俗 / 招嫖
	HazardViolenceTerror Hazard = "violence_terror" // 现实暴力 / 恐怖主义（虚构创作不算）
	HazardHarassment     Hazard = "harassment"      // 辱骂、骚扰、攻击他人
	HazardIllegal        Hazard = "illegal"         // 赌博、毒品、诈骗、非法交易
	HazardSpamAd         Hazard = "spam_ad"         // 广告、站外引流、刷单
	HazardPrivacy        Hazard = "privacy"         // 泄露他人隐私
	HazardSelfHarm       Hazard = "self_harm"       // 自伤 / 轻生倾向
	HazardManipulation   Hazard = "manipulation"    // 写给审核系统的指令，试图影响审核结果
)

// Hazards 全部风险类别（固定顺序，便于遍历与落库）。
var Hazards = []Hazard{
	HazardSexual, HazardViolenceTerror, HazardHarassment, HazardIllegal,
	HazardSpamAd, HazardPrivacy, HazardSelfHarm, HazardManipulation,
}

// Assessment 审核结果。
type Assessment struct {
	// Risks 各风险的概率（0–1），包含 Hazards 中的全部类别。
	Risks map[Hazard]float64
	// Severity 发布后的危害程度期望分（0–3：无 / 轻微 / 严重 / 极端）。
	Severity float64
	// SeverityConfidence Severity 的置信度（0–1），低置信度应转人工。
	SeverityConfidence float64
	// Model 实际作答的模型版本号（如 jev-1.13.0）。
	Model string
	// InputTokens 计费 input token 数。
	InputTokens int
	// Latency 本次 Assess 总耗时（含重试等待）。
	Latency time.Duration
	// Attempts 实际请求次数（1 = 无重试）。
	Attempts int
}

var (
	ErrModerationDisabled    = errors.New("content moderation is not enabled")
	ErrModerationUnavailable = errors.New("content moderation service unavailable")
	ErrModerationAuth        = errors.New("content moderation authentication failed")
	ErrModerationRejected    = errors.New("content moderation request rejected")
	ErrModerationBadResponse = errors.New("content moderation returned an invalid response")
)
