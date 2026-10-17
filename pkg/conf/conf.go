package conf

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var Config *AppConfig

// AppConfig 配置聚合根，字段对应 yaml 顶层段。各段结构体按段拆在 conf_<段名>.go；
// 新增配置段 = 新建 conf_xxx.go + 在此加一个字段（env 覆盖由 env.go 反射展开，无需登记）。
type AppConfig struct {
	Server        Server        `mapstructure:"server" json:"server" yaml:"server"`
	CORS          CORS          `mapstructure:"cors" json:"cors" yaml:"cors"`
	App           App           `mapstructure:"app" json:"app" yaml:"app"`
	Log           Log           `mapstructure:"log" json:"log" yaml:"log"`
	Pgsql         Pgsql         `mapstructure:"pgsql" json:"pgsql" yaml:"pgsql"`
	Oauth         Oauth         `mapstructure:"oauth" json:"oauth" yaml:"oauth"`
	Redis         Redis         `mapstructure:"redis" json:"redis" yaml:"redis"`
	SaToken       SaToken       `mapstructure:"sa_token" json:"sa_token" yaml:"sa_token"`
	S3            S3            `mapstructure:"s3" json:"s3" yaml:"s3"`
	Elasticsearch Elasticsearch `mapstructure:"elasticsearch" json:"elasticsearch" yaml:"elasticsearch"`
	Redpanda      Redpanda      `mapstructure:"redpanda" json:"redpanda" yaml:"redpanda"`
	Hot           Hot           `mapstructure:"hot" json:"hot" yaml:"hot"`
	Recommend     Recommend     `mapstructure:"recommend" json:"recommend" yaml:"recommend"`
	AiAgent       AiAgent       `mapstructure:"aiagent" json:"aiagent" yaml:"aiagent"`
	Trending      Trending      `mapstructure:"trending" json:"trending" yaml:"trending"`
	Discover      Discover      `mapstructure:"discover" json:"discover" yaml:"discover"`
	Notice        Notice        `mapstructure:"notice" json:"notice" yaml:"notice"`
	NoticeStream  NoticeStream  `mapstructure:"notice_stream" json:"notice_stream" yaml:"notice_stream"`
	Mailtrap      Mailtrap      `mapstructure:"mailtrap" json:"mailtrap" yaml:"mailtrap"`
	Security      Security      `mapstructure:"security" json:"security" yaml:"security"`
}

// InitConfig 是配置加载入口。
//
//	fallbackPath   — Nacos 不可用时的本地兜底配置文件(通过 -c 传入，默认 configs/config.yaml)。
//	bootstrapPath  — Nacos 引导文件(通过 -b 传入，默认 configs/bootstrap.yaml)。
//	                 为空或文件不存在时跳过 Nacos，直接加载 fallbackPath。
func InitConfig(fallbackPath, bootstrapPath string) {
	if bootstrapPath != "" {
		switch err := initFromNacos(bootstrapPath); {
		case err == nil:
			return // Nacos 加载成功，监听已注册
		case errors.Is(err, errNoBootstrap):
			// 静默回退：没有引导文件，直接用本地配置(开发者未接入 Nacos 的默认路径)
		default:
			log.Printf("[conf] Nacos load failed (%v); falling back to local %s", err, fallbackPath)
		}
	}
	initFromFile(fallbackPath)
}

// initFromFile 是本地文件加载器(含 fsnotify 热更新)，作为 Nacos 不可用时的兜底。
func initFromFile(path string) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	bindEnv(v)

	if err := v.ReadInConfig(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			panic(fmt.Errorf("config file %s not found: copy configs/config.example.yaml to %s and fill in real values (#49)", path, path))
		}
		panic(fmt.Errorf("fatal error config file: %s", err))
	}

	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("config file changed:", e.Name)
		if err := v.Unmarshal(&Config); err != nil {
			fmt.Println(err)
		}
	})

	if err := v.Unmarshal(&Config); err != nil {
		panic(err)
	}
}
