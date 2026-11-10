package conf

import "testing"

func TestFollow_MaxFollowingOrDefault(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, DefaultFollowMaxFollowing},
		{-1, DefaultFollowMaxFollowing},
		{1, 1},
		{5000, 5000},
	} {
		if got := (Follow{MaxFollowing: tc.in}).MaxFollowingOrDefault(); got != tc.want {
			t.Errorf("MaxFollowing=%d: got %d, want %d", tc.in, got, tc.want)
		}
	}
}
