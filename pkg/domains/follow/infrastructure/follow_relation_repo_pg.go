package infrastructure

import (
	"bytes"
	"context"

	"interestBar/pkg/domains/follow/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 关系行：每对用户一行（uk_user_follow_pair），取关后再关注复用该行并刷新 follow_time。
// ON CONFLICT 的 WHERE 让「已在关注」时影响 0 行，据此判断关系是否实际变化。
const (
	sqlFollowUpsert = `
INSERT INTO domains.user_follow AS f (id, follower_id, followee_id)
VALUES (?, ?, ?)
ON CONFLICT (follower_id, followee_id) DO UPDATE
SET deleted = 0, follow_time = CURRENT_TIMESTAMP, update_time = CURRENT_TIMESTAMP
WHERE f.deleted <> 0`

	sqlUnfollow = `
UPDATE domains.user_follow
SET deleted = 1, update_time = CURRENT_TIMESTAMP
WHERE follower_id = ? AND followee_id = ? AND deleted = 0`
)

// 计数行：首次出现时建行。关注数的自增带上限条件，影响 0 行即已达上限。
const (
	sqlIncrFollowing = `
INSERT INTO domains.user_follow_stat AS s (user_id, following_count)
VALUES (?, 1)
ON CONFLICT (user_id) DO UPDATE
SET following_count = s.following_count + 1, update_time = CURRENT_TIMESTAMP
WHERE s.following_count < ?`

	sqlIncrFollower = `
INSERT INTO domains.user_follow_stat AS s (user_id, follower_count)
VALUES (?, 1)
ON CONFLICT (user_id) DO UPDATE
SET follower_count = s.follower_count + 1, update_time = CURRENT_TIMESTAMP`

	sqlDecrFollowing = `
UPDATE domains.user_follow_stat
SET following_count = GREATEST(following_count - 1, 0), update_time = CURRENT_TIMESTAMP
WHERE user_id = ?`

	sqlDecrFollower = `
UPDATE domains.user_follow_stat
SET follower_count = GREATEST(follower_count - 1, 0), update_time = CURRENT_TIMESTAMP
WHERE user_id = ?`
)

// Follow 关注：关系行 upsert + 两侧计数自增在同一事务内。
//
// 并发：
//   - 同一对用户并发重复关注：在关系行（唯一索引）上串行，后到者影响 0 行，不动计数。
//   - 上限：关注者计数行的条件自增由行锁串行化，影响 0 行即返回 ErrFollowLimitReached，事务回滚（关系行一并撤销）。
//   - A 关注 B 与 B 关注 A 并发：两个事务的关系行不同，计数行都按 user_id 升序更新，不会互相等待成环。
func (r *followRepoPG) Follow(ctx context.Context, followerID, followeeID uuid.UUID, maxFollowing int) (bool, error) {
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(sqlFollowUpsert, sharedomain.NewID(), followerID, followeeID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 本来就在关注
		}
		if maxFollowing < 1 {
			return domain.ErrFollowLimitReached
		}
		changed = true
		return inStatOrder(followerID, followeeID,
			func() error {
				inc := tx.Exec(sqlIncrFollowing, followerID, maxFollowing)
				if inc.Error == nil && inc.RowsAffected == 0 {
					return domain.ErrFollowLimitReached
				}
				return inc.Error
			},
			func() error { return tx.Exec(sqlIncrFollower, followeeID).Error },
		)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// Unfollow 取关：关系行置 deleted=1 + 两侧计数自减在同一事务内；本来就没关注时不动计数。
func (r *followRepoPG) Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(sqlUnfollow, followerID, followeeID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 本来就没关注
		}
		changed = true
		return inStatOrder(followerID, followeeID,
			func() error { return tx.Exec(sqlDecrFollowing, followerID).Error },
			func() error { return tx.Exec(sqlDecrFollower, followeeID).Error },
		)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// inStatOrder 按 user_id 升序执行两侧计数行的更新（onFollower 改关注者的行，onFollowee 改被关注者的行）。
// 所有同时改两行计数的事务都走这里，锁顺序全局一致，互相并发关注不会死锁。
func inStatOrder(followerID, followeeID uuid.UUID, onFollower, onFollowee func() error) error {
	first, second := onFollower, onFollowee
	if bytes.Compare(followeeID[:], followerID[:]) < 0 {
		first, second = onFollowee, onFollower
	}
	if err := first(); err != nil {
		return err
	}
	return second()
}

// GetRelation 一次查询取两个方向的有效关系行（走 uk_user_follow_pair）。
func (r *followRepoPG) GetRelation(ctx context.Context, viewerID, targetID uuid.UUID) (domain.Relation, error) {
	var followers []uuid.UUID
	err := r.db.WithContext(ctx).Model(&domain.UserFollow{}).
		Where("deleted = 0 AND ((follower_id = ? AND followee_id = ?) OR (follower_id = ? AND followee_id = ?))",
			viewerID, targetID, targetID, viewerID).
		Pluck("follower_id", &followers).Error
	if err != nil {
		return domain.RelationNone, err
	}
	var following, followedBy bool
	for _, id := range followers {
		if id == viewerID {
			following = true
		} else {
			followedBy = true
		}
	}
	return domain.RelationOf(following, followedBy), nil
}
