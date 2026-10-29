// Package domain 存放 circle 领域的纯领域模型：实体、值对象、常量、
// Repository/Cache/Stats 接口，以及给跨领域调用用的 CircleBrief Facade 视图。
//
// 依赖规则：本包不得 import 任何 gorm/redis/gin 等基础设施或框架库，
// 也不得 import 其他领域包（domains/user 等）。
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Circle 兴趣圈/社区聚合根。
//
// 与旧 model.Circle 字段、TableName、GORM tag 完全一致，仅迁移归属。
type Circle struct {
	ID          uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	Name        string     `json:"name" gorm:"column:name;type:varchar(50);not null"`
	Slug        string     `json:"slug,omitempty" gorm:"column:slug;type:varchar(60)"`
	AvatarURL   string     `json:"avatar_url,omitempty" gorm:"column:avatar_url;type:varchar(500)"`
	CoverURL    string     `json:"cover_url,omitempty" gorm:"column:cover_url;type:varchar(500)"`
	Description string     `json:"description" gorm:"column:description;type:varchar(2000);not null"`
	Rule        string     `json:"rule,omitempty" gorm:"column:rule;type:text"`
	CreatorID   uuid.UUID  `json:"creator_id" gorm:"column:creator_id;type:uuid;not null"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty" gorm:"column:category_id;type:uuid"`
	Hot         int        `json:"hot" gorm:"column:hot;default:0"`
	MemberCount int        `json:"member_count" gorm:"column:member_count;default:0"`
	PostCount   int        `json:"post_count" gorm:"column:post_count;default:0"`
	JoinType    int16      `json:"join_type" gorm:"column:join_type;type:smallint;default:0"`
	Status      int16      `json:"status" gorm:"column:status;type:smallint;default:1"`
	Deleted     int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	CreateTime  time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名（与旧 model.Circle 一致）。
func (Circle) TableName() string {
	return "domains.circle"
}

// CircleJoinType 加入方式常量。
//
// 只允许写入 0（公开）/ 2（私密）。1（需审核）已下线：存量由迁移转为 2，
// 常量仅保留作历史取值说明，任何路径都不再写入。
const (
	CircleJoinTypeDirect   = 0 // 公开(直接加入)
	CircleJoinTypeApproval = 1 // 需审核（已下线，不再写入）
	CircleJoinTypePrivate  = 2 // 私密(邀请制)
)

// CircleStatus 圈子状态常量。
const (
	CircleStatusPending = 0 // 审核中
	CircleStatusNormal  = 1 // 正常
	CircleStatusBanned  = 2 // 被封禁/冻结
)

// CircleMember 圈子成员关系与权限实体。
type CircleMember struct {
	ID          uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CircleID    uuid.UUID  `json:"circle_id" gorm:"column:circle_id;type:uuid;not null"`
	UserID      uuid.UUID  `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	Role        int16      `json:"role" gorm:"column:role;type:smallint;default:10"`
	Status      int16      `json:"status" gorm:"column:status;type:smallint;default:1"`
	MuteEndTime *time.Time `json:"mute_end_time,omitempty" gorm:"column:mute_end_time"`
	IsTop       int16      `json:"is_top" gorm:"column:is_top;type:smallint;default:0"`
	IsDisturb   int16      `json:"is_disturb" gorm:"column:is_disturb;type:smallint;default:0"`
	CreateTime  time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名（与旧 model.CircleMember 一致）。
func (CircleMember) TableName() string {
	return "domains.circle_member"
}

// CircleMemberRole 角色常量。
const (
	MemberRoleMember = 10 // 普通成员
	MemberRoleAdmin  = 20 // 管理员
	MemberRoleOwner  = 30 // 圈主
)

// CircleMemberStatus 成员状态常量。
const (
	MemberStatusPending = 0 // 待审核(申请中)：入圈审核已下线，存量由迁移转为 4，不再写入
	MemberStatusNormal  = 1 // 正常
	MemberStatusMuted   = 2 // 禁言
	MemberStatusBanned  = 3 // 拉黑/踢出
	MemberStatusLeft    = 4 // 已退出
)

// JoinedMemberStatuses 「已加入」口径：正常 + 禁言（禁言仍计为成员，拉黑/退出/申请中不算）。
var JoinedMemberStatuses = []int16{MemberStatusNormal, MemberStatusMuted}
