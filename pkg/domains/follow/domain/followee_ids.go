package domain

import (
	"context"

	"github.com/google/uuid"
)

// FolloweeIDRepository 读取某用户关注的用户 ID（首页关注 tab 用，#220）。
type FolloweeIDRepository interface {
	// ListFolloweeIDs 返回 followerID 当前关注的用户 ID，按关注时间倒序，最多 limit 个。
	// 只含有效关系（已取关的不返回）；limit <= 0 返回空。
	ListFolloweeIDs(ctx context.Context, followerID uuid.UUID, limit int) ([]uuid.UUID, error)
}
