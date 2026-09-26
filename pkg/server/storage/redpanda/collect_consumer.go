package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// collectEventDelta 收藏事件增量
type collectEventDelta struct {
	UserID uuid.UUID
	PostID uuid.UUID
	Amount int64
}

// CollectEventAggregator 收藏事件聚合器
type CollectEventAggregator struct {
	mu       sync.Mutex
	deltas   map[string]*collectEventDelta
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

func collectDeltaKey(userID, postID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", userID.String(), postID.String())
}

// StartCollectEventConsumer 启动收藏事件消费者
func StartCollectEventConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing collect event consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		Resolver:  nil,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.CollectEventTopic,
		GroupID:        conf.Config.Redpanda.CollectEventConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		Dialer:         dialer,
	})

	logger.Log.Info(fmt.Sprintf("Collect event consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.CollectEventTopic, conf.Config.Redpanda.CollectEventConsumerGroup))

	aggregator := &CollectEventAggregator{
		deltas:   make(map[string]*collectEventDelta),
		ticker:   time.NewTicker(time.Duration(conf.Config.Redpanda.CollectEventFlushInterval) * time.Second),
		stopChan: make(chan struct{}),
	}

	go aggregator.run()

	go func() {
		defer r.Close()
		backoff := &readBackoff{}
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				waitAfterReadError(context.Background(), backoff, "collect event", err)
				continue
			}
			backoff.Reset()

			var collectMsg CollectEventMessage
			if err := json.Unmarshal(msg.Value, &collectMsg); err != nil {
				logger.Log.Error("Failed to unmarshal collect event: " + err.Error())
				continue
			}

			aggregator.addMessage(collectMsg)
		}
	}()

	return nil
}

func (a *CollectEventAggregator) addMessage(msg CollectEventMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}

	key := collectDeltaKey(msg.UserID, msg.PostID)
	delta, exists := a.deltas[key]
	if !exists {
		delta = &collectEventDelta{
			UserID: msg.UserID,
			PostID: msg.PostID,
		}
		a.deltas[key] = delta
	}
	delta.Amount += msg.Amount
}

func (a *CollectEventAggregator) run() {
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

func (a *CollectEventAggregator) flush() {
	a.mu.Lock()
	if len(a.deltas) == 0 {
		a.mu.Unlock()
		return
	}
	deltas := a.deltas
	a.deltas = make(map[string]*collectEventDelta)
	a.mu.Unlock()

	valid := make([]*collectEventDelta, 0, len(deltas))
	for _, d := range deltas {
		if d.Amount == 0 {
			continue
		}
		valid = append(valid, d)
	}

	if len(valid) > 0 {
		if err := batchUpdatePostCollects(valid); err != nil {
			logger.Log.Error("Failed to batch update post collects: " + err.Error())
		}
	}
}

// batchUpdatePostCollects 批量聚合 collect_count 增量到 post 表。
//
// post_collect 流水已由 collect.Toggle 即时入库，本函数只负责统计字段：
// 按 postID 聚合事件 amount，单事务批量 UPDATE collect_count（GREATEST 防负）。
func batchUpdatePostCollects(deltas []*collectEventDelta) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		// 按 postID 聚合 collect_count 增量，批量 UPDATE
		postCountDeltas := make(map[uuid.UUID]int64)
		for _, d := range deltas {
			postCountDeltas[d.PostID] += d.Amount
		}

		type row struct {
			PostID uuid.UUID `json:"post_id"`
			Delta  int64     `json:"delta"`
		}
		rows := make([]row, 0, len(postCountDeltas))
		for postID, delta := range postCountDeltas {
			if delta != 0 {
				rows = append(rows, row{PostID: postID, Delta: delta})
			}
		}
		if len(rows) > 0 {
			sql := `UPDATE domains.post p SET collect_count = GREATEST(p.collect_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
				FROM (SELECT * FROM jsonb_to_recordset(?::jsonb) AS v(post_id uuid, delta BIGINT)) v
				WHERE p.id = v.post_id AND p.deleted = 0`
			jsonBytes, _ := json.Marshal(rows)
			if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
				return fmt.Errorf("failed to batch update post collect counts: %w", err)
			}
		}

		logger.Log.Info(fmt.Sprintf("Successfully updated collect counts for %d posts", len(rows)))
		return nil
	})
}

// StartCollectEventConsumerWithRetry 启动收藏事件消费者，带重试机制
func StartCollectEventConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartCollectEventConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start collect event consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))
			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for collect event consumer, giving up")
				return
			}
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying collect event consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("Collect event consumer started successfully")
			return
		}
	}
}
