// Package application 是 follow 领域的应用层：关注 / 取关、关系状态、关注数 / 粉丝数。
//
// 文件按职责拆分：relation.go（关注 / 取关）、stats.go（计数与关系查询）、
// followee_ids.go（导出关注 ID 列表，供首页关注 tab 用）。
// FollowService 由各文件里的小接口组合而成；新增能力 = 新增文件 + 在 FollowService 里加一行。
// 设计见 docs/follow-design.md。
package application

import (
	"context"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
)

// UserChecker 跨域端口：目标用户是否存在且状态正常（未禁用、未删除）。
// 由 composition 用 user 领域的 UserFacade 桥接，判定与 UserService.GetByID 一致。
type UserChecker interface {
	Exists(ctx context.Context, userID uuid.UUID) (bool, error)
}

// FollowService 是 follow 领域的应用服务接口。
type FollowService interface {
	RelationService
	StatsService
	FolloweeIDsService
}

type followServiceImpl struct {
	relations domain.RelationRepository
	stats     domain.StatRepository
	users     UserChecker
	// maxFollowing 每次关注时读取关注上限（配置可热更新，所以是函数而不是固定值）。
	maxFollowing func() int
	followees    domain.FolloweeIDRepository
}

// NewFollowService 构造 FollowService。maxFollowing 返回当前生效的关注上限（≥ 1）。
func NewFollowService(
	relations domain.RelationRepository,
	stats domain.StatRepository,
	users UserChecker,
	maxFollowing func() int,
	followees domain.FolloweeIDRepository,
) FollowService {
	return &followServiceImpl{relations: relations, stats: stats, users: users, maxFollowing: maxFollowing, followees: followees}
}

// requireUser 目标用户不存在 / 已禁用 / 已删除时返回 domain.ErrUserNotFound。
func (s *followServiceImpl) requireUser(ctx context.Context, userID uuid.UUID) error {
	ok, err := s.users.Exists(ctx, userID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrUserNotFound
	}
	return nil
}
