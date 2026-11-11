package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// 哨兵错误。
var (
	// ErrCannotMessageSelf 不能给自己发私信（handler 映射 400）。
	ErrCannotMessageSelf = errors.New("cannot send message to yourself")
	// ErrRecipientNotFound 接收方不存在、已禁用或已删除（handler 映射 404）。
	ErrRecipientNotFound = errors.New("recipient not found")
	// ErrRecipientUnavailable 接收方不接收私信（AI 机器人账号，handler 映射 403）。
	ErrRecipientUnavailable = errors.New("recipient cannot receive messages")
	// ErrEmptyContent 正文为空或只有空白（handler 映射 400）。
	ErrEmptyContent = errors.New("message content is empty")
	// ErrContentTooLong 正文超过字符上限（handler 映射 400）。
	ErrContentTooLong = errors.New("message content is too long")
	// ErrConversationWithSelf 按对方用户 ID 查会话时对方是自己（handler 映射 400）。
	ErrConversationWithSelf = errors.New("cannot have a conversation with yourself")
	// ErrConversationNotFound 会话不存在，或当前用户不是会话成员（handler 映射 404，不区分两者，避免探测会话是否存在）。
	ErrConversationNotFound = errors.New("conversation not found")
	// ErrInvalidCursor 列表游标无法解析，或不属于本次请求的列表（handler 映射 400）。
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrSendLimitReached 未关注发送限制（#239）：对方没关注我、也没在会话里回复过我，我在该会话已发满上限条数（handler 映射 403）。
	ErrSendLimitReached = errors.New("dm limit reached")
	// ErrInvalidClientMsgID 发送幂等键 client_msg_id 超长或含白名单以外的字符（#248，handler 映射 400）。
	ErrInvalidClientMsgID = errors.New("invalid client_msg_id")
)

// SendResult SendDirect 的结果。
type SendResult struct {
	// Message 写入的消息（含会话 ID）。
	Message *Message
	// Remaining 发送限制生效时，本条发出后发送方在该会话还能再发的条数（0 = 已到上限，下一条会被拒绝）；
	// 不受限制（limit ≤ 0，或对方在会话里发过消息）时为 nil。
	Remaining *int
	// Replayed 为 true 表示命中发送幂等（#248）：发送方此前已用同一 client_msg_id 发过，Message 是第一次写入的那条消息，
	// 本次没有写任何数据（不新增消息、不改计数 / 未读 / 最新消息）；调用方不应再触发发送后的副作用（推送等）。
	// 此时 Remaining 按当前已发条数重新计算（= limit − sent_count，不再减本条），口径见 docs/message-design.md 第十二节。
	Replayed bool
}

// ConversationEntry 会话列表里的一行：成员状态 + 最后一条消息（由一次 JOIN 查询得到，没有 N+1）。
type ConversationEntry struct {
	ConversationID uuid.UUID
	Type           int16
	// PeerID 单聊对方；群聊为 uuid.Nil。
	PeerID      uuid.UUID
	UnreadCount int
	// LastMessageID 最后一条消息 ID，也是会话列表的 keyset 键。
	// 会话列表里一定有值；GetDirectConversation 在会话被我删除（隐藏）且之后没有新消息时为 uuid.Nil，
	// 此时 Last* 字段都是零值（隐藏截止位置之前的消息不可见）。
	LastMessageID uuid.UUID
	LastSenderID  uuid.UUID
	LastMsgType   int16
	// LastContent 最后一条消息正文的前若干个字符（摘要，长度由调用方指定）。
	LastContent     string
	LastMessageTime time.Time
}

// Repository 私信的写入与读取。
//
// 写操作（Send / MarkRead / Hide）各自在一个语句或一个事务内完成，成员行的未读数、最新消息、
// 已读位置、隐藏截止位置始终一起变化，不存在中间状态。
type Repository interface {
	// SendDirect 在一个事务内发送一条单聊消息：
	// 取得或创建 (senderID, receiverID) 的会话（并发首发只建一个，并锁住会话行串行化同一会话的发送）
	// → 在锁内生成消息 ID 并写入消息 → 更新双方成员行（不存在则建行）：
	// 双方 last_message_id 前移；发送方 sent_count+1、记录首次发送时间；接收方 unread_count+1。
	// 不校验用户状态（仓储不读 users 表），content 由调用方校验与清洗。返回写入的消息（含会话 ID）。
	//
	// limit > 0 时启用未关注发送限制（#239）：锁住会话行之后、写消息之前，若接收方在会话里从未发过消息
	// （接收方成员行不存在或 first_sent_time 为 NULL）且发送方 sent_count ≥ limit，返回 ErrSendLimitReached，事务回滚。
	// 判断与 sent_count+1 都在会话行锁内，同一会话的并发发送不会突破上限。limit ≤ 0 表示不限制（调用方已确认对方关注了发送方）。
	//
	// clientMsgID 非 nil 时启用发送幂等（#248）：锁住会话行之后、未关注发送限制检查之前，按 (senderID, *clientMsgID)
	// 查已有消息，命中即返回它（Replayed=true），不写任何数据、不检查发送限制；未命中再走正常发送，
	// 插入消息时以唯一索引 uk_message_sender_client_msg 兜底（并发重复时 DO NOTHING 并查回原消息，同样返回 Replayed=true）。
	// 命中按发送人 + client_msg_id 判断，不比较接收方与正文。nil 表示不启用，行为与 #238 / #239 一致。
	SendDirect(ctx context.Context, senderID, receiverID uuid.UUID, msgType int16, content string, limit int, clientMsgID *string) (*SendResult, error)
	// GetMember 返回 userID 在会话里的成员行；不是成员（或会话不存在）返回 ErrConversationNotFound。
	GetMember(ctx context.Context, conversationID, userID uuid.UUID) (*ConversationMember, error)
	// ListConversations 按最后一条消息倒序返回 userID 可见的会话（有消息、且最后一条在隐藏截止位置之后），最多 limit 条。
	// afterMessageID 为 nil 表示从头开始，否则只返回最后一条消息 ID 小于它的会话（keyset）。
	// previewChars 是 LastContent 截取的字符数。
	ListConversations(ctx context.Context, userID uuid.UUID, afterMessageID *uuid.UUID, limit, previewChars int) ([]ConversationEntry, error)
	// GetDirectConversation 按对方用户 ID 取 userID 与 peerID 的单聊会话（按 OrderPair 命中 uk_conversation_pair），
	// 返回 userID 视角的会话行：字段同 ListConversations；最后一条消息不在隐藏截止位置之后时 LastMessageID 为 uuid.Nil。
	// 两人之间没有会话（或 userID 不是成员）返回 ErrConversationNotFound。只读，不改未读数与隐藏状态。
	GetDirectConversation(ctx context.Context, userID, peerID uuid.UUID, previewChars int) (*ConversationEntry, error)
	// ListMessages 按 ID 倒序（新 → 旧）返回会话里的消息，最多 limit 条。
	// clearedMessageID 非 nil 时只返回 ID 大于它的消息（隐藏截止位置之后）；
	// beforeMessageID 非 nil 时只返回 ID 小于它的消息（向上翻页的 keyset）。不校验成员身份，由调用方先 GetMember。
	ListMessages(ctx context.Context, conversationID uuid.UUID, clearedMessageID, beforeMessageID *uuid.UUID, limit int) ([]Message, error)
	// MarkRead 把 userID 在会话里的未读清零，最后已读位置前移到当前最新消息。
	// 不是成员返回 ErrConversationNotFound。
	MarkRead(ctx context.Context, conversationID, userID uuid.UUID) error
	// Hide 删除会话（只对 userID 自己）：隐藏截止位置与已读位置都置为当前最新消息，未读清零。
	// 之后对方再发消息，会话重新出现在 userID 的列表里，只显示截止位置之后的消息。
	// 不是成员返回 ErrConversationNotFound。
	Hide(ctx context.Context, conversationID, userID uuid.UUID) error
	// SumUnread 返回 userID 的私信未读总数：各会话（成员行未退出）未读数之和（#241）。
	SumUnread(ctx context.Context, userID uuid.UUID) (int64, error)
}
