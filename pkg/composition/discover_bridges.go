// Package composition 的 discover_bridges.go：注入 discover 领域的跨域桥接器（post / circle / redis seed → discover）。
package composition

import (
	"context"

	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	discoverdomain "interestBar/pkg/domains/discover/domain"
	postapp "interestBar/pkg/domains/post/application"
	recommenddomain "interestBar/pkg/domains/recommend/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// ===== discover ← (post, circle) =====
//
// discover 是跨域编排器（无聚合根），5 个只读端口：post hydrate + 交互态、circle GetByIDs +
// joined IDs、seed 已交互帖。与 recommend/trending 桥接器同款风格。

// discoverPostHydrator 把 post.application.PostService.SearchPostsByIDs 适配为
// discover.domain.PostHydrator（[]PostListItem → []recommend.domain.FeedPostItem 字段拷贝，不含交互态）。
// 与 trendingPostHydrator 同款实现；discover.domain.PostHydrator 返回类型复用 recommend.domain.FeedPostItem。
type discoverPostHydrator struct {
	delegate postapp.PostService
}

func (h *discoverPostHydrator) Hydrate(ctx context.Context, postIDs []uuid.UUID) ([]recommenddomain.FeedPostItem, error) {
	items, err := h.delegate.SearchPostsByIDs(ctx, postIDs)
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

// discoverCircleLookup 把 circle.domain.CircleRepository 适配为 discover.domain.CircleLookup。
// 与 trendingCircleLookup 同款实现（返回完整 Circle 实体）。
type discoverCircleLookup struct {
	repo circledomain.CircleRepository
}

func (l *discoverCircleLookup) GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*circledomain.Circle, error) {
	return l.repo.GetByIDs(ctx, circleIDs)
}

// discoverSeedReader 把 redispkg ZSET 适配为 discover.domain.SeedReader（反气泡已交互帖）。
// 与 recommend 的 seedReaderRedis 同款实现（无状态，委托全局 Client）。
type discoverSeedReader struct{}

func (r *discoverSeedReader) LikedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.ListPostLikedIDs(userID, int64(limit))
	if err != nil {
		return nil, err
	}
	return parseIDsForDiscover(raw), nil
}

func (r *discoverSeedReader) CollectedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.ListPostCollectedIDs(userID, int64(limit))
	if err != nil {
		return nil, err
	}
	return parseIDsForDiscover(raw), nil
}

func (r *discoverSeedReader) ViewedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	entries, _, err := redispkg.ListPostViews(userID, 0, int64(limit))
	if err != nil {
		return nil, err
	}
	raw := make([]string, 0, len(entries))
	for _, e := range entries {
		raw = append(raw, e.ID)
	}
	return parseIDsForDiscover(raw), nil
}

// parseIDsForDiscover 把 string ID 列表转成 []uuid.UUID（跳过非法）。
func parseIDsForDiscover(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		if id, perr := uuid.Parse(s); perr == nil {
			out = append(out, id)
		}
	}
	return out
}

// discoverJoinedCircleLookup 把 circle.application.CircleService 适配为 discover.domain.JoinedCircleLookup。
// 与 recommendCircleLookup 同款实现。
type discoverJoinedCircleLookup struct {
	delegate circleapp.CircleService
}

func (l *discoverJoinedCircleLookup) ListJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return l.delegate.ListJoinedCircleIDs(ctx, userID, limit)
}

// 编译期保证桥接器满足 discover.domain 端口。
var (
	_ discoverdomain.PostHydrator       = (*discoverPostHydrator)(nil)
	_ discoverdomain.InteractionChecker = (*postInteractionChecker)(nil)
	_ discoverdomain.CircleLookup       = (*discoverCircleLookup)(nil)
	_ discoverdomain.SeedReader         = (*discoverSeedReader)(nil)
	_ discoverdomain.JoinedCircleLookup = (*discoverJoinedCircleLookup)(nil)
)
