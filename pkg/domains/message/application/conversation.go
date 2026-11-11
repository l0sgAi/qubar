package application

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ConversationService 会话列表、标记已读、删除会话。
type ConversationService interface {
	// ListConversations 返回 userID 的会话列表，按最后一条消息时间倒序，游标分页（cursor 为空表示第一页）。
	// 被删除（隐藏）且之后没有新消息的会话不在列表里。非法或不属于该列表的游标返回 domain.ErrInvalidCursor。
	ListConversations(ctx context.Context, userID uuid.UUID, cursor string, size int) (*ConversationListVO, error)
	// MarkRead 把会话内 userID 的未读清零（记录最后已读位置）。不是会话成员返回 domain.ErrConversationNotFound。
	MarkRead(ctx context.Context, userID, conversationID uuid.UUID) error
	// DeleteConversation 删除会话，只对 userID 自己隐藏；对方再发消息时会话重新出现，只显示删除之后的消息。
	// 不是会话成员返回 domain.ErrConversationNotFound。
	DeleteConversation(ctx context.Context, userID, conversationID uuid.UUID) error
}

// LastMessageVO 会话列表里的最后一条消息摘要。
type LastMessageVO struct {
	ID       string `json:"id"`
	SenderID string `json:"sender_id"`
	MsgType  int16  `json:"msg_type"`
	// Content 正文的前 PreviewChars 个字符。
	Content    string    `json:"content"`
	CreateTime time.Time `json:"create_time"`
}

// ConversationItemVO 会话列表项。
type ConversationItemVO struct {
	ConversationID string `json:"conversation_id"`
	Type           int16  `json:"type"`
	PeerID         string `json:"peer_id"`
	// Peer 对方简要信息；对方已禁用 / 已删除时为 null（会话与历史消息仍然保留）。
	Peer        *UserBrief    `json:"peer"`
	LastMessage LastMessageVO `json:"last_message"`
	UnreadCount int           `json:"unread_count"`
}

// ConversationListVO 一页会话。HasMore 为 false 时 NextCursor 为空串。
type ConversationListVO struct {
	Items      []ConversationItemVO `json:"items"`
	HasMore    bool                 `json:"has_more"`
	NextCursor string               `json:"next_cursor"`
}

func (s *messageServiceImpl) ListConversations(ctx context.Context, userID uuid.UUID, cursor string, size int) (*ConversationListVO, error) {
	size = normalizePageSize(size)
	after, err := parseCursor(cursor, listConversations, userID)
	if err != nil {
		return nil, err
	}
	// 多取一条用来判断后面还有没有。
	entries, err := s.repo.ListConversations(ctx, userID, after, size+1, PreviewChars)
	if err != nil {
		return nil, err
	}
	vo := &ConversationListVO{Items: make([]ConversationItemVO, 0, min(len(entries), size))}
	if len(entries) > size {
		vo.HasMore = true
		entries = entries[:size]
	}
	if len(entries) == 0 {
		return vo, nil
	}
	peerIDs := make([]uuid.UUID, 0, len(entries))
	for i := range entries {
		if entries[i].PeerID != uuid.Nil {
			peerIDs = append(peerIDs, entries[i].PeerID)
		}
	}
	briefs, err := s.users.GetBriefs(ctx, peerIDs)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		e := &entries[i]
		item := ConversationItemVO{
			ConversationID: e.ConversationID.String(),
			Type:           e.Type,
			LastMessage: LastMessageVO{
				ID:         e.LastMessageID.String(),
				SenderID:   e.LastSenderID.String(),
				MsgType:    e.LastMsgType,
				Content:    e.LastContent,
				CreateTime: e.LastMessageTime,
			},
			UnreadCount: e.UnreadCount,
		}
		if e.PeerID != uuid.Nil {
			item.PeerID = e.PeerID.String()
			if brief, ok := briefs[e.PeerID]; ok {
				item.Peer = &brief
			}
		}
		vo.Items = append(vo.Items, item)
	}
	if vo.HasMore {
		vo.NextCursor = encodeCursor(listConversations, userID, entries[len(entries)-1].LastMessageID)
	}
	return vo, nil
}

func (s *messageServiceImpl) MarkRead(ctx context.Context, userID, conversationID uuid.UUID) error {
	return s.repo.MarkRead(ctx, conversationID, userID)
}

func (s *messageServiceImpl) DeleteConversation(ctx context.Context, userID, conversationID uuid.UUID) error {
	return s.repo.Hide(ctx, conversationID, userID)
}
