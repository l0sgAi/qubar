package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// 发送频率限制（#242）与发送方状态的哨兵错误。
var (
	// ErrSendRateLimited 发送方超出每分钟 / 每天发送条数上限（handler 映射 429；message 带窗口与上限值）。
	ErrSendRateLimited = errors.New("message rate limit exceeded")
	// ErrNewConversationLimited 发送方当天主动开启的新会话数已满，本次发送会新建会话（handler 映射 429；message 带上限值）。
	ErrNewConversationLimited = errors.New("new conversation limit exceeded")
	// ErrSenderUnavailable 发送方账号已禁用或已删除（token 仍有效时也拒绝，handler 映射 403）。
	ErrSenderUnavailable = errors.New("sender account is unavailable")
)

// SendRateLimits 发送频率上限（按发送者计，均为正数；由配置 message.send_rate 兜底后给出）。
type SendRateLimits struct {
	PerMinute              int
	PerDay                 int
	NewConversationsPerDay int
}

// SendRateExceeded 频率判定结果里超出的窗口。
type SendRateExceeded int

const (
	// SendRateOK 未超限，已占用一次发送配额。
	SendRateOK SendRateExceeded = iota
	// SendRatePerMinute 超出每分钟上限。
	SendRatePerMinute
	// SendRatePerDay 超出每天上限。
	SendRatePerDay
)

// SendRateDecision SendRateLimiter.Acquire 的结果。
type SendRateDecision struct {
	// Exceeded 不为 SendRateOK 时本次没有占用任何配额。
	Exceeded SendRateExceeded
	// NewConversationReserved 放行时是否预占到了 1 个新会话名额（当天新会话数未满）。
	// 为 false 表示名额已用完：本次发送只能发往已有会话。
	NewConversationReserved bool
}

// SendRateLimiter 私信发送频率计数（多实例共享，实现见 infrastructure/send_rate_limiter_redis.go）。
type SendRateLimiter interface {
	// Acquire 原子地检查并占用 senderID 的一次发送配额（每分钟 / 每天各 +1），并在新会话数未满时预占 1 个名额。
	// 超限时不占用任何配额。
	Acquire(ctx context.Context, senderID uuid.UUID, limits SendRateLimits) (SendRateDecision, error)
	// Release 退还 Acquire 占用的配额：message 退还每分钟 / 每天计数，newConversation 退还预占的新会话名额。
	Release(ctx context.Context, senderID uuid.UUID, message, newConversation bool) error
}
