// Package domain 存放 message（私信）领域的纯领域模型：会话、会话成员状态、消息与端口接口。
//
// 依赖规则：本包不得 import 任何 gorm/redis/hertz 等基础设施或框架库，也不得 import 其他领域包。
// 设计见 docs/message-design.md。
package domain

import (
	"bytes"
	"time"

	"github.com/google/uuid"
)

// 会话类型（conversation.type）。取值写进库，不要改已有值。
const (
	// ConversationTypeDirect 单聊。
	ConversationTypeDirect int16 = 1
	// ConversationTypeGroup 群聊（#31 预留，当前不产生）。
	ConversationTypeGroup int16 = 2
)

// 消息类型（message.msg_type）。取值写进库，不要改已有值。
const (
	// MessageTypeText 文本消息（当前唯一支持的类型）。
	MessageTypeText int16 = 1
)

// Conversation 会话（domains.conversation）。单聊每对用户一行（uk_conversation_pair），
// UserLow / UserHigh 是双方 ID 按字节序排好的结果（见 OrderPair）；群聊两列为 nil。
type Conversation struct {
	ID         uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	Type       int16      `json:"type" gorm:"column:type;type:smallint;default:1"`
	UserLow    *uuid.UUID `json:"user_low" gorm:"column:user_low;type:uuid"`
	UserHigh   *uuid.UUID `json:"user_high" gorm:"column:user_high;type:uuid"`
	CreateTime time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	Deleted    int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
}

// TableName 指定表名。
func (Conversation) TableName() string { return "domains.conversation" }

// ConversationMember 会话成员状态（domains.conversation_member）：每个会话每个成员一行，与消息同事务维护。
//
// 消息 ID 是 UUIDv7（时间序 = 字节序），所以「位置」一律用消息 ID 表示：
//   - LastMessageID：会话最新一条消息，会话列表按它倒序；
//   - LastReadMessageID：最后已读位置；
//   - ClearedMessageID：隐藏截止位置（删除会话时写入），本成员只能看到 ID 大于它的消息。
//
// SentCount / FirstSentTime 是本成员自己的发送统计，供 #239（未关注发送限制）使用；
// 「对方首条消息时间」即对方成员行的 FirstSentTime。
type ConversationMember struct {
	ID                uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	ConversationID    uuid.UUID  `json:"conversation_id" gorm:"column:conversation_id;type:uuid;not null"`
	UserID            uuid.UUID  `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	PeerID            *uuid.UUID `json:"peer_id" gorm:"column:peer_id;type:uuid"`
	LastMessageID     *uuid.UUID `json:"last_message_id" gorm:"column:last_message_id;type:uuid"`
	LastReadMessageID *uuid.UUID `json:"last_read_message_id" gorm:"column:last_read_message_id;type:uuid"`
	UnreadCount       int        `json:"unread_count" gorm:"column:unread_count;default:0"`
	ClearedMessageID  *uuid.UUID `json:"cleared_message_id" gorm:"column:cleared_message_id;type:uuid"`
	SentCount         int        `json:"sent_count" gorm:"column:sent_count;default:0"`
	FirstSentTime     *time.Time `json:"first_sent_time" gorm:"column:first_sent_time"`
	CreateTime        time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime        time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	Deleted           int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
}

// TableName 指定表名。
func (ConversationMember) TableName() string { return "domains.conversation_member" }

// Message 私信消息（domains.message）：只追加，UUIDv7 主键即发送时间序。
type Message struct {
	ID             uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	ConversationID uuid.UUID `json:"conversation_id" gorm:"column:conversation_id;type:uuid;not null"`
	SenderID       uuid.UUID `json:"sender_id" gorm:"column:sender_id;type:uuid;not null"`
	MsgType        int16     `json:"msg_type" gorm:"column:msg_type;type:smallint;default:1"`
	Content        string    `json:"content" gorm:"column:content"`
	CreateTime     time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime     time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	Deleted        int16     `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
}

// TableName 指定表名。
func (Message) TableName() string { return "domains.message" }

// OrderPair 把单聊双方按 UUID 字节序（与 PG uuid 比较一致）排成 (low, high)，作为会话唯一键。
func OrderPair(a, b uuid.UUID) (low, high uuid.UUID) {
	if bytes.Compare(a[:], b[:]) <= 0 {
		return a, b
	}
	return b, a
}
