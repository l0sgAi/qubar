// Package infrastructure 是 message 领域的基础设施适配器（PostgreSQL）。
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"interestBar/pkg/domains/message/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type messageRepoPG struct {
	db *gorm.DB
}

// NewRepository 构造私信仓储。
func NewRepository(db *gorm.DB) domain.Repository {
	return &messageRepoPG{db: db}
}

// 发送：会话行 upsert → 写消息 → 双方成员行 upsert，同一事务。
//
// 会话行用 DO UPDATE（而不是 DO NOTHING）：冲突时也返回已有行的 ID，并对会话行加行锁，
// 同一会话的发送因此串行执行——消息 ID 在锁内生成，同一会话内 ID 顺序与提交顺序一致，
// 消息历史按 ID 翻页不会漏掉「晚提交、ID 更小」的消息；之后两条成员行的更新也不会互相等待成环。
// last_message_id 仍用 GREATEST 前移（NULL 被忽略），多实例时钟偏差下也不会回退。
const (
	sqlUpsertDirectConversation = `
INSERT INTO domains.conversation AS c (id, type, user_low, user_high)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_low, user_high) DO UPDATE SET update_time = CURRENT_TIMESTAMP
RETURNING c.id`

	sqlInsertMessage = `
INSERT INTO domains.message (id, conversation_id, sender_id, msg_type, content)
VALUES (?, ?, ?, ?, ?)
RETURNING create_time`

	sqlUpsertSenderMember = `
INSERT INTO domains.conversation_member AS m
  (id, conversation_id, user_id, peer_id, last_message_id, sent_count, first_sent_time)
VALUES (?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP)
ON CONFLICT (conversation_id, user_id) DO UPDATE
SET last_message_id = GREATEST(m.last_message_id, EXCLUDED.last_message_id),
    sent_count = m.sent_count + 1,
    first_sent_time = COALESCE(m.first_sent_time, EXCLUDED.first_sent_time),
    update_time = CURRENT_TIMESTAMP`

	sqlUpsertReceiverMember = `
INSERT INTO domains.conversation_member AS m
  (id, conversation_id, user_id, peer_id, last_message_id, unread_count)
VALUES (?, ?, ?, ?, ?, 1)
ON CONFLICT (conversation_id, user_id) DO UPDATE
SET last_message_id = GREATEST(m.last_message_id, EXCLUDED.last_message_id),
    unread_count = m.unread_count + 1,
    update_time = CURRENT_TIMESTAMP`
)

// SendDirect 见 domain.Repository。
func (r *messageRepoPG) SendDirect(
	ctx context.Context, senderID, receiverID uuid.UUID, msgType int16, content string,
) (*domain.Message, error) {
	low, high := domain.OrderPair(senderID, receiverID)
	msg := &domain.Message{SenderID: senderID, MsgType: msgType, Content: content}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conv struct{ ID uuid.UUID }
		if err := tx.Raw(sqlUpsertDirectConversation,
			sharedomain.NewID(), domain.ConversationTypeDirect, low, high).Scan(&conv).Error; err != nil {
			return err
		}
		if conv.ID == uuid.Nil {
			return errors.New("message: upsert conversation returned no id")
		}
		msg.ConversationID = conv.ID
		// 在会话行锁内生成消息 ID（见上方说明）。
		msg.ID = sharedomain.NewID()
		var created struct{ CreateTime time.Time }
		if err := tx.Raw(sqlInsertMessage,
			msg.ID, msg.ConversationID, senderID, msgType, content).Scan(&created).Error; err != nil {
			return err
		}
		msg.CreateTime = created.CreateTime
		if err := tx.Exec(sqlUpsertSenderMember,
			sharedomain.NewID(), msg.ConversationID, senderID, receiverID, msg.ID).Error; err != nil {
			return err
		}
		return tx.Exec(sqlUpsertReceiverMember,
			sharedomain.NewID(), msg.ConversationID, receiverID, senderID, msg.ID).Error
	})
	if err != nil {
		return nil, err
	}
	msg.UpdateTime = msg.CreateTime
	return msg, nil
}

// GetMember 见 domain.Repository。
func (r *messageRepoPG) GetMember(ctx context.Context, conversationID, userID uuid.UUID) (*domain.ConversationMember, error) {
	var m domain.ConversationMember
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND user_id = ? AND deleted = 0", conversationID, userID).
		Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// 会话列表：走 idx_conversation_member_list (user_id, last_message_id DESC) WHERE deleted = 0；
// 最后一条消息与会话类型按主键 JOIN（一次查询，没有 N+1）。%s 处是 keyset 条件（首页为空）。
const (
	sqlListConversations = `
SELECT m.conversation_id, c.type, m.peer_id, m.unread_count, m.last_message_id,
       msg.sender_id AS last_sender_id, msg.msg_type AS last_msg_type,
       left(msg.content, @preview) AS last_content, msg.create_time AS last_message_time
FROM domains.conversation_member m
JOIN domains.conversation c ON c.id = m.conversation_id
JOIN domains.message msg ON msg.id = m.last_message_id
WHERE m.user_id = @user AND m.deleted = 0
  AND m.last_message_id IS NOT NULL
  AND (m.cleared_message_id IS NULL OR m.last_message_id > m.cleared_message_id) %s
ORDER BY m.last_message_id DESC
LIMIT @limit`

	sqlListConversationsKeyset = `AND m.last_message_id < @after`
)

// conversationRow 会话列表查询的扫描目标（peer_id 可为 NULL）。
type conversationRow struct {
	ConversationID  uuid.UUID
	Type            int16
	PeerID          *uuid.UUID
	UnreadCount     int
	LastMessageID   uuid.UUID
	LastSenderID    uuid.UUID
	LastMsgType     int16
	LastContent     string
	LastMessageTime time.Time
}

// ListConversations 见 domain.Repository。
func (r *messageRepoPG) ListConversations(
	ctx context.Context, userID uuid.UUID, afterMessageID *uuid.UUID, limit, previewChars int,
) ([]domain.ConversationEntry, error) {
	args := map[string]any{"user": userID, "limit": limit, "preview": previewChars}
	keyset := ""
	if afterMessageID != nil {
		keyset = sqlListConversationsKeyset
		args["after"] = *afterMessageID
	}
	var rows []conversationRow
	if err := r.db.WithContext(ctx).Raw(fmt.Sprintf(sqlListConversations, keyset), args).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ConversationEntry, len(rows))
	for i, row := range rows {
		out[i] = domain.ConversationEntry{
			ConversationID:  row.ConversationID,
			Type:            row.Type,
			UnreadCount:     row.UnreadCount,
			LastMessageID:   row.LastMessageID,
			LastSenderID:    row.LastSenderID,
			LastMsgType:     row.LastMsgType,
			LastContent:     row.LastContent,
			LastMessageTime: row.LastMessageTime,
		}
		if row.PeerID != nil {
			out[i].PeerID = *row.PeerID
		}
	}
	return out, nil
}

// ListMessages 见 domain.Repository。走 idx_message_conversation (conversation_id, id DESC) WHERE deleted = 0。
func (r *messageRepoPG) ListMessages(
	ctx context.Context, conversationID uuid.UUID, clearedMessageID, beforeMessageID *uuid.UUID, limit int,
) ([]domain.Message, error) {
	q := r.db.WithContext(ctx).Where("conversation_id = ? AND deleted = 0", conversationID)
	if clearedMessageID != nil {
		q = q.Where("id > ?", *clearedMessageID)
	}
	if beforeMessageID != nil {
		q = q.Where("id < ?", *beforeMessageID)
	}
	var msgs []domain.Message
	if err := q.Order("id DESC").Limit(limit).Find(&msgs).Error; err != nil {
		return nil, err
	}
	return msgs, nil
}

// 已读 / 删除：单条 UPDATE，按成员行的当前 last_message_id 取值，与并发发送在行锁上串行，
// 不会出现「未读清零但已读位置落后」之类的中间状态。
const (
	sqlMarkRead = `
UPDATE domains.conversation_member
SET last_read_message_id = last_message_id, unread_count = 0, update_time = CURRENT_TIMESTAMP
WHERE conversation_id = ? AND user_id = ? AND deleted = 0`

	sqlHide = `
UPDATE domains.conversation_member
SET cleared_message_id = last_message_id, last_read_message_id = last_message_id,
    unread_count = 0, update_time = CURRENT_TIMESTAMP
WHERE conversation_id = ? AND user_id = ? AND deleted = 0`
)

// MarkRead 见 domain.Repository。
func (r *messageRepoPG) MarkRead(ctx context.Context, conversationID, userID uuid.UUID) error {
	return r.updateMember(ctx, sqlMarkRead, conversationID, userID)
}

// Hide 见 domain.Repository。
func (r *messageRepoPG) Hide(ctx context.Context, conversationID, userID uuid.UUID) error {
	return r.updateMember(ctx, sqlHide, conversationID, userID)
}

// updateMember 执行一条按 (conversation_id, user_id) 定位成员行的 UPDATE；影响 0 行即不是成员。
func (r *messageRepoPG) updateMember(ctx context.Context, sql string, conversationID, userID uuid.UUID) error {
	res := r.db.WithContext(ctx).Exec(sql, conversationID, userID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrConversationNotFound
	}
	return nil
}
