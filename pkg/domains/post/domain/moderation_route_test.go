package domain

import "testing"

// 与 conf.Moderation 默认值一致（domain 不 import conf，这里写死）。
var testThresholds = ModerationThresholds{
	Review:                0.35,
	Action:                0.70,
	SeverityBlock:         1.5,
	LowConfidence:         0.5,
	LowConfidenceSeverity: 1.0,
}

// safeAssessment 全部风险接近 0、severity 0 且高置信度。
func safeAssessment() Assessment {
	risks := make(map[Hazard]float64, len(Hazards))
	for _, h := range Hazards {
		risks[h] = 0.01
	}
	return Assessment{Risks: risks, Severity: 0.1, SeverityConfidence: 0.9}
}

func TestRouteModeration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(a *Assessment)
		want   ModerationDecision
	}{
		{"all low risks publish", func(*Assessment) {}, ModerationPublish},
		{"just below review threshold publishes", func(a *Assessment) { a.Risks[HazardSpamAd] = 0.349 }, ModerationPublish},

		// 规则 1：任一风险 >= action_threshold
		{"action + high severity rejects", func(a *Assessment) {
			a.Risks[HazardSexual], a.Severity = 0.9, 2.0
		}, ModerationReject},
		{"action at exact thresholds rejects", func(a *Assessment) {
			a.Risks[HazardIllegal], a.Severity = 0.70, 1.5
		}, ModerationReject},
		{"action + severity below block goes to review", func(a *Assessment) {
			a.Risks[HazardHarassment], a.Severity = 0.9, 1.49
		}, ModerationReview},
		{"self_harm above action only reviews", func(a *Assessment) {
			a.Risks[HazardSelfHarm], a.Severity = 0.99, 3.0
		}, ModerationReview},
		{"manipulation above action only reviews", func(a *Assessment) {
			a.Risks[HazardManipulation], a.Severity = 0.99, 3.0
		}, ModerationReview},

		// 规则 2：任一风险 >= review_threshold
		{"review threshold reached goes to review", func(a *Assessment) { a.Risks[HazardPrivacy] = 0.35 }, ModerationReview},
		{"between review and action goes to review", func(a *Assessment) {
			a.Risks[HazardViolenceTerror], a.Severity = 0.6, 3.0
		}, ModerationReview},

		// 规则 3：severity 高但置信度低
		{"low confidence on high severity goes to review", func(a *Assessment) {
			a.Severity, a.SeverityConfidence = 1.0, 0.49
		}, ModerationReview},
		{"low confidence on low severity publishes", func(a *Assessment) {
			a.Severity, a.SeverityConfidence = 0.99, 0.1
		}, ModerationPublish},
		{"confident high severity without risk publishes", func(a *Assessment) {
			a.Severity, a.SeverityConfidence = 2.0, 0.5
		}, ModerationPublish},

		// 优先级：驳回 > 人工待审 > 发布
		{"reject wins over self_harm review", func(a *Assessment) {
			a.Risks[HazardSelfHarm], a.Risks[HazardSexual], a.Severity = 0.95, 0.95, 2.5
		}, ModerationReject},
		{"reject wins over low confidence review", func(a *Assessment) {
			a.Risks[HazardSpamAd], a.Severity, a.SeverityConfidence = 0.8, 2.0, 0.1
		}, ModerationReject},
		{"review wins over publish", func(a *Assessment) {
			a.Risks[HazardManipulation] = 0.4
		}, ModerationReview},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := safeAssessment()
			tc.mutate(&a)
			if got := RouteModeration(a, testThresholds); got != tc.want {
				t.Fatalf("RouteModeration = %s, want %s (risks=%v severity=%.2f conf=%.2f)",
					got, tc.want, a.Risks, a.Severity, a.SeverityConfidence)
			}
		})
	}
}

func TestModerationDecision_TargetStatus(t *testing.T) {
	cases := []struct {
		d      ModerationDecision
		status int16
		ok     bool
	}{
		{ModerationPublish, PostStatusPublished, true},
		{ModerationFailOpen, PostStatusPublished, true},
		{ModerationReject, PostStatusRejected, true},
		{ModerationReview, 0, false},
		{ModerationReviewPublish, PostStatusPublished, true},
		{ModerationReportDismiss, 0, false},
		{ModerationReportRemove, 0, false},
	}
	for _, tc := range cases {
		status, ok := tc.d.TargetStatus()
		if status != tc.status || ok != tc.ok {
			t.Fatalf("%s.TargetStatus() = (%d, %v), want (%d, %v)", tc.d, status, ok, tc.status, tc.ok)
		}
	}
}
