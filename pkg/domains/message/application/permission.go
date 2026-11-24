package application

import (
	"context"
	"errors"

	"interestBar/pkg/domains/message/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// DMPermission 接收方的私信权限设置（#240）：谁可以给我发私信。取值与 user 域 user_setting.dm_permission 一一对应，
// 由 composition 桥接映射（见 message_bridges.go），message 域不 import user 域。
type DMPermission int16

const (
	// DMPermissionEveryone 所有人（默认）：仍受未关注 5 条限制（#239）。
	DMPermissionEveryone DMPermission = iota
	// DMPermissionFollowing 仅接收方关注的人。
	DMPermissionFollowing
	// DMPermissionMutual 仅互相关注（接收方关注发送方，且发送方关注接收方）。
	DMPermissionMutual
	// DMPermissionClosed 关闭：任何人都不能发。
	DMPermissionClosed
)

// DMPermissionReader 跨域端口：读取用户的私信权限设置。由 composition 用 user 领域的 SettingService 桥接；
// 用户没有设置行时返回 DMPermissionEveryone。
type DMPermissionReader interface {
	GetDMPermission(ctx context.Context, userID uuid.UUID) (DMPermission, error)
}

// PermissionService 私信权限相关的装配入口。
type PermissionService interface {
	// SetDMPermissionReader 注入私信权限读取端口（composition 在 user 设置服务构造后调用）。
	// 未注入时视为所有人都是「所有人可发」（与 #240 之前的行为一致）。
	SetDMPermissionReader(r DMPermissionReader)
}

// CannotSendReason「我不能给对方发私信」的原因（GET /message/conversations/with/:user_id 的 cannot_send_reason），
// 与发送接口的拒绝错误一一对应（见 cannotSendReasonOf）。
type CannotSendReason string

const (
	// ReasonRecipientNotFound 对方不存在 / 已禁用 / 已删除（发送返回 404）。
	ReasonRecipientNotFound CannotSendReason = "recipient_not_found"
	// ReasonRecipientUnavailable 对方是 AI 机器人账号，不接收私信（发送返回 403）。
	ReasonRecipientUnavailable CannotSendReason = "recipient_unavailable"
	// ReasonFollowingOnly 对方只接收其关注的人的私信，而对方没有关注我（发送返回 403）。
	ReasonFollowingOnly CannotSendReason = "following_only"
	// ReasonMutualOnly 对方只接收互相关注的人的私信，而我们不是互相关注（发送返回 403）。
	ReasonMutualOnly CannotSendReason = "mutual_only"
	// ReasonDMClosed 对方关闭了私信（发送返回 403）。
	ReasonDMClosed CannotSendReason = "dm_closed"
	// ReasonLimitReached 对方没关注我、也没回复过我，我在该会话已发满 UnfollowedSendLimit 条（#239，发送返回 403）。
	ReasonLimitReached CannotSendReason = "dm_limit_reached"
)

// SendPermissionVO「我能否给对方发私信」，与发送接口同一口径（判断顺序也相同）。
type SendPermissionVO struct {
	CanSend bool `json:"can_send"`
	// CannotSendReason can_send=false 时的原因；can_send=true 时为 null。
	CannotSendReason *CannotSendReason `json:"cannot_send_reason"`
}

func (s *messageServiceImpl) SetDMPermissionReader(r DMPermissionReader) { s.permissions = r }

// checkDMPermission 按接收方的私信权限设置判断 senderID 能否给 receiverID 发私信（#240）。
// 已有会话同样受约束，对方回复过也不豁免（维护者 2026-11-11 决策）；幂等重放的放行见 replayBypassesDMPermission。不允许时返回
// domain.ErrRecipientFollowingOnly / ErrRecipientMutualOnly / ErrRecipientDMClosed；所有人可发时不查关注关系。
func (s *messageServiceImpl) checkDMPermission(ctx context.Context, senderID, receiverID uuid.UUID) error {
	if s.permissions == nil {
		return nil
	}
	perm, err := s.permissions.GetDMPermission(ctx, receiverID)
	if err != nil {
		return err
	}
	switch perm {
	case DMPermissionEveryone:
		return nil
	case DMPermissionFollowing, DMPermissionMutual:
		denied := domain.ErrRecipientFollowingOnly
		if perm == DMPermissionMutual {
			denied = domain.ErrRecipientMutualOnly
		}
		followed, err := s.follows.IsFollowing(ctx, receiverID, senderID)
		if err != nil {
			return err
		}
		if !followed {
			return denied
		}
		if perm == DMPermissionFollowing {
			return nil
		}
		following, err := s.follows.IsFollowing(ctx, senderID, receiverID)
		if err != nil {
			return err
		}
		if !following {
			return denied
		}
		return nil
	default:
		// DMPermissionClosed，以及未知取值（应用层只写合法值；未知值按关闭处理，宁可拒绝也不放过）。
		return domain.ErrRecipientDMClosed
	}
}

// isDMPermissionDenied 是否为私信权限设置的拒绝（不含接收方状态、5 条限制等其他拒绝）。
func isDMPermissionDenied(err error) bool {
	return errors.Is(err, domain.ErrRecipientFollowingOnly) || errors.Is(err, domain.ErrRecipientMutualOnly) ||
		errors.Is(err, domain.ErrRecipientDMClosed)
}

// replayBypassesDMPermission 发送被私信权限设置拒绝（permErr）时判断是否是幂等重放（#248）：带 client_msg_id 且发送方已用它发过消息，
// 返回 true 放行，SendDirect 会返回原消息（维护者 2026-11-24 确认：原消息发出后对方改了设置，重试仍返回原消息）。
// 只在被权限拒绝时多这一次按唯一索引的查询，正常发送不额外查询。读设置 / 关注关系出错等其他错误不放行；查询出错按拒绝处理并记日志。
// 接收方状态（不存在 / 禁用 / 删除 / 机器人）的拒绝不经过这里，重放仍返回对应错误。
func (s *messageServiceImpl) replayBypassesDMPermission(ctx context.Context, senderID uuid.UUID, clientMsgID *string, permErr error) bool {
	if clientMsgID == nil || !isDMPermissionDenied(permErr) {
		return false
	}
	ok, err := s.repo.HasClientMsgID(ctx, senderID, *clientMsgID)
	if err != nil {
		logger.Log.Warn("message: check replay after dm permission denied failed for sender " + senderID.String() + ": " + err.Error())
		return false
	}
	return ok
}

// sendPermission 计算 senderID 现在能否给 receiverID 发私信，判断顺序与 Send 一致：
// 接收方状态 → 接收方私信权限设置 → 未关注 5 条限制（#239，只读当前计数，不加锁；与之后真正发送之间可能有并发变化）。
func (s *messageServiceImpl) sendPermission(ctx context.Context, senderID, receiverID uuid.UUID) (*SendPermissionVO, error) {
	err := s.checkRecipient(ctx, receiverID)
	if err == nil {
		err = s.checkDMPermission(ctx, senderID, receiverID)
	}
	if err == nil {
		err = s.checkSendLimitState(ctx, senderID, receiverID)
	}
	if err == nil {
		return &SendPermissionVO{CanSend: true}, nil
	}
	reason, ok := cannotSendReasonOf(err)
	if !ok {
		return nil, err
	}
	return &SendPermissionVO{CannotSendReason: &reason}, nil
}

// checkRecipient 接收方状态校验（与 Send 相同）：不存在 / 禁用 / 删除 domain.ErrRecipientNotFound，机器人 domain.ErrRecipientUnavailable。
func (s *messageServiceImpl) checkRecipient(ctx context.Context, receiverID uuid.UUID) error {
	status, err := s.recipients.CheckRecipient(ctx, receiverID)
	if err != nil {
		return err
	}
	switch status {
	case RecipientOK:
		return nil
	case RecipientBot:
		return domain.ErrRecipientUnavailable
	default:
		return domain.ErrRecipientNotFound
	}
}

// checkSendLimitState 只读地判断未关注 5 条限制（与 SendDirect 事务内的 checkSendLimit 同一口径）：
// 接收方关注了发送方、或接收方在会话里发过消息时不限制；否则发送方已发满时返回 domain.ErrSendLimitReached。
func (s *messageServiceImpl) checkSendLimitState(ctx context.Context, senderID, receiverID uuid.UUID) error {
	limit, err := s.sendLimitFor(ctx, senderID, receiverID)
	if err != nil || limit <= 0 {
		return err
	}
	state, err := s.repo.GetSendLimitState(ctx, senderID, receiverID)
	if err != nil {
		return err
	}
	if !state.PeerReplied && state.SentCount >= limit {
		return domain.ErrSendLimitReached
	}
	return nil
}

// cannotSendReasonOf 把发送拒绝错误映射为 CannotSendReason；其他错误（基础设施故障）返回 false。
func cannotSendReasonOf(err error) (CannotSendReason, bool) {
	for _, m := range []struct {
		err    error
		reason CannotSendReason
	}{
		{domain.ErrRecipientNotFound, ReasonRecipientNotFound},
		{domain.ErrRecipientUnavailable, ReasonRecipientUnavailable},
		{domain.ErrRecipientFollowingOnly, ReasonFollowingOnly},
		{domain.ErrRecipientMutualOnly, ReasonMutualOnly},
		{domain.ErrRecipientDMClosed, ReasonDMClosed},
		{domain.ErrSendLimitReached, ReasonLimitReached},
	} {
		if errors.Is(err, m.err) {
			return m.reason, true
		}
	}
	return "", false
}
