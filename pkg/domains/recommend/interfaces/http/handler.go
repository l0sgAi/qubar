// Package http 提供 recommend 领域的 HTTP 入站适配器。
package http

import (
	"encoding/json"
	"errors"

	"interestBar/pkg/domains/recommend/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 recommend 相关的 HTTP 请求。
type Handler struct {
	svc application.RecommendService
}

// NewHandler 构造 recommend Handler。
func NewHandler(svc application.RecommendService) *Handler {
	return &Handler{svc: svc}
}

// GetHomeFeedRequest 首页信息流请求。
type GetHomeFeedRequest struct {
	Tab         string `query:"tab"`          // recommend | hot | latest | following
	Size        int    `query:"size"`         // 每页数量，默认 20
	Offset      int    `query:"offset"`       // recommend：候选池偏移
	PoolToken   string `query:"pool_token"`   // recommend：上次返回的池版本 token
	SearchAfter string `query:"search_after"` // hot/latest/following：ES search_after 游标
}

// GetHomeFeed GET /post/home?tab=recommend
//
// 组级可选登录：hot/latest tab 访客可读（全局 ES 流，service BatchCheck 对 uuid.Nil 是
// best-effort，IsLiked/IsCollected 自然 false）；recommend/following tab 硬依赖 userID
// （用户行为池 / 已加圈子列表），匿名访问由 service 返回 ErrLoginRequired → handler 映射 401。
func (h *Handler) GetHomeFeed(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	var req GetHomeFeedRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	searchAfter, ok := parseSearchAfter(c, req.SearchAfter)
	if !ok {
		return
	}

	result, err := h.svc.GetHomeFeed(c, userID, req.Tab, req.Size, req.Offset, req.PoolToken, searchAfter)
	if err != nil {
		if errors.Is(err, application.ErrTabNotSupported) {
			httputil.BadRequest(c, "Unsupported home feed tab")
			return
		}
		if errors.Is(err, application.ErrLoginRequired) {
			httputil.Unauthorized(c, "This feed tab requires login")
			return
		}
		logger.Log.Error("home feed error: " + err.Error())
		httputil.SearchError(c, err, "Failed to get home feed")
		return
	}
	httputil.Success(c, result)
}

// requireUserIDAllowAnon 尝试返回 userID，但允许匿名（未登录返回 uuid.Nil, true）。
//
// 用于首页信息流这类访客可读接口：hot/latest tab 匿名可读；recommend/following tab
// 匿名时由 service 返回 ErrLoginRequired，handler 映射 401。不在此处提前拦截。
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

// parseSearchAfter 解析 search_after 游标（JSON 数组）。空串=首页；非法→400。
func parseSearchAfter(c appctx.AppContext, s string) ([]interface{}, bool) {
	if s == "" {
		return nil, true
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		httputil.BadRequest(c, "Invalid search_after parameter")
		return nil, false
	}
	return arr, true
}
