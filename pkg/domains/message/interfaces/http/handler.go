// Package http 提供 message（私信）领域的 HTTP 入站适配器。
package http

import (
	"errors"

	"interestBar/pkg/domains/message/application"
	"interestBar/pkg/domains/message/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理私信相关的 HTTP 请求。
type Handler struct {
	svc application.MessageService
}

// NewHandler 构造私信 Handler。
func NewHandler(svc application.MessageService) *Handler {
	return &Handler{svc: svc}
}

// SendRequest POST /message/send 的请求体。
type SendRequest struct {
	// ReceiverID 接收方用户 ID。
	ReceiverID string `json:"receiver_id"`
	// Content 正文：1–2000 个字符，不能只有空白。
	Content string `json:"content"`
}

// ListRequest 列表接口的 query 参数。
type ListRequest struct {
	// Cursor 上一页返回的 next_cursor，原样回传；不传表示第一页。
	Cursor string `query:"cursor"`
	// Size 页大小：不传或 ≤0 取 20，超过 50 按 50。
	Size int `query:"size"`
}

// Send POST /message/send —— 发送文本私信，首次发送自动建会话。
func (h *Handler) Send(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req SendRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request body")
		return
	}
	receiverID, err := uuid.Parse(req.ReceiverID)
	if err != nil || receiverID == uuid.Nil {
		httputil.BadRequest(c, "Invalid receiver_id")
		return
	}
	vo, err := h.svc.Send(c, userID, receiverID, req.Content)
	if err != nil {
		writeMessageError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// ListConversations GET /message/conversations —— 我的会话列表（按最后消息时间倒序）。
func (h *Handler) ListConversations(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	req, ok := bindListRequest(c)
	if !ok {
		return
	}
	vo, err := h.svc.ListConversations(c, userID, req.Cursor, req.Size)
	if err != nil {
		writeMessageError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// GetConversationWithUser GET /message/conversations/with/:user_id —— 我与某个用户的单聊会话（没有时 conversation 为 null，不创建）。
func (h *Handler) GetConversationWithUser(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	peerID, err := uuid.Parse(c.Param("user_id"))
	if err != nil || peerID == uuid.Nil {
		httputil.BadRequest(c, "Invalid user id")
		return
	}
	vo, err := h.svc.GetConversationWithUser(c, userID, peerID)
	if err != nil {
		writeMessageError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// ListMessages GET /message/conversations/:conversation_id/messages —— 消息历史（新 → 旧，向旧消息翻页）。
func (h *Handler) ListMessages(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	conversationID, ok := conversationIDParam(c)
	if !ok {
		return
	}
	req, ok := bindListRequest(c)
	if !ok {
		return
	}
	vo, err := h.svc.ListMessages(c, userID, conversationID, req.Cursor, req.Size)
	if err != nil {
		writeMessageError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// MarkRead POST /message/conversations/:conversation_id/read —— 会话内我的未读清零。
func (h *Handler) MarkRead(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	conversationID, ok := conversationIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.MarkRead(c, userID, conversationID); err != nil {
		writeMessageError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// DeleteConversation DELETE /message/conversations/:conversation_id —— 删除会话（只对自己隐藏）。
func (h *Handler) DeleteConversation(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	conversationID, ok := conversationIDParam(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteConversation(c, userID, conversationID); err != nil {
		writeMessageError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// ===== 辅助函数 =====

func requireUserID(c appctx.AppContext) (uuid.UUID, bool) {
	loginID, ok := c.LoginID()
	if !ok || loginID == "" {
		httputil.Unauthorized(c, "Token not found")
		return uuid.Nil, false
	}
	userID, err := uuid.Parse(loginID)
	if err != nil {
		httputil.BadRequest(c, "Invalid user ID")
		return uuid.Nil, false
	}
	return userID, true
}

// conversationIDParam 解析路径参数 :conversation_id；非法时写 400。
func conversationIDParam(c appctx.AppContext) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("conversation_id"))
	if err != nil || id == uuid.Nil {
		httputil.BadRequest(c, "Invalid conversation id")
		return uuid.Nil, false
	}
	return id, true
}

func bindListRequest(c appctx.AppContext) (ListRequest, bool) {
	var req ListRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return req, false
	}
	return req, true
}

// writeMessageError 把 service 层错误映射到 HTTP 响应。
func writeMessageError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrCannotMessageSelf):
		httputil.BadRequest(c, "Cannot send message to yourself")
	case errors.Is(err, domain.ErrConversationWithSelf):
		httputil.BadRequest(c, "Cannot have a conversation with yourself")
	case errors.Is(err, domain.ErrEmptyContent):
		httputil.BadRequest(c, "Message content is empty")
	case errors.Is(err, domain.ErrContentTooLong):
		// message 带上限值，如 "message content is too long: at most 2000 characters"
		httputil.BadRequest(c, err.Error())
	case errors.Is(err, domain.ErrInvalidCursor):
		httputil.BadRequest(c, "Invalid cursor")
	case errors.Is(err, domain.ErrRecipientNotFound):
		httputil.NotFound(c, "No such user")
	case errors.Is(err, domain.ErrRecipientUnavailable):
		httputil.Forbidden(c, "This user cannot receive private messages")
	case errors.Is(err, domain.ErrConversationNotFound):
		httputil.NotFound(c, "No such conversation")
	default:
		logger.Log.Error("message service error: " + err.Error())
		httputil.InternalError(c, "Failed to process message request")
	}
}
