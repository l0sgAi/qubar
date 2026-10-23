package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"interestBar/pkg/conf"
	noticedomain "interestBar/pkg/domains/notice/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// 通知事件类型 → domains.notification.notice_type 映射。
var noticeTypeCode = map[string]int16{
	NoticeTypeLikePost:     noticedomain.NoticeTypeLikePost,
	NoticeTypeLikeComment:  noticedomain.NoticeTypeLikeComment,
	NoticeTypeCollectPost:  noticedomain.NoticeTypeCollectPost,
	NoticeTypeCommentPost:  noticedomain.NoticeTypeCommentPost,
	NoticeTypeReplyComment: noticedomain.NoticeTypeReplyComment,
	NoticeTypeMention:      noticedomain.NoticeTypeMention,
	NoticeTypePostApproved: noticedomain.NoticeTypePostApproved,
	NoticeTypePostRejected: noticedomain.NoticeTypePostRejected,
	NoticeTypePostReported: noticedomain.NoticeTypePostReported,
	NoticeTypePostSelfHarm: noticedomain.NoticeTypePostSelfHarm,
	NoticeTypePostRemoved:  noticedomain.NoticeTypePostRemoved,
}

// 圈管类通知（举报 / 疑似自伤）的接收人：帖子所属圈子的圈主 + 管理员（role >= 20）且成员状态正常（与 circle 域取值一致）。
const (
	circleModeratorMinRole int16 = 20
	circleMemberNormal     int16 = 1
)

// isPostAuthorNotice 接收人为帖子作者、需反查帖子的类型。
func isPostAuthorNotice(t string) bool {
	switch t {
	case NoticeTypeLikePost, NoticeTypeCollectPost, NoticeTypeCommentPost,
		NoticeTypePostApproved, NoticeTypePostRejected, NoticeTypePostRemoved:
		return true
	}
	return false
}

// isCircleModeratorNotice 接收人为帖子所属圈子圈主 + 管理员的类型（#153）。
func isCircleModeratorNotice(t string) bool {
	return t == NoticeTypePostReported || t == NoticeTypePostSelfHarm
}

// noticeSnippetMaxRunes snippet 快照上限（与 VARCHAR(200) 对齐留余量）。
const noticeSnippetMaxRunes = 100

// NotificationEventAggregator 通知事件聚合器。
//
// 与统计聚合器不同：不做 delta 合并（每条事件都可能产生一行通知），
// 只在 flush 窗口内收集事件，批量反查接收人 + 规则过滤 + upsert。
type NotificationEventAggregator struct {
	mu       sync.Mutex
	events   []NotificationEventMessage
	ticker   *time.Ticker
	stopChan chan struct{}
	done     chan struct{} // run() 退出（末次 flush 完成）后关闭
	stopped  bool
	cancel   context.CancelFunc // 停掉 reader 的 ReadMessage 阻塞
}

// 全局句柄：StartNotificationEventConsumer 注册，供关停流程排干缓冲事件。
var (
	notificationConsumerMu sync.Mutex
	notificationAggregator *NotificationEventAggregator
)

// StartNotificationEventConsumer 启动通知事件消费者。
func StartNotificationEventConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing notification event consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.NoticeEventTopic,
		GroupID:        conf.Config.Redpanda.NoticeEventConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		Dialer:         dialer,
	})

	logger.Log.Info(fmt.Sprintf("Notification event consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.NoticeEventTopic, conf.Config.Redpanda.NoticeEventConsumerGroup))

	flushInterval := conf.Config.Redpanda.NoticeEventFlushInterval
	if flushInterval <= 0 {
		flushInterval = 5 // 兜底 5 秒
	}
	// reader 用可取消 ctx：关停时 cancel 解除 ReadMessage 阻塞，
	// 聚合器随后 close(stopChan) 触发最后一次 flush 排干缓冲。
	readerCtx, readerCancel := context.WithCancel(context.Background())
	aggregator := &NotificationEventAggregator{
		events:   make([]NotificationEventMessage, 0, 256),
		ticker:   time.NewTicker(time.Duration(flushInterval) * time.Second),
		stopChan: make(chan struct{}),
		done:     make(chan struct{}),
		cancel:   readerCancel,
	}

	notificationConsumerMu.Lock()
	notificationAggregator = aggregator
	notificationConsumerMu.Unlock()

	go aggregator.run()

	go func() {
		defer func() {
			if err := r.Close(); err != nil {
				logger.Log.Error("Failed to close notification event reader: " + err.Error())
			}
		}()
		for {
			msg, err := r.ReadMessage(readerCtx)
			if err != nil {
				if readerCtx.Err() != nil {
					return // 关停：cancel 已触发，退出读循环
				}
				errStr := err.Error()
				if containsIgnoreCase(errStr, "no data") ||
					containsIgnoreCase(errStr, "multiple Read calls return no data") ||
					containsIgnoreCase(errStr, "context deadline exceeded") ||
					containsIgnoreCase(errStr, "timeout") {
					// 队列无数据是常态，短退避后立即恢复轮询（长睡眠会
					// 让通知延迟积压，且阻塞关停——已改为可取消 ctx 双保险）。
					logger.Log.Debug("No messages in notification event queue, waiting...")
					if !sleepOrDone(readerCtx, 5*time.Second) {
						return
					}
					continue
				}
				logger.Log.Error("Failed to read notification event message: " + errStr)
				if !sleepOrDone(readerCtx, 5*time.Second) {
					return
				}
				continue
			}

			var event NotificationEventMessage
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				logger.Log.Error("Failed to unmarshal notification event: " + err.Error())
				continue
			}

			aggregator.addMessage(event)
		}
	}()

	return nil
}

// sleepOrDone 睡眠 d 或 ctx 取消即醒。返回 false 表示应停止循环。
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func (a *NotificationEventAggregator) addMessage(msg NotificationEventMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	a.events = append(a.events, msg)
}

func (a *NotificationEventAggregator) run() {
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

// StopNotificationEventConsumer 优雅排干（幂等）。先 cancel reader 停止拉取，
// 再 close stopChan 触发最后一次 flush，并等待其完成（缓冲事件落库）后返回。
// 在关停流程中调用。
func (a *NotificationEventAggregator) StopNotificationEventConsumer() {
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
	close(a.stopChan)
	<-a.done // 等末次 flush 完成，确保缓冲事件真正排干
}

// StopNotificationEventConsumerGlobal 停止全局通知事件消费者（幂等）。
// 在 server 关停序列中调用，确保 flush 窗口内缓冲的事件排干落库。
// 未启动（producer 初始化失败未走 Start）时为空操作。
func StopNotificationEventConsumerGlobal() {
	notificationConsumerMu.Lock()
	a := notificationAggregator
	notificationConsumerMu.Unlock()
	if a == nil {
		return
	}
	a.StopNotificationEventConsumer()
}

func (a *NotificationEventAggregator) flush() {
	a.mu.Lock()
	if len(a.events) == 0 {
		a.mu.Unlock()
		return
	}
	events := a.events
	a.events = make([]NotificationEventMessage, 0, 256)
	a.mu.Unlock()

	rows, unreadDeltas := buildNotificationRows(events)
	if len(rows) == 0 {
		return
	}

	if err := upsertNotifications(rows); err != nil {
		logger.Log.Error("Failed to batch upsert notifications: " + err.Error())
		return
	}
	incrNoticeUnread(unreadDeltas)
	notifyUnreadChanged(unreadDeltas)
	logger.Log.Info(fmt.Sprintf("Successfully upserted %d notifications", len(rows)))
}

// ===== 未读数变更回调（SSE 推送触发源）=====

// 包级 hook：composition 层注入 notice StreamHub.PublishBatch。
// 走函数注入而非 import notice application，守跨域红线（redpanda 不依赖领域应用层）。
var (
	noticeUnreadHookMu sync.RWMutex
	noticeUnreadHook   func([]uuid.UUID)
)

// SetNoticeUnreadHook 注册未读数变更回调（幂等覆盖，nil 跳过）。
func SetNoticeUnreadHook(f func([]uuid.UUID)) {
	noticeUnreadHookMu.Lock()
	noticeUnreadHook = f
	noticeUnreadHookMu.Unlock()
}

// notifyUnreadChanged 在未读计数器更新成功后通知回调（keys of unreadDeltas）。
func notifyUnreadChanged(deltas map[uuid.UUID]int64) {
	noticeUnreadHookMu.RLock()
	hook := noticeUnreadHook
	noticeUnreadHookMu.RUnlock()
	if hook == nil || len(deltas) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(deltas))
	for id := range deltas {
		ids = append(ids, id)
	}
	hook(ids)
}

// ===== 接收人解析与规则过滤 =====

// noticeRow 待 upsert 的通知行（jsonb_to_recordset 载荷）。
type noticeRow struct {
	ID          uuid.UUID  `json:"id"`
	RecipientID uuid.UUID  `json:"recipient_id"`
	ActorID     uuid.UUID  `json:"actor_id"`
	NoticeType  int16      `json:"notice_type"`
	PostID      *uuid.UUID `json:"post_id"`
	CommentID   *uuid.UUID `json:"comment_id"`
	Snippet     string     `json:"snippet"`
}

// postLookupRow 接收人/标题反查行（.Table 原始表名，避免跨域 import 实体）。
type postLookupRow struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	CircleID uuid.UUID
	Title    string
}

// commentLookupRow 评论反查行。
type commentLookupRow struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	ReplyToUserID *uuid.UUID
	Content       string
}

// buildNotificationRows 把一批事件解析成通知行 + 每接收人未读增量。
//
// 规则落点（设计 docs/notice-design.md §3.3）：
//   - 接收人消费端批量反查（post/comment 各一次 IN 查询；mention 自带接收人）
//   - 举报 / 疑似自伤（#153）：帖子所属圈子的圈主 + 管理员（circle_member 一次 IN 查询）
//   - R1 自动作不通知；R4 同 (recipient, comment) mention 优先；批内全键去重
//   - 目标已删（反查 miss）→ 丢弃
func buildNotificationRows(events []NotificationEventMessage) ([]noticeRow, map[uuid.UUID]int64) {
	postIDs, commentIDs := collectLookupIDs(events)
	posts := lookupPosts(postIDs)
	comments := lookupComments(commentIDs)
	moderators := lookupCircleModerators(moderatorCircleIDs(events, posts))

	// dedup 键: recipient|actor|type|post|comment（与 uk_notice_dedup 对齐）
	dedup := make(map[string]*noticeRow, len(events))
	// R4: (recipient|comment) → 是否已有 mention
	mentionIdx := make(map[string]bool)
	// 非 mention 候选，待 R4 过滤后合并
	type candidate struct {
		key string
		row *noticeRow
		r4  string // recipient|comment 键（无 comment 时含 post 防误伤）
	}
	var candidates []candidate

	addRow := func(recipient uuid.UUID, event NotificationEventMessage, snippet string) {
		// R1 自动作不通知
		if recipient == uuid.Nil || recipient == event.ActorID {
			return
		}
		key := fmt.Sprintf("%s|%s|%s|%s|%s",
			recipient, event.ActorID, event.Type, uuidPtrStr(event.PostID), uuidPtrStr(event.CommentID))
		if _, exists := dedup[key]; exists {
			return
		}
		row := &noticeRow{
			ID:          sharedomain.NewID(),
			RecipientID: recipient,
			ActorID:     event.ActorID,
			NoticeType:  noticeTypeCode[event.Type],
			PostID:      event.PostID,
			CommentID:   event.CommentID,
			Snippet:     truncateRunes(snippet, noticeSnippetMaxRunes),
		}
		dedup[key] = row
		r4Key := fmt.Sprintf("%s|%s", recipient, uuidPtrStr(event.CommentID))
		if event.Type == NoticeTypeMention {
			mentionIdx[r4Key] = true
		}
		candidates = append(candidates, candidate{key: key, row: row, r4: r4Key})
	}

	for _, event := range events {
		code, ok := noticeTypeCode[event.Type]
		if !ok || code == 0 {
			logger.Log.Error("Unknown notification event type: " + event.Type)
			continue
		}

		switch {
		case isPostAuthorNotice(event.Type):
			if event.PostID == nil {
				continue
			}
			post, ok := posts[*event.PostID]
			if !ok {
				continue // 目标已删
			}
			snippet := event.Snippet
			if event.Type != NoticeTypeCommentPost {
				snippet = post.Title // like/collect/审核结果 快照帖子标题（确认项 #4）
			}
			addRow(post.UserID, event, snippet)
			continue

		case isCircleModeratorNotice(event.Type):
			if event.PostID == nil {
				continue
			}
			post, ok := posts[*event.PostID]
			if !ok {
				continue // 目标已删
			}
			for _, recipient := range moderators[post.CircleID] {
				addRow(recipient, event, post.Title)
			}
			continue
		}

		switch event.Type {
		case NoticeTypeLikeComment:
			if event.CommentID == nil {
				continue
			}
			cm, ok := comments[*event.CommentID]
			if !ok {
				continue
			}
			addRow(cm.UserID, event, cm.Content)

		case NoticeTypeReplyComment:
			if event.CommentID == nil {
				continue
			}
			cm, ok := comments[*event.CommentID]
			if !ok || cm.ReplyToUserID == nil {
				continue
			}
			addRow(*cm.ReplyToUserID, event, event.Snippet)

		case NoticeTypeMention:
			for _, recipient := range event.MentionUserIDs {
				addRow(recipient, event, event.Snippet)
			}
		}
	}

	// R4 过滤 + 汇总
	rows := make([]noticeRow, 0, len(candidates))
	unreadDeltas := make(map[uuid.UUID]int64)
	for _, c := range candidates {
		// 同 (recipient, comment) 已有 mention → 丢弃 reply/comment_post（mention 优先）
		if c.row.NoticeType != noticedomain.NoticeTypeMention && c.row.CommentID != nil && mentionIdx[c.r4] {
			delete(dedup, c.key)
			continue
		}
		rows = append(rows, *c.row)
		unreadDeltas[c.row.RecipientID]++
	}
	return rows, unreadDeltas
}

// collectLookupIDs 收集本批事件需要反查的 post/comment ID。
func collectLookupIDs(events []NotificationEventMessage) ([]uuid.UUID, []uuid.UUID) {
	postSet := make(map[uuid.UUID]struct{})
	commentSet := make(map[uuid.UUID]struct{})
	for _, e := range events {
		if isPostAuthorNotice(e.Type) || isCircleModeratorNotice(e.Type) {
			if e.PostID != nil {
				postSet[*e.PostID] = struct{}{}
			}
			continue
		}
		switch e.Type {
		case NoticeTypeLikeComment, NoticeTypeReplyComment:
			if e.CommentID != nil {
				commentSet[*e.CommentID] = struct{}{}
			}
		}
	}
	return uuidSetSlice(postSet), uuidSetSlice(commentSet)
}

// lookupPosts 批量反查帖子作者与标题。
func lookupPosts(postIDs []uuid.UUID) map[uuid.UUID]postLookupRow {
	result := make(map[uuid.UUID]postLookupRow, len(postIDs))
	if len(postIDs) == 0 {
		return result
	}
	var rows []postLookupRow
	if err := pgsql.DB.WithContext(context.Background()).
		Table("domains.post").
		Select("id, user_id, circle_id, title").
		Where("id IN ? AND deleted = ?", postIDs, 0).
		Scan(&rows).Error; err != nil {
		logger.Log.Error("Failed to lookup posts for notifications: " + err.Error())
		return result
	}
	for _, r := range rows {
		result[r.ID] = r
	}
	return result
}

// moderatorCircleIDs 圈管类事件涉及的圈子（帖子已删的跳过）。
func moderatorCircleIDs(events []NotificationEventMessage, posts map[uuid.UUID]postLookupRow) []uuid.UUID {
	set := make(map[uuid.UUID]struct{})
	for _, e := range events {
		if !isCircleModeratorNotice(e.Type) || e.PostID == nil {
			continue
		}
		if p, ok := posts[*e.PostID]; ok {
			set[p.CircleID] = struct{}{}
		}
	}
	return uuidSetSlice(set)
}

// lookupCircleModerators 批量反查圈子的圈主 + 管理员（成员状态正常），走 idx_member_circle_role。
func lookupCircleModerators(circleIDs []uuid.UUID) map[uuid.UUID][]uuid.UUID {
	result := make(map[uuid.UUID][]uuid.UUID, len(circleIDs))
	if len(circleIDs) == 0 {
		return result
	}
	var rows []struct {
		CircleID uuid.UUID
		UserID   uuid.UUID
	}
	if err := pgsql.DB.WithContext(context.Background()).
		Table("domains.circle_member").
		Select("circle_id, user_id").
		Where("circle_id IN ? AND role >= ? AND status = ?", circleIDs, circleModeratorMinRole, circleMemberNormal).
		Scan(&rows).Error; err != nil {
		logger.Log.Error("Failed to lookup circle moderators for notifications: " + err.Error())
		return result
	}
	for _, r := range rows {
		result[r.CircleID] = append(result[r.CircleID], r.UserID)
	}
	return result
}

// lookupComments 批量反查评论作者/被回复人/正文。
func lookupComments(commentIDs []uuid.UUID) map[uuid.UUID]commentLookupRow {
	result := make(map[uuid.UUID]commentLookupRow, len(commentIDs))
	if len(commentIDs) == 0 {
		return result
	}
	var rows []commentLookupRow
	if err := pgsql.DB.WithContext(context.Background()).
		Table("domains.comment").
		Select("id, user_id, reply_to_user_id, content").
		Where("id IN ? AND deleted = ?", commentIDs, 0).
		Scan(&rows).Error; err != nil {
		logger.Log.Error("Failed to lookup comments for notifications: " + err.Error())
		return result
	}
	for _, r := range rows {
		result[r.ID] = r
	}
	return result
}

// upsertNotifications 批量 upsert（uk_notice_dedup 表达式索引锚点）。
//
// 冲突（重赞/at-least-once 重复投递）→ 复用行：重置未读 + 刷新 snippet/时间，不产生第二行。
func upsertNotifications(rows []noticeRow) error {
	jsonBytes, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("failed to marshal notification rows: %w", err)
	}
	sql := `INSERT INTO domains.notification (id, recipient_id, actor_id, notice_type, post_id, comment_id, snippet)
		SELECT * FROM jsonb_to_recordset(?::jsonb)
		AS v(id uuid, recipient_id uuid, actor_id uuid, notice_type smallint, post_id uuid, comment_id uuid, snippet varchar)
		ON CONFLICT (recipient_id, actor_id, notice_type,
			COALESCE(post_id, '00000000-0000-0000-0000-000000000000'::uuid),
			COALESCE(comment_id, '00000000-0000-0000-0000-000000000000'::uuid))
		WHERE deleted = 0
		DO UPDATE SET is_read = 0, snippet = EXCLUDED.snippet, update_time = CURRENT_TIMESTAMP`
	return pgsql.DB.Exec(sql, string(jsonBytes)).Error
}

// incrNoticeUnread 按接收人聚合累加未读计数（best-effort，软信号）。
func incrNoticeUnread(deltas map[uuid.UUID]int64) {
	if len(deltas) == 0 {
		return
	}
	ctx := context.Background()
	pipe := redispkg.Client.Pipeline()
	for userID, delta := range deltas {
		key := redispkg.GetNoticeUnreadKey(userID)
		pipe.IncrBy(ctx, key, delta)
		pipe.Expire(ctx, key, redispkg.NoticeUnreadTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Log.Error("Failed to incr notice unread counters: " + err.Error())
	}
}

// ===== 小工具 =====

func uuidPtrStr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func uuidSetSlice(set map[uuid.UUID]struct{}) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// StartNotificationEventConsumerWithRetry 启动通知事件消费者，带重试机制。
func StartNotificationEventConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartNotificationEventConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start notification event consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))
			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for notification event consumer, giving up")
				return
			}
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying notification event consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("Notification event consumer started successfully")
			return
		}
	}
}
