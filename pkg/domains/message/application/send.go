package application

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"interestBar/pkg/domains/message/domain"
	"interestBar/pkg/server/utils"

	"github.com/google/uuid"
)

// SendService 发送私信。
type SendService interface {
	// Send 给 receiverID 发一条文本私信；首次发送自动建会话。返回写入的消息。
	//
	// 错误：发给自己 domain.ErrCannotMessageSelf；正文为空 / 只有空白 domain.ErrEmptyContent；
	// 正文超过 MaxContentChars 个字符 包装了 domain.ErrContentTooLong 的错误（message 带上限值）；
	// 接收方不存在 / 已禁用 / 已删除 domain.ErrRecipientNotFound；接收方是 AI 机器人账号 domain.ErrRecipientUnavailable；
	// 接收方没关注发送方、也没在会话里发过消息，且发送方在该会话已发满 UnfollowedSendLimit 条 domain.ErrSendLimitReached（#239）。
	Send(ctx context.Context, senderID, receiverID uuid.UUID, content string) (*SendVO, error)
	// SendWithClientMsgID 同 Send，并支持发送幂等（#248）：clientMsgID 为客户端生成的幂等键，空串表示不提供（等同 Send）。
	// 同一发送人用同一 clientMsgID 再次请求（含并发重复）时返回第一次写入的那条消息，不新增消息、不改计数 / 未读 / 最新消息、
	// 不推送；正文与接收方不参与判断（正文不同也返回原消息）。重放判断在 #239 发送限制之前，已达上限时重试返回原消息而不是 403。
	//
	// 额外错误：clientMsgID 超过 MaxClientMsgIDLen 或含 [A-Za-z0-9_-] 以外的字符 domain.ErrInvalidClientMsgID。
	SendWithClientMsgID(ctx context.Context, senderID, receiverID uuid.UUID, content, clientMsgID string) (*SendVO, error)
}

// MessageVO 一条消息。
type MessageVO struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	SenderID       string    `json:"sender_id"`
	MsgType        int16     `json:"msg_type"`
	Content        string    `json:"content"`
	CreateTime     time.Time `json:"create_time"`
}

func toMessageVO(m *domain.Message) MessageVO {
	return MessageVO{
		ID:             m.ID.String(),
		ConversationID: m.ConversationID.String(),
		SenderID:       m.SenderID.String(),
		MsgType:        m.MsgType,
		Content:        m.Content,
		CreateTime:     m.CreateTime,
	}
}

func (s *messageServiceImpl) Send(ctx context.Context, senderID, receiverID uuid.UUID, content string) (*SendVO, error) {
	return s.SendWithClientMsgID(ctx, senderID, receiverID, content, "")
}

func (s *messageServiceImpl) SendWithClientMsgID(
	ctx context.Context, senderID, receiverID uuid.UUID, content, clientMsgID string,
) (*SendVO, error) {
	if senderID == receiverID {
		return nil, domain.ErrCannotMessageSelf
	}
	content, err := normalizeContent(content)
	if err != nil {
		return nil, err
	}
	idemKey, err := normalizeClientMsgID(clientMsgID)
	if err != nil {
		return nil, err
	}
	status, err := s.recipients.CheckRecipient(ctx, receiverID)
	if err != nil {
		return nil, err
	}
	switch status {
	case RecipientOK:
	case RecipientBot:
		return nil, domain.ErrRecipientUnavailable
	default:
		return nil, domain.ErrRecipientNotFound
	}
	limit, err := s.sendLimitFor(ctx, senderID, receiverID)
	if err != nil {
		return nil, err
	}
	res, err := s.repo.SendDirect(ctx, senderID, receiverID, domain.MessageTypeText, content, limit, idemKey)
	if err != nil {
		return nil, err
	}
	msg := res.Message
	// 事务已提交：接收方未读缓存与实时推送（#241，尽力而为）。
	// 发送幂等重放（#248）没有写任何数据：不改未读缓存、不重复推送。
	if !res.Replayed {
		s.afterSend(ctx, msg, receiverID)
	}
	vo := &SendVO{MessageVO: toMessageVO(msg), RemainingCount: res.Remaining}
	return vo, nil
}

// normalizeContent 清洗正文（SanitizeForPg 去掉 PG 不接受的字节）后校验长度：
// 至少一个非空白字符，至多 MaxContentChars 个字符。正文按原样保存（不去首尾空白）。
func normalizeContent(content string) (string, error) {
	content = utils.SanitizeForPg(content)
	if strings.TrimSpace(content) == "" {
		return "", domain.ErrEmptyContent
	}
	if utf8.RuneCountInString(content) > MaxContentChars {
		return "", fmt.Errorf("%w: at most %d characters", domain.ErrContentTooLong, MaxContentChars)
	}
	return content, nil
}
