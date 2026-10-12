package redpanda

import (
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const defaultHistoryFlushIntervalSeconds = 10

// userPostKey (user, post) 聚合键（浏览历史 / 互动灌数共用）。
type userPostKey struct {
	UserID uuid.UUID
	PostID uuid.UUID
}

// historyBuf (user, post) -> 浏览次数（flush 窗口内同 user+post 多次浏览聚合为 count）
type historyBuf map[userPostKey]int64

func (b historyBuf) merge(src historyBuf) {
	for k, n := range src {
		b[k] += n
	}
}

var historySpec = batchConsumerSpec[HistoryEventMessage, historyBuf]{
	name:   "history event",
	newBuf: func() historyBuf { return make(historyBuf) },
	add: func(b historyBuf, msg HistoryEventMessage) {
		if msg.UserID != uuid.Nil && msg.PostID != uuid.Nil {
			b[userPostKey{UserID: msg.UserID, PostID: msg.PostID}]++
		}
	},
	size:    func(b historyBuf) int { return len(b) },
	merge:   historyBuf.merge,
	persist: batchUpdatePostViewHistory,
}

// StartHistoryEventConsumer 启动浏览历史事件消费者
func StartHistoryEventConsumer() error {
	rp := conf.Config.Redpanda
	startBatchConsumer(historySpec, rp.HistoryEventTopic, rp.HistoryEventConsumerGroup,
		flushIntervalOr(rp.HistoryEventFlushInterval, defaultHistoryFlushIntervalSeconds, time.Second))
	return nil
}

// batchUpdatePostViewHistory 批量 upsert post_view_history(ON CONFLICT)。
//
// 单事务 + jsonb_to_recordset 批量:行存在 → bump update_time + view_count+=count;
// 行不存在 → 插入(id 列省略,走 DB DEFAULT uuidv7() 兜底生成 UUIDv7)。
// 入参已按 (user_id, post_id) 去重(聚合键),无同对重复。
func batchUpdatePostViewHistory(deltas historyBuf) error {
	type row struct {
		UserID    uuid.UUID `json:"user_id"`
		PostID    uuid.UUID `json:"post_id"`
		ViewCount int64     `json:"view_count"`
	}
	rows := make([]row, 0, len(deltas))
	for k, n := range deltas {
		rows = append(rows, row{UserID: k.UserID, PostID: k.PostID, ViewCount: n})
	}

	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
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
	startWithRetry("history event", StartHistoryEventConsumer)
}
