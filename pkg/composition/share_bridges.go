// Package composition 的 share_bridges.go：注入 share 领域的跨域桥接器（post → share，#232）。
package composition

import (
	"context"

	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"
	sharedomain "interestBar/pkg/domains/share/domain"

	"github.com/google/uuid"
)

// sharePostTarget 把 post.application.PostService 适配为 share.domain.PostTarget。
type sharePostTarget struct {
	delegate postapp.PostService
}

// GetShareable 帖子须存在、未删除、已发布，且对分享人可见（与点赞 / 收藏同一可见性判断 CanViewPost）。
// 审核中 / 驳回 / 屏蔽的帖子按不存在处理；私密圈帖子对成员可见，成员可以分享。
func (t *sharePostTarget) GetShareable(ctx context.Context, userID, postID uuid.UUID) (*sharedomain.SharedPost, error) {
	brief, err := t.delegate.GetPostBrief(ctx, postID)
	if err != nil || brief == nil || brief.Status != postdomain.PostStatusPublished {
		return nil, err
	}
	visible, err := t.delegate.CanViewPost(ctx, userID, postID)
	if err != nil || !visible {
		return nil, err
	}
	return &sharedomain.SharedPost{ID: brief.ID, AuthorID: brief.AuthorID, CircleID: brief.CircleID}, nil
}
