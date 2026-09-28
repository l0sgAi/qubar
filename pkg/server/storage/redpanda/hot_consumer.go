package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// postHotRow 帖子热度批量更新行（JSON 喂 jsonb_to_recordset）。
type postHotRow struct {
	PostID uuid.UUID `json:"post_id"`
	Delta  int64     `json:"delta"`
}

const defaultPostHotFlushMinutes = 13

// postHotSpec 帖子热度：累加 postID -> ΣΔ，按「N 分钟」或「M 条」先到先 flush：
//   - 批量 UPDATE domains.post.hot（CDC 自动同步 ES）—— 失败整批重试，不提交 offset
//   - fan-out：按 post.circle_id 聚合 circleID -> ΣΔ，INCR circle:hot:{circleID} 累加器
//     （best-effort：DB 已落库后再失败不重试，避免 post.hot 重复累加）
func postHotSpec() batchConsumerSpec[PostHotMessage, postDeltaBuf] {
	return batchConsumerSpec[PostHotMessage, postDeltaBuf]{
		name:   "post hot",
		newBuf: newPostDeltaBuf,
		add: func(b postDeltaBuf, msg PostHotMessage) {
			if msg.PostID != uuid.Nil && msg.Delta != 0 {
				b[msg.PostID] += msg.Delta
			}
		},
		size:          postDeltaBufSize,
		merge:         postDeltaBuf.merge,
		persist:       persistPostHot,
		flushMessages: conf.Config.Redpanda.PostHotFlushMessages,
	}
}

// StartPostHotConsumer 启动帖子热度消费者。
func StartPostHotConsumer() error {
	rp := conf.Config.Redpanda
	startBatchConsumer(postHotSpec(), rp.PostHotTopic, rp.PostHotConsumerGroup,
		flushIntervalOr(rp.PostHotFlushInterval, defaultPostHotFlushMinutes, time.Minute))
	return nil
}

// persistPostHot 落库热度增量 + fan-out 圈子热度累加器。
func persistPostHot(deltas postDeltaBuf) error {
	rows := make([]postHotRow, 0, len(deltas))
	for postID, delta := range deltas {
		if delta != 0 {
			rows = append(rows, postHotRow{PostID: postID, Delta: delta})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	logger.Log.Info(fmt.Sprintf("Flushing %d post hot updates", len(rows)))

	// 1. 批量 UPDATE post.hot（CDC 自动同步 ES）
	if err := updatePostHotDB(rows); err != nil {
		return err
	}

	// 2. fan-out circle:hot 累加器（按 post.circle_id 聚合）
	circleDeltas, err := resolveCircleDeltas(rows)
	if err != nil {
		logger.Log.Error("Failed to resolve circle deltas for hot fan-out: " + err.Error())
		return nil
	}
	if err := fanoutCircleHot(circleDeltas); err != nil {
		logger.Log.Error("Failed to fan-out circle hot: " + err.Error())
	}
	return nil
}

// updatePostHotDB 批量更新 domains.post.hot。
func updatePostHotDB(rows []postHotRow) error {
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

// StartPostHotConsumerWithRetry 启动帖子热度消费者，带重试机制。
func StartPostHotConsumerWithRetry() {
	startWithRetry("post hot", StartPostHotConsumer)
}
