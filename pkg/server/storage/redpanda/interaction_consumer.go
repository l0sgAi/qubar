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

// postInteractionRow 帖子互动批量 upsert 行（JSON 喂 jsonb_to_recordset）。
type postInteractionRow struct {
	UserID string `json:"user_id"`
	PostID string `json:"post_id"`
	Weight int16  `json:"weight"`
	TsMs   int64  `json:"ts_ms"`
}

// PostInteractionAggregator 帖子互动事件聚合器（CF 灌数）。
//
// 攒一批 PostInteractionMessage，按「N 分钟」或「M 条」先到先 flush：
// 批量 ON CONFLICT GREATEST upsert 到 domains.post_interaction。
// 幂等：upsert 以 (user_id, post_id) 去重，weight 取 max-ever，ts 取 max。
type PostInteractionAggregator struct {
	mu       sync.Mutex
	messages []PostInteractionMessage
	count    int
	ticker   *time.Ticker
	flushNow chan struct{}
	stopChan chan struct{}
	stopped  bool
}

// StartPostInteractionConsumer 启动帖子互动事件消费者。
func StartPostInteractionConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing post interaction consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{Timeout: 10 * time.Second, DualStack: true, Resolver: nil}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.PostInteractionTopic,
		GroupID:        conf.Config.Redpanda.PostInteractionConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		Dialer:         dialer,
	})

	interval := conf.Config.Redpanda.PostInteractionFlushInterval
	if interval <= 0 {
		interval = 2
	}
	aggregator := &PostInteractionAggregator{
		messages: make([]PostInteractionMessage, 0, 256),
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
				waitAfterReadError(context.Background(), backoff, "post interaction", err)
				continue
			}
			backoff.Reset()

			var im PostInteractionMessage
			if err := json.Unmarshal(msg.Value, &im); err != nil {
				logger.Log.Error("Failed to unmarshal post interaction message: " + err.Error())
				continue
			}
			aggregator.addMessage(im)
		}
	}()

	logger.Log.Info(fmt.Sprintf("Post interaction consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.PostInteractionTopic, conf.Config.Redpanda.PostInteractionConsumerGroup))
	return nil
}

// addMessage 添加帖子互动消息到聚合器。
func (a *PostInteractionAggregator) addMessage(msg PostInteractionMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}

	a.messages = append(a.messages, msg)
	a.count++

	if thresh := conf.Config.Redpanda.PostInteractionFlushMessages; thresh > 0 && a.count >= thresh {
		select {
		case a.flushNow <- struct{}{}:
		default:
		}
	}
}

// run 按时间或计数触发 flush。
func (a *PostInteractionAggregator) run() {
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

// flush 刷新待处理的互动事件到 domains.post_interaction。
func (a *PostInteractionAggregator) flush() {
	a.mu.Lock()
	if len(a.messages) == 0 {
		a.count = 0
		a.mu.Unlock()
		return
	}
	msgs := a.messages
	a.messages = make([]PostInteractionMessage, 0, 256)
	a.count = 0
	a.mu.Unlock()

	if err := a.batchUpsertDB(msgs); err != nil {
		logger.Log.Error("Failed to batch upsert post interaction: " + err.Error())
	}
}

// batchUpsertDB 批量 upsert domains.post_interaction（ON CONFLICT GREATEST，幂等）。
func (a *PostInteractionAggregator) batchUpsertDB(msgs []PostInteractionMessage) error {
	rows := make([]postInteractionRow, 0, len(msgs))
	for _, m := range msgs {
		if m.UserID == uuid.Nil || m.PostID == uuid.Nil {
			continue
		}
		rows = append(rows, postInteractionRow{
			UserID: m.UserID.String(),
			PostID: m.PostID.String(),
			Weight: m.Weight,
			TsMs:   m.Ts,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		sql := `
		INSERT INTO domains.post_interaction (user_id, post_id, weight, ts)
		SELECT v.user_id, v.post_id, v.weight, to_timestamp(v.ts_ms / 1000.0)
		FROM jsonb_to_recordset(?::jsonb) AS v(user_id uuid, post_id uuid, weight SMALLINT, ts_ms BIGINT)
		ON CONFLICT (user_id, post_id) DO UPDATE
		SET weight = GREATEST(post_interaction.weight, EXCLUDED.weight),
		    ts     = GREATEST(post_interaction.ts, EXCLUDED.ts)
		`
		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal post interaction rows: %w", err)
		}
		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to execute post interaction batch upsert: %w", err)
		}
		logger.Log.Info(fmt.Sprintf("Successfully upserted %d post interaction rows", len(rows)))
		return nil
	})
}

// StartPostInteractionConsumerWithRetry 启动帖子互动事件消费者，带重试。
func StartPostInteractionConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0
	for {
		attempt++
		err := StartPostInteractionConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start post interaction consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))
			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for post interaction consumer, giving up")
				return
			}
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying post interaction consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("Post interaction consumer started successfully")
			return
		}
	}
}
