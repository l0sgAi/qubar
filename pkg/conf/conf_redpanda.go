// conf_redpanda.go：配置段 redpanda，聚合进 AppConfig（conf.go）。

package conf

// Redpanda Redpanda配置
type Redpanda struct {
	Brokers                   []string `mapstructure:"brokers" json:"brokers" yaml:"brokers"`
	Topic                     string   `mapstructure:"topic" json:"topic" yaml:"topic"`
	ConsumerGroup             string   `mapstructure:"consumer_group" json:"consumer_group" yaml:"consumer_group"`
	FlushInterval             int      `mapstructure:"flush_interval" json:"flush_interval" yaml:"flush_interval"`
	FlushMessages             int      `mapstructure:"flush_messages" json:"flush_messages" yaml:"flush_messages"`
	PostTopic                 string   `mapstructure:"post_topic" json:"post_topic" yaml:"post_topic"`
	PostConsumerGroup         string   `mapstructure:"post_consumer_group" json:"post_consumer_group" yaml:"post_consumer_group"`
	PostFlushInterval         int      `mapstructure:"post_flush_interval" json:"post_flush_interval" yaml:"post_flush_interval"`
	PostStatsTTL              int      `mapstructure:"post_stats_ttl" json:"post_stats_ttl" yaml:"post_stats_ttl"`
	LikeEventTopic            string   `mapstructure:"like_event_topic" json:"like_event_topic" yaml:"like_event_topic"`
	LikeEventConsumerGroup    string   `mapstructure:"like_event_consumer_group" json:"like_event_consumer_group" yaml:"like_event_consumer_group"`
	LikeEventFlushInterval    int      `mapstructure:"like_event_flush_interval" json:"like_event_flush_interval" yaml:"like_event_flush_interval"`
	CollectEventTopic         string   `mapstructure:"collect_event_topic" json:"collect_event_topic" yaml:"collect_event_topic"`
	CollectEventConsumerGroup string   `mapstructure:"collect_event_consumer_group" json:"collect_event_consumer_group" yaml:"collect_event_consumer_group"`
	CollectEventFlushInterval int      `mapstructure:"collect_event_flush_interval" json:"collect_event_flush_interval" yaml:"collect_event_flush_interval"`
	HistoryEventTopic         string   `mapstructure:"history_event_topic" json:"history_event_topic" yaml:"history_event_topic"`
	HistoryEventConsumerGroup string   `mapstructure:"history_event_consumer_group" json:"history_event_consumer_group" yaml:"history_event_consumer_group"`
	HistoryEventFlushInterval int      `mapstructure:"history_event_flush_interval" json:"history_event_flush_interval" yaml:"history_event_flush_interval"`

	PostHotTopic           string `mapstructure:"post_hot_topic" json:"post_hot_topic" yaml:"post_hot_topic"`                                  // 帖子热度增量 topic
	PostHotConsumerGroup   string `mapstructure:"post_hot_consumer_group" json:"post_hot_consumer_group" yaml:"post_hot_consumer_group"`       // 帖子热度消费者组
	PostHotFlushInterval   int    `mapstructure:"post_hot_flush_interval" json:"post_hot_flush_interval" yaml:"post_hot_flush_interval"`       // 帖子热度刷新间隔(分钟)
	PostHotFlushMessages   int    `mapstructure:"post_hot_flush_messages" json:"post_hot_flush_messages" yaml:"post_hot_flush_messages"`       // 帖子热度批量刷新条数阈值
	CircleHotFlushInterval int    `mapstructure:"circle_hot_flush_interval" json:"circle_hot_flush_interval" yaml:"circle_hot_flush_interval"` // 圈子热度落库间隔(分钟)
	CircleHotTTL           int    `mapstructure:"circle_hot_ttl" json:"circle_hot_ttl" yaml:"circle_hot_ttl"`                                  // 圈子热度累加器 key TTL(小时)

	PostInteractionTopic         string `mapstructure:"post_interaction_topic" json:"post_interaction_topic" yaml:"post_interaction_topic"`                            // 帖子互动事件 topic（CF 灌数）
	PostInteractionConsumerGroup string `mapstructure:"post_interaction_consumer_group" json:"post_interaction_consumer_group" yaml:"post_interaction_consumer_group"` // 帖子互动事件消费者组
	PostInteractionFlushInterval int    `mapstructure:"post_interaction_flush_interval" json:"post_interaction_flush_interval" yaml:"post_interaction_flush_interval"` // 帖子互动刷新间隔(分钟)
	PostInteractionFlushMessages int    `mapstructure:"post_interaction_flush_messages" json:"post_interaction_flush_messages" yaml:"post_interaction_flush_messages"` // 帖子互动批量刷新条数阈值

	NoticeEventTopic         string `mapstructure:"notice_event_topic" json:"notice_event_topic" yaml:"notice_event_topic"`                            // 通知事件 topic
	NoticeEventConsumerGroup string `mapstructure:"notice_event_consumer_group" json:"notice_event_consumer_group" yaml:"notice_event_consumer_group"` // 通知事件消费者组
	NoticeEventFlushInterval int    `mapstructure:"notice_event_flush_interval" json:"notice_event_flush_interval" yaml:"notice_event_flush_interval"` // 通知落库间隔(秒)

	// 帖子 AI 审核事件（#117）：仅 moderation 启用（enabled 且配置了 api_key）时创建生产者 / 消费者。
	PostModerationTopic         string `mapstructure:"post_moderation_topic" json:"post_moderation_topic" yaml:"post_moderation_topic"`                            // 帖子审核事件 topic
	PostModerationConsumerGroup string `mapstructure:"post_moderation_consumer_group" json:"post_moderation_consumer_group" yaml:"post_moderation_consumer_group"` // 帖子审核 worker 消费者组
}
