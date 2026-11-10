package application

import (
	"context"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
)

// StatsService 关注数 / 粉丝数与关系状态查询。
type StatsService interface {
	// GetStats 返回 userID 的关注数、粉丝数，以及 viewerID 与 userID 的关系。
	// viewerID 为 uuid.Nil（访客）或等于 userID（查看自己）时 relation 为 0。
	// 目标用户不存在 / 已禁用 / 已删除返回 domain.ErrUserNotFound。
	GetStats(ctx context.Context, viewerID, userID uuid.UUID) (*StatsVO, error)
}

// StatsVO 用户主页的关注统计。两个计数对所有人公开（含访客）。
type StatsVO struct {
	UserID         string          `json:"user_id"`
	FollowingCount int             `json:"following_count"`
	FollowerCount  int             `json:"follower_count"`
	Relation       domain.Relation `json:"relation"`
}

func (s *followServiceImpl) GetStats(ctx context.Context, viewerID, userID uuid.UUID) (*StatsVO, error) {
	if err := s.requireUser(ctx, userID); err != nil {
		return nil, err
	}
	stat, err := s.stats.GetStat(ctx, userID)
	if err != nil {
		return nil, err
	}
	vo := &StatsVO{
		UserID:         userID.String(),
		FollowingCount: stat.FollowingCount,
		FollowerCount:  stat.FollowerCount,
		Relation:       domain.RelationNone,
	}
	if viewerID != uuid.Nil && viewerID != userID {
		if vo.Relation, err = s.relations.GetRelation(ctx, viewerID, userID); err != nil {
			return nil, err
		}
	}
	return vo, nil
}
