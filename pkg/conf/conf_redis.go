// conf_redis.go：配置段 redis，聚合进 AppConfig（conf.go）。

package conf

type Redis struct {
	Host     string `mapstructure:"host" json:"host" yaml:"host"`
	Port     int    `mapstructure:"port" json:"port" yaml:"port"`
	Password string `mapstructure:"password" json:"password" yaml:"password"`
	D        int    `mapstructure:"db" json:"db" yaml:"db"`
	PoolSize int    `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size"`
}
