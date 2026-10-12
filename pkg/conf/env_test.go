package conf

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const testYAML = `
server:
  port: 8888
pgsql:
  path: 127.0.0.1
  password: from-yaml
redpanda:
  brokers: ["127.0.0.1:19092"]
`

func loadWithEnv(t *testing.T, env map[string]string) *AppConfig {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(testYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := Config
	t.Cleanup(func() { Config = prev })
	Config = nil
	initFromFile(path)
	return Config
}

func TestEnvOverride_FileLoader(t *testing.T) {
	c := loadWithEnv(t, map[string]string{
		"QUBAR_PGSQL_PASSWORD":        "from-env",
		"QUBAR_SERVER_PORT":           "9999",
		"QUBAR_SECURITY_DATA_KEY":     "key-not-in-yaml", // YAML 中不存在的 key 也能覆盖
		"QUBAR_REDPANDA_BROKERS":      "a:9092,b:9092",
		"QUBAR_NOTICE_STREAM_ENABLED": "true",
	})
	if c.Pgsql.Password != "from-env" {
		t.Errorf("pgsql.password = %q", c.Pgsql.Password)
	}
	if c.Server.Port != 9999 {
		t.Errorf("server.port = %d", c.Server.Port)
	}
	if c.Security.DataKey != "key-not-in-yaml" {
		t.Errorf("security.data_key = %q", c.Security.DataKey)
	}
	if len(c.Redpanda.Brokers) != 2 || c.Redpanda.Brokers[1] != "b:9092" {
		t.Errorf("redpanda.brokers = %v", c.Redpanda.Brokers)
	}
	if !c.NoticeStream.Enabled {
		t.Error("notice_stream.enabled not overridden")
	}
	if c.Pgsql.Path != "127.0.0.1" {
		t.Errorf("unset env must keep YAML value, pgsql.path = %q", c.Pgsql.Path)
	}
}

func TestEnvOverride_NacosContent(t *testing.T) {
	t.Setenv("QUBAR_PGSQL_PASSWORD", "from-env")
	prev := Config
	t.Cleanup(func() { Config = prev })
	Config = nil
	if err := feedViper(testYAML); err != nil {
		t.Fatal(err)
	}
	if Config.Pgsql.Password != "from-env" {
		t.Errorf("pgsql.password = %q", Config.Pgsql.Password)
	}
}

func TestConfigKeys_CoversNestedLeaves(t *testing.T) {
	keys := map[string]bool{}
	for _, k := range configKeys(reflectTypeOfAppConfig(), "") {
		keys[k] = true
	}
	for _, want := range []string{"pgsql.password", "security.password_hash.memory", "recommend.cf.enabled", "oauth.google.client_secret", "redpanda.brokers"} {
		if !keys[want] {
			t.Errorf("missing key %s", want)
		}
	}
}

func reflectTypeOfAppConfig() reflect.Type { return reflect.TypeOf(AppConfig{}) }
