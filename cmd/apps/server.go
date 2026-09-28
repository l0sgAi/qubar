package apps

import (
	"fmt"
	"interestBar/pkg/composition"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/router"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/storage/elasticsearch"
	"interestBar/pkg/server/storage/redis"
	redpanda "interestBar/pkg/server/storage/redpanda"
	s3storage "interestBar/pkg/server/storage/s3"
	emailutil "interestBar/pkg/util/email"
	"interestBar/pkg/util/password"
	"strings"
)

func Run(configPath, bootstrapPath string) {
	// 1. Init Config (优先 Nacos，不可用时回退本地文件)
	conf.InitConfig(configPath, bootstrapPath)

	// 1.5 应用密码哈希参数（从 conf.Security.PasswordHash 注入）。
	// 任一字段为 0 时 password 包内部回退到默认值。
	ph := conf.Config.Security.PasswordHash
	password.SetParams(password.Params{
		Time:    ph.Time,
		Memory:  ph.Memory,
		Threads: ph.Threads,
		KeyLen:  ph.KeyLen,
		SaltLen: ph.SaltLen,
	})

	// 2. Init Logger
	logger.InitLogger()

	// 3. Init DB
	pgsql.InitDB()

	// 4. Init Redis for caching
	redisAddr := fmt.Sprintf("%s:%d", conf.Config.Redis.Host, conf.Config.Redis.Port)
	if err := redis.InitRedis(redisAddr, conf.Config.Redis.Password, conf.Config.Redis.D); err != nil {
		logger.Log.Fatal("Failed to initialize Redis: " + err.Error())
	}
	logger.Log.Info("Redis initialized successfully for caching")

	// 5. Init Sa-Token (includes Redis connection)
	if err := auth.InitSaToken(); err != nil {
		logger.Log.Fatal("Failed to initialize Sa-Token: " + err.Error())
	}

	// 6. Init S3 Client for file storage
	if err := s3storage.InitS3Client(); err != nil {
		logger.Log.Fatal("Failed to initialize S3 client: " + err.Error())
	}

	// 7. Init Elasticsearch for full-text search
	if err := elasticsearch.InitElasticsearch(); err != nil {
		logger.Log.Warn("Failed to initialize Elasticsearch: " + err.Error())
		logger.Log.Info("Running without Elasticsearch search functionality")
	}

	// 7.5 Init Mailtrap email client
	if err := emailutil.InitEmail(); err != nil {
		logger.Log.Warn("Failed to initialize Mailtrap email client: " + err.Error())
		logger.Log.Info("Running without email sending functionality")
	}

	// 8. Init Redpanda for async statistics persistence
	if err := redpanda.InitRedpandaProducer(); err != nil {
		logger.Log.Error(fmt.Sprintf("Failed to initialize Redpanda producer: %s", err.Error()))
		logger.Log.Warn("Circle statistics persistence to database is disabled. Redis cache will still be updated in real-time.")
		logger.Log.Info("To enable Redpanda:")
		logger.Log.Info("  1. Check Redpanda is running at: " + strings.Join(conf.Config.Redpanda.Brokers, ","))
		logger.Log.Info("  2. Verify network connectivity to Redpanda brokers")
		logger.Log.Info("  3. Ensure topic 'circle_statistics' exists or set AllowAutoTopicCreation=true")
	} else {
		logger.Log.Info("Redpanda producer initialized successfully")
		// 启动消费者处理统计信息聚合（PostgreSQL持久化）
		go redpanda.StartStatisticsConsumerWithRetry()
	}

	// 8.5 Init Post Stats Redpanda producer for async post statistics persistence
	if err := redpanda.InitPostStatsProducer(); err != nil {
		logger.Log.Error("Failed to initialize post stats producer: " + err.Error())
		logger.Log.Warn("Post statistics persistence to database is disabled. Redis cache will still be updated in real-time.")
	} else {
		logger.Log.Info("Post stats producer initialized successfully")
		go redpanda.StartPostStatisticsConsumerWithRetry()
	}

	// 8.7 Init Like event Redpanda producer
	if err := redpanda.InitLikeEventProducer(); err != nil {
		logger.Log.Error("Failed to initialize like event producer: " + err.Error())
		logger.Log.Warn("Like event persistence to database is disabled.")
	} else {
		logger.Log.Info("Like event producer initialized successfully")
		go redpanda.StartLikeEventConsumerWithRetry()
	}

	// 8.8 Init Like Lua scripts in Redis
	if err := redis.InitLikeLuaScripts(); err != nil {
		logger.Log.Error("Failed to load like Lua scripts: " + err.Error())
	} else {
		logger.Log.Info("Like Lua scripts loaded successfully")
	}

	// 8.10 Init Collect event Redpanda producer
	if err := redpanda.InitCollectEventProducer(); err != nil {
		logger.Log.Error("Failed to initialize collect event producer: " + err.Error())
		logger.Log.Warn("Collect event persistence to database is disabled.")
	} else {
		logger.Log.Info("Collect event producer initialized successfully")
		go redpanda.StartCollectEventConsumerWithRetry()
	}

	// 8.11 Init Collect Lua scripts in Redis
	if err := redis.InitCollectLuaScripts(); err != nil {
		logger.Log.Error("Failed to load collect Lua scripts: " + err.Error())
	} else {
		logger.Log.Info("Collect Lua scripts loaded successfully")
	}

	// 8.12 Init Notification event Redpanda producer（消息中心：点赞/收藏/评论/@提及 通知）
	if err := redpanda.InitNotificationEventProducer(); err != nil {
		logger.Log.Error("Failed to initialize notification event producer: " + err.Error())
		logger.Log.Warn("Notification persistence to database is disabled.")
	} else {
		logger.Log.Info("Notification event producer initialized successfully")
		go redpanda.StartNotificationEventConsumerWithRetry()
	}

	// 8.9 Init View Lua scripts in Redis
	if err := redis.InitViewLuaScripts(); err != nil {
		logger.Log.Error("Failed to load view Lua scripts: " + err.Error())
	} else {
		logger.Log.Info("View Lua scripts loaded successfully")
	}

	// 8.12 Init History event Redpanda producer
	if err := redpanda.InitHistoryEventProducer(); err != nil {
		logger.Log.Error("Failed to initialize history event producer: " + err.Error())
		logger.Log.Warn("History event persistence to database is disabled.")
	} else {
		logger.Log.Info("History event producer initialized successfully")
		go redpanda.StartHistoryEventConsumerWithRetry()
	}

	// 8.13 Init History Lua scripts in Redis
	if err := redis.InitHistoryLuaScripts(); err != nil {
		logger.Log.Error("Failed to load history Lua scripts: " + err.Error())
	} else {
		logger.Log.Info("History Lua scripts loaded successfully")
	}

	// 8.14 Init Hot Lua scripts（热度加权 × 方向 × clamp 原子脚本）
	if err := redis.InitHotLuaScripts(); err != nil {
		logger.Log.Error("Failed to load hot Lua scripts: " + err.Error())
	} else {
		logger.Log.Info("Hot Lua scripts loaded successfully")
	}

	// 8.15 Init Post hot Redpanda producer（帖子热度增量异步落库 + circle fan-out）
	if err := redpanda.InitPostHotProducer(); err != nil {
		logger.Log.Error("Failed to initialize post hot producer: " + err.Error())
		logger.Log.Warn("Post hot persistence to database is disabled.")
	} else {
		logger.Log.Info("Post hot producer initialized successfully")
		go redpanda.StartPostHotConsumerWithRetry()
	}

	// 8.16 Start Circle hot syncer（定时把 circle:hot 累加器落库 + 刷缓存）
	go redpanda.StartCircleHotSyncerWithRetry()

	// 8.16.1 Start Trending rank syncer（定时 ES 聚合 → 覆盖写 trending:* ZSET 榜单）
	go redpanda.StartTrendingSyncerWithRetry()

	// 8.17 Init Post interaction Redpanda producer（CF 灌数：互动事件 → post_interaction 表）
	if err := redpanda.InitPostInteractionProducer(); err != nil {
		logger.Log.Error("Failed to initialize post interaction producer: " + err.Error())
		logger.Log.Warn("Post interaction persistence to database is disabled. CF feed disabled.")
	} else {
		logger.Log.Info("Post interaction producer initialized successfully")
		go redpanda.StartPostInteractionConsumerWithRetry()
	}

	// 8.18 Start Item CF syncer（定时算 post↔post 共现相似度 → cf:item ZSET；P1）
	// 仅在 CF 开关打开时启动；依赖 post_interaction 表有数据（P0 灌数）。
	if conf.Config.Recommend.CF.Enabled {
		go redpanda.StartItemCFSyncerWithRetry()
	} else {
		logger.Log.Info("Item CF syncer disabled by config (recommend.cf.enabled=false)")
	}

	// 9. Init Router
	r := router.InitRouter()

	// 10. Run Server
	addr := fmt.Sprintf(":%d", conf.Config.Server.Port)
	logger.Log.Info("Server starting on " + addr)

	// Spin() 内部处理 SIGINT/SIGTERM 并优雅关停（阻塞直到收到信号）。
	r.Spin()

	// 收到信号后 Spin 返回，执行资源清理。
	logger.Log.Info("Shutdown Server ...")
	// 先停通知消费者：排干 flush 窗口内缓冲事件（落库 + 未读计数依赖 DB/Redis，
	// 必须先于 CloseRedis 执行）。
	redpanda.StopNotificationEventConsumerGlobal()
	// 停点赞消费者：排干缓冲末态落库并提交 offset（仅依赖 PG）。
	redpanda.StopLikeEventConsumerGlobal()
	// 停 SSE 推流 hub 的 sweeper（存量连接由 hertz 关停随连接关闭回收）。
	composition.StopNoticeStreamHub()
	redis.CloseRedis()
	auth.CloseSaToken()
	redpanda.CloseRedpandaProducer()
	redpanda.ClosePostStatsProducer()
	redpanda.CloseLikeEventProducer()
	redpanda.CloseCollectEventProducer()
	redpanda.CloseHistoryEventProducer()
	redpanda.ClosePostHotProducer()
	redpanda.ClosePostInteractionProducer()
	redpanda.CloseNotificationEventProducer()
	redpanda.StopCircleHotSyncer()
	redpanda.StopItemCFSyncer()
	redpanda.StopTrendingSyncer()
	redpanda.StopDiscoverSyncer()
	logger.Log.Info("Server shutdown complete")
}
