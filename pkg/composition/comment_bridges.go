// Package composition 的 comment_bridges.go：注入 comment 领域的跨域桥接器（user / post → comment）。
package composition

import (
	"context"

	commentapp "interestBar/pkg/domains/comment/application"
	postapp "interestBar/pkg/domains/post/application"
	userapp "interestBar/pkg/domains/user/application"

	"github.com/google/uuid"
)

// ===== user → comment =====

// commentUserFacade 把 user.application.UserFacade 适配为 comment.application.UserFacade。
type commentUserFacade struct {
	delegate userapp.UserFacade
}

func (f *commentUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]commentapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]commentapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = commentapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

func (f *commentUserFacade) GetBrief(ctx context.Context, userID string) (*commentapp.UserBrief, error) {
	b, err := f.delegate.GetBrief(ctx, userID)
	if err != nil || b == nil {
		return nil, err
	}
	return &commentapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}, nil
}

// GetAgentCircleIDs 批量返回 userID → 机器人绑定圈子ID（mention 兜底剔除越圈机器人用，
// 入出参类型一致直接透传）。
func (f *commentUserFacade) GetAgentCircleIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	return f.delegate.GetAgentCircleIDs(ctx, userIDs)
}

// ===== post → comment（帖子元信息 + 评论计数端口）=====

// commentPostLookup 把 post.application.PostService 适配为 comment.application.PostLookup。
//
// comment 领域发评论时需要：
//   - 校验帖子存在性/状态/锁定 → GetPost
//   - 恢复帖子统计缓存 + 递增评论计数 → RestoreStatsAndIncrCommentCount
//   - 私密圈可见性（评论读写对非成员 404）→ CanView
type commentPostLookup struct {
	delegate postapp.PostService
}

func (l *commentPostLookup) GetPost(ctx context.Context, postID uuid.UUID) (*commentapp.PostInfo, error) {
	meta, err := l.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return nil, err
	}
	return &commentapp.PostInfo{
		ID:       meta.ID,
		Status:   meta.Status,
		IsLock:   meta.IsLock,
		CircleID: meta.CircleID,
	}, nil
}

func (l *commentPostLookup) RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error {
	return l.delegate.RestoreStatsAndIncrCommentCount(ctx, postID)
}

// CanView 私密圈可见性：复用 post 域统一判断（PostService.CanViewPost）。
func (l *commentPostLookup) CanView(ctx context.Context, viewerID, postID uuid.UUID) (bool, error) {
	return l.delegate.CanViewPost(ctx, viewerID, postID)
}
