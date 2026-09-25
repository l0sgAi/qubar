// Package application 提供 collect 领域的应用服务层。
//
// 职责：
//   - 收藏/取消收藏（先解析真实状态，DB 流水为权威，Redis Lua 原子设值；已处于期望态则 no-op）
//   - 「我的收藏」列表（DB keyset 分页 + 复用 post 领域组装）
//   - 收藏前恢复统计缓存（避免 Lua 脚本读到不存在的 stats Hash）
//   - 收藏流水即时入库（列表权威源，Toggle 同步 upsert post_collect）
//   - 流水真正迁移时才异步发布 collect_count 增量事件（Redpanda → 消费者批量聚合统计字段）
package application

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/collect/domain"
	postapp "interestBar/pkg/domains/post/application"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// PostFetcher collect 领域需要的帖子组装能力（「我的收藏」列表用）。
type PostFetcher interface {
	// GetPostsByIDs 按 ID 列表批量获取已组装的帖子（仅未删除 + 已发布）。
	// 顺序不保证，调用方按收藏时间自行排序；失效帖静默过滤（不在返回中）。
	GetPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error)
}

// ===== DTO =====

// ToggleResult 收藏切换结果（供 handler 序列化）。
type ToggleResult struct {
	IsCollected bool   `json:"is_collected"`
	PostID      string `json:"post_id"`
}

// ToggleInput 收藏/取消收藏入参。
type ToggleInput struct {
	PostID uuid.UUID
	Action string // 可选："collect" / "uncollect" 显式期望状态；空 = 切换
}

// ListCollectedPostsResult 「我的收藏」列表结果。
type ListCollectedPostsResult struct {
	Posts       []postapp.PostListItem `json:"posts"`
	Total       int64                  `json:"total"`
	Size        int                    `json:"size"`
	SearchAfter string                 `json:"search_after"`
}

// ===== Service 接口 =====

// CollectService 是 collect 领域的应用服务接口。
type CollectService interface {
	// Toggle 收藏/取消收藏：期望状态 = input.Action（显式）或真实当前状态取反（空）。
	Toggle(ctx context.Context, userID uuid.UUID, input ToggleInput) (*ToggleResult, error)
	// ListCollectedPosts 查看当前用户的收藏列表（按收藏时间倒序）。
	// keyword 非空时按 title/summary 过滤（ILIKE），仅返回匹配项。
	ListCollectedPosts(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter string) (*ListCollectedPostsResult, error)

	// SetPostTarget 注入帖子查询端口（存在性校验 + 统计缓存恢复）。
	SetPostTarget(t domain.PostTarget)
	// SetPostFetcher 注入帖子组装端口（「我的收藏」列表用）。
	SetPostFetcher(f PostFetcher)
}

type collectServiceImpl struct {
	cache       domain.PostCollectCache
	repo        domain.PostCollectRepository
	publisher   domain.CollectEventPublisher
	postTarget  domain.PostTarget
	postFetcher PostFetcher
}

// NewCollectService 构造 CollectService。
//
// postTarget / postFetcher 是跨领域依赖，通过 setter 注入（composition 层负责把它们连起来）。
func NewCollectService(
	cache domain.PostCollectCache,
	repo domain.PostCollectRepository,
	publisher domain.CollectEventPublisher,
) CollectService {
	return &collectServiceImpl{
		cache:     cache,
		repo:      repo,
		publisher: publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *collectServiceImpl) SetPostTarget(t domain.PostTarget) { s.postTarget = t }
func (s *collectServiceImpl) SetPostFetcher(f PostFetcher)     { s.postFetcher = f }

// Toggle 收藏/取消收藏。
//
// 与 like 的差异：收藏低频且「我的收藏」列表需即时可见，post_collect 流水同步入库并作为权威源
// （设计见 docs/design/like-pipeline-fix-design.md §三 F1.5）：
//  1. 校验帖子存在；确保帖子统计缓存存在（Lua 脚本依赖 stats Hash）；
//  2. 解析真实当前状态（ZSET miss 回源 DB 并回填），得出期望状态（显式 action 或取反）；
//  3. 同步 upsert 流水（单语句，返回行是否真正迁移）；失败直接返回，Redis 未改动无需补偿；
//  4. Redis Lua 原子设值（与 DB 对齐，幂等）；失败仅记日志——DB 已是权威，缓存随 TTL / 回源自愈；
//  5. 仅当流水真正迁移时发布 collect_count 增量事件（含热度 / CF / 通知）。
func (s *collectServiceImpl) Toggle(ctx context.Context, userID uuid.UUID, input ToggleInput) (*ToggleResult, error) {
	if s.postTarget == nil {
		return nil, errors.New("post target is not configured")
	}
	if _, err := domain.ResolveWant(false, input.Action); err != nil {
		return nil, err
	}

	exists, err := s.postTarget.Exists(ctx, input.PostID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrPostNotFound
	}

	if err := s.postTarget.RestoreStats(ctx, input.PostID); err != nil {
		logger.Log.Error("Failed to restore post stats cache: " + err.Error())
	}

	current, err := s.isCollected(ctx, userID, input.PostID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve post collect state: %w", err)
	}
	want, err := domain.ResolveWant(current, input.Action)
	if err != nil {
		return nil, err
	}

	changed, err := s.repo.SetCollected(ctx, userID, input.PostID, want)
	if err != nil {
		return nil, fmt.Errorf("failed to persist post collect: %w", err)
	}

	if _, err := s.cache.Set(ctx, userID, input.PostID, want); err != nil {
		logger.Log.Error("Failed to set post collect cache: " + err.Error())
	}

	if changed {
		amount := domain.ToggleResultUncollected
		if want {
			amount = domain.ToggleResultCollected
		}
		if err := s.publisher.PublishPostCollect(ctx, userID, input.PostID, amount.Int64()); err != nil {
			logger.Log.Error("Failed to publish post collect event: " + err.Error())
		}
	}

	return &ToggleResult{
		IsCollected: want,
		PostID:      input.PostID.String(),
	}, nil
}

// isCollected 用户当前是否已收藏（缓存优先，miss 回源 DB，DB 已收藏则回填缓存）。
// 用户收藏 ZSET 有 TTL 与容量上限，miss 不等于未收藏。缓存故障直接回源 DB；DB 错误原样返回。
func (s *collectServiceImpl) isCollected(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	hits, _, err := s.cache.BatchCheck(ctx, userID, []uuid.UUID{postID})
	if err == nil && hits[postID] {
		return true, nil
	}
	if err != nil {
		logger.Log.Error("Failed to check post collected from cache, falling back to DB: " + err.Error())
	}

	collected, err := s.repo.IsCollected(ctx, userID, postID)
	if err != nil {
		return false, err
	}
	if collected {
		if bfErr := s.cache.Backfill(ctx, userID, []uuid.UUID{postID}); bfErr != nil {
			logger.Log.Error("Failed to backfill post collect cache: " + bfErr.Error())
		}
	}
	return collected, nil
}

// ListCollectedPosts 查看当前用户的收藏列表。
//
// 数据源：DB post_collect（deleted=0），按收藏时间倒序 keyset 分页。
// ZSET 仅用于信息流「是否已收藏」回显，不作为列表权威源（有 2000 条上限 + TTL 失效）。
// 失效帖（被删/未发布）在 post 组装时静默过滤（决策 #4）。
//
// keyword 非空时由 repo JOIN domains.post 在 SQL 层过滤 title/summary（ILIKE），
// 仅返回匹配项（total 也仅计匹配）。
func (s *collectServiceImpl) ListCollectedPosts(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter string) (*ListCollectedPostsResult, error) {
	if s.postFetcher == nil {
		return nil, errors.New("post fetcher is not configured")
	}

	if size <= 0 || size > 100 {
		size = 20
	}

	postIDs, total, nextCursor, err := s.repo.ListCollectedPostIDs(ctx, userID, keyword, size, searchAfter)
	if err != nil {
		return nil, err
	}

	posts := make([]postapp.PostListItem, 0, len(postIDs))
	if len(postIDs) > 0 {
		fetched, err := s.postFetcher.GetPostsByIDs(ctx, postIDs)
		if err != nil {
			return nil, err
		}
		// 按 postIDs（收藏时间倒序）重排：GetPostsByIDs 不保证顺序，且会过滤失效帖。
		posts = orderByCollectTime(fetched, postIDs)
	}

	return &ListCollectedPostsResult{
		Posts:       posts,
		Total:       total,
		Size:        len(posts),
		SearchAfter: nextCursor,
	}, nil
}

// orderByCollectTime 把 fetched（无序、可能少于 postIDs）按 postIDs 的顺序重排。
func orderByCollectTime(fetched []postapp.PostListItem, orderedIDs []uuid.UUID) []postapp.PostListItem {
	byID := make(map[uuid.UUID]postapp.PostListItem, len(fetched))
	for _, p := range fetched {
		byID[p.ID] = p
	}
	result := make([]postapp.PostListItem, 0, len(fetched))
	for _, id := range orderedIDs {
		if p, ok := byID[id]; ok {
			result = append(result, p)
		}
	}
	return result
}
