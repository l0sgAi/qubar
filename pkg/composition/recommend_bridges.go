// Package composition 的 recommend_bridges.go：注入 recommend 领域的跨域桥接器（post / circle → recommend）。
package composition

import (
	"context"

	circleapp "interestBar/pkg/domains/circle/application"
	postapp "interestBar/pkg/domains/post/application"
	recommenddomain "interestBar/pkg/domains/recommend/domain"

	"github.com/google/uuid"
)

// ===== recommend ← (post, circle) =====

// recommendCircleLookup 把 circle.application.CircleService 适配为 recommend.domain.CircleLookup。
type recommendCircleLookup struct {
	delegate circleapp.CircleService
}

func (l *recommendCircleLookup) ListJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return l.delegate.ListJoinedCircleIDs(ctx, userID, limit)
}

// recommendPostMetaReader 把 post.application.PostService 适配为 recommend.domain.PostMetaReader。
type recommendPostMetaReader struct {
	delegate postapp.PostService
}

func (r *recommendPostMetaReader) ListCircleIDsByPostIDs(ctx context.Context, postIDs []uuid.UUID) ([]uuid.UUID, error) {
	return r.delegate.ListCircleIDsByPostIDs(ctx, postIDs)
}

// recommendPostHydrator 把 post.application.PostService.SearchPostsByIDs 适配为
// recommend.domain.PostHydrator（[]PostListItem → []FeedPostItem 字段拷贝，不含交互态）。
type recommendPostHydrator struct {
	delegate postapp.PostService
}

func (h *recommendPostHydrator) Hydrate(ctx context.Context, viewerID uuid.UUID, postIDs []uuid.UUID) ([]recommenddomain.FeedPostItem, error) {
	items, err := h.delegate.SearchPostsByIDs(ctx, viewerID, postIDs)
	if err != nil {
		return nil, err
	}
	out := make([]recommenddomain.FeedPostItem, 0, len(items))
	for _, p := range items {
		out = append(out, recommenddomain.FeedPostItem{
			ID: p.ID, CircleID: p.CircleID, UserID: p.UserID, Type: p.Type,
			Title: p.Title, Summary: p.Summary, Content: p.Content,
			ViewCount: p.ViewCount, CommentCount: p.CommentCount,
			LikeCount: p.LikeCount, CollectCount: p.CollectCount,
			IsPinned: p.IsPinned, IsEssence: p.IsEssence, IsLock: p.IsLock,
			Status: p.Status, CreateTime: p.CreateTime,
			AuthorName: p.AuthorName, AuthorAvatar: p.AuthorAvatar,
			CircleName: p.CircleName, CircleAvatar: p.CircleAvatar,
			Images: p.Images,
		})
	}
	return out, nil
}

var _ recommenddomain.InteractionChecker = (*postInteractionChecker)(nil)
