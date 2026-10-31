// Package domain 存放 comment 领域的纯领域模型。
//
// 与旧 model.Comment / model.CommentLike 字段、TableName、GORM tag 完全一致，
// 仅迁移到领域包内，并替换 BaseModel 为共享内核。
package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Comment 评论聚合根。
type Comment struct {
	ID            uuid.UUID       `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	PostID        uuid.UUID       `json:"post_id" gorm:"column:post_id;type:uuid;not null"`
	UserID        uuid.UUID       `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	RootID        *uuid.UUID      `json:"root_id,omitempty" gorm:"column:root_id;type:uuid"`
	ReplyToID     *uuid.UUID      `json:"reply_to_id,omitempty" gorm:"column:reply_to_id;type:uuid"`
	ReplyToUserID *uuid.UUID      `json:"reply_to_user_id,omitempty" gorm:"column:reply_to_user_id;type:uuid"`
	Content       string          `json:"content" gorm:"column:content;type:text;not null"`
	ExtraData     json.RawMessage `json:"extra_data" gorm:"column:extra_data;type:jsonb;default:'{}'::jsonb"`
	LikeCount     int             `json:"like_count" gorm:"column:like_count;default:0"`
	ReplyCount    int             `json:"reply_count" gorm:"column:reply_count;default:0"`
	Status        int16           `json:"status" gorm:"column:status;type:smallint;default:1"`
	Deleted       int16           `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	CreateTime    time.Time       `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime    time.Time       `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (Comment) TableName() string { return "domains.comment" }

// CommentStatus 评论状态常量。
const (
	CommentStatusNormal = 1 // 正常
	CommentStatusReview = 2 // 审核中
	CommentStatusHidden = 3 // 折叠/隐藏
)

// CommentLike 评论点赞流水实体（供 like 领域 + comment 领域共用）。
//
// 注意：CommentLike 表的主键/索引策略由 like 领域维护。
// comment 领域只读取它来判断"当前用户是否点赞"。
type CommentLike struct {
	ID         uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	UserID     uuid.UUID  `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	CommentID  uuid.UUID  `json:"comment_id" gorm:"column:comment_id;type:uuid;not null"`
	PostID     *uuid.UUID `json:"post_id,omitempty" gorm:"column:post_id;type:uuid"`
	Deleted    int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	CreateTime time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (CommentLike) TableName() string { return "domains.comment_like" }

// 点赞状态常量。
const (
	CommentLikeActive   = 0 // 有效点赞
	CommentLikeCanceled = 1 // 取消点赞
)

// 哨兵错误。
var (
	// ErrCommentNotFound 评论未找到。
	ErrCommentNotFound = errors.New("comment not found")
	// ErrPostNotFound 帖子未找到（发评论时校验用）。
	ErrPostNotFound = errors.New("post not found")
	// ErrPostLocked 帖子已锁定，不允许评论。
	ErrPostLocked = errors.New("post is locked, comments are not allowed")
	// ErrPostNotCommentable 帖子状态不允许评论（非已发布）。
	ErrPostNotCommentable = errors.New("cannot comment on this post")
	// ErrRootCommentNotFound 根评论未找到。
	ErrRootCommentNotFound = errors.New("root comment not found")
	// ErrRootCommentMismatch 根评论不属于该帖子。
	ErrRootCommentMismatch = errors.New("root comment does not belong to this post")
	// ErrRootCommentNotTopLevel root_id 指向的不是顶层评论（是一条回复）。
	ErrRootCommentNotTopLevel = errors.New("root comment is not a top-level comment")
	// ErrReplyTargetWithoutRoot 传了 reply_to_id 却没有 root_id（顶层评论不允许带 reply_to_id）。
	ErrReplyTargetWithoutRoot = errors.New("reply_to_id requires root_id")
	// ErrReplyTargetNotFound 被回复评论未找到。
	ErrReplyTargetNotFound = errors.New("reply target comment not found")
	// ErrReplyTargetNotInThread 被回复评论不在同一楼层（或不属于该帖子）。
	ErrReplyTargetNotInThread = errors.New("reply target does not belong to the same thread")
	// ErrEmptyContent 评论内容为空（清洗后）。
	ErrEmptyContent = errors.New("comment content is empty")
	// ErrInvalidCursor 游标格式非法（缺失字段、类型错误或 base64 损坏）。
	//
	// 由 infrastructure 层 decode 游标时返回，handler 据此映射 400 而非 500。
	// 防御点：游标来自用户可控的 query 参数，必须防御性解析，避免 panic。
	ErrInvalidCursor = errors.New("invalid cursor")
)
