// conf_discover.go：配置段 discover，聚合进 AppConfig（conf.go）。

package conf

// Discover 发现页配置（discover 领域 + DiscoverPoolSyncer）。设计见 docs/discover-design.md §九。
type Discover struct {
	RefreshIntervalMinutes int `mapstructure:"refresh_interval_minutes" json:"refresh_interval_minutes" yaml:"refresh_interval_minutes"` // DiscoverPoolSyncer 周期(分钟)
	PoolSize               int `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size"`                                              // 每分区候选池大小(采样数)
	MinPoolPosts           int `mapstructure:"min_pool_posts" json:"min_pool_posts" yaml:"min_pool_posts"`                               // 反气泡剔除后帖子最少保留数(不足则不排除重采)
	TTLMinutes             int `mapstructure:"ttl_minutes" json:"ttl_minutes" yaml:"ttl_minutes"`                                        // 候选池 + token TTL(分钟)
	DefaultSize            int `mapstructure:"default_size" json:"default_size" yaml:"default_size"`                                     // 接口默认 size
	MaxSize                int `mapstructure:"max_size" json:"max_size" yaml:"max_size"`                                                 // 接口 size 上限
	SeedLimit              int `mapstructure:"seed_limit" json:"seed_limit" yaml:"seed_limit"`                                           // 反气泡读取已交互帖上限(liked/collected/viewed 各)
}
