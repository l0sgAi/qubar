package conf

import (
	"reflect"
	"strings"

	"github.com/spf13/viper"
)

// EnvPrefix 环境变量覆盖前缀（l0sgAi/qubar#51）。
//
// 任意配置项都可用环境变量覆盖：前缀 + 路径大写、"." 换成 "_"。例：
//
//	pgsql.password          → QUBAR_PGSQL_PASSWORD
//	security.data_key       → QUBAR_SECURITY_DATA_KEY
//	redpanda.brokers        → QUBAR_REDPANDA_BROKERS=host1:9092,host2:9092（切片用逗号分隔）
//
// 优先级：环境变量 > Nacos / 本地 YAML > 代码默认值。便于容器 / 编排平台从密钥管理服务注入，
// 不再需要把密钥写进配置文件。
const EnvPrefix = "QUBAR"

// bindEnv 给 viper 开启环境变量覆盖，并为 AppConfig 的每个叶子字段显式 BindEnv——
// 仅 AutomaticEnv 时 Unmarshal 只认 YAML 里出现过的 key，YAML 缺省的字段会覆盖不到。
func bindEnv(v *viper.Viper) {
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	for _, key := range configKeys(reflect.TypeOf(AppConfig{}), "") {
		_ = v.BindEnv(key) // 仅在 key 为空时报错，这里不会发生
	}
}

// configKeys 按 mapstructure tag 展开结构体的全部叶子路径（如 "pgsql.password"）。
func configKeys(t reflect.Type, prefix string) []string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("mapstructure"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}
		if f.Type.Kind() == reflect.Struct {
			keys = append(keys, configKeys(f.Type, key)...)
			continue
		}
		keys = append(keys, key)
	}
	return keys
}
