package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// 关注列表 / 粉丝列表可见性（user_setting.follow_list_visibility）：一个开关同时控制两个列表。
const (
	// FollowListPublic 公开：所有人（含访客）可看名单。默认值，没有设置行时也是它。
	FollowListPublic int16 = 0
	// FollowListPrivate 私密：只有本人能看名单；关注数 / 粉丝数仍然公开。
	FollowListPrivate int16 = 1
)

// UserSetting 用户设置（domains.user_setting）：每个用户至多一行，首次修改设置时建行。
// 后续设置项在本表加列（一列一个设置，按列单独更新）。
type UserSetting struct {
	UserID               uuid.UUID `json:"user_id" gorm:"type:uuid;primaryKey;column:user_id"`
	FollowListVisibility int16     `json:"follow_list_visibility" gorm:"column:follow_list_visibility;type:smallint;default:0"`
	CreateTime           time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime           time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (UserSetting) TableName() string { return "domains.user_setting" }

// IsValidFollowListVisibility 判断取值是否合法（库里没有 CHECK，由应用层校验）。
func IsValidFollowListVisibility(v int16) bool {
	return v == FollowListPublic || v == FollowListPrivate
}

// SettingRepository 用户设置的持久化接口（由 infrastructure 实现）。
type SettingRepository interface {
	// Get 读取用户设置；没有设置行时返回全部为默认值的对象（不返回 nil）。
	Get(ctx context.Context, userID uuid.UUID) (*UserSetting, error)
	// SetFollowListVisibility 写入列表可见性（没有行时建行），只更新这一列。
	SetFollowListVisibility(ctx context.Context, userID uuid.UUID, visibility int16) error
}
