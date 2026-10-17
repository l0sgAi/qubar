// conf_trending.go：配置段 trending，聚合进 AppConfig（conf.go）。

package conf

// Trending 热点榜配置（trending 领域 + TrendingRankSyncer）。设计见 docs/trending-design.md §九。
type Trending struct {
	FlushIntervalMinutes int      `mapstructure:"flush_interval_minutes" json:"flush_interval_minutes" yaml:"flush_interval_minutes"` // TrendingRankSyncer 周期(分钟)
	TopN                 int      `mapstructure:"top_n" json:"top_n" yaml:"top_n"`                                                    // 每个 ZSET 榜单保留条数(> max_size 留翻页余量)
	Windows              []string `mapstructure:"windows" json:"windows" yaml:"windows"`                                              // 启用的时间窗(24h/7d)
	DefaultSize          int      `mapstructure:"default_size" json:"default_size" yaml:"default_size"`                               // 接口默认 size
	MaxSize              int      `mapstructure:"max_size" json:"max_size" yaml:"max_size"`                                           // 接口 size 上限
}
