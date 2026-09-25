// Package application 提供 like 领域的应用服务层。
//
// 职责：
//   - 点赞/取消点赞（先解析真实状态，再 Redis Lua 原子设值；已处于期望态则 no-op）
//   - 跨 post / comment 两种目标类型
//   - 点赞前恢复统计缓存（避免 Lua 脚本读到不存在的 stats Hash）
//   - 状态真正变化时才异步发布点赞事件（Redpanda → 消费者批量持久化到 DB）
package application

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/like/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// PostTarget like 领域需要的帖子查询能力。
type PostTarget interface {
	// Exists 检查帖子是否存在（未删除）。存在返回 true，不存在返回 false。
	Exists(ctx context.Context, postID uuid.UUID) (bool, error)
	// RestoreStats 恢复帖子统计缓存（如果不存在）。
	// 用于点赞前确保 Redis stats Hash 存在，避免 Lua 脚本读到空 stats。
	RestoreStats(ctx context.Context, postID uuid.UUID) error
	// IsLiked 用户当前是否已赞该帖（缓存优先，miss 回源 DB 并回填缓存）。
	// 用于点赞前解析真实状态：用户点赞 ZSET 有 TTL 和容量上限，miss 不等于未赞。
	IsLiked(ctx context.Context, userID, postID uuid.UUID) (bool, error)
}

// CommentTarget like 领域需要的评论查询能力。
type CommentTarget interface {
	// ExistsWithPostID 检查评论是否存在，并返回其所属帖子ID。
	// 未找到返回 nil, false, nil。
	ExistsWithPostID(ctx context.Context, commentID uuid.UUID) (postID *uuid.UUID, exists bool, err error)
	// RestoreStats 恢复评论统计缓存（如果不存在）。
	RestoreStats(ctx context.Context, commentID uuid.UUID) error
	// IsLiked 用户当前是否已赞该评论（缓存优先，miss 回源 DB 并回填缓存）。
	IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error)
}

// ===== DTO =====

// ToggleResult 点赞切换结果（供 handler 序列化）。
type ToggleResult struct {
	IsLiked  bool   `json:"is_liked"`
	Type     string `json:"type"`
	TargetID string `json:"target_id"`
}

// ToggleInput 点赞/取消点赞入参。
type ToggleInput struct {
	Type     string    // "comment" 或 "post"
	TargetID uuid.UUID // 评论ID 或 帖子ID
	Action   string    // 可选："like" / "unlike" 显式期望状态；空 = 切换
}

// ===== Service 接口 =====

// LikeService 是 like 领域的应用服务接口。
type LikeService interface {
	// Toggle 点赞/取消点赞：期望状态 = input.Action（显式）或真实当前状态取反（空），原子设值。
	Toggle(ctx context.Context, userID uuid.UUID, input ToggleInput) (*ToggleResult, error)

	// SetPostTarget 注入帖子查询端口。
	SetPostTarget(t PostTarget)
	// SetCommentTarget 注入评论查询端口。
	SetCommentTarget(t CommentTarget)
}

type likeServiceImpl struct {
	postCache     domain.PostLikeCache
	commentCache  domain.CommentLikeCache
	publisher     domain.LikeEventPublisher
	postTarget    PostTarget
	commentTarget CommentTarget
}

// NewLikeService 构造 LikeService。
//
// postTarget / commentTarget 是跨领域依赖，通过 setter 注入（composition 层负责把它们连起来）。
func NewLikeService(
	postCache domain.PostLikeCache,
	commentCache domain.CommentLikeCache,
	publisher domain.LikeEventPublisher,
) LikeService {
	return &likeServiceImpl{
		postCache:    postCache,
		commentCache: commentCache,
		publisher:    publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *likeServiceImpl) SetPostTarget(t PostTarget)       { s.postTarget = t }
func (s *likeServiceImpl) SetCommentTarget(t CommentTarget) { s.commentTarget = t }

// Toggle 点赞/取消点赞。
//
// 流程（设计见 docs/design/like-pipeline-fix-design.md §三 F1.2）：
//  1. 校验目标存在；恢复统计缓存（Lua 脚本依赖 stats Hash）。
//  2. 解析真实当前状态（ZSET miss 回源 DB 并回填），得出期望状态（显式 action 或取反）。
//     显式 action 也必须先解析：回填后 ZSET 才能正确判断"已处于期望态"。
//  3. Redis Lua 原子设值；已处于期望状态则 no-op。
//  4. 状态真正变化时才发布点赞事件（MQ 落库 + 热度 + CF 互动 + 通知）。
func (s *likeServiceImpl) Toggle(ctx context.Context, userID uuid.UUID, input ToggleInput) (*ToggleResult, error) {
	if _, err := domain.ResolveWant(false, input.Action); err != nil {
		return nil, err
	}
	switch domain.TargetType(input.Type) {
	case domain.TargetTypeComment:
		return s.toggleCommentLike(ctx, userID, input.TargetID, input.Action)
	case domain.TargetTypePost:
		return s.togglePostLike(ctx, userID, input.TargetID, input.Action)
	default:
		return nil, domain.ErrInvalidTargetType
	}
}

// togglePostLike 帖子点赞切换。
func (s *likeServiceImpl) togglePostLike(ctx context.Context, userID, postID uuid.UUID, action string) (*ToggleResult, error) {
	if s.postTarget == nil {
		return nil, errors.New("post target is not configured")
	}

	exists, err := s.postTarget.Exists(ctx, postID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrPostNotFound
	}

	if err := s.postTarget.RestoreStats(ctx, postID); err != nil {
		logger.Log.Error("Failed to restore post stats cache: " + err.Error())
	}

	// 真实状态解析失败时不猜测：猜错会让计数漂移，交由客户端重试。
	current, err := s.postTarget.IsLiked(ctx, userID, postID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve post like state: %w", err)
	}
	want, err := domain.ResolveWant(current, action)
	if err != nil {
		return nil, err
	}

	result, err := s.postCache.Set(ctx, userID, postID, want)
	if err != nil {
		return nil, fmt.Errorf("failed to set post like: %w", err)
	}

	if result != domain.ToggleResultUnchanged {
		if err := s.publisher.PublishPostLike(ctx, userID, postID, result.Int64()); err != nil {
			logger.Log.Error("Failed to publish post like event: " + err.Error())
		}
	}

	return &ToggleResult{
		IsLiked:  want,
		Type:     string(domain.TargetTypePost),
		TargetID: postID.String(),
	}, nil
}

// toggleCommentLike 评论点赞切换。
func (s *likeServiceImpl) toggleCommentLike(ctx context.Context, userID, commentID uuid.UUID, action string) (*ToggleResult, error) {
	if s.commentTarget == nil {
		return nil, errors.New("comment target is not configured")
	}

	postIDPtr, exists, err := s.commentTarget.ExistsWithPostID(ctx, commentID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrCommentNotFound
	}

	if err := s.commentTarget.RestoreStats(ctx, commentID); err != nil {
		logger.Log.Error("Failed to restore comment stats cache: " + err.Error())
	}

	current, err := s.commentTarget.IsLiked(ctx, userID, commentID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve comment like state: %w", err)
	}
	want, err := domain.ResolveWant(current, action)
	if err != nil {
		return nil, err
	}

	result, err := s.commentCache.Set(ctx, userID, commentID, want)
	if err != nil {
		return nil, fmt.Errorf("failed to set comment like: %w", err)
	}

	if result != domain.ToggleResultUnchanged {
		var postID uuid.UUID
		if postIDPtr != nil {
			postID = *postIDPtr
		}
		if err := s.publisher.PublishCommentLike(ctx, userID, commentID, postID, result.Int64()); err != nil {
			logger.Log.Error("Failed to publish comment like event: " + err.Error())
		}
	}

	return &ToggleResult{
		IsLiked:  want,
		Type:     string(domain.TargetTypeComment),
		TargetID: commentID.String(),
	}, nil
}
