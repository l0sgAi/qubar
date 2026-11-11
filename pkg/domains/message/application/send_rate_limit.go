package application

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/message/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// SendRateLimitConfigurer 注入私信发送频率限制（#242，设计见 docs/message-design.md 第十二节）。
type SendRateLimitConfigurer interface {
	// SetSendRateLimiter 注入频率计数与上限读取函数（每次发送时调用，配置热更新即时生效）。
	// limiter 为 nil 时不限制频率（单测 / 未接 Redis 的场景）。
	SetSendRateLimiter(limiter domain.SendRateLimiter, limits func() domain.SendRateLimits)
}

// SetSendRateLimiter 见 SendRateLimitConfigurer。
func (s *messageServiceImpl) SetSendRateLimiter(limiter domain.SendRateLimiter, limits func() domain.SendRateLimits) {
	s.rateLimiter, s.rateLimits = limiter, limits
}

// sendRateTicket 本次发送占用的频率配额，发送失败或没有新建会话时据此退还。
type sendRateTicket struct {
	// newConversation 是否预占了新会话名额。
	newConversation bool
}

// acquireSendRate 在写库之前检查并占用 senderID 的发送频率配额（每分钟 / 每天条数、每天新会话数）。
//
// 新会话名额在 Acquire 里先预占（未满时）；已用完时只允许发往已有会话：多一次按唯一索引的会话查询，
// 会话不存在（本次会新建）返回 ErrNewConversationLimited。只有当天新会话名额用完的发送者才会走这次查询。
// 被拒绝时不占用任何配额。limiter 未注入时返回 (nil, nil)。
// Redis 出错时拒绝发送（返回错误，handler 映射 500），与 auth 验证码频控的处理一致。
func (s *messageServiceImpl) acquireSendRate(ctx context.Context, senderID, receiverID uuid.UUID) (*sendRateTicket, error) {
	if s.rateLimiter == nil {
		return nil, nil
	}
	limits := s.rateLimits()
	d, err := s.rateLimiter.Acquire(ctx, senderID, limits)
	if err != nil {
		return nil, fmt.Errorf("message: check send rate: %w", err)
	}
	switch d.Exceeded {
	case domain.SendRateOK:
	case domain.SendRatePerMinute:
		return nil, fmt.Errorf("%w: at most %d messages per minute", domain.ErrSendRateLimited, limits.PerMinute)
	default:
		return nil, fmt.Errorf("%w: at most %d messages per day", domain.ErrSendRateLimited, limits.PerDay)
	}
	ticket := &sendRateTicket{newConversation: d.NewConversationReserved}
	if !d.NewConversationReserved {
		_, err := s.repo.GetDirectConversation(ctx, senderID, receiverID, 0)
		if errors.Is(err, domain.ErrConversationNotFound) {
			s.releaseSendRate(ctx, senderID, ticket, true)
			return nil, fmt.Errorf("%w: at most %d new conversations per day",
				domain.ErrNewConversationLimited, limits.NewConversationsPerDay)
		}
		if err != nil {
			s.releaseSendRate(ctx, senderID, ticket, true)
			return nil, err
		}
	}
	return ticket, nil
}

// settleSendRate 发送结束后结算占用的配额：发送失败（sendErr != nil）全部退还；
// 发送成功但没有新建会话时退还预占的新会话名额。
func (s *messageServiceImpl) settleSendRate(ctx context.Context, senderID uuid.UUID, ticket *sendRateTicket, res *domain.SendResult, sendErr error) {
	if ticket == nil {
		return
	}
	if sendErr != nil {
		s.releaseSendRate(ctx, senderID, ticket, true)
		return
	}
	if ticket.newConversation && !res.Created {
		s.releaseSendRate(ctx, senderID, &sendRateTicket{newConversation: true}, false)
	}
}

// releaseSendRate 退还配额（尽力而为：失败只记日志，代价是该发送者当前窗口少几条额度）。
func (s *messageServiceImpl) releaseSendRate(ctx context.Context, senderID uuid.UUID, ticket *sendRateTicket, message bool) {
	if !message && !ticket.newConversation {
		return
	}
	if err := s.rateLimiter.Release(ctx, senderID, message, ticket.newConversation); err != nil {
		logger.Log.Warn("message: release send rate failed for sender " + senderID.String() + ": " + err.Error())
	}
}

// checkSender 发送方账号状态实时校验（#242）：登录态只校验 token，账号被禁用 / 删除后 token 在过期前仍有效，
// 这里复用 RecipientChecker（与接收方同一判定）拒绝这类账号发送。AI 机器人账号不在此拦截（行为与之前一致）。
func (s *messageServiceImpl) checkSender(ctx context.Context, senderID uuid.UUID) error {
	status, err := s.recipients.CheckRecipient(ctx, senderID)
	if err != nil {
		return err
	}
	if status == RecipientNotFound {
		return domain.ErrSenderUnavailable
	}
	return nil
}
