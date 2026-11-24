package conf

import "testing"

func TestMessageSendRate_OrDefault(t *testing.T) {
	for _, tc := range []struct {
		in                        MessageSendRate
		wantMin, wantDay, wantNew int
	}{
		{MessageSendRate{}, DefaultMessageSendPerMinute, DefaultMessageSendPerDay, DefaultMessageNewConversationsPerDay},
		{MessageSendRate{PerMinute: -1, PerDay: -1, NewConversationsPerDay: -1},
			DefaultMessageSendPerMinute, DefaultMessageSendPerDay, DefaultMessageNewConversationsPerDay},
		{MessageSendRate{PerMinute: 3, PerDay: 7, NewConversationsPerDay: 1}, 3, 7, 1},
	} {
		if got := tc.in.PerMinuteOrDefault(); got != tc.wantMin {
			t.Errorf("%+v PerMinute: got %d, want %d", tc.in, got, tc.wantMin)
		}
		if got := tc.in.PerDayOrDefault(); got != tc.wantDay {
			t.Errorf("%+v PerDay: got %d, want %d", tc.in, got, tc.wantDay)
		}
		if got := tc.in.NewConversationsPerDayOrDefault(); got != tc.wantNew {
			t.Errorf("%+v NewConversationsPerDay: got %d, want %d", tc.in, got, tc.wantNew)
		}
	}
}
