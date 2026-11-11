// Package http 提供 share 领域的 HTTP 入站适配器。
package http

import (
	"errors"

	"interestBar/pkg/domains/share/application"
	"interestBar/pkg/domains/share/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 share 相关的 HTTP 请求。
type Handler struct {
	svc application.ShareService
}

// NewHandler 构造 share Handler。
func NewHandler(svc application.ShareService) *Handler {
	return &Handler{svc: svc}
}

// SharePostRequest 分享帖子请求。
type SharePostRequest struct {
	PostID uuid.UUID `json:"post_id" binding:"required"`
}

// SharePost POST /post/share
//
// 返回 { share_count, first_share }。重复分享幂等成功（first_share=false，不重复计数）；
// 帖子不存在 / 未发布 / 已删除 / 对当前用户不可见 → 404；分享事件投递失败 → 503（已回滚，可重试）。
func (h *Handler) SharePost(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req SharePostRequest
	if err := c.BindJSON(&req); err != nil || req.PostID == uuid.Nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.SharePost(c, userID, req.PostID)
	if err != nil {
		writeShareError(c, err)
		return
	}
	httputil.Success(c, result)
}

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

// writeShareError 把 service 层错误映射到 HTTP 响应。
func writeShareError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrPostNotFound):
		httputil.NotFound(c, "Post not found")
	case errors.Is(err, domain.ErrEventPublishFailed):
		logger.Log.Warn("share event publish failed: " + err.Error())
		httputil.ServiceUnavailable(c, "Share is temporarily unavailable, please retry")
	default:
		logger.Log.Error("share service error: " + err.Error())
		httputil.InternalError(c, "Failed to process share request")
	}
}
