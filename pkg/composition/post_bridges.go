// Package composition 的 post_bridges.go：注入 post 领域的跨域桥接器（user / circle / category / history → post）。
package composition

import (
	"context"
	"errors"

	categorydomain "interestBar/pkg/domains/category/domain"
	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	historyapp "interestBar/pkg/domains/history/application"
	postapp "interestBar/pkg/domains/post/application"
	userapp "interestBar/pkg/domains/user/application"

	"github.com/google/uuid"
)

// ===== user → post =====

// postUserFacade 把 user.application.UserFacade 适配为 post.application.UserFacade。
type postUserFacade struct {
	delegate userapp.UserFacade
}

func (f *postUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]postapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]postapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = postapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

func (f *postUserFacade) GetBrief(ctx context.Context, userID string) (*postapp.UserBrief, error) {
	b, err := f.delegate.GetBrief(ctx, userID)
	if err != nil || b == nil {
		return nil, err
	}
	return &postapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}, nil
}

// GetAgentMentionScopes 批量返回 userID → 机器人 @提及 可见范围（mention 兜底剔除越圈、
// 停用或已删除的机器人用）。
func (f *postUserFacade) GetAgentMentionScopes(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]postapp.AgentMentionScope, error) {
	scopes, err := f.delegate.GetAgentMentionScopes(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]postapp.AgentMentionScope, len(scopes))
	for id, sc := range scopes {
		result[id] = postapp.AgentMentionScope{CircleID: sc.CircleID, Mentionable: sc.Mentionable}
	}
	return result, nil
}

// ===== circle → post =====

// postCircleFacade 把 circle.application.CircleFacade 适配为 post.application.CircleFacade。
type postCircleFacade struct {
	delegate circleapp.CircleFacade
}

func (f *postCircleFacade) GetBriefs(ctx context.Context, circleIDs []string) (map[string]postapp.CircleBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, circleIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]postapp.CircleBrief, len(briefs))
	for id, b := range briefs {
		result[id] = postapp.CircleBrief{ID: b.ID, Name: b.Name, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// ===== circle → post（成员/状态校验 + 帖子计数端口）=====

// circleMemberCheckerForPost 把 circle MemberRepository 适配为 post 的 CircleMemberChecker。
type circleMemberCheckerForPost struct {
	memberRepo circledomain.MemberRepository
}

func (c *circleMemberCheckerForPost) GetMember(ctx context.Context, circleID, userID uuid.UUID) (*postapp.CircleMemberInfo, error) {
	m, err := c.memberRepo.GetMember(ctx, circleID, userID)
	if err != nil {
		if err == circledomain.ErrMemberNotFound {
			return nil, nil
		}
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	return &postapp.CircleMemberInfo{
		Role:        m.Role,
		Status:      m.Status,
		MuteEndTime: m.MuteEndTime,
	}, nil
}

// circleStatusCheckerForPost 把 circle CircleRepository 适配为 post 的 CircleStatusChecker。
type circleStatusCheckerForPost struct {
	circleRepo circledomain.CircleRepository
}

func (c *circleStatusCheckerForPost) IsCircleAvailable(ctx context.Context, circleID uuid.UUID) (bool, error) {
	circle, err := c.circleRepo.GetByID(ctx, circleID)
	if err != nil {
		if err == circledomain.ErrCircleNotFound {
			return false, nil
		}
		return false, err
	}
	return circle.Status == circledomain.CircleStatusNormal, nil
}

// circlePostCountPortForPost 把 circle CircleService 适配为 post 的 CirclePostCountPort。
type circlePostCountPortForPost struct {
	svc circleapp.CircleService
}

func (p *circlePostCountPortForPost) IncrPostCount(ctx context.Context, circleID uuid.UUID) error {
	return p.svc.IncrPostCount(ctx, circleID)
}

// ===== circle / category → post（审核输入的圈子语境，#117）=====

// postModerationCircleReader 读取圈子名称 + 分类名，供帖子 AI 审核判断语境（如同人圈的虚构暴力）。
// 分类表很小（几十行），每次审核查一次全部启用分类即可。
type postModerationCircleReader struct {
	circleRepo   circledomain.CircleRepository
	categoryRepo categorydomain.CategoryRepository
}

func (r *postModerationCircleReader) GetModerationCircle(ctx context.Context, circleID uuid.UUID) (*postapp.ModerationCircle, error) {
	circle, err := r.circleRepo.GetByID(ctx, circleID)
	if err != nil {
		if errors.Is(err, circledomain.ErrCircleNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := &postapp.ModerationCircle{Name: circle.Name}
	if circle.CategoryID == nil {
		return out, nil
	}
	categories, err := r.categoryRepo.GetAllActiveCategories(ctx)
	if err != nil {
		return out, nil // 分类只是辅助语境，查不到不影响审核
	}
	for _, c := range categories {
		if c.ID == *circle.CategoryID {
			out.Category = c.Name
			break
		}
	}
	return out, nil
}

// ===== history → post =====

// postHistoryRecorder 把 history.application.HistoryService 适配为 post.domain.HistoryRecorder。
// 供 post 域详情页浏览时 async 回调记录浏览历史。
type postHistoryRecorder struct {
	delegate historyapp.HistoryService
}

func (r *postHistoryRecorder) RecordView(ctx context.Context, userID, postID uuid.UUID) error {
	return r.delegate.RecordView(ctx, userID, postID)
}
