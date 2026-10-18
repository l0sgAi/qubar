// conf_elasticsearch.go：配置段 elasticsearch，聚合进 AppConfig（conf.go）。

package conf

// Elasticsearch Elasticsearch 配置
type Elasticsearch struct {
	URL             string `mapstructure:"url" json:"url" yaml:"url"`
	IndexPrefix     string `mapstructure:"index_prefix" json:"index_prefix" yaml:"index_prefix"`
	RefreshInterval string `mapstructure:"refresh_interval" json:"refresh_interval" yaml:"refresh_interval"`
}
