package application

import (
	"context"

	"github.com/google/uuid"
)

// FolloweeIDsService 导出「某用户关注的全部用户 ID」，供其他领域经 composition 桥接读取
// （首页关注 tab，#220）。
type FolloweeIDsService interface {
	// ListFolloweeIDs 返回 userID 当前关注的用户 ID（关注时间倒序）。
	// 数量上限为 follow.max_following：上限被调低后超出的部分只取最近关注的。
	// 直接读关系表，不校验 userID 是否存在，也不过滤已禁用 / 已删除的被关注者。
	ListFolloweeIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

func (s *followServiceImpl) ListFolloweeIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	if userID == uuid.Nil {
		return []uuid.UUID{}, nil
	}
	return s.followees.ListFolloweeIDs(ctx, userID, s.maxFollowing())
}
