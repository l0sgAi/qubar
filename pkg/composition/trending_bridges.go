// Package composition 的 trending_bridges.go：注入 trending 领域的跨域桥接器（post / circle / user → trending）。
package composition

import (
	"context"

	circledomain "interestBar/pkg/domains/circle/domain"
	postapp "interestBar/pkg/domains/post/application"
	recommenddomain "interestBar/pkg/domains/recommend/domain"
	trendingdomain "interestBar/pkg/domains/trending/domain"
	userapp "interestBar/pkg/domains/user/application"

	"github.com/google/uuid"
)

// ===== trending ← (post, circle, user) =====
//
// trending 是跨域编排器（无聚合根），4 个只读端口：post hydrate + 交互态、circle GetByIDs、user GetBriefs。
// 与 recommend 桥接器同款风格：DTO 字段拷贝（trending.domain 与生产者域 DTO 结构同形但名义不同）。

// trendingPostHydrator 把 post.application.PostService.SearchPostsByIDs 适配为
// trending.domain.PostHydrator（[]PostListItem → []recommend.domain.FeedPostItem 字段拷贝，不含交互态）。
//
// 复用 recommend.domain.FeedPostItem 作为 trending.domain.TrendingPostItem 的内嵌展示 DTO（纯值对象）。
type trendingPostHydrator struct {
	delegate postapp.PostService
}

func (h *trendingPostHydrator) Hydrate(ctx context.Context, viewerID uuid.UUID, postIDs []uuid.UUID) ([]recommenddomain.FeedPostItem, error) {
	items, err := h.delegate.SearchPostsByIDs(ctx, viewerID, postIDs)
	if err != nil {
		return nil, err
	}
	out := make([]recommenddomain.FeedPostItem, 0, len(items))
	for _, p := range items {
		out = append(out, recommenddomain.FeedPostItem{
			ID: p.ID, CircleID: p.CircleID, UserID: p.UserID, Type: p.Type,
			Title: p.Title, Summary: p.Summary, Content: p.Content,
			ViewCount: p.ViewCount, CommentCount: p.CommentCount,
			LikeCount: p.LikeCount, CollectCount: p.CollectCount,
			IsPinned: p.IsPinned, IsEssence: p.IsEssence, IsLock: p.IsLock,
			Status: p.Status, CreateTime: p.CreateTime,
			AuthorName: p.AuthorName, AuthorAvatar: p.AuthorAvatar,
			CircleName: p.CircleName, CircleAvatar: p.CircleAvatar,
			Images: p.Images,
		})
	}
	return out, nil
}

// trendingCircleLookup 把 circle.domain.CircleRepository 适配为 trending.domain.CircleLookup。
// 返回完整 Circle 实体（含 member_count/post_count/hot），由 trending service 组装 TrendingCircleItem。
type trendingCircleLookup struct {
	repo circledomain.CircleRepository
}

func (l *trendingCircleLookup) GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*circledomain.Circle, error) {
	return l.repo.GetByIDs(ctx, circleIDs)
}

// trendingUserLookup 把 user.application.UserFacade 适配为 trending.domain.UserLookup。
type trendingUserLookup struct {
	delegate userapp.UserFacade
}

func (l *trendingUserLookup) GetBriefs(ctx context.Context, userIDs []string) (map[string]trendingdomain.UserBrief, error) {
	briefs, err := l.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]trendingdomain.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = trendingdomain.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// 编译期保证桥接器满足 trending.domain 端口（与项目其它大适配器的 guard 惯例一致）。
var (
	_ trendingdomain.PostHydrator       = (*trendingPostHydrator)(nil)
	_ trendingdomain.InteractionChecker = (*postInteractionChecker)(nil)
	_ trendingdomain.CircleLookup       = (*trendingCircleLookup)(nil)
	_ trendingdomain.UserLookup         = (*trendingUserLookup)(nil)
)
