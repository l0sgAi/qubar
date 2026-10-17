// conf_recommend.go：配置段 recommend（feed / cf），聚合进 AppConfig（conf.go）。

package conf

// Recommend 推荐流配置（首页「推荐」tab 召回/排序参数）。
type Recommend struct {
	CF   CF   `mapstructure:"cf" json:"cf" yaml:"cf"`
	Feed Feed `mapstructure:"feed" json:"feed" yaml:"feed"`
}

// Feed 推荐流候选池 + 多路召回配额配置。
type Feed struct {
	PoolSize                  int  `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size"`                                                          // 候选池大小（每路按配额比例填充）
	TTLMinutes                int  `mapstructure:"ttl_minutes" json:"ttl_minutes" yaml:"ttl_minutes"`                                                    // 候选池 TTL(分钟)
	QuotaC1                   int  `mapstructure:"quota_c1" json:"quota_c1" yaml:"quota_c1"`                                                             // 兴趣圈子热门配额(百分比)
	QuotaC2                   int  `mapstructure:"quota_c2" json:"quota_c2" yaml:"quota_c2"`                                                             // 全局热门配额(百分比)
	QuotaC3                   int  `mapstructure:"quota_c3" json:"quota_c3" yaml:"quota_c3"`                                                             // 行为圈子配额(百分比)
	QuotaC4                   int  `mapstructure:"quota_c4" json:"quota_c4" yaml:"quota_c4"`                                                             // 最新配额(百分比)
	QuotaC5                   int  `mapstructure:"quota_c5" json:"quota_c5" yaml:"quota_c5"`                                                             // CF 相似配额(百分比)
	ExcludeInteracted         bool `mapstructure:"exclude_interacted" json:"exclude_interacted" yaml:"exclude_interacted"`                               // 剔除已点赞/收藏/浏览过的帖
	InterestCirclesTTLMinutes int  `mapstructure:"interest_circles_ttl_minutes" json:"interest_circles_ttl_minutes" yaml:"interest_circles_ttl_minutes"` // C3 行为圈子缓存 TTL(分钟)
}

// CF item-based 协同过滤配置。设计见 docs/cf-item-based-design.md。
type CF struct {
	Enabled               bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`                                                 // 是否启用 CF（灌数 + Syncer 全开关）
	InteractionWindowDays int  `mapstructure:"interaction_window_days" json:"interaction_window_days" yaml:"interaction_window_days"` // 共现计算回溯窗口(天)
	CandidateFreshDays    int  `mapstructure:"candidate_fresh_days" json:"candidate_fresh_days" yaml:"candidate_fresh_days"`          // 候选帖创建时间窗(天)，防爆
	MinCooccur            int  `mapstructure:"min_cooccur" json:"min_cooccur" yaml:"min_cooccur"`                                     // 最小共现次数(砍噪声)
	TopK                  int  `mapstructure:"topk" json:"topk" yaml:"topk"`                                                          // 每帖保留相似帖数
	SeedCollect           int  `mapstructure:"seed_collect" json:"seed_collect" yaml:"seed_collect"`                                  // C5 召回取收藏 seed 数(P2)
	SeedLike              int  `mapstructure:"seed_like" json:"seed_like" yaml:"seed_like"`                                           // C5 召回取点赞 seed 数(P2)
	RecallTop             int  `mapstructure:"recall_top" json:"recall_top" yaml:"recall_top"`                                        // C5 输出候选数(P2)
	ZsetTTLHours          int  `mapstructure:"zset_ttl_hours" json:"zset_ttl_hours" yaml:"zset_ttl_hours"`                            // cf:item ZSET TTL(小时)
	SyncIntervalHours     int  `mapstructure:"sync_interval_hours" json:"sync_interval_hours" yaml:"sync_interval_hours"`             // ItemCFSyncer 运行间隔(小时)
	CleanupDays           int  `mapstructure:"cleanup_days" json:"cleanup_days" yaml:"cleanup_days"`                                  // interaction 行保留天数(超出删除)
}
