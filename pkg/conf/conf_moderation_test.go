package conf

import "testing"

func TestModeration_WithDefaults(t *testing.T) {
	got := Moderation{}.WithDefaults()
	if got.BaseURL != DefaultModerationBaseURL || got.Model != DefaultModerationModel ||
		got.TimeoutSec != DefaultModerationTimeoutSec || got.MaxRetries != DefaultModerationMaxRetries ||
		got.ContentMaxRunes != DefaultModerationContentMaxRunes {
		t.Fatalf("unexpected call defaults: %+v", got)
	}
	if got.ReviewThreshold != 0.35 || got.ActionThreshold != 0.70 || got.SeverityBlock != 1.5 ||
		got.LowConfidence != 0.5 || got.LowConfidenceSeverity != 1.0 {
		t.Fatalf("unexpected threshold defaults: %+v", got)
	}
	if got.FailOpen == nil || !*got.FailOpen || !got.FailOpenEnabled() {
		t.Fatal("fail_open should default to true")
	}
	if got.Active() {
		t.Fatal("disabled by default")
	}
	if got.ReviewAction != ModerationReviewActionPublish || !got.ReviewPublishes() ||
		got.ReportNotifyThreshold != 1 || got.ReportHideThreshold != 0 || got.ReportDailyLimit != 20 {
		t.Fatalf("unexpected report defaults: %+v", got)
	}
}

// review_action：只有显式 hold 才保留待审；非法值按默认 publish。
func TestModeration_ReviewAction(t *testing.T) {
	for in, want := range map[string]bool{"": true, "publish": true, "hold": false, "HOLD": true, "typo": true} {
		m := Moderation{ReviewAction: in}
		if got := m.ReviewPublishes(); got != want {
			t.Errorf("ReviewPublishes(%q) = %v, want %v", in, got, want)
		}
		if got := m.WithDefaults().ReviewPublishes(); got != want {
			t.Errorf("WithDefaults().ReviewPublishes(%q) = %v, want %v", in, got, want)
		}
	}
	if got := (Moderation{ReportHideThreshold: -3}).WithDefaults().ReportHideThreshold; got != 0 {
		t.Fatalf("negative report_hide_threshold = %d, want 0 (off)", got)
	}
}

func TestModeration_WithDefaultsKeepsExplicitValues(t *testing.T) {
	no := false
	in := Moderation{
		Enabled: true, APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "jev-1.14.0",
		TimeoutSec: 3, MaxRetries: 5, ContentMaxRunes: 100, FailOpen: &no,
		ReviewThreshold: 0.4, ActionThreshold: 0.8, SeverityBlock: 2, LowConfidence: 0.3, LowConfidenceSeverity: 1.2,
		ReviewAction: ModerationReviewActionHold, ReportNotifyThreshold: 3, ReportHideThreshold: 5, ReportDailyLimit: 7,
	}
	got := in.WithDefaults()
	if got.FailOpenEnabled() {
		t.Fatal("explicit fail_open=false must be kept")
	}
	got.FailOpen = in.FailOpen
	if got != in {
		t.Fatalf("explicit values changed:\nwant %+v\ngot  %+v", in, got)
	}
	if !got.Active() {
		t.Fatal("enabled + api_key should be active")
	}
}
