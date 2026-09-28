package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// StatisticsAggregator 统计聚合器
type StatisticsAggregator struct {
	mu           sync.Mutex
	circleCounts map[uuid.UUID]int64 // circle_id -> 累计成员变化量
	postCounts   map[uuid.UUID]int64 // circle_id -> 累计帖子变化量
	ticker       *time.Ticker
	stopChan     chan struct{}
	stopped      bool
}

// containsIgnoreCase 不区分大小写检查字符串是否包含子串
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// StartStatisticsConsumer 启动统计消费者
func StartStatisticsConsumer() error {
	// 记录配置信息
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing Redpanda consumer with brokers: %v (len=%d)", brokers, len(brokers)))
	if len(brokers) > 0 {
		logger.Log.Info(fmt.Sprintf("First broker address: %s", brokers[0]))
	}

	// 创建自定义Dialer（与Producer保持一致）
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		// 关键：禁用resolver缓存，避免使用advertised地址
		Resolver: nil,
	}

	// 创建Kafka Reader
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.Topic,
		GroupID:        conf.Config.Redpanda.ConsumerGroup,
		MinBytes:       10e3,        // 10KB
		MaxBytes:       10e6,        // 10MB
		CommitInterval: time.Second, // 自动提交间隔
		Dialer:         dialer,      // 添加自定义Dialer
	})

	logger.Log.Info(fmt.Sprintf("Redpanda consumer created successfully: brokers=%v, topic=%s, group=%s",
		brokers, conf.Config.Redpanda.Topic, conf.Config.Redpanda.ConsumerGroup))

	// 创建聚合器
	aggregator := &StatisticsAggregator{
		circleCounts: make(map[uuid.UUID]int64),
		postCounts:   make(map[uuid.UUID]int64),
		ticker:       time.NewTicker(time.Duration(conf.Config.Redpanda.FlushInterval) * time.Minute),
		stopChan:     make(chan struct{}),
	}

	// 启动聚合处理协程
	go aggregator.run()

	// 启动消息接收协程
	go func() {
		defer r.Close()
		backoff := &readBackoff{}
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				waitAfterReadError(context.Background(), backoff, "circle statistics", err)
				continue
			}
			backoff.Reset()

			// 解析消息
			var statsMsg CircleStatisticsMessage
			if err := json.Unmarshal(msg.Value, &statsMsg); err != nil {
				logger.Log.Error("Failed to unmarshal statistics message: " + err.Error())
				// 解析失败，跳过该消息（自动提交会处理）
				continue
			}

			// 添加到聚合器
			aggregator.addMessage(statsMsg)
		}
	}()

	return nil
}

// addMessage 添加消息到聚合器
func (a *StatisticsAggregator) addMessage(msg CircleStatisticsMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stopped {
		return
	}

	switch msg.Type {
	case StatisticsTypeCircleCount:
		a.circleCounts[msg.CircleID] += msg.Value
	case StatisticsTypePostCount:
		a.postCounts[msg.CircleID] += msg.Value
	default:
		logger.Log.Warn(fmt.Sprintf("Unknown statistics type: %s", msg.Type))
	}
}

// run 运行聚合器，定期批量处理
func (a *StatisticsAggregator) run() {
	for {
		select {
		case <-a.ticker.C:
			a.flush()
		case <-a.stopChan:
			a.ticker.Stop()
			// Stop 时 flush 最后一批数据
			a.flush()
			return
		}
	}
}

// flush 刷新待处理的消息到数据库
func (a *StatisticsAggregator) flush() {
	a.mu.Lock()
	if len(a.circleCounts) == 0 && len(a.postCounts) == 0 {
		a.mu.Unlock()
		return
	}

	circleCounts := a.circleCounts
	postCounts := a.postCounts
	a.circleCounts = make(map[uuid.UUID]int64)
	a.postCounts = make(map[uuid.UUID]int64)
	a.mu.Unlock()

	// 统计消息数量
	totalMessages := len(circleCounts) + len(postCounts)
	logger.Log.Info(fmt.Sprintf("Flushing %d circle statistics updates", totalMessages))

	// 分别处理成员计数和帖子计数
	if len(circleCounts) > 0 {
		if err := a.batchUpdateMemberCounts(circleCounts); err != nil {
			logger.Log.Error("Failed to batch update member counts: " + err.Error())
		}
	}

	if len(postCounts) > 0 {
		if err := a.batchUpdatePostCounts(postCounts); err != nil {
			logger.Log.Error("Failed to batch update post counts: " + err.Error())
		}
	}
}

// batchUpdateMemberCounts 批量更新圈子成员计数到数据库
func (a *StatisticsAggregator) batchUpdateMemberCounts(deltas map[uuid.UUID]int64) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		// 构造更新数据
		type updateRow struct {
			CircleID uuid.UUID `json:"circle_id"`
			Delta    int64     `json:"delta"`
		}

		rows := make([]updateRow, 0, len(deltas))
		for circleID, delta := range deltas {
			if delta != 0 {
				rows = append(rows, updateRow{CircleID: circleID, Delta: delta})
			}
		}

		if len(rows) == 0 {
			return nil
		}

		// 使用JSON批量更新
		sql := `
		UPDATE domains.circle c
		SET member_count = GREATEST(c.member_count + v.delta, 0),
		    update_time = CURRENT_TIMESTAMP
		FROM (
			SELECT * FROM jsonb_to_recordset(?::jsonb)
			AS v(circle_id uuid, delta BIGINT)
		) v
		WHERE c.id = v.circle_id AND c.deleted = 0
		`

		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal update rows: %w", err)
		}

		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to execute batch update: %w", err)
		}

		logger.Log.Info(fmt.Sprintf("Successfully updated %d circle member counts", len(rows)))
		return nil
	})
}

// batchUpdatePostCounts 批量更新圈子帖子计数到数据库
func (a *StatisticsAggregator) batchUpdatePostCounts(deltas map[uuid.UUID]int64) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		// 构造更新数据
		type updateRow struct {
			CircleID uuid.UUID `json:"circle_id"`
			Delta    int64     `json:"delta"`
		}

		rows := make([]updateRow, 0, len(deltas))
		for circleID, delta := range deltas {
			if delta != 0 {
				rows = append(rows, updateRow{CircleID: circleID, Delta: delta})
			}
		}

		if len(rows) == 0 {
			return nil
		}

		// 使用JSON批量更新
		sql := `
		UPDATE domains.circle c
		SET post_count = GREATEST(c.post_count + v.delta, 0),
		    update_time = CURRENT_TIMESTAMP
		FROM (
			SELECT * FROM jsonb_to_recordset(?::jsonb)
			AS v(circle_id uuid, delta BIGINT)
		) v
		WHERE c.id = v.circle_id AND c.deleted = 0
		`

		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal update rows: %w", err)
		}

		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to execute batch update: %w", err)
		}

		logger.Log.Info(fmt.Sprintf("Successfully updated %d circle post counts", len(rows)))
		return nil
	})
}

// Stop 停止聚合器
func (a *StatisticsAggregator) Stop() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	a.mu.Unlock()

	close(a.stopChan)
}

// StartStatisticsConsumerWithRetry 启动消费者，带重试机制
func StartStatisticsConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartStatisticsConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start statistics consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))

			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached, giving up")
				return
			}

			// 等待后重试
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			// 成功启动，退出重试循环
			logger.Log.Info("Statistics consumer started successfully, exiting retry loop")
			return
		}
	}
}

// ==================== 帖子统计消费者 ====================

// postStatDelta 帖子统计增量（内部聚合使用）
type postStatDelta struct {
	ViewCount    int64
	CommentCount int64
	LikeCount    int64
	CollectCount int64
}

// PostStatisticsAggregator 帖子统计聚合器
type PostStatisticsAggregator struct {
	mu       sync.Mutex
	deltas   map[uuid.UUID]*postStatDelta // post_id -> 累计变化量
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

// StartPostStatisticsConsumer 启动帖子统计消费者
func StartPostStatisticsConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing post statistics consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		Resolver:  nil,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.PostTopic,
		GroupID:        conf.Config.Redpanda.PostConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		Dialer:         dialer,
	})

	logger.Log.Info(fmt.Sprintf("Post statistics consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.PostTopic, conf.Config.Redpanda.PostConsumerGroup))

	aggregator := &PostStatisticsAggregator{
		deltas:   make(map[uuid.UUID]*postStatDelta),
		ticker:   time.NewTicker(time.Duration(conf.Config.Redpanda.PostFlushInterval) * time.Minute),
		stopChan: make(chan struct{}),
	}

	go aggregator.run()

	go func() {
		defer r.Close()
		backoff := &readBackoff{}
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				waitAfterReadError(context.Background(), backoff, "post statistics", err)
				continue
			}
			backoff.Reset()

			var statsMsg PostStatisticsMessage
			if err := json.Unmarshal(msg.Value, &statsMsg); err != nil {
				logger.Log.Error("Failed to unmarshal post stats message: " + err.Error())
				continue
			}

			aggregator.addMessage(statsMsg)
		}
	}()

	return nil
}

// addMessage 添加帖子统计消息到聚合器
func (a *PostStatisticsAggregator) addMessage(msg PostStatisticsMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stopped {
		return
	}

	delta, exists := a.deltas[msg.PostID]
	if !exists {
		delta = &postStatDelta{}
		a.deltas[msg.PostID] = delta
	}

	switch msg.Type {
	case StatisticsTypePostView:
		delta.ViewCount += msg.Value
	case StatisticsTypePostLike:
		delta.LikeCount += msg.Value
	case StatisticsTypePostCollect:
		delta.CollectCount += msg.Value
	default:
		logger.Log.Warn(fmt.Sprintf("Unknown post statistics type: %s", msg.Type))
	}
}

// run 运行聚合器，定期批量处理
func (a *PostStatisticsAggregator) run() {
	for {
		select {
		case <-a.ticker.C:
			a.flush()
		case <-a.stopChan:
			a.ticker.Stop()
			a.flush()
			return
		}
	}
}

// flush 刷新待处理的帖子统计消息到数据库
func (a *PostStatisticsAggregator) flush() {
	a.mu.Lock()
	if len(a.deltas) == 0 {
		a.mu.Unlock()
		return
	}

	deltas := a.deltas
	a.deltas = make(map[uuid.UUID]*postStatDelta)
	a.mu.Unlock()

	logger.Log.Info(fmt.Sprintf("Flushing %d post statistics updates", len(deltas)))

	if err := a.batchUpdatePostStats(deltas); err != nil {
		logger.Log.Error("Failed to batch update post statistics: " + err.Error())
	}
}

// batchUpdatePostStats 批量更新帖子统计计数到数据库
func (a *PostStatisticsAggregator) batchUpdatePostStats(deltas map[uuid.UUID]*postStatDelta) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		type updateRow struct {
			PostID       uuid.UUID `json:"post_id"`
			ViewDelta    int64     `json:"view_delta"`
			CommentDelta int64     `json:"comment_delta"`
			LikeDelta    int64     `json:"like_delta"`
			CollectDelta int64     `json:"collect_delta"`
		}

		rows := make([]updateRow, 0, len(deltas))
		for postID, delta := range deltas {
			if delta.ViewCount != 0 || delta.CommentCount != 0 || delta.LikeCount != 0 || delta.CollectCount != 0 {
				rows = append(rows, updateRow{
					PostID:       postID,
					ViewDelta:    delta.ViewCount,
					CommentDelta: delta.CommentCount,
					LikeDelta:    delta.LikeCount,
					CollectDelta: delta.CollectCount,
				})
			}
		}

		if len(rows) == 0 {
			return nil
		}

		// 使用JSON批量更新所有统计字段
		sql := `
		UPDATE domains.post p
		SET view_count = LEAST(GREATEST(p.view_count + v.view_delta, 0), 1000000000),
		    comment_count = GREATEST(p.comment_count + v.comment_delta, 0),
		    like_count = GREATEST(p.like_count + v.like_delta, 0),
		    collect_count = GREATEST(p.collect_count + v.collect_delta, 0),
		    update_time = CURRENT_TIMESTAMP
		FROM (
		    SELECT * FROM jsonb_to_recordset(?::jsonb)
		    AS v(post_id uuid, view_delta BIGINT, comment_delta BIGINT, like_delta BIGINT, collect_delta BIGINT)
		) v
		WHERE p.id = v.post_id AND p.deleted = 0
		`

		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal post stats update rows: %w", err)
		}

		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to execute post stats batch update: %w", err)
		}

		logger.Log.Info(fmt.Sprintf("Successfully updated %d post statistics", len(rows)))
		return nil
	})
}

// StopPostAggregator 停止帖子统计聚合器
func StopPostAggregator() {
	// 目前 post aggregator 在消费者内部管理，无需单独停止
	// 如需优雅停止可扩展此方法
}

// StartPostStatisticsConsumerWithRetry 启动帖子统计消费者，带重试机制
func StartPostStatisticsConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartPostStatisticsConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start post statistics consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))

			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for post statistics consumer, giving up")
				return
			}

			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying post statistics consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("Post statistics consumer started successfully")
			return
		}
	}
}
