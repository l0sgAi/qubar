package infrastructure

import (
	"context"

	"interestBar/pkg/domains/recommend/domain"
	elasticsearch "interestBar/pkg/server/storage/elasticsearch"

	"github.com/google/uuid"
)

// homeFeedSearcherES 基于 elasticsearch.SearchHomeFeed 的 HomeFeedSearcher 实现。
type homeFeedSearcherES struct{}

// NewHomeFeedSearcher 构造 HomeFeedSearcher。
func NewHomeFeedSearcher() domain.HomeFeedSearcher {
	return &homeFeedSearcherES{}
}

// Search 调 ES SearchHomeFeed，边界提取 PostDoc.ID → 有序 postID（纯 ID 合并）。
func (s *homeFeedSearcherES) Search(ctx context.Context, sort string, circleIDs, excludeCircleIDs []uuid.UUID, size int, searchAfter []interface{}) ([]uuid.UUID, []interface{}, error) {
	res, err := elasticsearch.SearchHomeFeed(ctx, sort, circleIDs, excludeCircleIDs, size, searchAfter)
	if err != nil {
		return nil, nil, err
	}
	return postIDs(res), res.SearchAfter, nil
}

// SearchFollowing 调 ES SearchFollowingFeed（圈子 或 作者，发帖时间倒序）。
func (s *homeFeedSearcherES) SearchFollowing(ctx context.Context, circleIDs, authorIDs, excludeCircleIDs []uuid.UUID, size int, searchAfter []interface{}) ([]uuid.UUID, []interface{}, error) {
	res, err := elasticsearch.SearchFollowingFeed(ctx, circleIDs, authorIDs, excludeCircleIDs, size, searchAfter)
	if err != nil {
		return nil, nil, err
	}
	return postIDs(res), res.SearchAfter, nil
}

// postIDs 按命中顺序提取帖子 ID（解析失败的跳过）。
func postIDs(res *elasticsearch.PostListResponse) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(res.Posts))
	for _, doc := range res.Posts {
		if id, perr := uuid.Parse(doc.ID); perr == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
