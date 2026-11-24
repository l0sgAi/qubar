package http

import (
	"interestBar/pkg/domains/user/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
	"interestBar/pkg/shared/routing"
)

// SettingHandler 处理用户设置（个人中心开关类设置）的 HTTP 请求。只读写当前登录用户自己的设置。
type SettingHandler struct {
	svc application.SettingService
}

// NewSettingHandler 构造 SettingHandler。
func NewSettingHandler(svc application.SettingService) *SettingHandler {
	return &SettingHandler{svc: svc}
}

// RegisterSettingRoutes 注册用户设置路由（都需登录，仅本人）。
//
//	GET /user/settings   读取当前用户的设置
//	PUT /user/settings   修改当前用户的设置（部分更新）
func RegisterSettingRoutes(rg routing.RouterGroup, svc application.SettingService, authCheck routing.HandlerFunc) {
	h := NewSettingHandler(svc)
	priv := rg.Group("/user", authCheck)
	{
		priv.GET("/settings", h.GetSettings)
		priv.PUT("/settings", h.UpdateSettings)
	}
}

// UpdateSettingsRequest 修改设置的请求体。字段都是可选的，只改传了的设置项。
type UpdateSettingsRequest struct {
	// FollowListVisibility 关注列表 / 粉丝列表可见性：0=公开，1=私密。取值在 service 层校验。
	FollowListVisibility *int `json:"follow_list_visibility"`
	// DMPermission 谁可以给我发私信（#240）：0=所有人，1=仅我关注的人，2=仅互相关注，3=关闭。取值在 service 层校验。
	DMPermission *int `json:"dm_permission"`
}

// GetSettings GET /user/settings —— 读取当前用户的设置。
func (h *SettingHandler) GetSettings(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	vo, err := h.svc.GetSettings(c, userID)
	if err != nil {
		logger.Log.Error("Failed to get user settings: " + err.Error())
		httputil.InternalError(c, "Failed to get user settings")
		return
	}
	httputil.Success(c, vo)
}

// UpdateSettings PUT /user/settings —— 修改当前用户的设置。
func (h *SettingHandler) UpdateSettings(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req UpdateSettingsRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	vo, err := h.svc.UpdateSettings(c, userID, application.UpdateSettingsInput{
		FollowListVisibility: req.FollowListVisibility,
		DMPermission:         req.DMPermission,
	})
	switch {
	case err == nil:
		httputil.Success(c, vo)
	case application.IsNoSettingFieldErr(err):
		httputil.BadRequest(c, "At least one setting must be provided")
	case application.IsFollowListVisibilityErr(err):
		httputil.BadRequest(c, "follow_list_visibility must be 0 (public) or 1 (private)")
	case application.IsDMPermissionErr(err):
		httputil.BadRequest(c, "dm_permission must be 0 (everyone), 1 (people I follow), 2 (mutual follows) or 3 (no one)")
	default:
		logger.Log.Error("Failed to update user settings: " + err.Error())
		httputil.InternalError(c, "Failed to update user settings")
	}
}
