// Package domain 存放 user 领域的纯领域模型：实体、值对象、常量、
// Repository/Cache 接口，以及给跨领域调用用的 UserBrief 视图。
//
// 依赖规则：本包不得 import 任何 gorm/redis/gin 等基础设施或框架库，
// 也不得 import 其他领域包（domains/circle 等）。
package domain

import (
	"time"

	"github.com/google/uuid"
)

// SysUser 用户聚合根。
//
// 与旧 model.SysUser 字段、TableName、GORM tag 完全一致，仅迁移归属：
// 现在它属于 user 领域。表名保持 domains.users 不变，无需 DB 变更。
type SysUser struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	Username    string    `json:"username" gorm:"column:username;not null"`
	Email       string    `json:"email" gorm:"column:email;unique;not null"`
	Phone       string    `json:"phone,omitempty" gorm:"column:phone"`
	Pwd         string    `json:"-" gorm:"column:pwd"`
	GoogleID    string    `json:"google_id,omitempty" gorm:"column:google_id"`
	XID         string    `json:"x_id,omitempty" gorm:"column:x_id"`
	GithubID    string    `json:"github_id,omitempty" gorm:"column:github_id"`
	MicrosoftID string    `json:"microsoft_id,omitempty" gorm:"column:microsoft_id"`
	AvatarURL   string    `json:"avatar_url,omitempty" gorm:"column:avatar_url"`
	// AgentCircleID 机器人绑定圈子ID（ai_agent.circle_id 的投影，供 @提及 作用域过滤）。
	// nil=普通用户或全局机器人；仅 role=2 行有意义。不回显（内部机制）。
	AgentCircleID *uuid.UUID `json:"-" gorm:"column:agent_circle_id;type:uuid"`
	Gender        int        `json:"gender" gorm:"column:gender;default:0"`
	Birthdate     *time.Time `json:"birthdate,omitempty" gorm:"column:birthdate"`
	Status        int        `json:"status" gorm:"column:status;default:1"`
	Role          int        `json:"role" gorm:"column:role;default:0"`
	Deleted       int        `json:"deleted" gorm:"column:deleted;default:0"`
	CreateTime    time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime    time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名（与旧 model.SysUser 保持一致）。
func (SysUser) TableName() string {
	return "domains.users"
}

// 用户状态常量（与旧 controller 中的字面量保持一致）。
const (
	UserStatusActive  = 1 // 正常
	UserStatusDeleted = 1 // deleted 字段：0=未删除
	UserNotDeleted    = 0
)

// UserBrief 是给跨领域调用用的用户精简视图（Facade DTO）。
//
// 当 post/comment/circle 领域需要"组装帖子作者信息"时，它们不应依赖
// 完整的 SysUser（含密码哈希等敏感字段），而是通过 UserFacade 拿到
// UserBrief——只含展示所需的字段。
//
// 故意使用 string 类型的 ID：跨领域通信中以字符串（UUIDv7）作为 ID
// 载体，避免在被调用方的 domain 包里强制引入 uuid 类型耦合。
type UserBrief struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url,omitempty"`
}
