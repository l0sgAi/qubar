// Package composition 的 message_bridges.go：注入 message（私信）领域的跨域桥接器
// （user → message：接收方状态、对方简要信息；follow → message：未关注发送限制的关注关系）。
package composition

import (
	"context"

	followapp "interestBar/pkg/domains/follow/application"
	followdomain "interestBar/pkg/domains/follow/domain"
	messageapp "interestBar/pkg/domains/message/application"
	userapp "interestBar/pkg/domains/user/application"

	"github.com/google/uuid"
)

// ===== user → message（接收方能否收私信）=====

// messageRecipientChecker 把 user.application.UserService 适配为 message.application.RecipientChecker。
type messageRecipientChecker struct {
	delegate userapp.UserService
}

var _ messageapp.RecipientChecker = (*messageRecipientChecker)(nil)

// CheckRecipient 复用 UserService.GetByID（缓存优先，不存在 / 已禁用 / 已删除都返回 nil）；
// AI 机器人账号（role=2，见 agent_bridges.go 的 botUserRole）不接收私信。
func (c *messageRecipientChecker) CheckRecipient(ctx context.Context, userID uuid.UUID) (messageapp.RecipientStatus, error) {
	user, err := c.delegate.GetByID(ctx, userID)
	if err != nil {
		return messageapp.RecipientNotFound, err
	}
	switch {
	case user == nil:
		return messageapp.RecipientNotFound, nil
	case user.Role == botUserRole:
		return messageapp.RecipientBot, nil
	default:
		return messageapp.RecipientOK, nil
	}
}

// ===== user → message（会话列表里对方的简要信息）=====

// messageUserBriefReader 把 user.application.UserFacade 适配为 message.application.UserBriefReader。
type messageUserBriefReader struct {
	delegate userapp.UserFacade
}

var _ messageapp.UserBriefReader = (*messageUserBriefReader)(nil)

// GetBriefs 复用 UserFacade.GetBriefs：不存在 / 已禁用 / 已删除的用户不在返回里，会话列表据此把 peer 置为 null。
func (r *messageUserBriefReader) GetBriefs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]messageapp.UserBrief, error) {
	ids := make([]string, len(userIDs))
	for i, id := range userIDs {
		ids[i] = id.String()
	}
	briefs, err := r.delegate.GetBriefs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]messageapp.UserBrief, len(briefs))
	for _, id := range userIDs {
		if b, ok := briefs[id.String()]; ok {
			out[id] = messageapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
		}
	}
	return out, nil
}

// ===== follow → message（未关注发送限制，#239）=====

// messageFollowChecker 把 follow.application.FollowService 适配为 message.application.FollowChecker。
type messageFollowChecker struct {
	delegate followapp.FollowService
}

var _ messageapp.FollowChecker = (*messageFollowChecker)(nil)

// IsFollowing 复用 FollowService.GetRelations（以 followerID 为查看者查 followeeID，直接读关系行，不走缓存）：
// 关系值的「我关注了对方」位即 followerID 关注了 followeeID。
func (c *messageFollowChecker) IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error) {
	vo, err := c.delegate.GetRelations(ctx, followerID, []uuid.UUID{followeeID})
	if err != nil {
		return false, err
	}
	return vo.Relations[followeeID.String()]&followdomain.RelationFollowing != 0, nil
}
