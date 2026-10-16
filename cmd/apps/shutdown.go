package apps

import (
	"interestBar/pkg/composition"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/storage/redis"
	redpanda "interestBar/pkg/server/storage/redpanda"
)

// shutdown 在 Spin 返回后按依赖顺序释放资源：先排干消费者（依赖 DB/Redis），
// 再关 Redis / Sa-Token，最后关生产者与 syncer。
func shutdown() {
	// 收到信号后 Spin 返回，执行资源清理。
	logger.Log.Info("Shutdown Server ...")
	// 先停通知消费者：排干 flush 窗口内缓冲事件（落库 + 未读计数依赖 DB/Redis，
	// 必须先于 CloseRedis 执行）。
	redpanda.StopNotificationEventConsumerGlobal()
	// 停点赞消费者：排干缓冲末态落库并提交 offset（仅依赖 PG）。
	redpanda.StopLikeEventConsumerGlobal()
	// 停统计类批量消费者（圈子/帖子统计、收藏、浏览历史、热度、互动）：排干缓冲并提交 offset。
	// 须早于 CloseRedis（帖子热度 fan-out 写 Redis）。
	redpanda.StopBatchConsumersGlobal()
	// 停 SSE 推流 hub 的 sweeper（存量连接由 hertz 关停随连接关闭回收）。
	composition.StopNoticeStreamHub()
	if err := redis.CloseRedis(); err != nil {
		logger.Log.Error("Failed to close redis: " + err.Error())
	}
	if err := auth.CloseSaToken(); err != nil {
		logger.Log.Error("Failed to close sa-token storage: " + err.Error())
	}
	// 各 Producer 的 Close 内部已记录错误日志，关停阶段无需再处理返回值。
	_ = redpanda.CloseRedpandaProducer()
	_ = redpanda.ClosePostStatsProducer()
	_ = redpanda.CloseLikeEventProducer()
	_ = redpanda.CloseCollectEventProducer()
	_ = redpanda.CloseHistoryEventProducer()
	_ = redpanda.ClosePostHotProducer()
	_ = redpanda.ClosePostInteractionProducer()
	_ = redpanda.CloseNotificationEventProducer()
	redpanda.StopCircleHotSyncer()
	redpanda.StopItemCFSyncer()
	redpanda.StopTrendingSyncer()
	redpanda.StopDiscoverSyncer()
	logger.Log.Info("Server shutdown complete")
}
