// Package domain 存放 follow 领域的纯领域模型：关注关系、关注计数、关系状态与端口接口。
//
// 依赖规则：本包不得 import 任何 gorm/redis/hertz 等基础设施或框架库，也不得 import 其他领域包。
// 设计见 docs/follow-design.md。
package domain

import (
	"time"

	"github.com/google/uuid"
)

// UserFollow 关注关系（domains.user_follow）：每对 follower+followee 至多一行。
//
// 取关置 Deleted=1，再次关注复用该行并刷新 FollowTime；因此 ID 不代表关注时间，
// 列表排序一律用 (FollowTime, ID)。
type UserFollow struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	FollowerID uuid.UUID `json:"follower_id" gorm:"column:follower_id;type:uuid;not null"`
	FolloweeID uuid.UUID `json:"followee_id" gorm:"column:followee_id;type:uuid;not null"`
	FollowTime time.Time `json:"follow_time" gorm:"column:follow_time"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	Deleted    int16     `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
}

// TableName 指定表名。
func (UserFollow) TableName() string { return "domains.user_follow" }

// UserFollowStat 用户关注计数（domains.user_follow_stat）：与关系行同事务更新。
// 用户首次关注 / 被关注时建行，无行视为两个计数都是 0。
type UserFollowStat struct {
	UserID         uuid.UUID `json:"user_id" gorm:"type:uuid;primaryKey;column:user_id"`
	FollowingCount int       `json:"following_count" gorm:"column:following_count;default:0"`
	FollowerCount  int       `json:"follower_count" gorm:"column:follower_count;default:0"`
	CreateTime     time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime     time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (UserFollowStat) TableName() string { return "domains.user_follow_stat" }

// Relation 查看者与目标用户的关系（接口契约值，不落库）。
// 两个方向各占一位：bit0=我关注了对方，bit1=对方关注了我。
type Relation int

const (
	// RelationNone 未关注（访客、查看自己时也是它）。
	RelationNone Relation = 0
	// RelationFollowing 我关注了对方。
	RelationFollowing Relation = 1
	// RelationFollowedBy 对方关注了我。
	RelationFollowedBy Relation = 2
	// RelationMutual 互相关注。
	RelationMutual Relation = 3
)

// RelationOf 由两个方向的关注状态得出关系值。
func RelationOf(following, followedBy bool) Relation {
	r := RelationNone
	if following {
		r |= RelationFollowing
	}
	if followedBy {
		r |= RelationFollowedBy
	}
	return r
}
