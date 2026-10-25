// Package composition 的 notice_bridges.go：注入 notice 领域的跨域桥接器（user / post → notice）。
package composition

import (
	"context"

	noticeapp "interestBar/pkg/domains/notice/application"
	postapp "interestBar/pkg/domains/post/application"
	userapp "interestBar/pkg/domains/user/application"

	"github.com/google/uuid"
)

// ===== user → notice =====

// noticeUserFacade 把 user.application.UserFacade 适配为 notice.application.UserFacade。
type noticeUserFacade struct {
	delegate userapp.UserFacade
}

func (f *noticeUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]noticeapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]noticeapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = noticeapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// ===== post → notice =====

// noticePostFacade 把 post.application.PostService 适配为 notice.application.PostFacade（审核类通知 circle_id 回填）。
type noticePostFacade struct {
	delegate postapp.PostService
}

func (f *noticePostFacade) GetCircleIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	return f.delegate.GetCircleIDsByPostIDs(ctx, postIDs)
}
