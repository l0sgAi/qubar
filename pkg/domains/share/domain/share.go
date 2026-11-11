// Package domain 存放 share 领域的纯领域模型。
//
// share 领域是「帖子外部分享」用例的聚合点（#232）：前端拼链接 / 调系统分享面板，后端只记录分享。
// 同一用户对同一帖子只记一次、不可撤销；首次分享累加分享数与帖子热度，并发布分享事件（供声望消费）。
// post_share 记录表由本领域持有。
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// PostShare 帖子分享记录实体（表 domains.post_share）。每对 (user_id, post_id) 一行，只插入不更新。
type PostShare struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	UserID     uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	PostID     uuid.UUID `json:"post_id" gorm:"column:post_id;type:uuid;not null"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
}

// TableName 指定表名。
func (PostShare) TableName() string { return "domains.post_share" }

// SharedPost 可分享帖子的归属信息（分享事件需要作者与圈子）。
type SharedPost struct {
	ID       uuid.UUID
	AuthorID uuid.UUID
	CircleID uuid.UUID
}

// 哨兵错误。
var (
	// ErrPostNotFound 帖子不存在、未发布、已删除或对当前用户不可见（私密圈非成员），统一按不存在处理。
	ErrPostNotFound = errors.New("post not found")
	// ErrEventPublishFailed 分享事件未能投递（已删除刚插入的分享记录，客户端可重试）。
	ErrEventPublishFailed = errors.New("share event publish failed, please retry")
)
