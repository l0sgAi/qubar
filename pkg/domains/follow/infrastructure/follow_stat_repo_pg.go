package infrastructure

import (
	"context"
	"errors"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	sqlEnsureStat = `
INSERT INTO domains.user_follow_stat (user_id) VALUES (?)
ON CONFLICT (user_id) DO NOTHING`

	sqlRecountStat = `
UPDATE domains.user_follow_stat
SET following_count = (SELECT count(*) FROM domains.user_follow WHERE follower_id = ? AND deleted = 0),
    follower_count = (SELECT count(*) FROM domains.user_follow WHERE followee_id = ? AND deleted = 0),
    update_time = CURRENT_TIMESTAMP
WHERE user_id = ?`
)

// GetStat 读取计数行；没有行返回两个计数为 0 的值。
func (r *followRepoPG) GetStat(ctx context.Context, userID uuid.UUID) (*domain.UserFollowStat, error) {
	var stat domain.UserFollowStat
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&stat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &domain.UserFollowStat{UserID: userID}, nil
	}
	if err != nil {
		return nil, err
	}
	return &stat, nil
}

// RecountStat 按关系明细重算并覆盖该用户的两个计数。
//
// 先锁住计数行再统计：任何改动该用户计数的关注 / 取关事务都要拿这把行锁，
// 所以统计语句（READ COMMITTED 下取新快照）看到的明细与已提交的计数变更一致；
// 重算期间排队的事务在提交后再 ±1，不会丢更新。
func (r *followRepoPG) RecountStat(ctx context.Context, userID uuid.UUID) (*domain.UserFollowStat, error) {
	var stat domain.UserFollowStat
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(sqlEnsureStat, userID).Error; err != nil {
			return err
		}
		var locked domain.UserFollowStat
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).First(&locked).Error; err != nil {
			return err
		}
		if err := tx.Exec(sqlRecountStat, userID, userID, userID).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).First(&stat).Error
	})
	if err != nil {
		return nil, err
	}
	return &stat, nil
}
