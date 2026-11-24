package application

import (
	"context"
	"fmt"
	"time"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
)

const (
	// DefaultListSize / MaxListSize 列表页大小：不传或 ≤0 取默认值，超过上限按上限。
	DefaultListSize = 20
	MaxListSize     = 50
	// MaxRelationTargets 批量关系查询一次最多带的用户 ID 数。
	MaxRelationTargets = 100
	// maxListRounds 一页最多向仓储取几轮。列表会过滤已禁用 / 已删除的用户，过滤后不足一页时接着往后取，
	// 避免返回「空页但还有下一页」；轮数有上限，极端情况下一页可以少于 size 条（has_more 仍然准确）。
	maxListRounds = 3
)

// UserBrief 列表项里的用户简要信息。
type UserBrief struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
}

// UserBriefReader 跨域端口：批量读取用户简要信息。
// 只返回状态正常的用户：不存在 / 已禁用 / 已删除的 ID 不出现在 map 里。由 composition 用 UserFacade.GetBriefs 桥接。
type UserBriefReader interface {
	GetBriefs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]UserBrief, error)
}

// ListVisibilityReader 跨域端口：用户是否把关注 / 粉丝列表设为私密（user 域的用户设置，没有设置时为公开）。
type ListVisibilityReader interface {
	IsFollowListPrivate(ctx context.Context, userID uuid.UUID) (bool, error)
}

// ListDeps 列表能力的依赖，打包成一个参数传给 NewFollowService。
type ListDeps struct {
	Repo       domain.ListRepository
	Users      UserBriefReader
	Visibility ListVisibilityReader
}

// ListService 关注 / 粉丝 / 互关列表与批量关系查询。
//
// 列表都是游标分页，按关注时间倒序；cursor 为空表示第一页，非法或不属于该列表的游标返回 domain.ErrInvalidCursor。
// 列表不返回已禁用 / 已删除的用户（计数不因此调整，两者可能有少量差异）。
type ListService interface {
	// ListFollowing 返回 userID 关注的人。viewerID 为 uuid.Nil 表示访客。
	// userID 不存在 / 已禁用 / 已删除返回 domain.ErrUserNotFound；
	// userID 把列表设为私密且 viewerID 不是本人返回 domain.ErrFollowListPrivate。
	ListFollowing(ctx context.Context, viewerID, userID uuid.UUID, cursor string, size int) (*FollowListVO, error)
	// ListFollowers 返回关注 userID 的人，规则同 ListFollowing。
	ListFollowers(ctx context.Context, viewerID, userID uuid.UUID, cursor string, size int) (*FollowListVO, error)
	// ListMutual 返回与 userID 互相关注的人，按 userID 关注对方的时间倒序。
	// 只给本人用（handler 传当前登录用户），不受列表隐私开关影响。
	ListMutual(ctx context.Context, userID uuid.UUID, cursor string, size int) (*FollowListVO, error)
	// GetRelations 批量返回 viewerID 与 targetIDs 中每个用户的关系（没有关系、目标不存在或是自己都为 0）。
	// 超过 MaxRelationTargets 个返回包装了 domain.ErrTooManyTargets 的错误。
	GetRelations(ctx context.Context, viewerID uuid.UUID, targetIDs []uuid.UUID) (*RelationsVO, error)
}

// FollowListItemVO 列表项。
type FollowListItemVO struct {
	User UserBrief `json:"user"`
	// FollowTime 关注时间：关注 / 互关列表是列表所属用户关注 TA 的时间，粉丝列表是 TA 关注列表所属用户的时间。
	FollowTime time.Time `json:"follow_time"`
	// Relation 查看者（不是列表所属用户）与该用户的关系；访客、或该用户就是查看者本人时为 0。
	Relation domain.Relation `json:"relation"`
}

// FollowListVO 一页列表。HasMore 为 false 时 NextCursor 为空串。
type FollowListVO struct {
	Items      []FollowListItemVO `json:"items"`
	HasMore    bool               `json:"has_more"`
	NextCursor string             `json:"next_cursor"`
}

// RelationsVO 批量关系查询结果：key 是请求里的每个用户 ID。
type RelationsVO struct {
	Relations map[string]domain.Relation `json:"relations"`
}

func (s *followServiceImpl) ListFollowing(ctx context.Context, viewerID, userID uuid.UUID, cursor string, size int) (*FollowListVO, error) {
	return s.listOf(ctx, viewerID, domain.ListFollowing, userID, cursor, size)
}

func (s *followServiceImpl) ListFollowers(ctx context.Context, viewerID, userID uuid.UUID, cursor string, size int) (*FollowListVO, error) {
	return s.listOf(ctx, viewerID, domain.ListFollowers, userID, cursor, size)
}

func (s *followServiceImpl) ListMutual(ctx context.Context, userID uuid.UUID, cursor string, size int) (*FollowListVO, error) {
	return s.listPage(ctx, userID, domain.ListMutual, userID, cursor, size)
}

func (s *followServiceImpl) GetRelations(ctx context.Context, viewerID uuid.UUID, targetIDs []uuid.UUID) (*RelationsVO, error) {
	if len(targetIDs) > MaxRelationTargets {
		return nil, fmt.Errorf("%w: at most %d per request", domain.ErrTooManyTargets, MaxRelationTargets)
	}
	relations, err := s.lists.Repo.GetRelations(ctx, viewerID, targetIDs)
	if err != nil {
		return nil, err
	}
	vo := &RelationsVO{Relations: make(map[string]domain.Relation, len(targetIDs))}
	for _, id := range targetIDs {
		vo.Relations[id.String()] = relations[id]
	}
	return vo, nil
}

// listVisibleTo viewerID 能否看 ownerID 的关注 / 粉丝名单：本人总是可以，其他人（含访客）取决于隐私开关。
func (s *followServiceImpl) listVisibleTo(ctx context.Context, viewerID, ownerID uuid.UUID) (bool, error) {
	if viewerID == ownerID {
		return true, nil
	}
	private, err := s.lists.Visibility.IsFollowListPrivate(ctx, ownerID)
	return !private, err
}

// listOf 关注 / 粉丝列表的公共入口：目标用户存在 → 列表对查看者可见 → 取一页。
func (s *followServiceImpl) listOf(
	ctx context.Context, viewerID uuid.UUID, kind domain.ListKind, ownerID uuid.UUID, cursor string, size int,
) (*FollowListVO, error) {
	if err := s.requireUser(ctx, ownerID); err != nil {
		return nil, err
	}
	visible, err := s.listVisibleTo(ctx, viewerID, ownerID)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, domain.ErrFollowListPrivate
	}
	return s.listPage(ctx, viewerID, kind, ownerID, cursor, size)
}

// listPage 取一页：仓储按 keyset 取关系 → 批量取用户简要信息（顺带过滤已禁用 / 已删除）→ 批量取查看者与本页用户的关系。
func (s *followServiceImpl) listPage(
	ctx context.Context, viewerID uuid.UUID, kind domain.ListKind, ownerID uuid.UUID, cursor string, size int,
) (*FollowListVO, error) {
	size = normalizeListSize(size)
	after, err := parseListCursor(cursor, kind, ownerID)
	if err != nil {
		return nil, err
	}
	vo := &FollowListVO{Items: make([]FollowListItemVO, 0, size)}
	userIDs := make([]uuid.UUID, 0, size)
	for round := 0; round < maxListRounds && len(vo.Items) < size; round++ {
		// 多取一条用来判断后面还有没有；本轮只消费 need 条，所以 after 始终是最后一条已消费的关系。
		need := size - len(vo.Items)
		edges, err := s.lists.Repo.ListEdges(ctx, kind, ownerID, after, need+1)
		if err != nil {
			return nil, err
		}
		vo.HasMore = len(edges) > need
		if vo.HasMore {
			edges = edges[:need]
		}
		if len(edges) == 0 {
			break
		}
		ids := make([]uuid.UUID, len(edges))
		for i := range edges {
			ids[i] = edges[i].UserID
		}
		briefs, err := s.lists.Users.GetBriefs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if brief, ok := briefs[edge.UserID]; ok {
				vo.Items = append(vo.Items, FollowListItemVO{User: brief, FollowTime: edge.FollowTime})
				userIDs = append(userIDs, edge.UserID)
			}
		}
		after = &edges[len(edges)-1]
		if !vo.HasMore {
			break
		}
	}
	if vo.HasMore {
		vo.NextCursor = encodeListCursor(kind, ownerID, *after)
	}
	if viewerID == uuid.Nil || len(userIDs) == 0 {
		return vo, nil
	}
	relations, err := s.lists.Repo.GetRelations(ctx, viewerID, userIDs)
	if err != nil {
		return nil, err
	}
	for i, id := range userIDs {
		vo.Items[i].Relation = relations[id]
	}
	return vo, nil
}

// normalizeListSize ≤0 取默认值，超过上限按上限。
func normalizeListSize(size int) int {
	switch {
	case size <= 0:
		return DefaultListSize
	case size > MaxListSize:
		return MaxListSize
	default:
		return size
	}
}
