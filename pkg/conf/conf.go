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

// 新增：对应 yaml 中的 oauth 层级
type Oauth struct {
	// ProxyURL OAuth 出站（换 token / 拉用户信息）使用的 HTTP 代理。
	// 为空则直连。本地开发在网络受限时可配置（如 http://127.0.0.1:6268）。
	ProxyURL string `mapstructure:"proxy_url" json:"proxy_url" yaml:"proxy_url"`

	Google    Google    `mapstructure:"google" json:"google" yaml:"google"`
	Github    Github    `mapstructure:"github" json:"github" yaml:"github"`
	Microsoft Microsoft `mapstructure:"microsoft" json:"microsoft" yaml:"microsoft"`
}

// 新增:对应 yaml 中的 google 层级
type Google struct {
	ClientID            string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret        string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL         string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url" json:"frontend_redirect_url" yaml:"frontend_redirect_url"`
}

// 新增:对应 yaml 中的 github 层级
type Github struct {
	ClientID            string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret        string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL         string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url" json:"frontend_redirect_url" yaml:"frontend_redirect_url"`
}

// Microsoft 对应 yaml 中的 microsoft 层级
type Microsoft struct {
	ClientID            string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret        string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL         string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url" json:"frontend_redirect_url" yaml:"frontend_redirect_url"`
}

type Pgsql struct {
	Path         string `mapstructure:"path" json:"path" yaml:"path"`
	Port         string `mapstructure:"port" json:"port" yaml:"port"`
	Config       string `mapstructure:"config" json:"config" yaml:"config"`
	DbName       string `mapstructure:"db_name" json:"db_name" yaml:"db_name"`
	Username     string `mapstructure:"username" json:"username" yaml:"username"`
	Password     string `mapstructure:"password" json:"password" yaml:"password"`
	MaxIdleConns int    `mapstructure:"max_idle_conns" json:"max_idle_conns" yaml:"max_idle_conns"`
	MaxOpenConns int    `mapstructure:"max_open_conns" json:"max_open_conns" yaml:"max_open_conns"`
	LogMode      string `mapstructure:"log_mode" json:"log_mode" yaml:"log_mode"`
}

type Redis struct {
	Host     string `mapstructure:"host" json:"host" yaml:"host"`
	Port     int    `mapstructure:"port" json:"port" yaml:"port"`
	Password string `mapstructure:"password" json:"password" yaml:"password"`
	D        int    `mapstructure:"db" json:"db" yaml:"db"`
	PoolSize int    `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size"`
}

type SaToken struct {
	TokenName     string `mapstructure:"token_name" json:"token_name" yaml:"token_name"`
	Timeout       int    `mapstructure:"timeout" json:"timeout" yaml:"timeout"`
	ActiveTimeout int    `mapstructure:"active_timeout" json:"active_timeout" yaml:"active_timeout"`
	IsConcurrent  bool   `mapstructure:"is_concurrent" json:"is_concurrent" yaml:"is_concurrent"`
	IsShare       bool   `mapstructure:"is_share" json:"is_share" yaml:"is_share"`
}

// S3 AWS S3 对象存储配置
type S3 struct {
	AccessKeyID      string `mapstructure:"access_key_id" json:"access_key_id" yaml:"access_key_id"`
	SecretAccessKey  string `mapstructure:"secret_access_key" json:"secret_access_key" yaml:"secret_access_key"`
	Region           string `mapstructure:"region" json:"region" yaml:"region"`
	Bucket           string `mapstructure:"bucket" json:"bucket" yaml:"bucket"`
	Endpoint         string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`
	PresignURLExpire int    `mapstructure:"presign_url_expire" json:"presign_url_expire" yaml:"presign_url_expire"`
	CloudfrontDomain string `mapstructure:"cloudfront_domain" json:"cloudfront_domain" yaml:"cloudfront_domain"`
}

// Elasticsearch Elasticsearch 配置
type Elasticsearch struct {
	URL             string `mapstructure:"url" json:"url" yaml:"url"`
	IndexPrefix     string `mapstructure:"index_prefix" json:"index_prefix" yaml:"index_prefix"`
	RefreshInterval string `mapstructure:"refresh_interval" json:"refresh_interval" yaml:"refresh_interval"`
}

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
}

// Notice 消息中心配置。设计见 docs/notice-design.md。
type Notice struct {
	MentionMax int `mapstructure:"mention_max" json:"mention_max" yaml:"mention_max"` // 单条内容 @ 提及用户上限(<=0 兜底 10)
}

// NoticeStream SSE 未读数推流配置。设计见 docs/design/sse-notification-design.md §五。
// 数值字段 <=0 时用代码内默认值（项目惯例）。
type NoticeStream struct {
	Enabled         bool `mapstructure:"enabled" json:"enabled" yaml:"enabled"`                                  // false 时不注册路由（灰度开关）
	HeartbeatSec    int  `mapstructure:"heartbeat_sec" json:"heartbeat_sec" yaml:"heartbeat_sec"`                // 心跳+token复检周期(秒)，<=0 兜底 25
	MaxConnsPerUser int  `mapstructure:"max_conns_per_user" json:"max_conns_per_user" yaml:"max_conns_per_user"` // 每用户连接上限，<=0 兜底 5
	CoalesceMs      int  `mapstructure:"coalesce_ms" json:"coalesce_ms" yaml:"coalesce_ms"`                      // 合并推送窗口(毫秒)，<=0 兜底 1000
	RetryMs         int  `mapstructure:"retry_ms" json:"retry_ms" yaml:"retry_ms"`                               // 写给客户端的重连建议(毫秒)，<=0 兜底 5000
}

// Hot 热度计算配置（权重 + 上限）。
type Hot struct {
	Weight HotWeight `mapstructure:"weight" json:"weight" yaml:"weight"`
	Cap    HotCap    `mapstructure:"cap" json:"cap" yaml:"cap"`
}

// HotWeight 各互动事件的热度权重（hot Δ = weight × 方向）。
type HotWeight struct {
	PostLike    int `mapstructure:"post_like" json:"post_like" yaml:"post_like"`          // 帖子点赞
	PostCollect int `mapstructure:"post_collect" json:"post_collect" yaml:"post_collect"` // 帖子收藏
	PostShare   int `mapstructure:"post_share" json:"post_share" yaml:"post_share"`       // 帖子分享（TODO: 分享功能未实现）
	Comment     int `mapstructure:"comment" json:"comment" yaml:"comment"`                // 评论
	CommentLike int `mapstructure:"comment_like" json:"comment_like" yaml:"comment_like"` // 评论点赞
}

// HotCap 热度贡献上限（per-post，防刷分）。
// 不变式：cap 必须为对应 weight 的整数倍，否则 undo 与 clamp 边界产生漂移。
type HotCap struct {
	Comment     int `mapstructure:"comment" json:"comment" yaml:"comment"`                // 评论 hot 贡献上限
	CommentLike int `mapstructure:"comment_like" json:"comment_like" yaml:"comment_like"` // 评论点赞 hot 贡献上限
}

// Recommend 推荐流配置（首页「推荐」tab 召回/排序参数）。
type Recommend struct {
	CF   CF   `mapstructure:"cf" json:"cf" yaml:"cf"`
	Feed Feed `mapstructure:"feed" json:"feed" yaml:"feed"`
}

// AiAgent AI 机器人回复执行链路配置（设计见 docs/agent-reply-design.md）。
// 各字段 <=0 时用代码内默认值（30s / 4000 字符 / 3）。
type AiAgent struct {
	TimeoutSec       int `mapstructure:"timeout_sec" json:"timeout_sec" yaml:"timeout_sec"`                   // LLM 单次调用超时(秒)，默认 30
	MaxContentChars  int `mapstructure:"max_content_chars" json:"max_content_chars" yaml:"max_content_chars"` // Prompt 各段(title/summary/评论)截断长度，默认 4000
	ReplyConcurrency int `mapstructure:"reply_concurrency" json:"reply_concurrency" yaml:"reply_concurrency"` // 关键词触发异步执行并发上限，默认 3
	// AllowPrivateBaseURL 允许 base_url 使用 http 及内网/环回地址（#48 防 SSRF 开关）。
	// 默认 false；仅本地开发或内网自建模型网关时开启，开启后圈主可让服务端访问内网。
	AllowPrivateBaseURL bool `mapstructure:"allow_private_base_url" json:"allow_private_base_url" yaml:"allow_private_base_url"`
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

// Trending 热点榜配置（trending 领域 + TrendingRankSyncer）。设计见 docs/trending-design.md §九。
type Trending struct {
	FlushIntervalMinutes int      `mapstructure:"flush_interval_minutes" json:"flush_interval_minutes" yaml:"flush_interval_minutes"` // TrendingRankSyncer 周期(分钟)
	TopN                 int      `mapstructure:"top_n" json:"top_n" yaml:"top_n"`                                                    // 每个 ZSET 榜单保留条数(> max_size 留翻页余量)
	Windows              []string `mapstructure:"windows" json:"windows" yaml:"windows"`                                              // 启用的时间窗(24h/7d)
	DefaultSize          int      `mapstructure:"default_size" json:"default_size" yaml:"default_size"`                               // 接口默认 size
	MaxSize              int      `mapstructure:"max_size" json:"max_size" yaml:"max_size"`                                           // 接口 size 上限
}

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

// Mailtrap 邮件发送配置
type Mailtrap struct {
	APIToken    string            `mapstructure:"api_token" json:"api_token" yaml:"api_token"`
	SenderEmail string            `mapstructure:"sender_email" json:"sender_email" yaml:"sender_email"`
	SenderName  string            `mapstructure:"sender_name" json:"sender_name" yaml:"sender_name"`
	APIURL      string            `mapstructure:"api_url" json:"api_url" yaml:"api_url"`
	Templates   MailtrapTemplates `mapstructure:"templates" json:"templates" yaml:"templates"`
}

// MailtrapTemplates Mailtrap 邮件模板配置
type MailtrapTemplates struct {
	VerificationCode MailtrapTemplate `mapstructure:"verification_code" json:"verification_code" yaml:"verification_code"`
	PasswordReset    MailtrapTemplate `mapstructure:"password_reset" json:"password_reset" yaml:"password_reset"`
}

// MailtrapTemplate 支持多语言的模板 UUID 映射
type MailtrapTemplate struct {
	Zh string `mapstructure:"zh" json:"zh" yaml:"zh"`
	En string `mapstructure:"en" json:"en" yaml:"en"`
}

// Security 安全相关配置（密码哈希、加密参数等）。
type Security struct {
	// PasswordHash 密码哈希配置。
	PasswordHash PasswordHash `mapstructure:"password_hash" json:"password_hash" yaml:"password_hash"`

	// DataKey 敏感字段应用层加密密钥（AES-256-GCM，SHA-256 派生）。
	// 用于 ai_agent.api_key 等入库前加密。为空时相关加密接口返回 ErrEmptyKey。
	DataKey string `mapstructure:"data_key" json:"data_key" yaml:"data_key"`
}

// PasswordHash 密码哈希算法参数（Argon2id）。
//
// OWASP 2024 推荐参数：time=2, memory=19MB, threads=1（最低）。
// 本项目默认 time=3, memory=64MB, threads=4，单次约 150ms，比推荐更强。
// 所有字段为 0 时使用默认值，便于配置文件不写时正常工作。
type PasswordHash struct {
	// Time Argon2id 迭代次数（time cost），建议 >=2，默认 3。
	Time uint32 `mapstructure:"time" json:"time" yaml:"time"`
	// Memory Argon2id 内存消耗（KiB），建议 >=19456 (19MB)，默认 65536 (64MB)。
	Memory uint32 `mapstructure:"memory" json:"memory" yaml:"memory"`
	// Threads Argon2id 并行度，建议 1-4，默认 4。
	Threads uint8 `mapstructure:"threads" json:"threads" yaml:"threads"`
	// KeyLen 输出哈希长度（字节），默认 32。
	KeyLen uint32 `mapstructure:"key_len" json:"key_len" yaml:"key_len"`
	// SaltLen 随机 salt 长度（字节），默认 16。
	SaltLen uint32 `mapstructure:"salt_len" json:"salt_len" yaml:"salt_len"`
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
