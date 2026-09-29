package auth

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

// 生成的 URL 必须被 go-redis 解析成「无用户名 + 原样密码」，含特殊字符也不能错位。
func TestBuildRedisURL(t *testing.T) {
	for _, pw := range []string{"", "plain", "p@ss:w/rd#1?x=y%", "a b"} {
		raw := buildRedisURL("redis.internal", 6379, 2, pw)
		opts, err := redis.ParseURL(raw)
		if err != nil {
			t.Fatalf("pw=%q url=%q: %v", pw, raw, err)
		}
		if opts.Username != "" || opts.Password != pw {
			t.Errorf("pw=%q: got username=%q password=%q (url %q)", pw, opts.Username, opts.Password, raw)
		}
		if opts.Addr != "redis.internal:6379" || opts.DB != 2 {
			t.Errorf("pw=%q: addr=%q db=%d", pw, opts.Addr, opts.DB)
		}
	}
}
