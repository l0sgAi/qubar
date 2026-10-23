// Package domain 存放 notice 领域的纯领域模型。
//
// notice 领域负责站内通知（消息中心）的读侧：列表/未读数/已读。
// 写路径不在本域：4 触发域（like/collect/comment/post）发 notification_events
// Redpanda 事件，NotificationEventConsumer 批量 upsert domains.notification。
// 设计见 docs/notice-design.md。
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// 通知类型（与 domains.notification.notice_type 列、Redpanda 事件 type 一一对应）。
const (
	NoticeTypeLikePost     int16 = 1 // 帖子被赞 → 帖子作者
	NoticeTypeLikeComment  int16 = 2 // 评论被赞 → 评论作者
	NoticeTypeCollectPost  int16 = 3 // 帖子被收藏 → 帖子作者
	NoticeTypeCommentPost  int16 = 4 // 帖子被评论(顶层) → 帖子作者
	NoticeTypeReplyComment int16 = 5 // 评论被回复 → 被回复评论作者
	NoticeTypeMention      int16 = 6 // @提及 → 被提及用户
	NoticeTypePostApproved int16 = 7 // 帖子人工审核通过 → 帖子作者（#118）
	NoticeTypePostRejected int16 = 8 // 帖子人工审核驳回 → 帖子作者（#118）

	// NoticeTypeMax 最大合法通知类型（列表 type 过滤校验上限）。
	NoticeTypeMax = NoticeTypePostRejected
)

// HidesActor 该类型是否对接收人隐藏 actor：审核结果通知不暴露审核人（actor_id 仍落库供排查）。
func HidesActor(noticeType int16) bool {
	return noticeType == NoticeTypePostApproved || noticeType == NoticeTypePostRejected
}

// 已读状态。
const (
	NoticeUnread int16 = 0
	NoticeRead   int16 = 1
)

// Notification 站内通知实体（domains.notification）。
//
// 去重模型：每对 (recipient, actor, type, target) 仅一行（uk_notice_dedup 表达式索引），
// 重复触发（重赞）upsert 复用行 + 重置未读；取消赞/收藏不回收（负向事件不发布）。
type Notification struct {
	ID          uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	RecipientID uuid.UUID  `json:"recipient_id" gorm:"column:recipient_id"`
	ActorID     uuid.UUID  `json:"actor_id" gorm:"column:actor_id"`
	NoticeType  int16      `json:"notice_type" gorm:"column:notice_type"`
	PostID      *uuid.UUID `json:"post_id,omitempty" gorm:"column:post_id"`
	CommentID   *uuid.UUID `json:"comment_id,omitempty" gorm:"column:comment_id"`
	Snippet     string     `json:"snippet" gorm:"column:snippet"`
	IsRead      int16      `json:"is_read" gorm:"column:is_read"`
	Deleted     int16      `json:"deleted" gorm:"column:deleted;default:0"`
	CreateTime  time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名（domains schema）。
func (Notification) TableName() string { return "domains.notification" }

// 哨兵错误。
var (
	// ErrNotificationNotFound 通知未找到。
	ErrNotificationNotFound = errors.New("notification not found")
	// ErrInvalidCursor 非法翻页游标。
	ErrInvalidCursor = errors.New("invalid cursor")
)
