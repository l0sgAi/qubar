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

// 发送：会话行 upsert →（启用时）未关注发送限制检查 → 写消息 → 双方成员行 upsert，同一事务。
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

	// 未关注发送限制（#239）：在会话行锁之后读发送方已发条数与「接收方是否在会话里发过消息」。
	// READ COMMITTED 下每条语句取新快照，锁之后的这条读能看到之前持锁事务已提交的 sent_count。
	sqlSendLimitState = `
SELECT
  COALESCE((SELECT sent_count FROM domains.conversation_member
            WHERE conversation_id = @conv AND user_id = @sender), 0) AS sent_count,
  EXISTS (SELECT 1 FROM domains.conversation_member
          WHERE conversation_id = @conv AND user_id = @receiver AND first_sent_time IS NOT NULL) AS peer_replied`
)

// sendLimitState 发送方在会话里的已发条数，以及接收方是否在会话里发过消息。
type sendLimitState struct {
	SentCount   int
	PeerReplied bool
}

// readSendLimitState 在会话行锁内读取 sendLimitState（sqlSendLimitState）。
func readSendLimitState(tx *gorm.DB, conversationID, senderID, receiverID uuid.UUID) (sendLimitState, error) {
	var state sendLimitState
	err := tx.Raw(sqlSendLimitState, map[string]any{
		"conv": conversationID, "sender": senderID, "receiver": receiverID,
	}).Scan(&state).Error
	return state, err
}

// checkSendLimit 在会话行锁内检查未关注发送限制（limit > 0 时调用）。
// 对方发过消息返回 nil（永久解除）；已发满 limit 条返回 ErrSendLimitReached；否则返回本条发出后的剩余条数。
func checkSendLimit(tx *gorm.DB, conversationID, senderID, receiverID uuid.UUID, limit int) (*int, error) {
	state, err := readSendLimitState(tx, conversationID, senderID, receiverID)
	if err != nil {
		return nil, err
	}
	if state.PeerReplied {
		return nil, nil
	}
	if state.SentCount >= limit {
		return nil, domain.ErrSendLimitReached
	}
	remaining := limit - state.SentCount - 1
	return &remaining, nil
}

// SendDirect 见 domain.Repository。
func (r *messageRepoPG) SendDirect(
	ctx context.Context, senderID, receiverID uuid.UUID, msgType int16, content string, limit int, clientMsgID *string,
) (*domain.SendResult, error) {
	low, high := domain.OrderPair(senderID, receiverID)
	msg := &domain.Message{SenderID: senderID, MsgType: msgType, Content: content, ClientMsgID: clientMsgID}
	var remaining *int
	var replayed *domain.Message // 命中发送幂等时的原消息（#248）
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
		// 发送幂等（#248）：在会话行锁之后、发送限制之前查重放——已达上限时重试也返回原消息而不是 ErrSendLimitReached。
		if clientMsgID != nil {
			orig, err := findByClientMsgID(tx, senderID, *clientMsgID)
			if err != nil {
				return err
			}
			if orig != nil {
				replayed = orig
				remaining, err = replayRemaining(tx, conv.ID, senderID, receiverID, limit)
				return err
			}
		}
		if limit > 0 {
			var err error
			if remaining, err = checkSendLimit(tx, conv.ID, senderID, receiverID, limit); err != nil {
				return err
			}
		}
		// 在会话行锁内生成消息 ID（见上方说明）。
		msg.ID = sharedomain.NewID()
		if clientMsgID != nil {
			orig, err := insertMessageIdempotent(tx, msg, *clientMsgID)
			if err != nil || orig != nil {
				replayed = orig
				return err
			}
		} else {
			var created struct{ CreateTime time.Time }
			if err := tx.Raw(sqlInsertMessage,
				msg.ID, msg.ConversationID, senderID, msgType, content).Scan(&created).Error; err != nil {
				return err
			}
			msg.CreateTime = created.CreateTime
		}
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
	if replayed != nil {
		return &domain.SendResult{Message: replayed, Remaining: remaining, Replayed: true}, nil
	}
	msg.UpdateTime = msg.CreateTime
	return &domain.SendResult{Message: msg, Remaining: remaining}, nil
}

// 发送幂等（#248）：唯一部分索引 uk_message_sender_client_msg (sender_id, client_msg_id) WHERE client_msg_id IS NOT NULL。
//
// 同一发送人同一 client_msg_id 的请求大多发往同一会话，被会话行锁串行化：后到的请求拿到锁时前一个已提交，
// 锁之后的 findByClientMsgID（READ COMMITTED 每条语句新快照）就能看到原消息。唯一索引兜底锁覆盖不到的情况
// （同一 client_msg_id 发往不同接收方，两次请求锁的是不同会话行）：插入用 ON CONFLICT DO NOTHING，
// 冲突时等对方事务提交后不插入、不报错，再按同一键查回原消息，不会变成 500。
const (
	sqlInsertMessageIdempotent = `
INSERT INTO domains.message (id, conversation_id, sender_id, msg_type, content, client_msg_id)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (sender_id, client_msg_id) WHERE client_msg_id IS NOT NULL DO NOTHING
RETURNING create_time`

	sqlFindByClientMsgID = `
SELECT id, conversation_id, sender_id, msg_type, content, client_msg_id, create_time, update_time, deleted
FROM domains.message
WHERE sender_id = ? AND client_msg_id = ?`
)

// findByClientMsgID 按 (senderID, clientMsgID) 查原消息（走 uk_message_sender_client_msg）；没有返回 nil。
func findByClientMsgID(tx *gorm.DB, senderID uuid.UUID, clientMsgID string) (*domain.Message, error) {
	var rows []domain.Message
	if err := tx.Raw(sqlFindByClientMsgID, senderID, clientMsgID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// insertMessageIdempotent 带 client_msg_id 插入消息。插入成功回填 msg.CreateTime 并返回 nil；
// 撞唯一索引（并发的同键请求已先提交）时不插入，查回并返回原消息。
func insertMessageIdempotent(tx *gorm.DB, msg *domain.Message, clientMsgID string) (*domain.Message, error) {
	var created []struct{ CreateTime time.Time }
	if err := tx.Raw(sqlInsertMessageIdempotent,
		msg.ID, msg.ConversationID, msg.SenderID, msg.MsgType, msg.Content, clientMsgID).Scan(&created).Error; err != nil {
		return nil, err
	}
	if len(created) > 0 {
		msg.CreateTime = created[0].CreateTime
		return nil, nil
	}
	orig, err := findByClientMsgID(tx, msg.SenderID, clientMsgID)
	if err != nil {
		return nil, err
	}
	if orig == nil {
		// DO NOTHING 只在冲突行已提交时发生，此时一定查得到；查不到说明数据被并发删除，按异常处理。
		return nil, errors.New("message: client_msg_id conflict but original message not found")
	}
	return orig, nil
}

// replayRemaining 重放时的剩余条数（#239 口径，#248 决定）：按会话当前已发条数重新计算 limit − sent_count（不再减本条，最小 0）；
// 不受限制（limit ≤ 0，或对方在会话里发过消息）时为 nil。与发送接口「本条发出后还能再发几条」一致：重放的那条已经计入 sent_count。
func replayRemaining(tx *gorm.DB, conversationID, senderID, receiverID uuid.UUID, limit int) (*int, error) {
	if limit <= 0 {
		return nil, nil
	}
	state, err := readSendLimitState(tx, conversationID, senderID, receiverID)
	if err != nil || state.PeerReplied {
		return nil, err
	}
	remaining := max(limit-state.SentCount, 0)
	return &remaining, nil
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

// 按对方查会话：按 (user_low, user_high) 命中 uk_conversation_pair，再按 uk_conversation_member 取我的成员行；
// 最后一条消息用 LEFT JOIN，且只在它位于隐藏截止位置之后时才连上（与会话列表的可见条件一致），
// 删除后没有新消息时 msg.* 为 NULL。只读。
const sqlGetDirectConversation = `
SELECT m.conversation_id, c.type, m.peer_id, m.unread_count,
       msg.id AS last_message_id, msg.sender_id AS last_sender_id, msg.msg_type AS last_msg_type,
       left(msg.content, @preview) AS last_content, msg.create_time AS last_message_time
FROM domains.conversation c
JOIN domains.conversation_member m ON m.conversation_id = c.id AND m.user_id = @user AND m.deleted = 0
LEFT JOIN domains.message msg ON msg.id = m.last_message_id
  AND (m.cleared_message_id IS NULL OR m.last_message_id > m.cleared_message_id)
WHERE c.user_low = @low AND c.user_high = @high`

// directConversationRow 按对方查会话的扫描目标（最后一条消息不可见时 last_* 为 NULL）。
type directConversationRow struct {
	ConversationID  uuid.UUID
	Type            int16
	PeerID          *uuid.UUID
	UnreadCount     int
	LastMessageID   *uuid.UUID
	LastSenderID    *uuid.UUID
	LastMsgType     *int16
	LastContent     *string
	LastMessageTime *time.Time
}

// GetDirectConversation 见 domain.Repository。
func (r *messageRepoPG) GetDirectConversation(
	ctx context.Context, userID, peerID uuid.UUID, previewChars int,
) (*domain.ConversationEntry, error) {
	low, high := domain.OrderPair(userID, peerID)
	args := map[string]any{"user": userID, "low": low, "high": high, "preview": previewChars}
	var rows []directConversationRow
	if err := r.db.WithContext(ctx).Raw(sqlGetDirectConversation, args).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrConversationNotFound
	}
	row := rows[0]
	e := &domain.ConversationEntry{ConversationID: row.ConversationID, Type: row.Type, UnreadCount: row.UnreadCount}
	if row.PeerID != nil {
		e.PeerID = *row.PeerID
	}
	if row.LastMessageID != nil {
		e.LastMessageID = *row.LastMessageID
		e.LastSenderID = derefOr(row.LastSenderID, uuid.Nil)
		e.LastMsgType = derefOr(row.LastMsgType, 0)
		e.LastContent = derefOr(row.LastContent, "")
		e.LastMessageTime = derefOr(row.LastMessageTime, time.Time{})
	}
	return e, nil
}

// derefOr 解引用可空列，NULL 时返回 def。
func derefOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
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
