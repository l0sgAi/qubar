// Package composition 的 like_bridges.go：注入 like 领域的跨域桥接器（post / comment → like）。
package composition

import (
	"context"

	commentapp "interestBar/pkg/domains/comment/application"
	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// ===== post → like（帖子存在性 + 统计缓存恢复 + 真实点赞状态）=====

// likePostTarget 把 post.application.PostService 适配为 like.application.PostTarget。
type likePostTarget struct {
	delegate postapp.PostService
}

// Exists 帖子存在且已发布才可操作：审核中 / 驳回 / 屏蔽的帖子对外不可见（#117），
// 按不存在处理，避免借点赞 / 收藏产生计数、热度与通知。
func (t *likePostTarget) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	meta, err := t.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return false, err
	}
	return meta.Status == postdomain.PostStatusPublished, nil
}

func (t *likePostTarget) RestoreStats(ctx context.Context, postID uuid.UUID) error {
	return t.delegate.RestoreStats(ctx, postID)
}

func (t *likePostTarget) IsLiked(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	return t.delegate.IsLikedByUser(ctx, userID, postID)
}

// CanView 私密圈可见性：复用 post 域统一判断（PostService.CanViewPost）。
func (t *likePostTarget) CanView(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	return t.delegate.CanViewPost(ctx, userID, postID)
}

// ===== comment → like（评论存在性 + 所属帖子ID + 统计缓存恢复 + 真实点赞状态）=====

// likeCommentTarget 把 comment.application.CommentService 适配为 like.application.CommentTarget。
type likeCommentTarget struct {
	delegate commentapp.CommentService
}

func (t *likeCommentTarget) ExistsWithPostID(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, bool, error) {
	postID, err := t.delegate.GetCommentMeta(ctx, commentID)
	if err != nil {
		return nil, false, err
	}
	if postID == nil {
		return nil, false, nil
	}
	return postID, true, nil
}

func (t *likeCommentTarget) RestoreStats(ctx context.Context, commentID uuid.UUID) error {
	return t.delegate.RestoreCommentStats(ctx, commentID)
}

func (t *likeCommentTarget) IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	return t.delegate.IsLikedByUser(ctx, userID, commentID)
}
