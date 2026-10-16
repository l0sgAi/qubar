// conf_server.go：配置段 server / cors / app / log，聚合进 AppConfig（conf.go）。

package conf

type Server struct {
	Port int    `mapstructure:"port" json:"port" yaml:"port"`
	Mode string `mapstructure:"mode" json:"mode" yaml:"mode"`
}

type CORS struct {
	AllowedOrigins []string `mapstructure:"allowed_origins" json:"allowed_origins" yaml:"allowed_origins"`
}

type App struct {
	Name    string `mapstructure:"name" json:"name" yaml:"name"`
	Version string `mapstructure:"version" json:"version" yaml:"version"`
}

type Log struct {
	Level    string `mapstructure:"level" json:"level" yaml:"level"`
	Format   string `mapstructure:"format" json:"format" yaml:"format"`
	Director string `mapstructure:"director" json:"director" yaml:"director"`
}
