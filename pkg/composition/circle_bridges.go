// Package composition 的 circle_bridges.go：注入 circle 领域的跨域桥接器（user / post → circle）。
package composition

import (
	"context"

	circleapp "interestBar/pkg/domains/circle/application"
	postapp "interestBar/pkg/domains/post/application"
	userapp "interestBar/pkg/domains/user/application"
)

// ===== user → circle =====

// circleUserFacade 把 user.application.UserFacade 适配为 circle.application.UserFacade。
type circleUserFacade struct {
	delegate userapp.UserFacade
}

func (f *circleUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]circleapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]circleapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = circleapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// SearchBriefs 按关键字搜索用户（circle 成员管理搜索用），保序映射为 circle 域的
// UserBrief，并透传命中总数 total（total > len(列表) 表示按 limit 截断）。
func (f *circleUserFacade) SearchBriefs(ctx context.Context, keyword string, limit int) ([]circleapp.UserBrief, int64, error) {
	briefs, total, err := f.delegate.SearchBriefs(ctx, keyword, limit)
	if err != nil {
		return nil, 0, err
	}
	result := make([]circleapp.UserBrief, 0, len(briefs))
	for _, b := range briefs {
		result = append(result, circleapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL})
	}
	return result, total, nil
}

// ===== post → circle =====

// postMediaFetcherForCircle 把 post.application.PostService 适配为
// circle.application.PostMediaFetcher。
type postMediaFetcherForCircle struct {
	delegate postapp.PostService
}

func (f *postMediaFetcherForCircle) GetMediaByPostIDs(ctx context.Context, postIDs []string) (map[string][]string, error) {
	return f.delegate.GetMediaByPostIDs(ctx, postIDs)
}
