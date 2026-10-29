package infrastructure

import (
	"context"
	"encoding/json"

	"interestBar/pkg/domains/circle/application"
	elasticsearch "interestBar/pkg/server/storage/elasticsearch"

	"github.com/google/uuid"
)

// circleSearcherES 基于 pkg/server/storage/elasticsearch 的 CircleSearcher 实现。
type circleSearcherES struct{}

// NewCircleSearcher 构造 CircleSearcher。
func NewCircleSearcher() application.CircleSearcher {
	return &circleSearcherES{}
}

// Search 搜索圈子。
func (s *circleSearcherES) Search(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*application.CircleSearchResult, error) {
	result, err := elasticsearch.SearchCircles(ctx, keyword, size, searchAfter)
	if err != nil {
		return nil, err
	}
	return toCircleSearchResult(result), nil
}

// SearchMy 在用户已加入的圈子范围内搜索。
func (s *circleSearcherES) SearchMy(ctx context.Context, circleIDs []uuid.UUID, keyword string, size int, searchAfter []interface{}) (*application.MyCircleSearchResult, error) {
	result, err := elasticsearch.SearchMyCircles(ctx, circleIDs, keyword, size, searchAfter)
	if err != nil {
		return nil, err
	}
	return toMyCircleSearchResult(result), nil
}

// SearchCirclePosts 搜索圈内帖子（返回原始 ES 结果，由 circle application 层组装）。
func (s *circleSearcherES) SearchCirclePosts(ctx context.Context, circleID uuid.UUID, sortType, size int, searchAfter []interface{}) (*application.RawCirclePostResult, error) {
	result, err := elasticsearch.SearchCirclePosts(ctx, circleID, sortType, size, searchAfter)
	if err != nil {
		return nil, err
	}

	posts := make([]application.PostDoc, 0, len(result.Posts))
	for _, doc := range result.Posts {
		posts = append(posts, application.PostDoc{
			ID: doc.ID, CircleID: doc.CircleID, UserID: doc.UserID, Type: doc.Type,
			Title: doc.Title, Summary: doc.Summary, Content: doc.Content,
			ViewCount: doc.ViewCount, CommentCount: doc.CommentCount,
			LikeCount: doc.LikeCount, CollectCount: doc.CollectCount,
			IsPinned: doc.IsPinned, IsEssence: doc.IsEssence, IsLock: doc.IsLock,
			Status: doc.Status, CreateTime: doc.CreateTime,
		})
	}

	return &application.RawCirclePostResult{
		Posts:       posts,
		Total:       result.Total,
		Size:        result.Size,
		SearchAfter: marshalSearchAfter(result.SearchAfter),
	}, nil
}

// SearchActive 近期活跃圈子聚合（ES terms 聚合 + 时间窗口，按发帖数排序）。
func (s *circleSearcherES) SearchActive(ctx context.Context, size, offset int, excludeCircleIDs []uuid.UUID) (*application.RawActiveCircleResult, error) {
	result, err := elasticsearch.AggregateActiveCircles(ctx, size, offset, excludeCircleIDs)
	if err != nil {
		return nil, err
	}
	items := make([]application.RawActiveCircleItem, 0, len(result.Buckets))
	for _, b := range result.Buckets {
		id, parseErr := uuid.Parse(b.CircleID)
		if parseErr != nil {
			continue
		}
		items = append(items, application.RawActiveCircleItem{
			CircleID:        id,
			RecentPostCount: b.RecentPostCount,
		})
	}
	return &application.RawActiveCircleResult{
		Items:     items,
		Total:     result.Total,
		Truncated: result.Truncated,
	}, nil
}

// SearchRandom 随机圈子查询（ES random_score，每次结果不同；只取 ID，明细由 application 层组装）。
func (s *circleSearcherES) SearchRandom(ctx context.Context, size int) (*application.RawRandomCircleResult, error) {
	result, err := elasticsearch.SearchRandomCircles(ctx, size)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(result.Circles))
	for _, doc := range result.Circles {
		id, parseErr := uuid.Parse(doc.ID)
		if parseErr != nil {
			continue
		}
		ids = append(ids, id)
	}
	return &application.RawRandomCircleResult{
		CircleIDs: ids,
		Total:     result.Total,
	}, nil
}

func toCircleSearchResult(r *elasticsearch.CircleListResponse) *application.CircleSearchResult {
	circles := make([]application.CircleDoc, 0, len(r.Circles))
	for _, doc := range r.Circles {
		circles = append(circles, application.CircleDoc{
			ID: doc.ID, Name: doc.Name, Slug: doc.Slug, AvatarURL: doc.AvatarURL,
			Description: doc.Description, Hot: doc.Hot, CategoryID: doc.CategoryID,
			MemberCount: doc.MemberCount, PostCount: doc.PostCount,
			CreateTime: doc.CreateTime, Status: doc.Status, JoinType: doc.JoinType,
		})
	}
	return &application.CircleSearchResult{
		Circles:     circles,
		Total:       r.Total,
		Size:        r.Size,
		SearchAfter: marshalSearchAfter(r.SearchAfter),
	}
}

func toMyCircleSearchResult(r *elasticsearch.CircleListResponse) *application.MyCircleSearchResult {
	circles := make([]application.MyCircleDoc, 0, len(r.Circles))
	for _, doc := range r.Circles {
		circles = append(circles, application.MyCircleDoc{
			ID: doc.ID, Name: doc.Name, Slug: doc.Slug, AvatarURL: doc.AvatarURL, MemberCount: doc.MemberCount,
		})
	}
	return &application.MyCircleSearchResult{
		Circles:     circles,
		Total:       r.Total,
		Size:        r.Size,
		SearchAfter: marshalSearchAfter(r.SearchAfter),
	}
}

// marshalSearchAfter 把 []interface{} 序列化为 JSON 字符串（旧 controller 行为）。
func marshalSearchAfter(arr []interface{}) string {
	if arr == nil {
		return ""
	}
	if bytes, err := json.Marshal(arr); err == nil {
		return string(bytes)
	}
	return ""
}
