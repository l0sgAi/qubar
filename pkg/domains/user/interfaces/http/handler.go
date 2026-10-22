// Package http 提供 user 领域的 HTTP 入站适配器（handler + 路由注册）。
package http

import (
	"encoding/json"
	"time"

	"interestBar/pkg/domains/user/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 user 相关的 HTTP 请求。
type Handler struct {
	svc application.UserService
}

// NewHandler 构造一个 user Handler。
func NewHandler(svc application.UserService) *Handler {
	return &Handler{svc: svc}
}

// GetCurrentUser GET /user/get —— 返回当前会话用户。
//
// 对应旧 UserController.GetUser：从 sa-token 会话取出 loginID，
// 再查询用户信息返回。会话信息由 RequireLogin 中间件填充到 AppContext。
func (h *Handler) GetCurrentUser(c appctx.AppContext) {
	loginID, ok := c.LoginID()
	if !ok || loginID == "" {
		httputil.Unauthorized(c, "Token not found")
		return
	}

	user, err := h.svc.GetByIDStr(c, loginID)
	if err != nil || user == nil {
		httputil.InternalError(c, "User info not found")
		return
	}
	httputil.Success(c, user)
}

// UpdateProfileRequest 修改用户信息的请求结构（与旧 controller 字段一致）。
type UpdateProfileRequest struct {
	Username  *string    `json:"username" binding:"omitempty,min=1,max=50"`
	AvatarURL *string    `json:"avatar_url" binding:"omitempty,url"`
	Phone     *string    `json:"phone" binding:"omitempty"`
	Gender    *int       `json:"gender" binding:"omitempty,min=0,max=3"`
	Birthdate *time.Time `json:"birthdate" binding:"omitempty"`
	// Password / ConfirmPassword 重置密码：两者同时传入才生效。
	// 长度/一致性校验在 service 层完成（与 auth 注册一致），故 binding 仅 omitempty。
	Password        *string `json:"password" binding:"omitempty"`
	ConfirmPassword *string `json:"confirm_password" binding:"omitempty"`
}

// SearchUsersRequest 搜索用户的请求结构。
type SearchUsersRequest struct {
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
	// CircleID 圈子作用域（@选人）：空=全站（排除圈内机器人）；
	// 传圈子 uuid=圈内@选人（普通用户+全局机器人+本圈机器人可见）。
	CircleID string `query:"circle_id" binding:"omitempty,uuid"`
}

// UpdateProfile PUT /user/update —— 修改用户自身资料。
func (h *Handler) UpdateProfile(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req UpdateProfileRequest
	if err := c.BindJSON(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.UpdateProfile(c, userID, application.UpdateProfileInput{
		Username:        req.Username,
		AvatarURL:       req.AvatarURL,
		Phone:           req.Phone,
		Gender:          req.Gender,
		Birthdate:       req.Birthdate,
		Password:        req.Password,
		ConfirmPassword: req.ConfirmPassword,
	})
	if err != nil {
		writeUpdateProfileError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Profile updated successfully", result)
}

// SearchUsers GET /user/search —— 搜索用户。
func (h *Handler) SearchUsers(c appctx.AppContext) {
	var req SearchUsersRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	size := req.Size
	if size <= 0 || size > 100 {
		size = 20
	}

	var searchAfter []interface{}
	if req.SearchAfter != "" {
		if err := json.Unmarshal([]byte(req.SearchAfter), &searchAfter); err != nil {
			httputil.BadRequest(c, "Invalid search_after parameter")
			return
		}
	}

	// circle_id：空=Nil=全站；非空必须为合法 uuid（binding 已校验，此处兜底）。
	circleID := uuid.Nil
	if req.CircleID != "" {
		id, err := uuid.Parse(req.CircleID)
		if err != nil {
			httputil.BadRequest(c, "Invalid circle_id parameter")
			return
		}
		circleID = id
	}

	result, err := h.svc.Search(c, req.Keyword, size, searchAfter, circleID)
	if err != nil {
		logger.Log.Error("Failed to search users: " + err.Error())
		httputil.SearchError(c, err, "Failed to search users")
		return
	}
	httputil.Success(c, result)
}

// GetUserDetail GET /user/detail/:id —— 获取用户详情（带缓存）。
func (h *Handler) GetUserDetail(c appctx.AppContext) {
	idStr := c.Param("id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.BadRequest(c, "Invalid user id")
		return
	}

	user, err := h.svc.GetByID(c, userID)
	if err != nil {
		httputil.InternalError(c, "Failed to get user info")
		return
	}
	if user == nil {
		httputil.NotFound(c, "No such user")
		return
	}
	httputil.Success(c, user)
}

// writeUpdateProfileError 把 service 层的可识别错误映射到对应的 HTTP 响应。
func writeUpdateProfileError(c appctx.AppContext, err error) {
	switch {
	case application.IsAtLeastOneFieldErr(err):
		httputil.BadRequest(c, "At least one field must be provided")
	case application.IsUsernameEmptyErr(err):
		httputil.BadRequest(c, "Username cannot be empty")
	case application.IsGenderRangeErr(err):
		httputil.BadRequest(c, "Gender must be 0 (unknown), 1 (male), or 2 (female) 3 (others)")
	case application.IsBirthdateFutureErr(err):
		httputil.BadRequest(c, "Birthdate cannot be in the future")
	case application.IsPasswordTooShortErr(err):
		httputil.BadRequest(c, "Password must be at least 6 characters")
	case application.IsPasswordMismatchErr(err):
		httputil.BadRequest(c, "Password and confirm password do not match")
	case application.IsPasswordIncompleteErr(err):
		httputil.BadRequest(c, "password and confirm_password must be provided together")
	default:
		httputil.InternalError(c, "Failed to update user info")
	}
}

// requireUserID 从 AppContext 取出已登录的 userID（UUIDv7）。
// RequireLogin 中间件已填充 SetLoginID，这里 parse 成 uuid.UUID。
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
