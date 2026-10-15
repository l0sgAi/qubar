// Package application 提供 trending 领域的应用服务层（热点榜聚合编排）。
package application

import (
	"context"

	"github.com/google/uuid"

	"interestBar/pkg/conf"
	recommenddomain "interestBar/pkg/domains/recommend/domain"
	"interestBar/pkg/domains/trending/domain"
)

// TrendingService 热点榜应用服务。
type TrendingService interface {
	// GetTrending 读取热点聚合看板。
	//   window  = "24h" | "7d"（非法回落 24h）
	//   section = "all" | "posts" | "circles" | "users"（all=三类各返 size 条，首屏聚合）
	//   size    = 每板块条数（<=0 或 >max 回落默认）
	//   offset  = 单板块翻页偏移（section=all 时忽略）
	GetTrending(ctx context.Context, userID uuid.UUID, window, section string, size, offset int) (*domain.TrendingBoard, error)
}

type trendingServiceImpl struct {
	board    domain.BoardStore
	hydrator domain.PostHydrator
	checker  domain.InteractionChecker
	circle   domain.CircleLookup
	user     domain.UserLookup
}

// NewTrendingService 构造 TrendingService。
//
// board 为 trending 同域 infra；hydrator/checker/circle/user 为跨域桥接器（composition 注入）。
func NewTrendingService(
	board domain.BoardStore,
	hydrator domain.PostHydrator,
	checker domain.InteractionChecker,
	circle domain.CircleLookup,
	user domain.UserLookup,
) TrendingService {
	return &trendingServiceImpl{
		board:    board,
		hydrator: hydrator,
		checker:  checker,
		circle:   circle,
		user:     user,
	}
}

// GetTrending 实现 TrendingService。
func (s *trendingServiceImpl) GetTrending(ctx context.Context, userID uuid.UUID, window, section string, size, offset int) (*domain.TrendingBoard, error) {
	window = normalizeWindow(window)
	section = normalizeSection(section)
	size = normalizeSize(size)

	board := &domain.TrendingBoard{
		Window: window,
		Size:   size,
	}
	if section != domain.SectionAll {
		board.Offset = offset
	}

	// section=all 时 offset 忽略（首屏不分页）。
	readOffset := int64(offset)
	if section == domain.SectionAll {
		readOffset = 0
	}

	// refreshedAt 取所选板块的最大值（最近一次刷新）。
	var maxRefreshedAt int64

	switch section {
	case domain.SectionAll, domain.SectionPosts:
		posts, at, err := s.fillPosts(ctx, userID, window, readOffset, size)
		if err != nil {
			return nil, err
		}
		board.Posts = posts
		if at > maxRefreshedAt {
			maxRefreshedAt = at
		}
	}
	switch section {
	case domain.SectionAll, domain.SectionCircles:
		circles, at, err := s.fillCircles(ctx, window, readOffset, size)
		if err != nil {
			return nil, err
		}
		board.Circles = circles
		if at > maxRefreshedAt {
			maxRefreshedAt = at
		}
	}
	switch section {
	case domain.SectionAll, domain.SectionUsers:
		users, at, err := s.fillUsers(ctx, window, readOffset, size)
		if err != nil {
			return nil, err
		}
		board.Users = users
		if at > maxRefreshedAt {
			maxRefreshedAt = at
		}
	}

	board.RefreshedAt = maxRefreshedAt
	return board, nil
}

// fillPosts 读帖子榜 ZSET → hydrate 展示信息 → 回填 is_liked/is_collected，保序。
func (s *trendingServiceImpl) fillPosts(ctx context.Context, userID uuid.UUID, window string, offset int64, size int) ([]domain.TrendingPostItem, int64, error) {
	scored, err := s.board.Range(ctx, domain.DimensionPost, window, offset, int64(size))
	if err != nil {
		return nil, 0, err
	}
	if len(scored) == 0 {
		return nil, s.refreshedAtSafe(ctx, domain.DimensionPost, window), nil
	}

	ids := make([]uuid.UUID, 0, len(scored))
	for _, sc := range scored {
		ids = append(ids, sc.ID)
	}

	items, err := s.hydrator.Hydrate(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[uuid.UUID]recommenddomain.FeedPostItem, len(items))
	// 复用 recommend.domain.FeedPostItem 作为内嵌字段类型；这里用别名映射便于书写。
	for _, it := range items {
		byID[it.ID] = it
	}

	liked, collected, err := s.checker.BatchCheck(ctx, userID, ids)
	if err != nil {
		// 交互态 best-effort：失败按 false，不影响榜单展示。
		liked = nil
		collected = nil
	}

	out := make([]domain.TrendingPostItem, 0, len(scored))
	for _, sc := range scored { // ★ 按 ZSET 序遍历，还原排名
		it, ok := byID[sc.ID]
		if !ok {
			continue // 已删除/过滤则跳过（榜单可断层）
		}
		if liked != nil {
			it.IsLiked = liked[sc.ID]
		}
		if collected != nil {
			it.IsCollected = collected[sc.ID]
		}
		out = append(out, domain.TrendingPostItem{FeedPostItem: it, HotScore: sc.Score})
	}
	return out, s.refreshedAtSafe(ctx, domain.DimensionPost, window), nil
}

// fillCircles 读圈子榜 ZSET → GetByIDs 回填展示信息，保序，跳过已删/禁用。
func (s *trendingServiceImpl) fillCircles(ctx context.Context, window string, offset int64, size int) ([]domain.TrendingCircleItem, int64, error) {
	scored, err := s.board.Range(ctx, domain.DimensionCircle, window, offset, int64(size))
	if err != nil {
		return nil, 0, err
	}
	if len(scored) == 0 {
		return nil, s.refreshedAtSafe(ctx, domain.DimensionCircle, window), nil
	}

	ids := make([]uuid.UUID, 0, len(scored))
	for _, sc := range scored {
		ids = append(ids, sc.ID)
	}

	circles, err := s.circle.GetByIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}

	out := make([]domain.TrendingCircleItem, 0, len(scored))
	for _, sc := range scored {
		c, ok := circles[sc.ID]
		if !ok {
			continue // GetByIDs 已跳过 deleted；status 异常也跳过，保持桶顺序不断层
		}
		categoryID := ""
		if c.CategoryID != nil {
			categoryID = c.CategoryID.String()
		}
		out = append(out, domain.TrendingCircleItem{
			ID:          c.ID.String(),
			Name:        c.Name,
			AvatarURL:   c.AvatarURL,
			Description: c.Description,
			CategoryID:  categoryID,
			MemberCount: c.MemberCount,
			PostCount:   c.PostCount,
			Hot:         c.Hot,
			JoinType:    c.JoinType,
			CreateTime:  c.CreateTime.Format("2006-01-02 15:04:05"),
			HotScore:    sc.Score,
		})
	}
	return out, s.refreshedAtSafe(ctx, domain.DimensionCircle, window), nil
}

// fillUsers 读用户榜 ZSET → GetBriefs 回填展示信息，保序，跳过已删/未命中。
func (s *trendingServiceImpl) fillUsers(ctx context.Context, window string, offset int64, size int) ([]domain.TrendingUserItem, int64, error) {
	scored, err := s.board.Range(ctx, domain.DimensionUser, window, offset, int64(size))
	if err != nil {
		return nil, 0, err
	}
	if len(scored) == 0 {
		return nil, s.refreshedAtSafe(ctx, domain.DimensionUser, window), nil
	}

	ids := make([]string, 0, len(scored))
	for _, sc := range scored {
		ids = append(ids, sc.ID.String())
	}

	briefs, err := s.user.GetBriefs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}

	out := make([]domain.TrendingUserItem, 0, len(scored))
	for _, sc := range scored {
		b, ok := briefs[sc.ID.String()]
		if !ok {
			continue // 用户已删/禁用未命中 → 跳过
		}
		out = append(out, domain.TrendingUserItem{
			ID:        b.ID,
			Username:  b.Username,
			AvatarURL: b.AvatarURL,
			HotScore:  sc.Score,
		})
	}
	return out, s.refreshedAtSafe(ctx, domain.DimensionUser, window), nil
}

// refreshedAtSafe 读榜单刷新时间，失败降级为 0（读路径不应因 meta 失败而 5xx）。
func (s *trendingServiceImpl) refreshedAtSafe(ctx context.Context, dimension, window string) int64 {
	at, err := s.board.RefreshedAt(ctx, dimension, window)
	if err != nil {
		return 0
	}
	return at
}

// ===== 入参规整（兜底默认值，与 handler 复用同一份逻辑） =====

// normalizeWindow 时间窗：非法值回落 24h。
func normalizeWindow(window string) string {
	switch window {
	case domain.Window24h, domain.Window7d:
		return window
	default:
		return domain.Window24h
	}
}

// normalizeSection 板块：非法值回落 all。
func normalizeSection(section string) string {
	switch section {
	case domain.SectionAll, domain.SectionPosts, domain.SectionCircles, domain.SectionUsers:
		return section
	default:
		return domain.SectionAll
	}
}

// normalizeSize 每板块条数：<=0 或超 max 回落默认（默认/上限来自配置）。
func normalizeSize(size int) int {
	maxSize := conf.Config.Trending.MaxSize
	if maxSize <= 0 {
		maxSize = 50
	}
	def := conf.Config.Trending.DefaultSize
	if def <= 0 {
		def = 20
	}
	if size <= 0 || size > maxSize {
		return def
	}
	return size
}
