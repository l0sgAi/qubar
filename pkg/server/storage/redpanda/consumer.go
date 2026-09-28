package redpanda

import (
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// containsIgnoreCase 不区分大小写检查字符串是否包含子串
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// 兜底 flush 间隔（分钟），与 configs/config.yaml 默认值一致。
const (
	defaultCircleStatsFlushMinutes = 27
	defaultPostStatsFlushMinutes   = 15
)

// ==================== 圈子统计消费者 ====================

// circleStatsBuf 圈子统计聚合缓冲：circle_id -> 累计成员 / 帖子变化量。
type circleStatsBuf struct {
	members map[uuid.UUID]int64
	posts   map[uuid.UUID]int64
}

func newCircleStatsBuf() *circleStatsBuf {
	return &circleStatsBuf{members: make(map[uuid.UUID]int64), posts: make(map[uuid.UUID]int64)}
}

func (b *circleStatsBuf) add(msg CircleStatisticsMessage) {
	switch msg.Type {
	case StatisticsTypeCircleCount:
		b.members[msg.CircleID] += msg.Value
	case StatisticsTypePostCount:
		b.posts[msg.CircleID] += msg.Value
	default:
		logger.Log.Warn(fmt.Sprintf("Unknown statistics type: %s", msg.Type))
	}
}

func (b *circleStatsBuf) size() int { return len(b.members) + len(b.posts) }

func (b *circleStatsBuf) merge(src *circleStatsBuf) {
	for id, d := range src.members {
		b.members[id] += d
	}
	for id, d := range src.posts {
		b.posts[id] += d
	}
}

var circleStatsSpec = batchConsumerSpec[CircleStatisticsMessage, *circleStatsBuf]{
	name:    "circle statistics",
	newBuf:  newCircleStatsBuf,
	add:     (*circleStatsBuf).add,
	size:    (*circleStatsBuf).size,
	merge:   (*circleStatsBuf).merge,
	persist: persistCircleStats,
}

// StartStatisticsConsumer 启动圈子统计消费者
func StartStatisticsConsumer() error {
	rp := conf.Config.Redpanda
	startBatchConsumer(circleStatsSpec, rp.Topic, rp.ConsumerGroup,
		flushIntervalOr(rp.FlushInterval, defaultCircleStatsFlushMinutes, time.Minute))
	return nil
}

// persistCircleStats 成员数与帖子数在同一事务内落库（任一失败整批重试，避免部分重复）。
func persistCircleStats(b *circleStatsBuf) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		if err := applyCircleCountDeltas(tx, "member_count", b.members); err != nil {
			return err
		}
		return applyCircleCountDeltas(tx, "post_count", b.posts)
	})
}

// applyCircleCountDeltas 批量更新 domains.circle 的计数列。column 仅限内部常量。
func applyCircleCountDeltas(tx *gorm.DB, column string, deltas map[uuid.UUID]int64) error {
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

	sql := fmt.Sprintf(`
		UPDATE domains.circle c
		SET %[1]s = GREATEST(c.%[1]s + v.delta, 0),
		    update_time = CURRENT_TIMESTAMP
		FROM (
			SELECT * FROM jsonb_to_recordset(?::jsonb)
			AS v(circle_id uuid, delta BIGINT)
		) v
		WHERE c.id = v.circle_id AND c.deleted = 0`, column)

	jsonBytes, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("failed to marshal circle %s rows: %w", column, err)
	}
	if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
		return fmt.Errorf("failed to batch update circle %s: %w", column, err)
	}
	logger.Log.Info(fmt.Sprintf("Successfully updated %d circle %s", len(rows), column))
	return nil
}

// StartStatisticsConsumerWithRetry 启动消费者，带重试机制
func StartStatisticsConsumerWithRetry() {
	startWithRetry("statistics", StartStatisticsConsumer)
}

// ==================== 帖子统计消费者 ====================

// postStatDelta 帖子统计增量（内部聚合使用）
type postStatDelta struct {
	ViewCount    int64
	CommentCount int64
	LikeCount    int64
	CollectCount int64
}

func (d *postStatDelta) addDelta(o *postStatDelta) {
	d.ViewCount += o.ViewCount
	d.CommentCount += o.CommentCount
	d.LikeCount += o.LikeCount
	d.CollectCount += o.CollectCount
}

// postStatsBuf post_id -> 累计变化量
type postStatsBuf map[uuid.UUID]*postStatDelta

func (b postStatsBuf) get(postID uuid.UUID) *postStatDelta {
	d, ok := b[postID]
	if !ok {
		d = &postStatDelta{}
		b[postID] = d
	}
	return d
}

func (b postStatsBuf) add(msg PostStatisticsMessage) {
	switch msg.Type {
	case StatisticsTypePostView:
		b.get(msg.PostID).ViewCount += msg.Value
	case StatisticsTypePostLike:
		b.get(msg.PostID).LikeCount += msg.Value
	case StatisticsTypePostCollect:
		b.get(msg.PostID).CollectCount += msg.Value
	default:
		logger.Log.Warn(fmt.Sprintf("Unknown post statistics type: %s", msg.Type))
	}
}

func (b postStatsBuf) merge(src postStatsBuf) {
	for id, d := range src {
		b.get(id).addDelta(d)
	}
}

var postStatsSpec = batchConsumerSpec[PostStatisticsMessage, postStatsBuf]{
	name:    "post statistics",
	newBuf:  func() postStatsBuf { return make(postStatsBuf) },
	add:     postStatsBuf.add,
	size:    func(b postStatsBuf) int { return len(b) },
	merge:   postStatsBuf.merge,
	persist: batchUpdatePostStats,
}

// StartPostStatisticsConsumer 启动帖子统计消费者
func StartPostStatisticsConsumer() error {
	rp := conf.Config.Redpanda
	startBatchConsumer(postStatsSpec, rp.PostTopic, rp.PostConsumerGroup,
		flushIntervalOr(rp.PostFlushInterval, defaultPostStatsFlushMinutes, time.Minute))
	return nil
}

// batchUpdatePostStats 批量更新帖子统计计数到数据库
func batchUpdatePostStats(deltas postStatsBuf) error {
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

// StartPostStatisticsConsumerWithRetry 启动帖子统计消费者，带重试机制
func StartPostStatisticsConsumerWithRetry() {
	startWithRetry("post statistics", StartPostStatisticsConsumer)
}

// startWithRetry 启动消费者，失败线性退避重试（最多 10 次）。
func startWithRetry(name string, start func() error) {
	const maxAttempts = 10
	for attempt := 1; ; attempt++ {
		err := start()
		if err == nil {
			logger.Log.Info(fmt.Sprintf("%s consumer started successfully", name))
			return
		}
		logger.Log.Error(fmt.Sprintf("Failed to start %s consumer (attempt %d/%d): %s",
			name, attempt, maxAttempts, err.Error()))
		if attempt >= maxAttempts {
			logger.Log.Error(fmt.Sprintf("Max retry attempts reached for %s consumer, giving up", name))
			return
		}
		waitTime := time.Duration(attempt) * 5 * time.Second
		logger.Log.Info(fmt.Sprintf("Retrying %s consumer in %v...", name, waitTime))
		time.Sleep(waitTime)
	}
}
