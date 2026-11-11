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
	// 接收方不存在 / 已禁用 / 已删除 domain.ErrRecipientNotFound；接收方是 AI 机器人账号 domain.ErrRecipientUnavailable。
	Send(ctx context.Context, senderID, receiverID uuid.UUID, content string) (*MessageVO, error)
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

func (s *messageServiceImpl) Send(ctx context.Context, senderID, receiverID uuid.UUID, content string) (*MessageVO, error) {
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
	msg, err := s.repo.SendDirect(ctx, senderID, receiverID, domain.MessageTypeText, content)
	if err != nil {
		return nil, err
	}
	// 事务已提交：接收方未读缓存与实时推送（#241，尽力而为）。
	s.afterSend(ctx, msg, receiverID)
	vo := toMessageVO(msg)
	return &vo, nil
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
