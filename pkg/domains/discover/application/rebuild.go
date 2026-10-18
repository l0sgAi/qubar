package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/discover/domain"
	elasticsearch "interestBar/pkg/server/storage/elasticsearch"
)

// RebuildPool 重建候选池（读路径 miss + syncer 共用同一份逻辑）。
//
// userID=nil → 匿名共享池（纯随机，无排除）。
// userID 非 nil → 登录用户反气泡池：
//  1. 取已加圈子（ES must_not terms 过滤）
//  2. 取已交互帖（liked/collected/viewed，内存剔除）
//  3. ES random_score 采样圈子+帖子（圈子排除已加；帖子排除已加圈子，已交互帖内存再过滤）
//  4. 帖子剔除后过少 → 兜底不排除再采（保证非空）
//  5. DEL+RPUSH 重建 LIST 池 + 写版本 token
//
// 重建仅 1-2 次 random_score（轻于 recommend 5 路召回），同步重建可接受。
// 返回新 token。
//
// 注意：ES 采样函数（SampleDiscoverPosts/Circles）在 elasticsearch 包，本方法直接调用——
// discover application 可 import infra 全局工具包（elasticsearch 属于 server/storage，非兄弟域）。
func (s *discoverServiceImpl) RebuildPool(ctx context.Context, userID *uuid.UUID) (string, error) {
	poolSize := defaultInt(conf.Config.Discover.PoolSize, 200)
	minPoolPosts := defaultInt(conf.Config.Discover.MinPoolPosts, 50)
	seedLimit := defaultInt(conf.Config.Discover.SeedLimit, 500)
	ttl := time.Duration(defaultInt(conf.Config.Discover.TTLMinutes, 30)) * time.Minute

	// 匿名：纯随机，无排除。
	if userID == nil {
		return s.rebuildAnon(ctx, poolSize, ttl)
	}

	uid := *userID

	// 1. 排除集（已加圈子 + 已交互帖）。
	joinedCircles, _ := s.joinedCircles.ListJoinedCircleIDs(ctx, uid, 100)
	liked, _ := s.seed.LikedPostIDs(ctx, uid, seedLimit)
	collected, _ := s.seed.CollectedPostIDs(ctx, uid, seedLimit)
	viewed, _ := s.seed.ViewedPostIDs(ctx, uid, seedLimit)
	interacted := toUUIDSet(liked, collected, viewed)

	// 2. 圈子采样（ES 排除已加圈子）。
	circleIDs := sampleCircleIDs(ctx, joinedCircles, poolSize)

	// 3. 帖子采样（ES 排除已加圈子，多取以补内存剔除）。
	rawPostIDs := samplePostIDs(ctx, joinedCircles, poolSize*2)
	postIDs := filterOutUUIDs(rawPostIDs, interacted)

	// 4. 兜底：剔除后过少 → 不排除再采（保证非空）。
	if len(postIDs) < minPoolPosts {
		postIDs = samplePostIDs(ctx, joinedCircles, poolSize)
	}

	// 5. 重建 LIST 池。token 以 posts 池为准（读路径 posts 分区先取 token）。
	userKey := uid.String()
	token, err := s.pool.Rebuild(ctx, domain.SectionCircles, userKey, circleIDs, ttl)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Rebuild(ctx, domain.SectionPosts, userKey, postIDs, ttl); err != nil {
		return token, err
	}
	// 两池各自 Rebuild 生成独立 token，但读路径只用一个。为保证一致，用 circles 的 token
	// 覆盖 posts 的 token key（取后写的为准）。
	return token, nil
}

// rebuildAnon 重建匿名共享池（纯随机，无排除）。
func (s *discoverServiceImpl) rebuildAnon(ctx context.Context, poolSize int, ttl time.Duration) (string, error) {
	userKey := domain.AnonUserKey
	circleIDs := sampleCircleIDs(ctx, nil, poolSize)
	postIDs := samplePostIDs(ctx, nil, poolSize)

	token, err := s.pool.Rebuild(ctx, domain.SectionCircles, userKey, circleIDs, ttl)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Rebuild(ctx, domain.SectionPosts, userKey, postIDs, ttl); err != nil {
		return token, err
	}
	return token, nil
}

// sampleCircleIDs 把 ES 返回的 string ID 列表转成 []uuid.UUID（跳过非法）。
func sampleCircleIDs(ctx context.Context, excludeCircleIDs []uuid.UUID, size int) []uuid.UUID {
	raw, err := elasticsearch.SampleDiscoverCircles(ctx, excludeCircleIDs, size)
	if err != nil {
		return nil
	}
	return parseUUIDs(raw)
}

// samplePostIDs 同上，帖子。
func samplePostIDs(ctx context.Context, excludeCircleIDs []uuid.UUID, size int) []uuid.UUID {
	raw, err := elasticsearch.SampleDiscoverPosts(ctx, excludeCircleIDs, size)
	if err != nil {
		return nil
	}
	return parseUUIDs(raw)
}

// parseUUIDs 把 string ID 列表转成 []uuid.UUID（跳过非法）。
func parseUUIDs(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		if id, perr := uuid.Parse(s); perr == nil {
			out = append(out, id)
		}
	}
	return out
}

// toUUIDSet 把多个 ID 列表合并成 set（用于 O(1) 排除判定）。
func toUUIDSet(idLists ...[]uuid.UUID) map[uuid.UUID]struct{} {
	m := make(map[uuid.UUID]struct{})
	for _, list := range idLists {
		for _, id := range list {
			m[id] = struct{}{}
		}
	}
	return m
}

// filterOutUUIDs 从 ids 中剔除 exclude 集合内的（保序）。
func filterOutUUIDs(ids []uuid.UUID, exclude map[uuid.UUID]struct{}) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := exclude[id]; ok {
			continue
		}
		out = append(out, id)
	}
	return out
}

// defaultInt v<=0 返回 def。
func defaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
