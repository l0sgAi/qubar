// Package http 提供 follow 领域的 HTTP 入站适配器。
package http

import (
	"errors"

	"interestBar/pkg/domains/follow/application"
	"interestBar/pkg/domains/follow/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 follow 相关的 HTTP 请求。
type Handler struct {
	svc application.FollowService
}

// NewHandler 构造 follow Handler。
func NewHandler(svc application.FollowService) *Handler {
	return &Handler{svc: svc}
}

// Follow POST /follow/:user_id —— 关注（幂等）。
func (h *Handler) Follow(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	targetID, ok := targetUserID(c)
	if !ok {
		return
	}
	vo, err := h.svc.Follow(c, userID, targetID)
	if err != nil {
		writeFollowError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// Unfollow DELETE /follow/:user_id —— 取关（幂等）。
func (h *Handler) Unfollow(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	targetID, ok := targetUserID(c)
	if !ok {
		return
	}
	vo, err := h.svc.Unfollow(c, userID, targetID)
	if err != nil {
		writeFollowError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// GetStats GET /follow/stats/:user_id —— 关注数 / 粉丝数 / 关系状态（访客可读）。
func (h *Handler) GetStats(c appctx.AppContext) {
	viewerID := optionalUserID(c)
	targetID, ok := targetUserID(c)
	if !ok {
		return
	}
	vo, err := h.svc.GetStats(c, viewerID, targetID)
	if err != nil {
		writeFollowError(c, err)
		return
	}
	httputil.Success(c, vo)
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

// optionalUserID 返回登录用户 ID；访客（或 loginID 无法解析）返回 uuid.Nil，不写 401。
func optionalUserID(c appctx.AppContext) uuid.UUID {
	loginID, ok := c.LoginID()
	if !ok || loginID == "" {
		return uuid.Nil
	}
	userID, err := uuid.Parse(loginID)
	if err != nil {
		return uuid.Nil
	}
	return userID
}

// targetUserID 解析路径参数 :user_id；非法时写 400。
func targetUserID(c appctx.AppContext) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("user_id"))
	if err != nil || id == uuid.Nil {
		httputil.BadRequest(c, "Invalid user id")
		return uuid.Nil, false
	}
	return id, true
}

// writeFollowError 把 service 层错误映射到 HTTP 响应。
func writeFollowError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrCannotFollowSelf):
		httputil.BadRequest(c, "Cannot follow yourself")
	case errors.Is(err, domain.ErrUserNotFound):
		httputil.NotFound(c, "No such user")
	case errors.Is(err, domain.ErrFollowLimitReached):
		// message 带上限值，如 "following limit reached: you can follow at most 2000 users"
		httputil.BadRequest(c, err.Error())
	default:
		logger.Log.Error("follow service error: " + err.Error())
		httputil.InternalError(c, "Failed to process follow request")
	}
}
