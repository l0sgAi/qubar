package redpanda

import (
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"
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
// 两条 UPDATE 涉及的圈子先合并按 id 升序一次性取锁，避免分两段取锁与其他批量写入方交叉成环（见 batch_lock.go）。
func persistCircleStats(b *circleStatsBuf) error {
	ids := make([]uuid.UUID, 0, b.size())
	for _, m := range []map[uuid.UUID]int64{b.members, b.posts} {
		for id, delta := range m {
			if delta != 0 {
				ids = append(ids, id)
			}
		}
	}
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockRowsByID(tx, "domains.circle", ids); err != nil {
			return err
		}
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
	sortByID(rows, func(r updateRow) uuid.UUID { return r.CircleID })

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

// postStatDelta 帖子统计增量（内部聚合使用）。
//
// 评论数不累加增量（#130）：CommentDirty 只标记「该帖评论有变化」，落库时按 domains.comment 明细重算。
// 重算与消息条数、重投次数无关，flush 失败重试、提交 offset 前进程被杀导致的重投都不会重复计数。
type postStatDelta struct {
	ViewCount    int64
	CommentDirty bool
	LikeCount    int64
	CollectCount int64
}

func (d *postStatDelta) addDelta(o *postStatDelta) {
	d.ViewCount += o.ViewCount
	d.CommentDirty = d.CommentDirty || o.CommentDirty
	d.LikeCount += o.LikeCount
	d.CollectCount += o.CollectCount
}

func (d *postStatDelta) empty() bool {
	return d.ViewCount == 0 && !d.CommentDirty && d.LikeCount == 0 && d.CollectCount == 0
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
	case StatisticsTypePostComment:
		b.get(msg.PostID).CommentDirty = true
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
	persist: persistPostStats,
}

// StartPostStatisticsConsumer 启动帖子统计消费者
func StartPostStatisticsConsumer() error {
	rp := conf.Config.Redpanda
	startBatchConsumer(postStatsSpec, rp.PostTopic, rp.PostConsumerGroup,
		flushIntervalOr(rp.PostFlushInterval, defaultPostStatsFlushMinutes, time.Minute))
	return nil
}

// postStatsRow 帖子统计批量更新行（JSON 喂 jsonb_to_recordset）。
type postStatsRow struct {
	PostID       uuid.UUID `json:"post_id"`
	ViewDelta    int64     `json:"view_delta"`
	CommentDirty bool      `json:"comment_dirty"`
	LikeDelta    int64     `json:"like_delta"`
	CollectDelta int64     `json:"collect_delta"`
}

// postStatsApplySQL 批量更新帖子统计：view / like / collect 累加增量，comment_count 对标记过的帖子按明细重算
// （deleted=0 的评论 + 回复行数，与原同步 +1 的口径一致）。
// 重算结果与当前值相同且没有其他增量的行不写：不产生新的行版本，也就不产生 WAL / CDC 事件（#130）。
const postStatsApplySQL = `
WITH v AS (
	SELECT * FROM jsonb_to_recordset(?::jsonb)
	AS v(post_id uuid, view_delta BIGINT, comment_dirty BOOLEAN, like_delta BIGINT, collect_delta BIGINT)
),
cc AS (
	SELECT v.post_id,
	       (SELECT count(*) FROM domains.comment c WHERE c.post_id = v.post_id AND c.deleted = 0) AS cnt
	FROM v WHERE v.comment_dirty
)
UPDATE domains.post p
SET view_count = LEAST(GREATEST(p.view_count + v.view_delta, 0), 1000000000),
    comment_count = COALESCE(cc.cnt, p.comment_count),
    like_count = GREATEST(p.like_count + v.like_delta, 0),
    collect_count = GREATEST(p.collect_count + v.collect_delta, 0),
    update_time = CURRENT_TIMESTAMP
FROM v LEFT JOIN cc ON cc.post_id = v.post_id
WHERE p.id = v.post_id AND p.deleted = 0
  AND (v.view_delta <> 0 OR v.like_delta <> 0 OR v.collect_delta <> 0
       OR p.comment_count <> COALESCE(cc.cnt, p.comment_count))`

// persistPostStats 落库帖子统计，成功后校准 Redis 中的评论数缓存。
func persistPostStats(deltas postStatsBuf) error {
	commentIDs, err := batchUpdatePostStats(deltas)
	if err != nil {
		return err
	}
	reconcilePostCommentCounts(commentIDs)
	return nil
}

// batchUpdatePostStats 批量更新帖子统计计数到数据库（按 id 升序取锁，见 batch_lock.go）。
// 返回本批重算了 comment_count 的帖子 ID（供落库后校准缓存）。
func batchUpdatePostStats(deltas postStatsBuf) ([]uuid.UUID, error) {
	rows := make([]postStatsRow, 0, len(deltas))
	ids := make([]uuid.UUID, 0, len(deltas))
	var commentIDs []uuid.UUID
	for postID, delta := range deltas {
		if delta.empty() {
			continue
		}
		rows = append(rows, postStatsRow{
			PostID:       postID,
			ViewDelta:    delta.ViewCount,
			CommentDirty: delta.CommentDirty,
			LikeDelta:    delta.LikeCount,
			CollectDelta: delta.CollectCount,
		})
		ids = append(ids, postID)
		if delta.CommentDirty {
			commentIDs = append(commentIDs, postID)
		}
	}
	if len(rows) == 0 {
		return nil, nil
	}
	sortByID(rows, func(r postStatsRow) uuid.UUID { return r.PostID })

	jsonBytes, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal post stats update rows: %w", err)
	}

	err = pgsql.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockRowsByID(tx, "domains.post", ids); err != nil {
			return err
		}
		res := tx.Exec(postStatsApplySQL, string(jsonBytes))
		if res.Error != nil {
			return fmt.Errorf("failed to execute post stats batch update: %w", res.Error)
		}
		logger.Log.Info(fmt.Sprintf("Successfully updated %d/%d post statistics", res.RowsAffected, len(rows)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return commentIDs, nil
}

// reconcilePostCommentCounts 落库提交后把 Redis post:stats 中低于 PG 的 comment_count 抬回 PG 值。
// 与点赞 #128 同一口径：缓存过期后由 PG 回填时，窗口内的评论尚未落库，回填值偏低。
// best-effort：失败只记日志，不影响落库与 offset 提交。
func reconcilePostCommentCounts(postIDs []uuid.UUID) {
	ids := sortedIDs(postIDs)
	if len(ids) == 0 {
		return
	}
	var rows []struct {
		ID           uuid.UUID
		CommentCount int64
	}
	if err := pgsql.DB.Table("domains.post").Select("id, comment_count").
		Where("id IN ? AND deleted = 0", ids).Scan(&rows).Error; err != nil {
		logger.Log.Warn("Failed to load post comment_count for cache reconcile: " + err.Error())
		return
	}
	counts := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		counts[r.ID] = r.CommentCount
	}
	fixed, err := redispkg.ReconcilePostCommentCounts(counts)
	if err != nil {
		logger.Log.Warn("Failed to reconcile post stats comment_count: " + err.Error())
	}
	if fixed > 0 {
		logger.Log.Info(fmt.Sprintf("Reconciled comment_count of %d cached post stats", fixed))
	}
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
