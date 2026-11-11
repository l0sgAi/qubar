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
	// 发送方账号已禁用 / 已删除 domain.ErrSenderUnavailable；超出每分钟 / 每天发送条数上限 包装了 domain.ErrSendRateLimited 的错误；
	// 当天新会话数已满且本次会新建会话 包装了 domain.ErrNewConversationLimited 的错误（#242，message 带上限值）。
	Send(ctx context.Context, senderID, receiverID uuid.UUID, content string) (*SendVO, error)
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
	if senderID == receiverID {
		return nil, domain.ErrCannotMessageSelf
	}
	content, err := normalizeContent(content)
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
	if err := s.checkSender(ctx, senderID); err != nil {
		return nil, err
	}
	limit, err := s.sendLimitFor(ctx, senderID, receiverID)
	if err != nil {
		return nil, err
	}
	// 发送频率限制（#242）：写库之前占用配额，被拒绝的请求不落库、不计入 sent_count、不推送。
	ticket, err := s.acquireSendRate(ctx, senderID, receiverID)
	if err != nil {
		return nil, err
	}
	res, err := s.repo.SendDirect(ctx, senderID, receiverID, domain.MessageTypeText, content, limit)
	s.settleSendRate(ctx, senderID, ticket, res, err)
	if err != nil {
		return nil, err
	}
	msg := res.Message
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
