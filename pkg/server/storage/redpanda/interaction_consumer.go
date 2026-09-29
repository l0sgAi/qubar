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

// postInteractionRow 帖子互动批量 upsert 行（JSON 喂 jsonb_to_recordset）。
type postInteractionRow struct {
	UserID string `json:"user_id"`
	PostID string `json:"post_id"`
	Weight int16  `json:"weight"`
	TsMs   int64  `json:"ts_ms"`
}

const defaultPostInteractionFlushMinutes = 2

// interactionAgg 同一 (user, post) 在一个 flush 窗口内的合并结果：weight / ts 各取最大（与 upsert 语义一致）。
type interactionAgg struct {
	Weight int16
	TsMs   int64
}

// postInteractionBuf (user, post) -> 合并后的互动。
// 必须按键去重：同一语句内 ON CONFLICT DO UPDATE 不能命中同一行两次（SQLSTATE 21000），否则整批失败。
type postInteractionBuf map[userPostKey]interactionAgg

func (b postInteractionBuf) put(k userPostKey, v interactionAgg) {
	cur, ok := b[k]
	if !ok {
		b[k] = v
		return
	}
	if v.Weight > cur.Weight {
		cur.Weight = v.Weight
	}
	if v.TsMs > cur.TsMs {
		cur.TsMs = v.TsMs
	}
	b[k] = cur
}

func (b postInteractionBuf) add(msg PostInteractionMessage) {
	if msg.UserID == uuid.Nil || msg.PostID == uuid.Nil {
		return
	}
	b.put(userPostKey{UserID: msg.UserID, PostID: msg.PostID}, interactionAgg{Weight: msg.Weight, TsMs: msg.Ts})
}

func (b postInteractionBuf) merge(src postInteractionBuf) {
	for k, v := range src {
		b.put(k, v)
	}
}

// postInteractionSpec 帖子互动事件（CF 灌数）：按「N 分钟」或「M 条」先到先 flush，
// 批量 ON CONFLICT GREATEST upsert 到 domains.post_interaction。
// 幂等：weight 取 max-ever，ts 取 max，重投无副作用。
func postInteractionSpec() batchConsumerSpec[PostInteractionMessage, postInteractionBuf] {
	return batchConsumerSpec[PostInteractionMessage, postInteractionBuf]{
		name:          "post interaction",
		newBuf:        func() postInteractionBuf { return make(postInteractionBuf) },
		add:           postInteractionBuf.add,
		size:          func(b postInteractionBuf) int { return len(b) },
		merge:         postInteractionBuf.merge,
		persist:       batchUpsertPostInteractions,
		flushMessages: conf.Config.Redpanda.PostInteractionFlushMessages,
	}
}

// StartPostInteractionConsumer 启动帖子互动事件消费者。
func StartPostInteractionConsumer() error {
	rp := conf.Config.Redpanda
	startBatchConsumer(postInteractionSpec(), rp.PostInteractionTopic, rp.PostInteractionConsumerGroup,
		flushIntervalOr(rp.PostInteractionFlushInterval, defaultPostInteractionFlushMinutes, time.Minute))
	return nil
}

// batchUpsertPostInteractions 批量 upsert domains.post_interaction（ON CONFLICT GREATEST，幂等）。
func batchUpsertPostInteractions(buf postInteractionBuf) error {
	rows := make([]postInteractionRow, 0, len(buf))
	for k, v := range buf {
		rows = append(rows, postInteractionRow{
			UserID: k.UserID.String(),
			PostID: k.PostID.String(),
			Weight: v.Weight,
			TsMs:   v.TsMs,
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
	startWithRetry("post interaction", StartPostInteractionConsumer)
}
