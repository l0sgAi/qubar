// Package composition 的 collect_bridges.go：注入 collect 领域的跨域桥接器（post → collect）。
package composition

import (
	"context"

	postapp "interestBar/pkg/domains/post/application"

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

func (t *collectPostTarget) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	meta, err := t.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return false, err
	}
	return true, nil
}

func (t *collectPostTarget) RestoreStats(ctx context.Context, postID uuid.UUID) error {
	return t.delegate.RestoreStats(ctx, postID)
}

// collectPostFetcher 把 post.application.PostService 适配为 collect.application.PostFetcher。
type collectPostFetcher struct {
	delegate postapp.PostService
}

func (f *collectPostFetcher) GetPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error) {
	return f.delegate.GetPostsByIDs(ctx, postIDs)
}
