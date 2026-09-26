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

// historyEventDelta 浏览历史事件增量(flush 窗口内同 user+post 多次浏览聚合为 count)。
type historyEventDelta struct {
	UserID uuid.UUID
	PostID uuid.UUID
	Count  int64
}

// HistoryEventAggregator 浏览历史事件聚合器(同构 CollectEventAggregator)。
type HistoryEventAggregator struct {
	mu       sync.Mutex
	deltas   map[string]*historyEventDelta
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

func historyDeltaKey(userID, postID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", userID.String(), postID.String())
}

// StartHistoryEventConsumer 启动浏览历史事件消费者
func StartHistoryEventConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing history event consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		Resolver:  nil,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.HistoryEventTopic,
		GroupID:        conf.Config.Redpanda.HistoryEventConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		Dialer:         dialer,
	})

	logger.Log.Info(fmt.Sprintf("History event consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.HistoryEventTopic, conf.Config.Redpanda.HistoryEventConsumerGroup))

	aggregator := &HistoryEventAggregator{
		deltas:   make(map[string]*historyEventDelta),
		ticker:   time.NewTicker(time.Duration(conf.Config.Redpanda.HistoryEventFlushInterval) * time.Second),
		stopChan: make(chan struct{}),
	}

	go aggregator.run()

	go func() {
		defer r.Close()
		backoff := &readBackoff{}
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				waitAfterReadError(context.Background(), backoff, "history event", err)
				continue
			}
			backoff.Reset()

			var historyMsg HistoryEventMessage
			if err := json.Unmarshal(msg.Value, &historyMsg); err != nil {
				logger.Log.Error("Failed to unmarshal history event: " + err.Error())
				continue
			}

			aggregator.addMessage(historyMsg)
		}
	}()

	return nil
}

func (a *HistoryEventAggregator) addMessage(msg HistoryEventMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}

	key := historyDeltaKey(msg.UserID, msg.PostID)
	delta, exists := a.deltas[key]
	if !exists {
		delta = &historyEventDelta{
			UserID: msg.UserID,
			PostID: msg.PostID,
		}
		a.deltas[key] = delta
	}
	delta.Count++
}

func (a *HistoryEventAggregator) run() {
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

func (a *HistoryEventAggregator) flush() {
	a.mu.Lock()
	if len(a.deltas) == 0 {
		a.mu.Unlock()
		return
	}
	deltas := a.deltas
	a.deltas = make(map[string]*historyEventDelta)
	a.mu.Unlock()

	valid := make([]*historyEventDelta, 0, len(deltas))
	for _, d := range deltas {
		valid = append(valid, d)
	}

	if len(valid) > 0 {
		if err := batchUpdatePostViewHistory(valid); err != nil {
			logger.Log.Error("Failed to batch update post view history: " + err.Error())
		}
	}
}

// batchUpdatePostViewHistory 批量 upsert post_view_history(ON CONFLICT)。
//
// 单事务 + jsonb_to_recordset 批量:行存在 → bump update_time + view_count+=count;
// 行不存在 → 插入(id 列省略,走 DB DEFAULT uuidv7() 兜底生成 UUIDv7)。
// 入参已按 (user_id, post_id) 去重(aggregator 按 key 聚合),无同对重复。
func batchUpdatePostViewHistory(deltas []*historyEventDelta) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		type row struct {
			UserID    uuid.UUID `json:"user_id"`
			PostID    uuid.UUID `json:"post_id"`
			ViewCount int64     `json:"view_count"`
		}
		rows := make([]row, 0, len(deltas))
		for _, d := range deltas {
			rows = append(rows, row{UserID: d.UserID, PostID: d.PostID, ViewCount: d.Count})
		}

		sql := `INSERT INTO domains.post_view_history (user_id, post_id, view_count)
			SELECT v.user_id, v.post_id, v.view_count
			FROM jsonb_to_recordset(?::jsonb) AS v(user_id uuid, post_id uuid, view_count BIGINT)
			ON CONFLICT (user_id, post_id) DO UPDATE
			SET update_time = CURRENT_TIMESTAMP,
			    view_count  = post_view_history.view_count + EXCLUDED.view_count`

		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal history event rows: %w", err)
		}
		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to batch upsert post view history: %w", err)
		}

		logger.Log.Info(fmt.Sprintf("Successfully upserted %d post view history rows", len(rows)))
		return nil
	})
}

// StartHistoryEventConsumerWithRetry 启动浏览历史事件消费者,带重试机制
func StartHistoryEventConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartHistoryEventConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start history event consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))
			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for history event consumer, giving up")
				return
			}
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying history event consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("History event consumer started successfully")
			return
		}
	}
}
