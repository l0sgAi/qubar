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
}

func TestModeration_WithDefaultsKeepsExplicitValues(t *testing.T) {
	no := false
	in := Moderation{
		Enabled: true, APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "jev-1.14.0",
		TimeoutSec: 3, MaxRetries: 5, ContentMaxRunes: 100, FailOpen: &no,
		ReviewThreshold: 0.4, ActionThreshold: 0.8, SeverityBlock: 2, LowConfidence: 0.3, LowConfidenceSeverity: 1.2,
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
