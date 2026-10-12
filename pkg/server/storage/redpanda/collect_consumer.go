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

const defaultCollectFlushIntervalSeconds = 10

// postDeltaBuf post_id -> 累计增量（收藏计数 / 帖子热度共用）。
type postDeltaBuf map[uuid.UUID]int64

func (b postDeltaBuf) merge(src postDeltaBuf) {
	for id, d := range src {
		b[id] += d
	}
}

func newPostDeltaBuf() postDeltaBuf { return make(postDeltaBuf) }

func postDeltaBufSize(b postDeltaBuf) int { return len(b) }

// collectSpec 收藏事件：post_collect 流水已由 collect.Toggle 即时入库，这里只聚合 collect_count 增量。
var collectSpec = batchConsumerSpec[CollectEventMessage, postDeltaBuf]{
	name:   "collect event",
	newBuf: newPostDeltaBuf,
	add: func(b postDeltaBuf, msg CollectEventMessage) {
		if msg.PostID != uuid.Nil && msg.Amount != 0 {
			b[msg.PostID] += msg.Amount
		}
	},
	size:    postDeltaBufSize,
	merge:   postDeltaBuf.merge,
	persist: batchUpdatePostCollects,
}

// StartCollectEventConsumer 启动收藏事件消费者
func StartCollectEventConsumer() error {
	rp := conf.Config.Redpanda
	startBatchConsumer(collectSpec, rp.CollectEventTopic, rp.CollectEventConsumerGroup,
		flushIntervalOr(rp.CollectEventFlushInterval, defaultCollectFlushIntervalSeconds, time.Second))
	return nil
}

// batchUpdatePostCollects 单事务批量 UPDATE collect_count（GREATEST 防负）。
func batchUpdatePostCollects(deltas postDeltaBuf) error {
	type row struct {
		PostID uuid.UUID `json:"post_id"`
		Delta  int64     `json:"delta"`
	}
	rows := make([]row, 0, len(deltas))
	for postID, delta := range deltas {
		if delta != 0 {
			rows = append(rows, row{PostID: postID, Delta: delta})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		sql := `UPDATE domains.post p SET collect_count = GREATEST(p.collect_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
			FROM (SELECT * FROM jsonb_to_recordset(?::jsonb) AS v(post_id uuid, delta BIGINT)) v
			WHERE p.id = v.post_id AND p.deleted = 0`
		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal post collect rows: %w", err)
		}
		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to batch update post collect counts: %w", err)
		}
		logger.Log.Info(fmt.Sprintf("Successfully updated collect counts for %d posts", len(rows)))
		return nil
	})
}

// StartCollectEventConsumerWithRetry 启动收藏事件消费者，带重试机制
func StartCollectEventConsumerWithRetry() {
	startWithRetry("collect event", StartCollectEventConsumer)
}
