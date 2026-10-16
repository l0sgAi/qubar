// Package composition 的 history_bridges.go：注入 history 领域的跨域桥接器（post → history）。
package composition

import (
	"context"

	postapp "interestBar/pkg/domains/post/application"

	"github.com/google/uuid"
)

// ===== post → history =====

// historyPostFetcher 把 post.application.PostService 适配为 history.application.PostFetcher(ES 来源)。
// 供 history「最近浏览」列表按 postID 从 ES 批量取已组装帖子。
type historyPostFetcher struct {
	delegate postapp.PostService
}

func (f *historyPostFetcher) SearchByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error) {
	return f.delegate.SearchPostsByIDs(ctx, postIDs)
}

func (f *historyPostFetcher) SearchByIDsAndKeyword(ctx context.Context, postIDs []uuid.UUID, keyword string, size, offset int) ([]postapp.PostListItem, int64, error) {
	return f.delegate.SearchPostsByIDsAndKeyword(ctx, postIDs, keyword, size, offset)
}
