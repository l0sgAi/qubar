package conf

import (
	"strings"
	"testing"
)

func TestSecurityWarnings_SafeConfigIsQuiet(t *testing.T) {
	c := &AppConfig{}
	c.Security.DataKey = strings.Repeat("k", 44)
	c.Pgsql.LogMode = "error"
	c.Pgsql.Path = "db.internal"
	c.Pgsql.Config = "sslmode=require"
	c.Log.Level = "info"
	c.CORS.AllowedOrigins = []string{"https://qubar.site"}
	if w := c.SecurityWarnings(); len(w) != 0 {
		t.Fatalf("want no warnings, got %v", w)
	}
}

func TestSecurityWarnings_FlagsDevDefaults(t *testing.T) {
	c := &AppConfig{}
	c.Security.DataKey = "TEST_DATA_KEY_1234567890"
	c.Pgsql.LogMode = "debug"
	c.Pgsql.Path = "10.0.0.5"
	c.Pgsql.Config = "sslmode=disable TimeZone=Asia/Shanghai"
	c.Log.Level = "DEBUG"
	c.CORS.AllowedOrigins = []string{"https://qubar.site", "http://localhost:*", "http://127.0.0.1:*"}

	w := strings.Join(c.SecurityWarnings(), "\n")
	for _, want := range []string{"data_key is shorter", "log_mode=debug", "log.level=debug", "sslmode=disable", "localhost", "127.0.0.1"} {
		if !strings.Contains(w, want) {
			t.Errorf("missing warning %q in:\n%s", want, w)
		}
	}
}

func TestSecurityWarnings_EmptyDataKeyAndLocalDB(t *testing.T) {
	c := &AppConfig{}
	c.Pgsql.Path = "127.0.0.1"
	c.Pgsql.Config = "sslmode=disable"
	w := c.SecurityWarnings()
	if len(w) != 1 || !strings.Contains(w[0], "data_key is empty") {
		t.Fatalf("want only empty data_key warning (local sslmode=disable is fine), got %v", w)
	}
}
