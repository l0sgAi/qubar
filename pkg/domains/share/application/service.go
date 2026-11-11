// Package application 提供 share 领域的应用服务层。
//
// 职责（#232）：
//   - 分享帖子：校验可分享（已发布、未删除、对当前用户可见）→ post_share 去重插入（影响行数即「是否首次」）
//   - 首次分享：同步发布分享事件（失败回滚记录、返回可重试错误）→ 分享数落库标记 + 热度（best-effort）→ 分享数缓存 +1
//   - 重复分享：幂等成功，不发事件、不加计数与热度
package application

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/share/domain"
	"interestBar/pkg/logger"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
)

// ShareResult 分享结果（供 handler 序列化）。
type ShareResult struct {
	ShareCount int64 `json:"share_count"` // 分享后的分享数（去重后的分享人数）
	FirstShare bool  `json:"first_share"` // 本次是否为该用户对该帖的首次分享（重复分享为 false，不重复计数）
}

// ShareService 是 share 领域的应用服务接口。
type ShareService interface {
	// SharePost 记录一次帖子外部分享。帖子不可分享（不存在 / 未发布 / 已删除 / 不可见）返回 domain.ErrPostNotFound；
	// 分享事件投递失败返回 domain.ErrEventPublishFailed（已回滚，可重试）。
	SharePost(ctx context.Context, userID, postID uuid.UUID) (*ShareResult, error)

	// SetPostTarget 注入帖子查询端口（可分享校验 + 作者 / 圈子）。
	SetPostTarget(t domain.PostTarget)
}

type shareServiceImpl struct {
	repo       domain.PostShareRepository
	cache      domain.ShareCountCache
	publisher  domain.ShareEventPublisher
	postTarget domain.PostTarget
}

// NewShareService 构造 ShareService。postTarget 是跨领域依赖，通过 setter 注入。
func NewShareService(repo domain.PostShareRepository, cache domain.ShareCountCache, publisher domain.ShareEventPublisher) ShareService {
	return &shareServiceImpl{repo: repo, cache: cache, publisher: publisher}
}

func (s *shareServiceImpl) SetPostTarget(t domain.PostTarget) { s.postTarget = t }

// SharePost 记录一次帖子外部分享。
//
//  1. 可分享校验：不可分享一律 ErrPostNotFound，不写记录、不产生计数与热度；
//  2. post_share 去重插入：唯一索引 (user_id, post_id) 保证并发同一用户只有一个请求插入成功；
//  3. 重复分享：直接返回当前分享数；
//  4. 首次分享：同步发布分享事件。投递失败则删除刚插入的记录并返回 ErrEventPublishFailed，
//     否则声望等下游永久漏掉这次分享；成功后分享数缓存 +1（缓存不存在时按记录数回种，已含本次）。
func (s *shareServiceImpl) SharePost(ctx context.Context, userID, postID uuid.UUID) (*ShareResult, error) {
	if s.postTarget == nil {
		return nil, errors.New("post target is not configured")
	}
	post, err := s.postTarget.GetShareable(ctx, userID, postID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, domain.ErrPostNotFound
	}

	share := &domain.PostShare{ID: sharedomain.NewID(), UserID: userID, PostID: postID}
	created, err := s.repo.Create(ctx, share)
	if err != nil {
		return nil, fmt.Errorf("failed to persist post share: %w", err)
	}
	if !created {
		count, err := s.currentCount(ctx, postID)
		if err != nil {
			return nil, err
		}
		return &ShareResult{ShareCount: count, FirstShare: false}, nil
	}

	if err := s.publisher.PublishPostShare(ctx, share, post); err != nil {
		if delErr := s.repo.Delete(ctx, share.ID); delErr != nil {
			logger.Log.Error("Failed to roll back post share row: " + delErr.Error())
		}
		return nil, fmt.Errorf("%w: %v", domain.ErrEventPublishFailed, err)
	}

	count, ok, err := s.cache.IncrIfCached(ctx, postID)
	if err != nil {
		logger.Log.Error("Failed to incr post share count cache: " + err.Error())
	}
	if !ok {
		// 缓存不存在（或故障）：按记录数回种，记录数已含本次分享
		if count, err = s.currentCount(ctx, postID); err != nil {
			return nil, err
		}
	}
	return &ShareResult{ShareCount: count, FirstShare: true}, nil
}

// currentCount 当前分享数：缓存优先，miss 按 post_share 记录数回种。缓存故障时直接返回 DB 记录数。
func (s *shareServiceImpl) currentCount(ctx context.Context, postID uuid.UUID) (int64, error) {
	count, ok, err := s.cache.Get(ctx, postID)
	if err == nil && ok {
		return count, nil
	}
	if err != nil {
		logger.Log.Error("Failed to get post share count from cache, falling back to DB: " + err.Error())
	}
	dbCount, err := s.repo.CountByPost(ctx, postID)
	if err != nil {
		return 0, fmt.Errorf("failed to count post shares: %w", err)
	}
	seeded, err := s.cache.Seed(ctx, postID, dbCount)
	if err != nil {
		logger.Log.Error("Failed to seed post share count cache: " + err.Error())
		return dbCount, nil
	}
	return seeded, nil
}
