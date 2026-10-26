// pipelines.go：异步链路启动序列（Redpanda 生产者/消费者、Redis Lua 脚本、定时 syncer）。
// 新链路按依赖顺序追加到 initPipelines，并在 shutdown.go 补对应的 Stop / Close。

package apps

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/redis"
	redpanda "interestBar/pkg/server/storage/redpanda"
	"strings"
)

// initPipelines 按固定顺序初始化 Redpanda 生产者（成功才起消费者）、加载 Lua 脚本、
// 启动定时 syncer。任一步失败只记录日志并降级，不中断启动。
func initPipelines() {
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

	// 8.17.1 Init Post moderation Redpanda producer（帖子 AI 审核事件，#117；仅 moderation 启用时，
	// 消费者需要 post 服务，在 composition.RegisterDomainRoutes 里启动）
	if conf.Config.Moderation.Active() {
		if err := redpanda.InitPostModerationProducer(); err != nil {
			logger.Log.Error("Failed to initialize post moderation producer: " + err.Error())
			logger.Log.Warn("Post moderation events cannot be published; new posts fall back to fail_open.")
		} else {
			logger.Log.Info("Post moderation producer initialized successfully")
		}
	}

	// 8.18 Start Item CF syncer（定时算 post↔post 共现相似度 → cf:item ZSET；P1）
	// 仅在 CF 开关打开时启动；依赖 post_interaction 表有数据（P0 灌数）。
	if conf.Config.Recommend.CF.Enabled {
		go redpanda.StartItemCFSyncerWithRetry()
	} else {
		logger.Log.Info("Item CF syncer disabled by config (recommend.cf.enabled=false)")
	}
}
