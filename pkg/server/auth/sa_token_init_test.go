package auth

import "testing"

// TestRenewMaxRefresh 续期阈值 = Timeout - renew_interval，边界回落（#131）。
func TestRenewMaxRefresh(t *testing.T) {
	cases := []struct {
		name              string
		timeout, interval int64
		want              int64
	}{
		{"默认 3 天 + 10 分钟", 259200, 600, 258600},
		{"未配置间隔回落 600", 259200, 0, 258600},
		{"负间隔回落 600", 259200, -5, 258600},
		{"间隔等于有效期退化为一半", 600, 600, 300},
		{"间隔大于有效期退化为一半", 600, 3600, 300},
		{"极短有效期阈值至少 1", 1, 600, 1},
		{"永不过期不限制", -1, 600, -1},
		{"有效期 0 不限制", 0, 600, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenewMaxRefresh(tc.timeout, tc.interval); got != tc.want {
				t.Fatalf("RenewMaxRefresh(%d, %d) = %d, want %d", tc.timeout, tc.interval, got, tc.want)
			}
		})
	}
}
