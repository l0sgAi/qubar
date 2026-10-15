// Package application 提供 discover 领域的应用服务层（发现页随机+反气泡编排）。
package application

import (
	"context"

	"github.com/google/uuid"

	"interestBar/pkg/conf"
	circledomain "interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/domains/discover/domain"
	recommenddomain "interestBar/pkg/domains/recommend/domain"
	"interestBar/pkg/logger"
)

// DiscoverService 发现页应用服务。
//
// 登录态：random_score 采样 + 反气泡排除（已加圈子 ES 过滤 + 已交互帖内存剔除）。
// 匿名：纯随机（无排除）。读路径 miss/token 不匹配时同步重建（重建仅 1-2 次 random_score，轻）。
type DiscoverService interface {
	// GetDiscover 读取发现页聚合看板。
	//   userID  = 登录用户 ID；nil=匿名（纯随机退化）
	//   section = "all" | "posts" | "circles"（all=两分区各返 size 条，首屏聚合）
	//   size    = 每分区条数（<=0 或 >max 回落默认）
	//   offset  = 单分区翻页偏移（section=all 时忽略，回 0）
	//   poolToken = 客户端回传的池版本；不匹配→重建 + 回 offset=0 + pool_refreshed=true
	GetDiscover(ctx context.Context, userID *uuid.UUID, section string, size, offset int, poolToken string) (*domain.DiscoverBoard, error)

	// RebuildPool 重建候选池（读路径 miss + syncer 共用）。
	// userID=nil=匿名共享池（纯随机）；非 nil=登录用户反气泡池。
	// 返回新 token。
	RebuildPool(ctx context.Context, userID *uuid.UUID) (string, error)
}

type discoverServiceImpl struct {
	pool          domain.DiscoverPoolStore
	hydrator      domain.PostHydrator
	checker       domain.InteractionChecker
	circle        domain.CircleLookup
	seed          domain.SeedReader
	joinedCircles domain.JoinedCircleLookup
}

// NewDiscoverService 构造 DiscoverService。
//
// pool 为 discover 同域 infra；hydrator/checker/circle/seed/joinedCircles 为跨域桥接器（composition 注入）。
func NewDiscoverService(
	pool domain.DiscoverPoolStore,
	hydrator domain.PostHydrator,
	checker domain.InteractionChecker,
	circle domain.CircleLookup,
	seed domain.SeedReader,
	joinedCircles domain.JoinedCircleLookup,
) DiscoverService {
	return &discoverServiceImpl{
		pool:          pool,
		hydrator:      hydrator,
		checker:       checker,
		circle:        circle,
		seed:          seed,
		joinedCircles: joinedCircles,
	}
}

// GetDiscover 实现 DiscoverService。
func (s *discoverServiceImpl) GetDiscover(ctx context.Context, userID *uuid.UUID, section string, size, offset int, poolToken string) (*domain.DiscoverBoard, error) {
	section = normalizeSection(section)
	size = normalizeSize(size)

	userKey := domain.AnonUserKey
	if userID != nil {
		userKey = userID.String()
	}

	board := &domain.DiscoverBoard{
		Size: size,
	}

	// section=all 是首屏聚合：忽略 offset，回 0；posts/circles 用 offset 翻页。
	curOffset := offset
	if section == domain.SectionAll {
		curOffset = 0
	}

	// 读 posts 与 circles 用同一 userKey（登录/匿名决定是否反气泡）。
	board.Offset = curOffset

	// 注意：两分区共用同一个 token（按 userKey 一份池版本），客户端只需回传一个 pool_token。
	switch section {
	case domain.SectionAll, domain.SectionPosts:
		posts, hasMore, refreshed, token, err := s.readPosts(ctx, userID, userKey, size, curOffset, poolToken)
		if err != nil {
			return nil, err
		}
		board.Posts = posts
		board.HasMore = hasMore
		board.PoolRefreshed = board.PoolRefreshed || refreshed
		board.PoolToken = token
		// section=all 时刷新后 offset 回 0
		if refreshed && section == domain.SectionAll {
			board.Offset = 0
		}
		if section == domain.SectionPosts {
			return board, nil
		}
		// all 继续读 circles（已取 posts，circles 用同 token 不再重建）
		circles, circleHasMore, err := s.readCircles(ctx, userKey, size, board.Offset, board.PoolToken, board.PoolRefreshed)
		if err != nil {
			return board, nil // best-effort：圈子失败仍返回已取帖子
		}
		board.Circles = circles
		// all 首屏 HasMore 取两分区任一有更多
		board.HasMore = board.HasMore || circleHasMore
		return board, nil

	case domain.SectionCircles:
		circles, hasMore, refreshed, token, err := s.readCirclesWithRebuild(ctx, userID, userKey, size, curOffset, poolToken)
		if err != nil {
			return nil, err
		}
		board.Circles = circles
		board.HasMore = hasMore
		board.PoolRefreshed = refreshed
		board.PoolToken = token
		if refreshed {
			board.Offset = 0
		}
		return board, nil

	default:
		return nil, errSection
	}
}

// readPosts 读帖子分区（含 miss/token 不匹配重建）。返回 posts/hasMore/refreshed/token。
//
// 重建只在 posts 分区触发一次（circles 复用同 token，不再重建）——避免两分区各自重建导致 token 抖动。
func (s *discoverServiceImpl) readPosts(ctx context.Context, userID *uuid.UUID, userKey string, size, offset int, poolToken string) ([]domain.DiscoverPostItem, bool, bool, string, error) {
	refreshed := false
	exists, err := s.pool.Exists(ctx, domain.SectionPosts, userKey)
	if err != nil {
		// best-effort：缓存错只记日志不返回（stats 是软信号）。
		logger.Log.Error("discover pool exists failed: " + err.Error())
	}
	curToken, _ := s.pool.Token(ctx, userKey)

	// 池不存在 OR token 不匹配 → 重建。
	if !exists || (poolToken != "" && poolToken != curToken) {
		newToken, rebuildErr := s.RebuildPool(ctx, userID)
		if rebuildErr != nil {
			logger.Log.Error("discover rebuild pool failed: " + rebuildErr.Error())
			return nil, false, false, "", nil // 重建失败返回空（前端空态）
		}
		curToken = newToken
		refreshed = true
		offset = 0
	}

	ids, err := s.pool.Range(ctx, domain.SectionPosts, userKey, int64(offset), int64(size))
	if err != nil || len(ids) == 0 {
		return nil, false, refreshed, curToken, nil
	}

	total, _ := s.pool.Len(ctx, domain.SectionPosts, userKey)
	hasMore := int64(offset)+int64(size) < total

	posts := s.fillPosts(ctx, ids, userID)
	return posts, hasMore, refreshed, curToken, nil
}

// readCircles 读圈子分区（不触发重建，复用 posts 已建立的池/token）。
// refreshed/curToken 由 posts 分区决定（all 模式下已重建过）。
func (s *discoverServiceImpl) readCircles(ctx context.Context, userKey string, size, offset int, _ /*poolToken*/ string, _ /*refreshed*/ bool) ([]domain.DiscoverCircleItem, bool, error) {
	ids, err := s.pool.Range(ctx, domain.SectionCircles, userKey, int64(offset), int64(size))
	if err != nil || len(ids) == 0 {
		return nil, false, nil
	}
	total, _ := s.pool.Len(ctx, domain.SectionCircles, userKey)
	hasMore := int64(offset)+int64(size) < total
	circles := s.fillCircles(ctx, ids)
	return circles, hasMore, nil
}

// readCirclesWithRebuild 读圈子分区（section=circles 单独请求时，需自带重建逻辑）。
func (s *discoverServiceImpl) readCirclesWithRebuild(ctx context.Context, userID *uuid.UUID, userKey string, size, offset int, poolToken string) ([]domain.DiscoverCircleItem, bool, bool, string, error) {
	refreshed := false
	exists, _ := s.pool.Exists(ctx, domain.SectionCircles, userKey)
	curToken, _ := s.pool.Token(ctx, userKey)
	if !exists || (poolToken != "" && poolToken != curToken) {
		newToken, rebuildErr := s.RebuildPool(ctx, userID)
		if rebuildErr != nil {
			logger.Log.Error("discover rebuild pool failed: " + rebuildErr.Error())
			return nil, false, false, "", nil
		}
		curToken = newToken
		refreshed = true
		offset = 0
	}

	ids, err := s.pool.Range(ctx, domain.SectionCircles, userKey, int64(offset), int64(size))
	if err != nil || len(ids) == 0 {
		return nil, false, refreshed, curToken, nil
	}
	total, _ := s.pool.Len(ctx, domain.SectionCircles, userKey)
	hasMore := int64(offset)+int64(size) < total
	circles := s.fillCircles(ctx, ids)
	return circles, hasMore, refreshed, curToken, nil
}

// fillPosts LRANGE ids → hydrate 展示信息 → 回填 is_liked/is_collected，保随机序。
//
// 保序关键：按 ids（LIST 写入序=随机序）遍历，而非 hydrate 返回的 map 序，否则 map 打乱随机性。
// 已删除/过滤的帖子跳过（池可断层，同 trending）。
func (s *discoverServiceImpl) fillPosts(ctx context.Context, ids []uuid.UUID, userID *uuid.UUID) []domain.DiscoverPostItem {
	items, err := s.hydrator.Hydrate(ctx, ids)
	if err != nil {
		return nil
	}
	byID := make(map[uuid.UUID]recommenddomain.FeedPostItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	var liked, collected map[uuid.UUID]bool
	if userID != nil { // 登录态回填交互（best-effort：失败按 false）
		if l, c, err := s.checker.BatchCheck(ctx, *userID, ids); err == nil {
			liked, collected = l, c
		}
	}

	out := make([]domain.DiscoverPostItem, 0, len(ids))
	for _, id := range ids { // ★ 按 LIST 序遍历，保随机序
		it, ok := byID[id]
		if !ok {
			continue // 已删除/过滤则跳过（池可断层）
		}
		if liked != nil {
			it.IsLiked = liked[id]
		}
		if collected != nil {
			it.IsCollected = collected[id]
		}
		out = append(out, domain.DiscoverPostItem{FeedPostItem: it})
	}
	return out
}

// fillCircles LRANGE ids → GetByIDs 回填展示信息，保随机序，跳过已删/禁用。
func (s *discoverServiceImpl) fillCircles(ctx context.Context, ids []uuid.UUID) []domain.DiscoverCircleItem {
	if len(ids) == 0 {
		return nil
	}
	circles, err := s.circle.GetByIDs(ctx, ids)
	if err != nil {
		return nil
	}
	out := make([]domain.DiscoverCircleItem, 0, len(ids))
	for _, id := range ids { // ★ 按 LIST 序遍历，保随机序
		c, ok := circles[id]
		if !ok {
			continue // GetByIDs 已跳过 deleted；status 异常也跳过
		}
		out = append(out, toDiscoverCircleItem(c))
	}
	return out
}

// toDiscoverCircleItem 把 Circle 实体组装成 DiscoverCircleItem。
func toDiscoverCircleItem(c *circledomain.Circle) domain.DiscoverCircleItem {
	categoryID := ""
	if c.CategoryID != nil {
		categoryID = c.CategoryID.String()
	}
	return domain.DiscoverCircleItem{
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
	}
}

// ===== 入参规整（兜底默认值） =====

// normalizeSection 板块：非法值回落 all。
func normalizeSection(section string) string {
	switch section {
	case domain.SectionAll, domain.SectionPosts, domain.SectionCircles:
		return section
	default:
		return domain.SectionAll
	}
}

// normalizeSize 每分区条数：<=0 或超 max 回落默认（默认/上限来自配置）。
func normalizeSize(size int) int {
	maxSize := conf.Config.Discover.MaxSize
	if maxSize <= 0 {
		maxSize = 50
	}
	def := conf.Config.Discover.DefaultSize
	if def <= 0 {
		def = 20
	}
	if size <= 0 || size > maxSize {
		return def
	}
	return size
}
