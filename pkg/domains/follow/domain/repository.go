package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// 哨兵错误。
var (
	// ErrCannotFollowSelf 不能关注 / 取关自己。
	ErrCannotFollowSelf = errors.New("cannot follow yourself")
	// ErrUserNotFound 目标用户不存在、已禁用或已删除。
	ErrUserNotFound = errors.New("user not found")
	// ErrFollowLimitReached 关注数已达上限（follow.max_following）。
	ErrFollowLimitReached = errors.New("following limit reached")
)

// RelationRepository 关注关系的写入与关系查询。
//
// Follow / Unfollow 把关系行变更和两侧计数放在同一个事务里，且只在关系实际变化时改计数。
type RelationRepository interface {
	// Follow 让 followerID 关注 followeeID。changed=false 表示本来就在关注（幂等，不动计数）。
	// 关注数已达 maxFollowing（必须 ≥ 1）时返回 ErrFollowLimitReached，关系不变。
	Follow(ctx context.Context, followerID, followeeID uuid.UUID, maxFollowing int) (changed bool, err error)
	// Unfollow 取消关注。changed=false 表示本来就没关注（幂等，不动计数）。
	Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) (changed bool, err error)
	// GetRelation 返回 viewerID 与 targetID 的关系（两个方向各查一次关系行，不读冗余字段）。
	GetRelation(ctx context.Context, viewerID, targetID uuid.UUID) (Relation, error)
}

// StatRepository 关注计数的读取与重算。
type StatRepository interface {
	// GetStat 返回用户的关注数 / 粉丝数；没有计数行时返回两个计数为 0 的值（不返回 nil）。
	GetStat(ctx context.Context, userID uuid.UUID) (*UserFollowStat, error)
	// RecountStat 按关系明细重算并覆盖该用户的两个计数，返回重算后的值。
	// 与并发的关注 / 取关互斥（锁计数行），结果始终等于明细。
	RecountStat(ctx context.Context, userID uuid.UUID) (*UserFollowStat, error)
}
