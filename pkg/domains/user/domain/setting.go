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

// 谁可以给我发私信（user_setting.dm_permission，#240）。message 域经 composition 桥接读取，判断发送方能否给本用户发私信。
const (
	// DMPermissionEveryone 所有人（默认，没有设置行时也是它）：仍受私信的未关注 5 条限制（#239）。
	DMPermissionEveryone int16 = 0
	// DMPermissionFollowing 仅我关注的人：只有本用户关注了的人能发。
	DMPermissionFollowing int16 = 1
	// DMPermissionMutual 仅互相关注：本用户关注了对方、且对方也关注了本用户。
	DMPermissionMutual int16 = 2
	// DMPermissionClosed 关闭：任何人都不能给本用户发私信。
	DMPermissionClosed int16 = 3
)

// UserSetting 用户设置（domains.user_setting）：每个用户至多一行，首次修改设置时建行。
// 后续设置项在本表加列（一列一个设置，按列单独更新）。
type UserSetting struct {
	UserID               uuid.UUID `json:"user_id" gorm:"type:uuid;primaryKey;column:user_id"`
	FollowListVisibility int16     `json:"follow_list_visibility" gorm:"column:follow_list_visibility;type:smallint;default:0"`
	DMPermission         int16     `json:"dm_permission" gorm:"column:dm_permission;type:smallint;default:0"`
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
	// SetDMPermission 写入私信权限（没有行时建行），只更新这一列。
	SetDMPermission(ctx context.Context, userID uuid.UUID, permission int16) error
}
