package infrastructure

import (
	"context"

	"interestBar/pkg/domains/message/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// sendRateLimiterRedis 私信发送频率计数的 Redis 实现（#242）：key 与 Lua 见 pkg/server/storage/redis/message_rate_lua.go。
type sendRateLimiterRedis struct{}

// NewSendRateLimiter 构造 Redis 版 SendRateLimiter（使用全局 Redis 客户端）。
func NewSendRateLimiter() domain.SendRateLimiter {
	return sendRateLimiterRedis{}
}

// Acquire 见 domain.SendRateLimiter。
func (sendRateLimiterRedis) Acquire(
	ctx context.Context, senderID uuid.UUID, limits domain.SendRateLimits,
) (domain.SendRateDecision, error) {
	exceeded, reserved, err := redispkg.AcquireMessageSendRate(ctx, senderID,
		limits.PerMinute, limits.PerDay, limits.NewConversationsPerDay)
	if err != nil {
		return domain.SendRateDecision{}, err
	}
	switch exceeded {
	case redispkg.MessageSendRatePerMinute:
		return domain.SendRateDecision{Exceeded: domain.SendRatePerMinute}, nil
	case redispkg.MessageSendRatePerDay:
		return domain.SendRateDecision{Exceeded: domain.SendRatePerDay}, nil
	default:
		return domain.SendRateDecision{NewConversationReserved: reserved}, nil
	}
}

// Release 见 domain.SendRateLimiter。
func (sendRateLimiterRedis) Release(ctx context.Context, senderID uuid.UUID, message, newConversation bool) error {
	return redispkg.ReleaseMessageSendRate(ctx, senderID, message, newConversation)
}
