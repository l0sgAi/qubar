package infrastructure

import (
	"context"
	"errors"

	"interestBar/pkg/domains/user/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 一列一条 upsert：只改自己这一列，并发修改不同设置项不会互相覆盖。
const sqlSetFollowListVisibility = `
INSERT INTO domains.user_setting AS s (user_id, follow_list_visibility)
VALUES (?, ?)
ON CONFLICT (user_id) DO UPDATE
SET follow_list_visibility = EXCLUDED.follow_list_visibility, update_time = CURRENT_TIMESTAMP
WHERE s.follow_list_visibility <> EXCLUDED.follow_list_visibility`

// 私信权限（#240），写法同上。
const sqlSetDMPermission = `
INSERT INTO domains.user_setting AS s (user_id, dm_permission)
VALUES (?, ?)
ON CONFLICT (user_id) DO UPDATE
SET dm_permission = EXCLUDED.dm_permission, update_time = CURRENT_TIMESTAMP
WHERE s.dm_permission <> EXCLUDED.dm_permission`

// userSettingRepoPG 基于 GORM 的 SettingRepository 实现。
type userSettingRepoPG struct {
	db *gorm.DB
}

// NewSettingRepository 构造基于 PostgreSQL 的 SettingRepository。
func NewSettingRepository(db *gorm.DB) domain.SettingRepository {
	return &userSettingRepoPG{db: db}
}

// Get 按主键读取；没有行返回默认值。
func (r *userSettingRepoPG) Get(ctx context.Context, userID uuid.UUID) (*domain.UserSetting, error) {
	var setting domain.UserSetting
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &domain.UserSetting{
			UserID: userID, FollowListVisibility: domain.FollowListPublic, DMPermission: domain.DMPermissionEveryone,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

// SetFollowListVisibility upsert 列表可见性；值没变时不产生写入。
func (r *userSettingRepoPG) SetFollowListVisibility(ctx context.Context, userID uuid.UUID, visibility int16) error {
	return r.db.WithContext(ctx).Exec(sqlSetFollowListVisibility, userID, visibility).Error
}

// SetDMPermission upsert 私信权限；值没变时不产生写入。
func (r *userSettingRepoPG) SetDMPermission(ctx context.Context, userID uuid.UUID, permission int16) error {
	return r.db.WithContext(ctx).Exec(sqlSetDMPermission, userID, permission).Error
}
