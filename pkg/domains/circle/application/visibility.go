package application

import (
	"context"
	"slices"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// CircleVisibility 私密圈内容可见性 Facade（跨域只读，composition 桥接给 post / comment / like /
// collect / history / recommend / trending / discover）。
//
// 可见性定义：圈子不是私密圈（join_type≠2），或查看者是该圈成员且 status ∈ JoinedMemberStatuses
// （正常 / 禁言）。访客（viewerID==uuid.Nil）一律视为非成员；成员身份直查 PG，退出 / 移出立即生效。
type CircleVisibility interface {
	// CanView 查看者能否看到该圈内容。circleID 为 uuid.Nil（如草稿未绑定圈子）视为可见。
	CanView(ctx context.Context, viewerID, circleID uuid.UUID) (bool, error)
	// HiddenCircleIDs 查看者不可见的圈子 ID：所有私密圈 − 查看者已加入的私密圈。
	// ES 帖子查询用作 must_not terms circle_id，PG 回源结果按它过滤。
	HiddenCircleIDs(ctx context.Context, viewerID uuid.UUID) ([]uuid.UUID, error)
}

// NewCircleVisibility 构造 CircleVisibility。cache 可为 nil（每次直查 PG）。
func NewCircleVisibility(repo domain.CircleRepository, memberRepo domain.MemberRepository, cache domain.PrivateCircleCache) CircleVisibility {
	return newCircleVisibility(repo, memberRepo, cache)
}

func newCircleVisibility(repo domain.CircleRepository, memberRepo domain.MemberRepository, cache domain.PrivateCircleCache) *circleVisibility {
	return &circleVisibility{repo: repo, memberRepo: memberRepo, cache: cache}
}

// circleVisibility CircleVisibility 实现（circle service 内部的详情 / 圈内帖子判断共用同一实例逻辑）。
type circleVisibility struct {
	repo       domain.CircleRepository
	memberRepo domain.MemberRepository
	cache      domain.PrivateCircleCache
}

// CanView 实现 CircleVisibility。
func (v *circleVisibility) CanView(ctx context.Context, viewerID, circleID uuid.UUID) (bool, error) {
	if circleID == uuid.Nil {
		return true, nil
	}
	privateIDs, err := v.privateIDs(ctx)
	if err != nil {
		return false, err
	}
	if !slices.Contains(privateIDs, circleID) {
		return true, nil
	}
	return v.isJoined(ctx, viewerID, circleID)
}

// HiddenCircleIDs 实现 CircleVisibility。
func (v *circleVisibility) HiddenCircleIDs(ctx context.Context, viewerID uuid.UUID) ([]uuid.UUID, error) {
	privateIDs, err := v.privateIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(privateIDs) == 0 || viewerID == uuid.Nil {
		return privateIDs, nil
	}
	joined, err := v.memberRepo.ListJoinedMemberships(ctx, viewerID, privateIDs)
	if err != nil {
		return nil, err
	}
	hidden := make([]uuid.UUID, 0, len(privateIDs))
	for _, id := range privateIDs {
		if _, ok := joined[id]; !ok {
			hidden = append(hidden, id)
		}
	}
	return hidden, nil
}

// isJoined 查看者是否以正常 / 禁言身份加入该圈（访客恒 false）。
func (v *circleVisibility) isJoined(ctx context.Context, viewerID, circleID uuid.UUID) (bool, error) {
	if viewerID == uuid.Nil {
		return false, nil
	}
	joined, err := v.memberRepo.ListJoinedMemberships(ctx, viewerID, []uuid.UUID{circleID})
	if err != nil {
		return false, err
	}
	_, ok := joined[circleID]
	return ok, nil
}

// privateIDs 私密圈 ID 集合（cache-aside：Redis 失败降级直查 PG，回填 best-effort）。
func (v *circleVisibility) privateIDs(ctx context.Context) ([]uuid.UUID, error) {
	if v.cache != nil {
		ids, ok, err := v.cache.Get(ctx)
		if err == nil && ok {
			return ids, nil
		}
		if err != nil {
			logger.Log.Warn("Failed to read private circle ids cache: " + err.Error())
		}
	}
	ids, err := v.repo.ListPrivateIDs(ctx)
	if err != nil {
		return nil, err
	}
	if v.cache != nil {
		if err := v.cache.Set(ctx, ids); err != nil {
			logger.Log.Warn("Failed to cache private circle ids: " + err.Error())
		}
	}
	return ids, nil
}

// invalidatePrivateIDs 失效私密圈 ID 集合（建圈 / 修改 join_type 后调用，best-effort，TTL 兜底）。
func (v *circleVisibility) invalidatePrivateIDs(ctx context.Context) {
	if v.cache == nil {
		return
	}
	if err := v.cache.Delete(ctx); err != nil {
		logger.Log.Error("Failed to invalidate private circle ids cache: " + err.Error())
	}
}
