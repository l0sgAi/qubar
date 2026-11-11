package application

import (
	"context"

	"github.com/google/uuid"
)

// HistoryService 消息历史。
type HistoryService interface {
	// ListMessages 返回会话里的消息，新 → 旧（按消息 ID 倒序），游标向更旧的消息翻页（cursor 为空表示最新一页）。
	// 只有会话成员可读，否则返回 domain.ErrConversationNotFound；删除过会话的成员只能看到删除之后的消息。
	// 非法或不属于该会话的游标返回 domain.ErrInvalidCursor。
	ListMessages(ctx context.Context, userID, conversationID uuid.UUID, cursor string, size int) (*MessageListVO, error)
}

// MessageListVO 一页消息（新 → 旧）。HasMore 为 false 时 NextCursor 为空串。
type MessageListVO struct {
	Items      []MessageVO `json:"items"`
	HasMore    bool        `json:"has_more"`
	NextCursor string      `json:"next_cursor"`
	// LastReadMessageID 我在该会话的最后已读位置（空串 = 从未读过），前端可据此画「以下为新消息」分隔线。
	LastReadMessageID string `json:"last_read_message_id"`
}

func (s *messageServiceImpl) ListMessages(ctx context.Context, userID, conversationID uuid.UUID, cursor string, size int) (*MessageListVO, error) {
	size = normalizePageSize(size)
	before, err := parseCursor(cursor, listMessages, conversationID)
	if err != nil {
		return nil, err
	}
	member, err := s.repo.GetMember(ctx, conversationID, userID)
	if err != nil {
		return nil, err
	}
	msgs, err := s.repo.ListMessages(ctx, conversationID, member.ClearedMessageID, before, size+1)
	if err != nil {
		return nil, err
	}
	vo := &MessageListVO{}
	if member.LastReadMessageID != nil {
		vo.LastReadMessageID = member.LastReadMessageID.String()
	}
	if len(msgs) > size {
		vo.HasMore = true
		msgs = msgs[:size]
	}
	vo.Items = make([]MessageVO, len(msgs))
	for i := range msgs {
		vo.Items[i] = toMessageVO(&msgs[i])
	}
	if vo.HasMore {
		vo.NextCursor = encodeCursor(listMessages, conversationID, msgs[len(msgs)-1].ID)
	}
	return vo, nil
}
