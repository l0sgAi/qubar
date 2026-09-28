package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	sharedomain "interestBar/pkg/shared/domain"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// 点赞事件消费者（设计见 docs/design/like-pipeline-fix-design.md §三 F2/F3）。
//
// 语义要点：
//   - 末态为准：事件 amount=+1 表示"期望已赞"、-1 表示"期望未赞"。同一 (type,user,target)
//     在一个 flush 窗口内只保留最后一条（writer 按 key 哈希分区，同一对事件同分区有序）。
//   - 计数由行状态迁移推导：只有 post_like/comment_like 行真正 0→1 / 1→0 时才改 like_count，
//     重复投递、已赞再赞都是 no-op（幂等）。
//   - 落库后提交：FetchMessage 拉取，flush 事务成功后才 CommitMessages；失败则合并回缓冲下轮重试。
//   - 关停排干：StopLikeEventConsumerGlobal 停止拉取 → 末次 flush + commit → 关闭 reader。

const (
	likeEventTypePost    = "post_like"
	likeEventTypeComment = "comment_like"

	defaultLikeFlushIntervalSeconds = 10
	likeCommitTimeout               = 10 * time.Second
)

// likeState 聚合后单个 (type,user,target) 的期望末态。
type likeState struct {
	EventType string
	UserID    uuid.UUID
	TargetID  uuid.UUID
	PostID    uuid.UUID // 仅评论点赞：冗余帖子ID
	Liked     bool
}

func likeStateKey(eventType string, userID, targetID uuid.UUID) string {
	return eventType + ":" + userID.String() + ":" + targetID.String()
}

// toLikeState 把事件转换为末态；无效事件（未知类型 / 空 ID / amount=0）返回 false。
func toLikeState(msg LikeEventMessage) (*likeState, bool) {
	if msg.Type != likeEventTypePost && msg.Type != likeEventTypeComment {
		return nil, false
	}
	if msg.UserID == uuid.Nil || msg.TargetID == uuid.Nil || msg.Amount == 0 {
		return nil, false
	}
	return &likeState{
		EventType: msg.Type,
		UserID:    msg.UserID,
		TargetID:  msg.TargetID,
		PostID:    msg.PostID,
		Liked:     msg.Amount > 0,
	}, true
}

// likeBuffer 一个 flush 窗口内的缓冲：末态 + 每分区待提交的最大 offset 消息。非并发安全，由聚合器加锁。
type likeBuffer struct {
	states  map[string]*likeState
	offsets map[int]kafka.Message
}

func newLikeBuffer() likeBuffer {
	return likeBuffer{
		states:  make(map[string]*likeState),
		offsets: make(map[int]kafka.Message),
	}
}

// add 记录一条消息。state 为 nil（无效/无法解析的消息）时仅推进 offset，使其随下次成功 flush 被提交跳过。
func (b *likeBuffer) add(state *likeState, km kafka.Message) {
	if state != nil {
		b.states[likeStateKey(state.EventType, state.UserID, state.TargetID)] = state
	}
	b.trackOffset(km)
}

func (b *likeBuffer) trackOffset(km kafka.Message) {
	if prev, ok := b.offsets[km.Partition]; !ok || km.Offset > prev.Offset {
		b.offsets[km.Partition] = km
	}
}

func (b *likeBuffer) empty() bool {
	return len(b.states) == 0 && len(b.offsets) == 0
}

// take 换出当前缓冲并重置。
func (b *likeBuffer) take() likeBuffer {
	out := *b
	*b = newLikeBuffer()
	return out
}

// restoreStates 把 flush 失败的末态合并回缓冲：已被更新事件覆盖的 key 不放回（保留更新的末态）。
func (b *likeBuffer) restoreStates(states map[string]*likeState) {
	for k, s := range states {
		if _, newer := b.states[k]; !newer {
			b.states[k] = s
		}
	}
}

// restoreOffsets 把未提交的 offset 合并回缓冲（每分区保留最大值）。
func (b *likeBuffer) restoreOffsets(offsets map[int]kafka.Message) {
	for _, km := range offsets {
		b.trackOffset(km)
	}
}

// messageCommitter 抽象 kafka.Reader 的提交能力（便于测试）。
type messageCommitter interface {
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}

// LikeEventAggregator 点赞事件聚合器。
type LikeEventAggregator struct {
	mu        sync.Mutex
	buf       likeBuffer
	persist   func(states []*likeState) error
	committer messageCommitter
	ticker    *time.Ticker
	stopChan  chan struct{}
	done      chan struct{} // run() 退出（末次 flush + commit 完成）后关闭
	stopped   bool
	cancel    context.CancelFunc // 停止 reader 的 FetchMessage
	readDone  chan struct{}      // 读循环退出后关闭
}

// 全局句柄：StartLikeEventConsumer 注册，供关停流程排干缓冲事件。
var (
	likeConsumerMu sync.Mutex
	likeAggregator *LikeEventAggregator
)

// StartLikeEventConsumer 启动点赞事件消费者
func StartLikeEventConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing like event consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		Resolver:  nil,
	}

	// CommitInterval=0：CommitMessages 同步提交，仅在 flush 落库成功后调用。
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.LikeEventTopic,
		GroupID:        conf.Config.Redpanda.LikeEventConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: 0,
		Dialer:         dialer,
	})

	logger.Log.Info(fmt.Sprintf("Like event consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.LikeEventTopic, conf.Config.Redpanda.LikeEventConsumerGroup))

	flushInterval := conf.Config.Redpanda.LikeEventFlushInterval
	if flushInterval <= 0 {
		flushInterval = defaultLikeFlushIntervalSeconds
	}

	readerCtx, readerCancel := context.WithCancel(context.Background())
	aggregator := &LikeEventAggregator{
		buf:       newLikeBuffer(),
		persist:   persistLikeStates,
		committer: r,
		ticker:    time.NewTicker(time.Duration(flushInterval) * time.Second),
		stopChan:  make(chan struct{}),
		done:      make(chan struct{}),
		cancel:    readerCancel,
		readDone:  make(chan struct{}),
	}

	likeConsumerMu.Lock()
	likeAggregator = aggregator
	likeConsumerMu.Unlock()

	// reader 在末次 flush + commit 之后才关闭（关闭后无法再提交）。
	go func() {
		aggregator.run()
		if err := r.Close(); err != nil {
			logger.Log.Error("Failed to close like event reader: " + err.Error())
		}
	}()

	go func() {
		defer close(aggregator.readDone)
		backoff := &readBackoff{}
		for {
			km, err := r.FetchMessage(readerCtx)
			if err != nil {
				if !waitAfterReadError(readerCtx, backoff, "like event", err) {
					return // 关停：cancel 已触发
				}
				continue
			}
			backoff.Reset()

			var likeMsg LikeEventMessage
			if err := json.Unmarshal(km.Value, &likeMsg); err != nil {
				logger.Log.Error("Failed to unmarshal like event: " + err.Error())
				aggregator.addMessage(nil, km) // 毒消息：仅推进 offset
				continue
			}
			state, ok := toLikeState(likeMsg)
			if !ok {
				logger.Log.Warn(fmt.Sprintf("Skip invalid like event: type=%s user=%s target=%s amount=%d",
					likeMsg.Type, likeMsg.UserID, likeMsg.TargetID, likeMsg.Amount))
				aggregator.addMessage(nil, km)
				continue
			}
			aggregator.addMessage(state, km)
		}
	}()

	return nil
}

func (a *LikeEventAggregator) addMessage(state *likeState, km kafka.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return // 未提交，重启后重投
	}
	a.buf.add(state, km)
}

func (a *LikeEventAggregator) run() {
	defer close(a.done)
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

// flush 落库 → 提交 offset。落库失败：末态与 offset 合并回缓冲，下轮重试；
// 提交失败：只记日志、不合并回——同分区后续更大 offset 的提交会覆盖它；即便未覆盖，
// 重启/再均衡后重投也是幂等 no-op。合并回反而可能让再均衡后失效的 offset 拖累之后每次提交。
func (a *LikeEventAggregator) flush() {
	a.mu.Lock()
	if a.buf.empty() {
		a.mu.Unlock()
		return
	}
	batch := a.buf.take()
	a.mu.Unlock()

	if len(batch.states) > 0 {
		states := make([]*likeState, 0, len(batch.states))
		for _, s := range batch.states {
			states = append(states, s)
		}
		if err := a.persist(states); err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to persist %d like states, will retry: %s", len(states), err.Error()))
			a.mu.Lock()
			a.buf.restoreStates(batch.states)
			a.buf.restoreOffsets(batch.offsets)
			a.mu.Unlock()
			return
		}
		logger.Log.Info(fmt.Sprintf("Successfully persisted %d like states", len(states)))
	}

	if len(batch.offsets) == 0 {
		return
	}
	msgs := make([]kafka.Message, 0, len(batch.offsets))
	for _, km := range batch.offsets {
		msgs = append(msgs, km)
	}
	ctx, cancel := context.WithTimeout(context.Background(), likeCommitTimeout)
	defer cancel()
	if err := a.committer.CommitMessages(ctx, msgs...); err != nil {
		logger.Log.Warn("Failed to commit like event offsets (redelivery is idempotent): " + err.Error())
	}
}

// StopLikeEventConsumer 优雅排干（幂等）：停止拉取 → 末次 flush + commit → 返回。
func (a *LikeEventAggregator) StopLikeEventConsumer() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	if a.readDone != nil {
		<-a.readDone // 读循环退出后不会再有 addMessage
	}
	close(a.stopChan)
	<-a.done
}

// StopLikeEventConsumerGlobal 停止全局点赞事件消费者（幂等）。
// 在 server 关停序列中调用，确保 flush 窗口内缓冲的点赞排干落库。未启动时为空操作。
func StopLikeEventConsumerGlobal() {
	likeConsumerMu.Lock()
	a := likeAggregator
	likeConsumerMu.Unlock()
	if a == nil {
		return
	}
	a.StopLikeEventConsumer()
}

// ===== 落库（F3：单条集合式 SQL，计数由行迁移推导）=====

// postLikeRow / commentLikeRow jsonb_to_recordset 行。ID 仅在新建行时使用（冲突时忽略）。
type postLikeRow struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
	PostID uuid.UUID `json:"post_id"`
	Liked  bool      `json:"liked"`
}

type commentLikeRow struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	CommentID uuid.UUID  `json:"comment_id"`
	PostID    *uuid.UUID `json:"post_id"` // 缺失时为 NULL
	Liked     bool       `json:"liked"`
}

// buildLikeRows 按目标类型拆分末态为落库行。
func buildLikeRows(states []*likeState) (posts []postLikeRow, comments []commentLikeRow) {
	for _, s := range states {
		switch s.EventType {
		case likeEventTypePost:
			posts = append(posts, postLikeRow{
				ID: sharedomain.NewID(), UserID: s.UserID, PostID: s.TargetID, Liked: s.Liked,
			})
		case likeEventTypeComment:
			var postID *uuid.UUID
			if s.PostID != uuid.Nil {
				p := s.PostID
				postID = &p
			}
			comments = append(comments, commentLikeRow{
				ID: sharedomain.NewID(), UserID: s.UserID, CommentID: s.TargetID, PostID: postID, Liked: s.Liked,
			})
		}
	}
	return posts, comments
}

// postLikeApplySQL 帖子点赞：行迁移（ins: 0→1 新建/复活，del: 1→0）→ 按迁移聚合 → 更新 like_count。
// v 中每个 (user_id, post_id) 至多一行（聚合器已去重），满足 ON CONFLICT DO UPDATE 单语句约束。
const postLikeApplySQL = `
WITH v AS (
	SELECT * FROM jsonb_to_recordset(?::jsonb)
	AS v(id uuid, user_id uuid, post_id uuid, liked boolean)
),
ins AS (
	INSERT INTO domains.post_like AS pl (id, user_id, post_id, deleted)
	SELECT v.id, v.user_id, v.post_id, 0 FROM v WHERE v.liked
	ON CONFLICT (user_id, post_id) DO UPDATE
		SET deleted = 0, update_time = CURRENT_TIMESTAMP
		WHERE pl.deleted = 1
	RETURNING pl.post_id
),
del AS (
	UPDATE domains.post_like pl
	SET deleted = 1, update_time = CURRENT_TIMESTAMP
	FROM v
	WHERE NOT v.liked AND pl.user_id = v.user_id AND pl.post_id = v.post_id AND pl.deleted = 0
	RETURNING pl.post_id
),
d AS (
	SELECT post_id, SUM(delta) AS delta
	FROM (SELECT post_id, 1 AS delta FROM ins UNION ALL SELECT post_id, -1 AS delta FROM del) x
	GROUP BY post_id
)
UPDATE domains.post p
SET like_count = GREATEST(p.like_count + d.delta, 0), update_time = CURRENT_TIMESTAMP
FROM d
WHERE p.id = d.post_id AND p.deleted = 0 AND d.delta <> 0`

// commentLikeApplySQL 评论点赞：同 postLikeApplySQL，复活时补齐缺失的冗余 post_id。
const commentLikeApplySQL = `
WITH v AS (
	SELECT * FROM jsonb_to_recordset(?::jsonb)
	AS v(id uuid, user_id uuid, comment_id uuid, post_id uuid, liked boolean)
),
ins AS (
	INSERT INTO domains.comment_like AS cl (id, user_id, comment_id, post_id, deleted)
	SELECT v.id, v.user_id, v.comment_id, v.post_id, 0 FROM v WHERE v.liked
	ON CONFLICT (user_id, comment_id) DO UPDATE
		SET deleted = 0, post_id = COALESCE(cl.post_id, EXCLUDED.post_id), update_time = CURRENT_TIMESTAMP
		WHERE cl.deleted = 1
	RETURNING cl.comment_id
),
del AS (
	UPDATE domains.comment_like cl
	SET deleted = 1, update_time = CURRENT_TIMESTAMP
	FROM v
	WHERE NOT v.liked AND cl.user_id = v.user_id AND cl.comment_id = v.comment_id AND cl.deleted = 0
	RETURNING cl.comment_id
),
d AS (
	SELECT comment_id, SUM(delta) AS delta
	FROM (SELECT comment_id, 1 AS delta FROM ins UNION ALL SELECT comment_id, -1 AS delta FROM del) x
	GROUP BY comment_id
)
UPDATE domains.comment c
SET like_count = GREATEST(c.like_count + d.delta, 0), update_time = CURRENT_TIMESTAMP
FROM d
WHERE c.id = d.comment_id AND c.deleted = 0 AND d.delta <> 0`

// persistLikeStates 单事务落库：每种目标类型各一条语句，与批大小无关。
func persistLikeStates(states []*likeState) error {
	posts, comments := buildLikeRows(states)
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		if len(posts) > 0 {
			payload, err := json.Marshal(posts)
			if err != nil {
				return fmt.Errorf("failed to marshal post like rows: %w", err)
			}
			if err := tx.Exec(postLikeApplySQL, string(payload)).Error; err != nil {
				return fmt.Errorf("failed to apply post likes: %w", err)
			}
		}
		if len(comments) > 0 {
			payload, err := json.Marshal(comments)
			if err != nil {
				return fmt.Errorf("failed to marshal comment like rows: %w", err)
			}
			if err := tx.Exec(commentLikeApplySQL, string(payload)).Error; err != nil {
				return fmt.Errorf("failed to apply comment likes: %w", err)
			}
		}
		return nil
	})
}

// StartLikeEventConsumerWithRetry 启动点赞事件消费者，带重试机制
func StartLikeEventConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartLikeEventConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start like event consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))
			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for like event consumer, giving up")
				return
			}
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying like event consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("Like event consumer started successfully")
			return
		}
	}
}
