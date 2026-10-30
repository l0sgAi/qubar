// Package middleware 的访问日志查询参数打码单元测试：邀请 token 不得以明文进入访问日志（#169）。
package middleware

import "testing"

func TestRedactQuery(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"size=20", "size=20"},
		{"token=abc", "token=REDACTED"},
		{"a=1&token=abc-_xyz&b=2", "a=1&token=REDACTED&b=2"},
		{"TOKEN=abc", "TOKEN=REDACTED"},
		{"%74oken=abc", "token=REDACTED"},
		{"token_type=x&notoken=y", "token_type=x&notoken=y"},
		{"token", "token"},
	}
	for _, tt := range tests {
		if got := redactQuery(tt.in); got != tt.want {
			t.Errorf("redactQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
