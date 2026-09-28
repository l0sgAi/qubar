package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// postHotRow 帖子热度批量更新行（JSON 喂 jsonb_to_recordset）。
type postHotRow struct {
	PostID uuid.UUID `json:"post_id"`
	Delta  int64     `json:"delta"`
}

// PostHotAggregator 帖子热度聚合器。
//
// 累加 postID -> ΣΔ，按「N 分钟」或「M 条」先到先 flush：
//   - 批量 UPDATE domains.post.hot（CDC 自动同步 ES）
//   - fan-out：按 post.circle_id 聚合 circleID -> ΣΔ，INCR circle:hot:{circleID} 累加器
type PostHotAggregator struct {
	mu       sync.Mutex
	deltas   map[uuid.UUID]int64 // post_id -> 累计 hot Δ
	count    int                 // 自上次 flush 累计消息数（计数触发用）
	ticker   *time.Ticker
	flushNow chan struct{} // 计数阈值触发的即时 flush 信号（缓冲 1，不阻塞生产者）
	stopChan chan struct{}
	stopped  bool
}

// StartPostHotConsumer 启动帖子热度消费者。
func StartPostHotConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing post hot consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{Timeout: 10 * time.Second, DualStack: true, Resolver: nil}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.PostHotTopic,
		GroupID:        conf.Config.Redpanda.PostHotConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		Dialer:         dialer,
	})

	interval := conf.Config.Redpanda.PostHotFlushInterval
	if interval <= 0 {
		interval = 13
	}
	aggregator := &PostHotAggregator{
		deltas:   make(map[uuid.UUID]int64),
		ticker:   time.NewTicker(time.Duration(interval) * time.Minute),
		flushNow: make(chan struct{}, 1),
		stopChan: make(chan struct{}),
	}

	go aggregator.run()

	go func() {
		defer r.Close()
		backoff := &readBackoff{}
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				waitAfterReadError(context.Background(), backoff, "post hot", err)
				continue
			}
			backoff.Reset()

			var hotMsg PostHotMessage
			if err := json.Unmarshal(msg.Value, &hotMsg); err != nil {
				logger.Log.Error("Failed to unmarshal post hot message: " + err.Error())
				continue
			}
			aggregator.addMessage(hotMsg)
		}
	}()

	logger.Log.Info(fmt.Sprintf("Post hot consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.PostHotTopic, conf.Config.Redpanda.PostHotConsumerGroup))
	return nil
}

// addMessage 添加帖子热度消息到聚合器。
func (a *PostHotAggregator) addMessage(msg PostHotMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}

	a.deltas[msg.PostID] += msg.Delta
	a.count++

	// 计数阈值触发即时 flush（13min 或 N 条，先到先 flush）
	if thresh := conf.Config.Redpanda.PostHotFlushMessages; thresh > 0 && a.count >= thresh {
		select {
		case a.flushNow <- struct{}{}:
		default: // 已有待处理 flush 信号，不阻塞 addMessage
		}
	}
}

// run 运行聚合器，按时间或计数触发 flush。
func (a *PostHotAggregator) run() {
	for {
		select {
		case <-a.ticker.C:
			a.flush()
		case <-a.flushNow:
			a.flush()
		case <-a.stopChan:
			a.ticker.Stop()
			a.flush()
			return
		}
	}
}

// flush 刷新待处理的热度增量到数据库 + circle 累加器。
func (a *PostHotAggregator) flush() {
	a.mu.Lock()
	if len(a.deltas) == 0 {
		a.count = 0
		a.mu.Unlock()
		return
	}
	deltas := a.deltas
	a.deltas = make(map[uuid.UUID]int64)
	a.count = 0
	a.mu.Unlock()

	rows := make([]postHotRow, 0, len(deltas))
	for postID, delta := range deltas {
		if delta != 0 {
			rows = append(rows, postHotRow{PostID: postID, Delta: delta})
		}
	}
	if len(rows) == 0 {
		return
	}

	logger.Log.Info(fmt.Sprintf("Flushing %d post hot updates", len(rows)))

	// 1. 批量 UPDATE post.hot（CDC 自动同步 ES）
	if err := a.updatePostHotDB(rows); err != nil {
		logger.Log.Error("Failed to batch update post hot: " + err.Error())
	}

	// 2. fan-out circle:hot 累加器（按 post.circle_id 聚合）
	circleDeltas, err := resolveCircleDeltas(rows)
	if err != nil {
		logger.Log.Error("Failed to resolve circle deltas for hot fan-out: " + err.Error())
		return
	}
	if err := fanoutCircleHot(circleDeltas); err != nil {
		logger.Log.Error("Failed to fan-out circle hot: " + err.Error())
	}
}

// updatePostHotDB 批量更新 domains.post.hot。
func (a *PostHotAggregator) updatePostHotDB(rows []postHotRow) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		sql := `
		UPDATE domains.post p
		SET hot = GREATEST(p.hot + v.delta, 0),
		    update_time = CURRENT_TIMESTAMP
		FROM (
		    SELECT * FROM jsonb_to_recordset(?::jsonb)
		    AS v(post_id uuid, delta BIGINT)
		) v
		WHERE p.id = v.post_id AND p.deleted = 0
		`
		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal post hot rows: %w", err)
		}
		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to execute post hot batch update: %w", err)
		}
		logger.Log.Info(fmt.Sprintf("Successfully updated %d post hot", len(rows)))
		return nil
	})
}

// resolveCircleDeltas 查 post.circle_id，聚合 circleID -> ΣΔ（仅未删帖）。
func resolveCircleDeltas(rows []postHotRow) (map[uuid.UUID]int64, error) {
	deltaByPost := make(map[uuid.UUID]int64, len(rows))
	postIDs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		deltaByPost[r.PostID] = r.Delta
		postIDs = append(postIDs, r.PostID)
	}

	type postCircle struct {
		ID       uuid.UUID
		CircleID uuid.UUID
	}
	var pcs []postCircle
	if err := pgsql.DB.Table("domains.post").
		Select("id, circle_id").
		Where("id IN ? AND deleted = 0", postIDs).
		Scan(&pcs).Error; err != nil {
		return nil, err
	}

	circleDeltas := make(map[uuid.UUID]int64)
	for _, pc := range pcs {
		if d, ok := deltaByPost[pc.ID]; ok {
			circleDeltas[pc.CircleID] += d
		}
	}
	return circleDeltas, nil
}

// fanoutCircleHot 把 circleID -> Δ 累加到 circle:hot:{circleID}（Redis，待 CircleHotSyncer 落库）。
func fanoutCircleHot(circleDeltas map[uuid.UUID]int64) error {
	if len(circleDeltas) == 0 {
		return nil
	}
	ttl := time.Duration(conf.Config.Redpanda.CircleHotTTL) * time.Hour
	if ttl <= 0 {
		ttl = 50 * time.Hour
	}

	ctx := context.Background()
	pipe := redispkg.Client.Pipeline()
	for circleID, delta := range circleDeltas {
		if delta == 0 {
			continue
		}
		key := redispkg.GetCircleHotKey(circleID)
		pipe.IncrBy(ctx, key, delta)
		pipe.Expire(ctx, key, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to fan-out circle hot: %w", err)
	}
	logger.Log.Info(fmt.Sprintf("Fan-out circle hot to %d circles", len(circleDeltas)))
	return nil
}

// StopPostHotAggregator 停止帖子热度聚合器。
func (a *PostHotAggregator) Stop() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	a.mu.Unlock()
	close(a.stopChan)
}

// StartPostHotConsumerWithRetry 启动帖子热度消费者，带重试机制。
func StartPostHotConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0
	for {
		attempt++
		err := StartPostHotConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start post hot consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))
			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for post hot consumer, giving up")
				return
			}
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying post hot consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("Post hot consumer started successfully")
			return
		}
	}
}
