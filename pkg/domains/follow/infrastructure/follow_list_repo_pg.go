package infrastructure

import (
	"context"
	"fmt"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NewListRepository 构造关注列表仓储。
func NewListRepository(db *gorm.DB) domain.ListRepository {
	return &followRepoPG{db: db}
}

// 三种列表的查询骨架相同：一个等值条件定位 owner，(follow_time, id) 行比较做 keyset，
// 走 idx_user_follow_following / idx_user_follow_follower（都是 WHERE deleted = 0 的部分索引，键序与 ORDER BY 一致）。
// %s 处是 keyset 条件（首页为空）。
const (
	sqlListFollowing = `
SELECT f.id, f.followee_id AS user_id, f.follow_time
FROM domains.user_follow f
WHERE f.follower_id = @owner AND f.deleted = 0 %s
ORDER BY f.follow_time DESC, f.id DESC
LIMIT @limit`

	sqlListFollowers = `
SELECT f.id, f.follower_id AS user_id, f.follow_time
FROM domains.user_follow f
WHERE f.followee_id = @owner AND f.deleted = 0 %s
ORDER BY f.follow_time DESC, f.id DESC
LIMIT @limit`

	// 互关 = 我关注的人里，对方也关注我的（反向行按 uk_user_follow_pair 点查），不冗余 is_mutual。
	sqlListMutual = `
SELECT f.id, f.followee_id AS user_id, f.follow_time
FROM domains.user_follow f
WHERE f.follower_id = @owner AND f.deleted = 0
  AND EXISTS (
    SELECT 1 FROM domains.user_follow b
    WHERE b.follower_id = f.followee_id AND b.followee_id = f.follower_id AND b.deleted = 0
  ) %s
ORDER BY f.follow_time DESC, f.id DESC
LIMIT @limit`

	sqlListKeyset = `AND (f.follow_time, f.id) < (@after_time, @after_id)`

	// 两个方向各一个分支，都能用 uk_user_follow_pair (follower_id, followee_id)。
	sqlGetRelations = `
SELECT follower_id, followee_id
FROM domains.user_follow
WHERE deleted = 0
  AND ((follower_id = @viewer AND followee_id IN @targets)
    OR (followee_id = @viewer AND follower_id IN @targets))`
)

// ListEdges 见 domain.ListRepository。
func (r *followRepoPG) ListEdges(
	ctx context.Context, kind domain.ListKind, ownerID uuid.UUID, after *domain.FollowEdge, limit int,
) ([]domain.FollowEdge, error) {
	var tpl string
	switch kind {
	case domain.ListFollowing:
		tpl = sqlListFollowing
	case domain.ListFollowers:
		tpl = sqlListFollowers
	case domain.ListMutual:
		tpl = sqlListMutual
	default:
		return nil, fmt.Errorf("follow: unknown list kind %q", kind)
	}
	args := map[string]any{"owner": ownerID, "limit": limit}
	keyset := ""
	if after != nil {
		keyset = sqlListKeyset
		args["after_time"], args["after_id"] = after.FollowTime, after.ID
	}
	var edges []domain.FollowEdge
	if err := r.db.WithContext(ctx).Raw(fmt.Sprintf(tpl, keyset), args).Scan(&edges).Error; err != nil {
		return nil, err
	}
	return edges, nil
}

// GetRelations 见 domain.ListRepository。
func (r *followRepoPG) GetRelations(
	ctx context.Context, viewerID uuid.UUID, targetIDs []uuid.UUID,
) (map[uuid.UUID]domain.Relation, error) {
	relations := make(map[uuid.UUID]domain.Relation, len(targetIDs))
	if len(targetIDs) == 0 {
		return relations, nil
	}
	var rows []struct {
		FollowerID uuid.UUID
		FolloweeID uuid.UUID
	}
	err := r.db.WithContext(ctx).
		Raw(sqlGetRelations, map[string]any{"viewer": viewerID, "targets": targetIDs}).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.FollowerID == viewerID {
			relations[row.FolloweeID] |= domain.RelationFollowing
		} else {
			relations[row.FollowerID] |= domain.RelationFollowedBy
		}
	}
	return relations, nil
}
