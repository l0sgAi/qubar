// Package domain 存放 collect 领域的纯领域模型。
//
// collect 领域是「帖子收藏」用例的聚合点（仅针对 post，评论无收藏语义）。
// 它管理"收藏/取消收藏"原子操作 + 异步事件发布；post_collect 流水表也由本领域持有。
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// PostCollect 帖子收藏流水实体（表 domains.post_collect）。
type PostCollect struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	UserID     uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	PostID     uuid.UUID `json:"post_id" gorm:"column:post_id;type:uuid;not null"`
	Deleted    int16     `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (PostCollect) TableName() string { return "domains.post_collect" }

// 收藏状态常量。
const (
	// PostCollectActive 有效收藏。
	PostCollectActive = 0
	// PostCollectCanceled 取消收藏。
	PostCollectCanceled = 1
)

// ToggleResult 收藏设值结果（与 redis.CollectSetResult 值一致）。
type ToggleResult int

const (
	// ToggleResultCollected 收藏成功（+1）。
	ToggleResultCollected ToggleResult = 1
	// ToggleResultUncollected 取消收藏（-1）。
	ToggleResultUncollected ToggleResult = -1
	// ToggleResultUnchanged 已处于期望状态，未变化。
	ToggleResultUnchanged ToggleResult = 0
)

// Action 期望动作（请求可选字段）。空 = 切换（以真实当前状态取反）。
const (
	// ActionCollect 期望已收藏（幂等）。
	ActionCollect = "collect"
	// ActionUncollect 期望未收藏（幂等）。
	ActionUncollect = "uncollect"
)

// ResolveWant 由真实当前状态与期望动作得出期望状态。
// action 为空时切换；"collect"/"uncollect" 为显式期望状态，重试/双击天然幂等。
func ResolveWant(current bool, action string) (bool, error) {
	switch action {
	case "":
		return !current, nil
	case ActionCollect:
		return true, nil
	case ActionUncollect:
		return false, nil
	default:
		return false, ErrInvalidAction
	}
}

// Int64 返回 ToggleResult 的 int64 值（用于事件发布的 amount 字段）。
func (r ToggleResult) Int64() int64 { return int64(r) }

// 哨兵错误。
var (
	// ErrPostNotFound 帖子未找到。
	ErrPostNotFound = errors.New("post not found")
	// ErrInvalidCursor 列表游标非法。
	ErrInvalidCursor = errors.New("invalid search_after cursor")
	// ErrInvalidAction 无效的期望动作。
	ErrInvalidAction = errors.New("invalid action, must be 'collect', 'uncollect' or empty")
	// ErrEventPublishFailed 收藏事件未能投递（已回滚流水与缓存，客户端可重试）。
	ErrEventPublishFailed = errors.New("collect event publish failed, please retry")
)
