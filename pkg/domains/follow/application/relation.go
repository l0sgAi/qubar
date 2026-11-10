package application

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
)

// RelationService 关注 / 取关。
type RelationService interface {
	// Follow 关注 followeeID（幂等：已关注时直接成功）。返回操作后的关系。
	// 关注自己返回 domain.ErrCannotFollowSelf；目标用户不存在 / 已禁用 / 已删除返回 domain.ErrUserNotFound；
	// 关注数已达上限返回包装了 domain.ErrFollowLimitReached 的错误（message 带上限值）。
	Follow(ctx context.Context, followerID, followeeID uuid.UUID) (*RelationVO, error)
	// Unfollow 取消关注（幂等：未关注时直接成功）。返回操作后的关系。
	// 不校验目标用户状态：对方被禁用 / 删除后仍然可以取关。
	Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) (*RelationVO, error)
}

// RelationVO 关注 / 取关的返回值：操作后查看者与目标用户的关系。
type RelationVO struct {
	Relation domain.Relation `json:"relation"`
}

func (s *followServiceImpl) Follow(ctx context.Context, followerID, followeeID uuid.UUID) (*RelationVO, error) {
	if followerID == followeeID {
		return nil, domain.ErrCannotFollowSelf
	}
	if err := s.requireUser(ctx, followeeID); err != nil {
		return nil, err
	}
	limit := s.maxFollowing()
	if _, err := s.relations.Follow(ctx, followerID, followeeID, limit); err != nil {
		if errors.Is(err, domain.ErrFollowLimitReached) {
			return nil, fmt.Errorf("%w: you can follow at most %d users", domain.ErrFollowLimitReached, limit)
		}
		return nil, err
	}
	return s.relationAfter(ctx, followerID, followeeID)
}

func (s *followServiceImpl) Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) (*RelationVO, error) {
	if followerID == followeeID {
		return nil, domain.ErrCannotFollowSelf
	}
	if _, err := s.relations.Unfollow(ctx, followerID, followeeID); err != nil {
		return nil, err
	}
	return s.relationAfter(ctx, followerID, followeeID)
}

// relationAfter 写入提交后回读关系（对方是否关注我只能查库得到）。
func (s *followServiceImpl) relationAfter(ctx context.Context, viewerID, targetID uuid.UUID) (*RelationVO, error) {
	relation, err := s.relations.GetRelation(ctx, viewerID, targetID)
	if err != nil {
		return nil, err
	}
	return &RelationVO{Relation: relation}, nil
}
