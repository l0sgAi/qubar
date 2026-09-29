package conf

import (
	"fmt"
	"strings"
)

// minDataKeyLen security.data_key 建议最小长度（openssl rand -base64 32 输出 44 字符）。
const minDataKeyLen = 32

// SecurityWarnings 返回不适合生产的配置项（l0sgAi/qubar#49）。启动时逐条打 WARN，不阻断启动。
func (c *AppConfig) SecurityWarnings() []string {
	if c == nil {
		return nil
	}
	var out []string
	switch k := c.Security.DataKey; {
	case k == "":
		out = append(out, "security.data_key is empty: AI agent api_key encryption is disabled")
	case len(k) < minDataKeyLen:
		out = append(out, fmt.Sprintf("security.data_key is shorter than %d chars: generate one with `openssl rand -base64 32`", minDataKeyLen))
	}
	if c.Pgsql.LogMode == "debug" {
		out = append(out, "pgsql.log_mode=debug logs every SQL statement (user data, volume); use error in production")
	}
	if strings.EqualFold(c.Log.Level, "debug") {
		out = append(out, "log.level=debug; use info in production")
	}
	if strings.Contains(c.Pgsql.Config, "sslmode=disable") && !isLocalHost(c.Pgsql.Path) {
		out = append(out, fmt.Sprintf("pgsql.config has sslmode=disable for non-local host %s", c.Pgsql.Path))
	}
	for _, o := range c.CORS.AllowedOrigins {
		if origin := strings.ToLower(o); strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1") {
			out = append(out, fmt.Sprintf("cors.allowed_origins contains local origin %q; remove it in production", o))
		}
	}
	return out
}

func isLocalHost(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	return h == "" || h == "localhost" || h == "127.0.0.1" || h == "::1"
}
