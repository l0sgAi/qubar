package infrastructure

import (
	"context"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NewFolloweeIDRepository 构造关注 ID 读取仓储。
func NewFolloweeIDRepository(db *gorm.DB) domain.FolloweeIDRepository {
	return &followRepoPG{db: db}
}

// ListFolloweeIDs 取 followerID 关注的用户 ID（关注时间倒序，最多 limit 个）。
// 过滤与排序都落在部分索引 idx_user_follow_following(follower_id, follow_time DESC, id DESC) WHERE deleted = 0 上。
func (r *followRepoPG) ListFolloweeIDs(ctx context.Context, followerID uuid.UUID, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		return []uuid.UUID{}, nil
	}
	ids := make([]uuid.UUID, 0)
	err := r.db.WithContext(ctx).Model(&domain.UserFollow{}).
		Where("follower_id = ? AND deleted = 0", followerID).
		Order("follow_time DESC, id DESC").
		Limit(limit).
		Pluck("followee_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}
