// Package http 提供 trending 领域的 HTTP 入站适配器。
package http

import (
	"github.com/google/uuid"

	"interestBar/pkg/domains/trending/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
)

// Handler 处理 trending 相关的 HTTP 请求。
type Handler struct {
	svc application.TrendingService
}

// NewHandler 构造 trending Handler。
func NewHandler(svc application.TrendingService) *Handler {
	return &Handler{svc: svc}
}

// GetTrendingRequest 热点看板请求（query 绑定，使用 query tag）。
type GetTrendingRequest struct {
	Window  string `query:"window"`  // 24h | 7d（默认 24h）
	Section string `query:"section"` // all | posts | circles | users（默认 all）
	Size    int    `query:"size"`    // 每板块条数，默认 20，上限 50
	Offset  int    `query:"offset"`  // 单板块翻页偏移（section=all 时忽略）
}

// GetTrending GET /trending?window=24h&section=all&size=20
//
// 访客可读：登录时 service 回填帖子的 is_liked/is_collected；匿名（uuid.Nil）时
// fillPosts 的 BatchCheck 是 best-effort，IsLiked/IsCollected 自然 false。
func (h *Handler) GetTrending(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	var req GetTrendingRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.GetTrending(c, userID, req.Window, req.Section, req.Size, req.Offset)
	if err != nil {
		logger.Log.Error("trending error: " + err.Error())
		httputil.SearchError(c, err, "Failed to get trending board")
		return
	}
	httputil.Success(c, result)
}

// requireUserIDAllowAnon 尝试返回 userID，但允许匿名（未登录返回 uuid.Nil, true）。
//
// 用于热点看板这类访客可读接口：登录时回填交互态；匿名时降级为 false。
// 不写 401——访客访问是合法路径。
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
