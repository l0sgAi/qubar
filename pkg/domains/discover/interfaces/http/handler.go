// Package http 提供 discover 领域的 HTTP 入站适配器。
package http

import (
	"github.com/google/uuid"

	"interestBar/pkg/domains/discover/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
)

// Handler 处理 discover 相关的 HTTP 请求。
type Handler struct {
	svc application.DiscoverService
}

// NewHandler 构造 discover Handler。
func NewHandler(svc application.DiscoverService) *Handler {
	return &Handler{svc: svc}
}

// GetDiscoverRequest 发现页请求（query 绑定，使用 query tag）。
type GetDiscoverRequest struct {
	Section   string `query:"section"`    // all | posts | circles（默认 all）
	Size      int    `query:"size"`       // 每分区条数，默认 20，上限 50
	Offset    int    `query:"offset"`     // 单分区翻页偏移（section=all 时忽略）
	PoolToken string `query:"pool_token"` // 候选池版本 token；不匹配→重建 + 回 offset=0
}

// GetDiscover GET /discover?section=all&size=20&offset=0&pool_token=...
//
// 允许匿名访问（requireUserIDAllowAnon）：登录→反气泡个性化，匿名→纯随机退化。
func (h *Handler) GetDiscover(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c) // 匿名返回 uuid.Nil

	var req GetDiscoverRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	// 匿名：userID==uuid.Nil → service 收到 nil（纯随机退化）。
	var uidPtr *uuid.UUID
	if userID != uuid.Nil {
		uidPtr = &userID
	}

	result, err := h.svc.GetDiscover(c, uidPtr, req.Section, req.Size, req.Offset, req.PoolToken)
	if err != nil {
		writeDiscoverError(c, err)
		return
	}
	httputil.Success(c, result)
}

// requireUserIDAllowAnon 尝试返回 userID，但允许匿名（未登录返回 uuid.Nil, true）。
//
// 发现页允许匿名访问（常为新用户落地页）：登录→反气泡个性化，匿名→纯随机。
// 注意：路由层仍挂 RequireLogin 中间件（保持与 composition.RequireLogin 一致的访问控制），
// 这里只是容忍鉴权失败写入的空 loginID（兼容 token 异常等极端情况）。
func requireUserIDAllowAnon(c appctx.AppContext) (uuid.UUID, bool) {
	loginID, ok := c.LoginID()
	if !ok || loginID == "" {
		return uuid.Nil, true
	}
	userID, err := uuid.Parse(loginID)
	if err != nil {
		return uuid.Nil, true
	}
	return userID, true
}

// writeDiscoverError 把 service 层错误映射到 HTTP 响应。
func writeDiscoverError(c appctx.AppContext, err error) {
	switch {
	case application.IsSectionErr(err):
		httputil.BadRequest(c, err.Error())
	default:
		logger.Log.Error("discover error: " + err.Error())
		httputil.SearchError(c, err, "Failed to get discover board")
	}
}
