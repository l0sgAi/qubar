// Package http 提供 like 领域的 HTTP 入站适配器。
package http

import (
	"errors"

	"interestBar/pkg/domains/like/application"
	"interestBar/pkg/domains/like/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 like 相关的 HTTP 请求。
type Handler struct {
	svc application.LikeService
}

// NewHandler 构造 like Handler。
func NewHandler(svc application.LikeService) *Handler {
	return &Handler{svc: svc}
}

// ToggleLikeRequest 点赞/取消点赞请求。
//
// action 可选："like" / "unlike" 为显式期望状态（重试/双击幂等，推荐前端使用）；
// 缺省为切换（兼容旧客户端）。取值校验在 application 层（domain.ErrInvalidAction）。
type ToggleLikeRequest struct {
	Type     string    `json:"type" binding:"required,oneof=comment post"` // "comment" 或 "post"
	TargetID uuid.UUID `json:"target_id" binding:"required"`
	Action   string    `json:"action,omitempty"` // "like" / "unlike" / 空
}

// ToggleLike POST /like/toggle
func (h *Handler) ToggleLike(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req ToggleLikeRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.Toggle(c, userID, application.ToggleInput{
		Type:     req.Type,
		TargetID: req.TargetID,
		Action:   req.Action,
	})
	if err != nil {
		writeLikeError(c, err)
		return
	}
	httputil.Success(c, result)
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

// writeLikeError 把 service 层错误映射到 HTTP 响应。
func writeLikeError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrPostNotFound):
		httputil.NotFound(c, "Post not found")
	case errors.Is(err, domain.ErrCommentNotFound):
		httputil.NotFound(c, "Comment not found")
	case errors.Is(err, domain.ErrInvalidTargetType):
		httputil.BadRequest(c, "Invalid target type")
	case errors.Is(err, domain.ErrInvalidAction):
		httputil.BadRequest(c, "Invalid action")
	default:
		logger.Log.Error("like service error: " + err.Error())
		httputil.InternalError(c, "Failed to toggle like")
	}
}
