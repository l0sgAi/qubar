// Package composition 的 collect_bridges.go：注入 collect 领域的跨域桥接器（post → collect）。
package composition

import (
	"context"

	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// ===== post → collect（帖子存在性 + 统计缓存恢复 + 列表组装）=====

// collectPostTarget 把 post.application.PostService 适配为 collect.domain.PostTarget。
//
// 与 likePostTarget 行为完全一致（collect.PostTarget 与 like.PostTarget 签名相同），
// 独立定义以保持命名清晰、避免跨领域类型耦合。
type collectPostTarget struct {
	delegate postapp.PostService
}

// Exists 帖子存在且已发布才可操作：审核中 / 驳回 / 屏蔽的帖子对外不可见（#117），
// 按不存在处理，避免借点赞 / 收藏产生计数、热度与通知。
func (t *collectPostTarget) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	meta, err := t.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return false, err
	}
	return meta.Status == postdomain.PostStatusPublished, nil
}

func (t *collectPostTarget) RestoreStats(ctx context.Context, postID uuid.UUID) error {
	return t.delegate.RestoreStats(ctx, postID)
}

// CanView 私密圈可见性：复用 post 域统一判断（PostService.CanViewPost）。
func (t *collectPostTarget) CanView(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	return t.delegate.CanViewPost(ctx, userID, postID)
}

// collectPostFetcher 把 post.application.PostService 适配为 collect.application.PostFetcher。
type collectPostFetcher struct {
	delegate postapp.PostService
}

func (f *collectPostFetcher) GetPostsByIDs(ctx context.Context, viewerID uuid.UUID, postIDs []uuid.UUID) ([]postapp.PostListItem, error) {
	return f.delegate.GetPostsByIDs(ctx, viewerID, postIDs)
}
