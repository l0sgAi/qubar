package application

import (
	"context"
	"sort"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// recallAll 跑 5 路召回 + 交错合并 + dedup + 剔除已交互，返回有序候选 postID（落候选池）。
//
// 每路独立 try/log（单路失败返回空，不影响整体）；ES 全挂仅剩 C5；C5 也空→C2 兜底（全局热门）。
// 永不 panic：合并结果至少是「能拿到的候选并集」。
func (s *recommendServiceImpl) recallAll(ctx context.Context, userID uuid.UUID) []uuid.UUID {
	feedCfg := conf.Config.Recommend.Feed
	cfCfg := conf.Config.Recommend.CF
	poolSize := defaultInt(feedCfg.PoolSize, 150)
	seedLike := defaultInt(cfCfg.SeedLike, 30)
	seedCollect := defaultInt(cfCfg.SeedCollect, 20)

	// 1. 共用种子：liked + collected（C3 圈子反查 + C5 CF seed 共用）
	likedIDs := s.safeLiked(ctx, userID, seedLike)
	collectedIDs := s.safeCollected(ctx, userID, seedCollect)
	seedIDs := union(likedIDs, collectedIDs)

	// 2. joined circles（C1 + C3 共用）
	joinedCircles := s.safeJoined(ctx, userID, 50)

	// 3. 行为圈子（C3）：seed 帖子反查 circle_id（带 TTL 缓存）− joined
	behaviorCircles := s.cachedBehaviorCircles(ctx, userID, seedIDs)
	c3Circles := dedupPreserveOrder(filterOut(behaviorCircles, toSet(joinedCircles)))

	// 私密圈：全局路（C2/C4）must_not 查看者不可见的圈子，C3 去掉已不可见的行为圈子；
	// C5 与旧池条目在 hydrate 时兜底过滤。失败降级为不排除（hydrate 仍会过滤）。
	hidden, err := s.vis.HiddenCircleIDs(ctx, userID)
	if err != nil {
		logger.Log.Error("recall hidden circles: " + err.Error())
	}
	c3Circles = filterOut(c3Circles, toSet(hidden))

	// 4. 五路召回（每路内部 try/log，返回有序 IDs）
	c1 := s.channelC1(ctx, joinedCircles, channelSize(poolSize, feedCfg.QuotaC1, 35))
	c2 := s.channelC2(ctx, hidden, channelSize(poolSize, feedCfg.QuotaC2, 25))
	c3 := s.channelC3(ctx, c3Circles, channelSize(poolSize, feedCfg.QuotaC3, 15))
	c4 := s.channelC4(ctx, hidden, channelSize(poolSize, feedCfg.QuotaC4, 10))
	c5 := s.channelC5(ctx, seedIDs, channelSize(poolSize, feedCfg.QuotaC5, 15))

	// 4. 交错 + dedup
	merged := interleave([][]uuid.UUID{c1, c2, c3, c4, c5})
	merged = dedupPreserveOrder(merged)

	// 5. 剔除已交互（liked/collected/viewed）——推荐流不重复推已互动内容
	if feedCfg.ExcludeInteracted {
		viewed := s.safeViewed(ctx, userID, 500)
		exclude := toSet(append(append(likedIDs, collectedIDs...), viewed...))
		merged = filterOut(merged, exclude)
	}

	// 6. 兜底：合并后过少（如新用户/部分失败）→ 补 C2 全局热门
	if len(merged) < poolSize {
		fill := s.channelC2(ctx, hidden, poolSize)
		merged = append(merged, filterOut(fill, toSet(merged))...)
	}

	// 7. 截断到 poolSize
	if len(merged) > poolSize {
		merged = merged[:poolSize]
	}
	return merged
}

// channelC1 兴趣圈子热门：joined circles ∩ rank_score desc。
func (s *recommendServiceImpl) channelC1(ctx context.Context, joinedCircles []uuid.UUID, size int) []uuid.UUID {
	if size <= 0 || len(joinedCircles) == 0 {
		return nil
	}
	ids, _, err := s.searcher.Search(ctx, "hot", joinedCircles, nil, size, nil)
	if err != nil {
		logger.Log.Error("recall C1 (joined circles hot): " + err.Error())
		return nil
	}
	return ids
}

// channelC2 全局热门：rank_score desc（C2 是质量基线 + 兜底）。
func (s *recommendServiceImpl) channelC2(ctx context.Context, hidden []uuid.UUID, size int) []uuid.UUID {
	if size <= 0 {
		return nil
	}
	ids, _, err := s.searcher.Search(ctx, "hot", nil, hidden, size, nil)
	if err != nil {
		logger.Log.Error("recall C2 (global hot): " + err.Error())
		return nil
	}
	return ids
}

// channelC3 行为圈子热门：在 c3Circles（seed 反查 circle_id − joined，已缓存）范围内 rank_score desc。
func (s *recommendServiceImpl) channelC3(ctx context.Context, circles []uuid.UUID, size int) []uuid.UUID {
	if size <= 0 || len(circles) == 0 {
		return nil
	}
	ids, _, err := s.searcher.Search(ctx, "hot", circles, nil, size, nil)
	if err != nil {
		logger.Log.Error("recall C3 (behavior circles hot): " + err.Error())
		return nil
	}
	return ids
}

// cachedBehaviorCircles 用户行为兴趣圈子（seed 帖子 → circle_id），带 TTL 缓存。
//
// miss 时从 DB 反查（ListCircleIDsByPostIDs）并落缓存，避免每轮 recall 都查 DB。
// 兴趣随点赞/收藏漂移，TTL 可配（默认 2h）。返回原始集合，调用方读路径再减 joined 得 C3 候选圈。
func (s *recommendServiceImpl) cachedBehaviorCircles(ctx context.Context, userID uuid.UUID, seedIDs []uuid.UUID) []uuid.UUID {
	if len(seedIDs) == 0 {
		return nil
	}
	if exists, _ := s.intCache.Exists(ctx, userID); exists {
		if ids, err := s.intCache.Get(ctx, userID); err == nil {
			return ids
		}
	}
	all, err := s.postMeta.ListCircleIDsByPostIDs(ctx, seedIDs)
	if err != nil {
		logger.Log.Error("behavior circles (post→circle): " + err.Error())
		return nil
	}
	ttl := time.Duration(defaultInt(conf.Config.Recommend.Feed.InterestCirclesTTLMinutes, 120)) * time.Minute
	if err := s.intCache.Set(ctx, userID, all, ttl); err != nil {
		logger.Log.Error("set interest circles cache: " + err.Error())
	}
	return all
}

// channelC4 最新：create_time desc（新鲜度补充，防信息茧房）。
func (s *recommendServiceImpl) channelC4(ctx context.Context, hidden []uuid.UUID, size int) []uuid.UUID {
	if size <= 0 {
		return nil
	}
	ids, _, err := s.searcher.Search(ctx, "latest", nil, hidden, size, nil)
	if err != nil {
		logger.Log.Error("recall C4 (latest): " + err.Error())
		return nil
	}
	return ids
}

// channelC5 CF 相似：seed 帖 → cf:item:{seed} 相似帖聚合（Σsim desc），排除 seed 自身。
func (s *recommendServiceImpl) channelC5(ctx context.Context, seedIDs []uuid.UUID, size int) []uuid.UUID {
	if size <= 0 || len(seedIDs) == 0 {
		return nil
	}
	scores, err := s.seed.CFSimilar(ctx, seedIDs, 20) // 每 seed 取 top 20 相似
	if err != nil {
		logger.Log.Error("recall C5 (CF similar): " + err.Error())
		return nil
	}
	seedSet := toSet(seedIDs)
	type kv struct {
		id    uuid.UUID
		score float64
	}
	list := make([]kv, 0, len(scores))
	for id, sc := range scores {
		if _, isSeed := seedSet[id]; isSeed {
			continue // 不把 seed 自身当候选
		}
		list = append(list, kv{id: id, score: sc})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].score > list[j].score })
	if len(list) > size {
		list = list[:size]
	}
	out := make([]uuid.UUID, 0, len(list))
	for _, v := range list {
		out = append(out, v.id)
	}
	return out
}

// ===== 种子/圈子读取（best-effort，失败返回空）=====

func (s *recommendServiceImpl) safeLiked(ctx context.Context, userID uuid.UUID, limit int) []uuid.UUID {
	ids, err := s.seed.LikedPostIDs(ctx, userID, limit)
	if err != nil {
		logger.Log.Error("seed liked: " + err.Error())
		return nil
	}
	return ids
}

func (s *recommendServiceImpl) safeCollected(ctx context.Context, userID uuid.UUID, limit int) []uuid.UUID {
	ids, err := s.seed.CollectedPostIDs(ctx, userID, limit)
	if err != nil {
		logger.Log.Error("seed collected: " + err.Error())
		return nil
	}
	return ids
}

func (s *recommendServiceImpl) safeViewed(ctx context.Context, userID uuid.UUID, limit int) []uuid.UUID {
	ids, err := s.seed.ViewedPostIDs(ctx, userID, limit)
	if err != nil {
		logger.Log.Error("seed viewed: " + err.Error())
		return nil
	}
	return ids
}

func (s *recommendServiceImpl) safeJoined(ctx context.Context, userID uuid.UUID, limit int) []uuid.UUID {
	ids, err := s.circle.ListJoinedCircleIDs(ctx, userID, limit)
	if err != nil {
		logger.Log.Error("joined circles: " + err.Error())
		return nil
	}
	return ids
}

// ===== 合并工具 =====

// channelSize 配额百分比 → 该路应取条数。
func channelSize(poolSize, pct, defaultPct int) int {
	if pct <= 0 {
		pct = defaultPct
	}
	n := poolSize * pct / 100
	if n <= 0 {
		n = 1
	}
	return n
}

// interleave 多路有序列表按 round-robin 交错（各路内部序保持）。
func interleave(channels [][]uuid.UUID) []uuid.UUID {
	maxLen := 0
	for _, c := range channels {
		if len(c) > maxLen {
			maxLen = len(c)
		}
	}
	out := make([]uuid.UUID, 0, maxLen*len(channels))
	for i := 0; i < maxLen; i++ {
		for _, c := range channels {
			if i < len(c) {
				out = append(out, c[i])
			}
		}
	}
	return out
}

func dedupPreserveOrder(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func union(a, b []uuid.UUID) []uuid.UUID {
	return dedupPreserveOrder(append(a, b...))
}

func toSet(ids []uuid.UUID) map[uuid.UUID]struct{} {
	m := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func filterOut(ids []uuid.UUID, exclude map[uuid.UUID]struct{}) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := exclude[id]; ok {
			continue
		}
		out = append(out, id)
	}
	return out
}
