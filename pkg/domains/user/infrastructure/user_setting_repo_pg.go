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
		return &domain.UserSetting{UserID: userID, FollowListVisibility: domain.FollowListPublic}, nil
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
