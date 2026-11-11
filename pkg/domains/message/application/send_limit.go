package application

import (
	"context"

	"github.com/google/uuid"
)

// UnfollowedSendLimit 未关注发送限制（#239）：对方没关注我、也没在会话里回复过我时，我在该会话累计最多发几条。
// 维护者 2026-11-11 决策：固定 5 条，不做成配置（需要可配置再开 issue）。
const UnfollowedSendLimit = 5

// FollowChecker 跨域端口：关注关系查询。由 composition 用 follow 领域的 FollowService.GetRelations 桥接。
type FollowChecker interface {
	// IsFollowing 返回 followerID 当前是否关注了 followeeID。
	IsFollowing(ctx context.Context, followerID, followeeID uuid.UUID) (bool, error)
}

// SendVO 发送接口的响应：消息字段平铺（与 #238 的响应一致，旧客户端不受影响）+ 剩余条数。
type SendVO struct {
	MessageVO
	// RemainingCount 未关注发送限制生效时，本条发出后在该会话还能再发的条数（0 = 已到上限，再发返回 403 dm limit reached）；
	// 不受限制（对方关注了我，或对方在会话里发过消息）时为 null。
	RemainingCount *int `json:"remaining_count"`
}

// sendLimitFor 返回 senderID 给 receiverID 发消息时要传给仓储的限制条数：接收方关注了发送方时不限制（0），
// 否则为 UnfollowedSendLimit。「对方是否回复过」在仓储事务内判断（锁会话行之后），这里只看关注关系。
func (s *messageServiceImpl) sendLimitFor(ctx context.Context, senderID, receiverID uuid.UUID) (int, error) {
	followed, err := s.follows.IsFollowing(ctx, receiverID, senderID)
	if err != nil {
		return 0, err
	}
	if followed {
		return 0, nil
	}
	return UnfollowedSendLimit, nil
}
