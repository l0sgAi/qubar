package application

import (
	"context"
	"time"
	"unicode/utf8"

	"interestBar/pkg/domains/message/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// UnreadService 私信未读总数与实时推送（#241，设计见 docs/message-design.md 第十节）。
type UnreadService interface {
	// GetUnreadTotal 返回 userID 的私信未读总数（各会话我的未读之和）。Redis 缓存优先，miss 回源 DB 并回填。
	// 也是 SSE dm-unread 通道的计数读取器（推送值与本接口同源）。
	GetUnreadTotal(ctx context.Context, userID uuid.UUID) (int64, error)
	// SetUnreadCache 注入未读总数缓存（composition 装配时调用）。未注入时每次读都查 DB。
	SetUnreadCache(cache domain.UnreadCache)
	// SetUnreadNotifier 注入实时推送端口（composition 装配时调用）。未注入时不推送。
	SetUnreadNotifier(n UnreadNotifier)
}

// UnreadNotifier 跨域端口：把私信的未读变化与新消息推给用户已连接的 SSE（/notice/stream）。
// 由 composition 桥接到 notice 的 StreamHub；message 域不 import notice。
// 实现必须非阻塞（只做进程内投递），调用发生在写事务提交之后。
type UnreadNotifier interface {
	// UnreadChanged userID 的私信未读总数可能变了（hub 合并窗口内读全量、去重后推 dm-unread）。
	UnreadChanged(userID uuid.UUID)
	// NewMessage 给接收方推一条新消息摘要（dm-message，即时、不合并）。
	NewMessage(receiverID uuid.UUID, ev NewMessageEvent)
}

// NewMessageEvent 新消息摘要（SSE dm-message 的 data），只推给接收方。
// 字段与 MessageVO 同名同义，另加 sender（发送人简要信息）与 truncated（正文是否被截断）。
type NewMessageEvent struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	SenderID       string `json:"sender_id"`
	// Sender 发送人简要信息；读取失败时为 null（前端用 sender_id 兜底）。
	Sender  *UserBrief `json:"sender"`
	MsgType int16      `json:"msg_type"`
	// Content 正文的前 PushPreviewChars 个字符。
	Content string `json:"content"`
	// Truncated 正文超过 PushPreviewChars 被截断时为 true，前端需拉消息历史拿全文。
	Truncated  bool      `json:"truncated"`
	CreateTime time.Time `json:"create_time"`
}

// PushPreviewChars 新消息摘要的正文字符数（与会话列表摘要 PreviewChars 相同）。
const PushPreviewChars = PreviewChars

func (s *messageServiceImpl) SetUnreadCache(cache domain.UnreadCache) {
	s.unreadCache = cache
}

func (s *messageServiceImpl) SetUnreadNotifier(n UnreadNotifier) {
	s.notifier = n
}

func (s *messageServiceImpl) GetUnreadTotal(ctx context.Context, userID uuid.UUID) (int64, error) {
	if s.unreadCache != nil {
		if count, ok, err := s.unreadCache.Get(ctx, userID); err != nil {
			logger.Log.Error("Failed to get message unread total from cache: " + err.Error())
		} else if ok {
			return count, nil
		}
	}
	total, err := s.repo.SumUnread(ctx, userID)
	if err != nil {
		return 0, err
	}
	if s.unreadCache != nil {
		if err := s.unreadCache.Set(ctx, userID, total); err != nil {
			logger.Log.Error("Failed to backfill message unread total: " + err.Error())
		}
	}
	return total, nil
}

// afterSend 发送事务提交后：接收方未读缓存 +1、推未读变化与新消息摘要。尽力而为，失败只记日志，不影响发送结果。
func (s *messageServiceImpl) afterSend(ctx context.Context, msg *domain.Message, receiverID uuid.UUID) {
	if s.unreadCache != nil {
		if err := s.unreadCache.IncrIfExists(ctx, receiverID, 1); err != nil {
			// 缓存可能少计 1：删掉让下一次读回源（删也失败就等 TTL 校正）。
			logger.Log.Error("Failed to incr message unread total: " + err.Error())
			_ = s.unreadCache.Invalidate(ctx, receiverID)
		}
	}
	if s.notifier == nil {
		return
	}
	s.notifier.UnreadChanged(receiverID)
	s.notifier.NewMessage(receiverID, s.newMessageEvent(ctx, msg))
}

// newMessageEvent 组装新消息摘要；发送人信息读取失败时 Sender 为 nil。
func (s *messageServiceImpl) newMessageEvent(ctx context.Context, msg *domain.Message) NewMessageEvent {
	content, truncated := truncateRunes(msg.Content, PushPreviewChars)
	ev := NewMessageEvent{
		ID:             msg.ID.String(),
		ConversationID: msg.ConversationID.String(),
		SenderID:       msg.SenderID.String(),
		MsgType:        msg.MsgType,
		Content:        content,
		Truncated:      truncated,
		CreateTime:     msg.CreateTime,
	}
	briefs, err := s.users.GetBriefs(ctx, []uuid.UUID{msg.SenderID})
	if err != nil {
		logger.Log.Error("Failed to get sender brief for message push: " + err.Error())
		return ev
	}
	if b, ok := briefs[msg.SenderID]; ok {
		ev.Sender = &b
	}
	return ev
}

// afterUnreadCleared 标记已读 / 删除会话成功后：失效 userID 的未读缓存并推未读变化。尽力而为。
func (s *messageServiceImpl) afterUnreadCleared(ctx context.Context, userID uuid.UUID) {
	if s.unreadCache != nil {
		if err := s.unreadCache.Invalidate(ctx, userID); err != nil {
			logger.Log.Error("Failed to invalidate message unread total: " + err.Error())
		}
	}
	if s.notifier != nil {
		s.notifier.UnreadChanged(userID)
	}
}

// truncateRunes 截取前 n 个字符（按 Unicode 字符计，与 PG left() 一致）；超出时 truncated=true。
func truncateRunes(s string, n int) (string, bool) {
	if utf8.RuneCountInString(s) <= n {
		return s, false
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos], true
		}
		i++
	}
	return s, false
}
