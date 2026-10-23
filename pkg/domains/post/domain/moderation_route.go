package domain

// ModerationDecision 帖子审核分流结果（同时是 post_moderation_log.decision 的取值）。
type ModerationDecision string

const (
	ModerationPublish  ModerationDecision = "publish"   // 发布（status 1）
	ModerationReview   ModerationDecision = "review"    // 人工待审（留在 status 2，一直不可见，#118 处理）
	ModerationReject   ModerationDecision = "reject"    // 驳回（status 3）
	ModerationFailOpen ModerationDecision = "fail_open" // 审核服务不可用，按 fail_open 放行发布（status 1）
)

// ModerationThresholds 分流阈值（来自 moderation 配置，调用方先 WithDefaults）。
type ModerationThresholds struct {
	Review                float64 // 任一风险概率 >= 此值转人工
	Action                float64 // 风险概率 >= 此值时进入驳回判断
	SeverityBlock         float64 // 驳回所需最低 severity（0–3）
	LowConfidence         float64 // severity 置信度低于此值视为不确定
	LowConfidenceSeverity float64 // severity >= 此值且置信度低时转人工
}

// reviewOnlyHazards 命中后只转人工、不自动驳回的风险：
// 自伤需要人工关怀而不是删帖；manipulation 命中说明模型可能被内容误导，结论不可信。
var reviewOnlyHazards = map[Hazard]bool{
	HazardSelfHarm:     true,
	HazardManipulation: true,
}

// RouteModeration 按阈值把审核结果分流为 发布 / 人工待审 / 驳回（纯函数，#117）。
//
// 规则（驳回优先于人工待审，人工待审优先于发布）：
//  1. 任一风险 >= Action：self_harm / manipulation → 人工待审；其他风险且 severity >= SeverityBlock → 驳回，否则人工待审
//  2. 任一风险 >= Review → 人工待审
//  3. severity >= LowConfidenceSeverity 且 severity_confidence < LowConfidence → 人工待审
//  4. 其余 → 发布
func RouteModeration(a Assessment, t ModerationThresholds) ModerationDecision {
	review := false
	for h, p := range a.Risks {
		switch {
		case p >= t.Action:
			if !reviewOnlyHazards[h] && a.Severity >= t.SeverityBlock {
				return ModerationReject // 最高优先级，可以提前返回
			}
			review = true
		case p >= t.Review:
			review = true
		}
	}
	if a.Severity >= t.LowConfidenceSeverity && a.SeverityConfidence < t.LowConfidence {
		review = true
	}
	if review {
		return ModerationReview
	}
	return ModerationPublish
}

// TargetStatus 分流结果对应的帖子目标状态；ok=false 表示不改状态（人工待审留在审核中）。
func (d ModerationDecision) TargetStatus() (status int16, ok bool) {
	switch d {
	case ModerationPublish, ModerationFailOpen:
		return PostStatusPublished, true
	case ModerationReject:
		return PostStatusRejected, true
	default:
		return 0, false
	}
}
