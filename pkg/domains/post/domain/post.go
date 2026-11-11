// Package domain 存放 post 领域的纯领域模型。
package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Post 帖子聚合根。
//
// 与旧 model.Post 字段、TableName、GORM tag 完全一致。
type Post struct {
	ID            uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CircleID      uuid.UUID      `json:"circle_id" gorm:"column:circle_id;type:uuid;not null"`
	UserID        uuid.UUID      `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	Type          int16          `json:"type" gorm:"column:type;type:smallint;default:1"`
	Title         string         `json:"title" gorm:"column:title;type:varchar(200);default:''"`
	Summary       string         `json:"summary" gorm:"column:summary;type:varchar(2000);default:''"`
	Content       string         `json:"content" gorm:"column:content;type:text;default:''"`
	MediaExtra    MediaExtraJSON `json:"media_extra" gorm:"column:media_extra;type:jsonb;default:'[]'::jsonb"`
	ViewCount     int            `json:"view_count" gorm:"column:view_count;default:0"`
	CommentCount  int            `json:"comment_count" gorm:"column:comment_count;default:0"`
	LikeCount     int            `json:"like_count" gorm:"column:like_count;default:0"`
	CollectCount  int            `json:"collect_count" gorm:"column:collect_count;default:0"`
	ShareCount    int            `json:"share_count" gorm:"column:share_count;default:0"` // 分享数（帖子统计消费者按 post_share 重算落库，读路径以 Redis post:sharecnt 为准，#232/#233）
	Hot           int            `json:"hot" gorm:"column:hot;default:0"`                 // 热度分（互动事件加权累积，CDC 同步 ES 排序）
	IsPinned      int16          `json:"is_pinned" gorm:"column:is_pinned;type:smallint;default:0"`
	IsEssence     int16          `json:"is_essence" gorm:"column:is_essence;type:smallint;default:0"`
	IsLock        int16          `json:"is_lock" gorm:"column:is_lock;type:smallint;default:0"`
	Status        int16          `json:"status" gorm:"column:status;type:smallint;default:1"`
	Deleted       int16          `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	LastReplyTime *time.Time     `json:"last_reply_time,omitempty" gorm:"column:last_reply_time"`
	CreateTime    time.Time      `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime    time.Time      `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (Post) TableName() string { return "domains.post" }

// PostType 帖子类型常量。
const (
	PostTypeTextImage = 1
	PostTypeVideo     = 2
	PostTypeVote      = 3
)

// PostStatus 帖子状态常量。
const (
	PostStatusDraft     = 0
	PostStatusPublished = 1
	PostStatusReviewing = 2
	PostStatusRejected  = 3
	PostStatusBlocked   = 4
)

// MediaExtraJSON 媒体扩展信息 JSON 类型（存储图片 URL 数组）。
//
// 与旧 model.MediaExtraJSON 完全一致：实现 sql.Scanner 和 driver.Valuer，
// 兼容老数据 "{}" → 空数组。
type MediaExtraJSON []string

// Scan 实现 sql.Scanner 接口。
func (m *MediaExtraJSON) Scan(value interface{}) error {
	if value == nil {
		*m = make(MediaExtraJSON, 0)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("MediaExtraJSON: cannot scan non-[]byte value")
	}
	if string(bytes) == "{}" {
		*m = make(MediaExtraJSON, 0)
		return nil
	}
	return json.Unmarshal(bytes, m)
}

// Value 实现 driver.Valuer 接口。
func (m MediaExtraJSON) Value() (driver.Value, error) {
	if len(m) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(m)
}

// 哨兵错误。
var (
	ErrPostNotFound = errors.New("post not found")
)

// PostLike 帖子点赞流水实体（供 like 领域 + post 领域共用）。
//
// 注意：PostLike 表的主键/索引策略由 like 领域维护。
// post 领域只读取它来判断"当前用户是否点赞"。
type PostLike struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	UserID     uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	PostID     uuid.UUID `json:"post_id" gorm:"column:post_id;type:uuid;not null"`
	Deleted    int16     `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (PostLike) TableName() string { return "domains.post_like" }

// 点赞状态常量。
const (
	PostLikeActive   = 0
	PostLikeCanceled = 1
)
