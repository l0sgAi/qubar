// Package http 提供 collect 领域的 HTTP 入站适配器。
package http

import (
	"errors"

	"interestBar/pkg/domains/collect/application"
	"interestBar/pkg/domains/collect/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 collect 相关的 HTTP 请求。
type Handler struct {
	svc application.CollectService
}

// NewHandler 构造 collect Handler。
func NewHandler(svc application.CollectService) *Handler {
	return &Handler{svc: svc}
}

// ToggleCollectRequest 收藏/取消收藏请求。
//
// action 可选："collect" / "uncollect" 为显式期望状态（重试/双击幂等，推荐前端使用）；
// 缺省为切换（兼容旧客户端）。取值校验在 application 层（domain.ErrInvalidAction）。
type ToggleCollectRequest struct {
	PostID uuid.UUID `json:"post_id" binding:"required"`
	Action string    `json:"action,omitempty"` // "collect" / "uncollect" / 空
}

// ToggleCollect POST /collect/toggle
func (h *Handler) ToggleCollect(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req ToggleCollectRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.Toggle(c, userID, application.ToggleInput{PostID: req.PostID, Action: req.Action})
	if err != nil {
		writeCollectError(c, err)
		return
	}
	httputil.Success(c, result)
}

// ListCollectedPostsRequest 「我的收藏」列表请求。
type ListCollectedPostsRequest struct {
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// ListCollectedPosts GET /collect/posts
func (h *Handler) ListCollectedPosts(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req ListCollectedPostsRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.ListCollectedPosts(c, userID, req.Keyword, req.Size, req.SearchAfter)
	if err != nil {
		writeCollectError(c, err)
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

// writeCollectError 把 service 层错误映射到 HTTP 响应。
func writeCollectError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrPostNotFound):
		httputil.NotFound(c, "Post not found")
	case errors.Is(err, domain.ErrInvalidCursor):
		httputil.BadRequest(c, "Invalid search_after parameter")
	case errors.Is(err, domain.ErrInvalidAction):
		httputil.BadRequest(c, "Invalid action")
	default:
		logger.Log.Error("collect service error: " + err.Error())
		httputil.InternalError(c, "Failed to process collect request")
	}
}
