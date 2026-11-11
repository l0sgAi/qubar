// Package composition 的 follow_bridges.go：注入 follow 领域的跨域桥接器（user → follow：存在性、简要信息、列表可见性）。
package composition

import (
	"context"

	followapp "interestBar/pkg/domains/follow/application"
	userapp "interestBar/pkg/domains/user/application"
	userdomain "interestBar/pkg/domains/user/domain"

	"github.com/google/uuid"
)

// ===== user → follow（目标用户存在性）=====

// followUserChecker 把 user.application.UserFacade 适配为 follow.application.UserChecker。
type followUserChecker struct {
	delegate userapp.UserFacade
}

var _ followapp.UserChecker = (*followUserChecker)(nil)

// Exists 复用 UserFacade.GetBrief（内部走 UserService.GetByID：缓存优先，
// 不存在 / 已禁用 / 已删除都返回 nil），AI 机器人账号（role=2）同样视为存在。
func (c *followUserChecker) Exists(ctx context.Context, userID uuid.UUID) (bool, error) {
	brief, err := c.delegate.GetBrief(ctx, userID.String())
	if err != nil {
		return false, err
	}
	return brief != nil, nil
}

// ===== user → follow（列表项的用户简要信息）=====

// followUserBriefReader 把 user.application.UserFacade 适配为 follow.application.UserBriefReader。
type followUserBriefReader struct {
	delegate userapp.UserFacade
}

var _ followapp.UserBriefReader = (*followUserBriefReader)(nil)

// GetBriefs 复用 UserFacade.GetBriefs：不存在 / 已禁用 / 已删除的用户不在返回里，follow 列表据此过滤。
func (r *followUserBriefReader) GetBriefs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]followapp.UserBrief, error) {
	ids := make([]string, len(userIDs))
	for i, id := range userIDs {
		ids[i] = id.String()
	}
	briefs, err := r.delegate.GetBriefs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]followapp.UserBrief, len(briefs))
	for _, id := range userIDs {
		if b, ok := briefs[id.String()]; ok {
			out[id] = followapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
		}
	}
	return out, nil
}

// ===== user → follow（关注 / 粉丝列表的隐私开关）=====

// followListVisibilityReader 把 user.application.SettingService 适配为 follow.application.ListVisibilityReader。
type followListVisibilityReader struct {
	delegate userapp.SettingService
}

var _ followapp.ListVisibilityReader = (*followListVisibilityReader)(nil)

// IsFollowListPrivate 读用户设置（按主键读一行，没有设置行时为公开）。
func (r *followListVisibilityReader) IsFollowListPrivate(ctx context.Context, userID uuid.UUID) (bool, error) {
	settings, err := r.delegate.GetSettings(ctx, userID)
	if err != nil {
		return false, err
	}
	return settings.FollowListVisibility == userdomain.FollowListPrivate, nil
}
