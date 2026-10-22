// Package http 提供 circle 领域的 HTTP 入站适配器（handler + 路由注册）。
package http

import (
	"encoding/json"
	"errors"
	"strconv"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 circle 相关的 HTTP 请求。
type Handler struct {
	svc application.CircleService
}

// NewHandler 构造 circle Handler。
func NewHandler(svc application.CircleService) *Handler {
	return &Handler{svc: svc}
}

// CreateCircleRequest 创建圈子请求。
type CreateCircleRequest struct {
	Name        string    `json:"name" binding:"required,min=1,max=50"`
	Slug        string    `json:"slug" binding:"omitempty,max=60"`
	AvatarURL   string    `json:"avatar_url" binding:"omitempty,url"`
	CoverURL    string    `json:"cover_url" binding:"omitempty,url"`
	Description string    `json:"description" binding:"required,min=1,max=2000"`
	Rule        string    `json:"rule" binding:"omitempty,max=2000"`
	CategoryID  uuid.UUID `json:"category_id" binding:"required"`
	JoinType    int16     `json:"join_type" binding:"omitempty,min=0,max=2"`
}

// CreateCircle POST /circle/create
func (h *Handler) CreateCircle(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req CreateCircleRequest
	if err := c.BindJSON(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters:")
		return
	}

	err := h.svc.CreateCircle(c, userID, application.CreateCircleInput{
		Name: req.Name, Slug: req.Slug, AvatarURL: req.AvatarURL,
		CoverURL: req.CoverURL, Description: req.Description, Rule: req.Rule,
		CategoryID: req.CategoryID, JoinType: req.JoinType,
	})
	if err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "创建圈子成功", nil)
}

// GetCirclesRequest 获取圈子列表请求。
type GetCirclesRequest struct {
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetCircles GET /circle/list
func (h *Handler) GetCircles(c appctx.AppContext) {
	var req GetCirclesRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	size := normalizeSize(req.Size)
	searchAfter, ok := parseSearchAfter(c, req.SearchAfter)
	if !ok {
		return
	}

	result, err := h.svc.SearchCircles(c, req.Keyword, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search circles: " + err.Error())
		httputil.SearchError(c, err, "Failed to search circles")
		return
	}
	httputil.Success(c, result)
}

// GetCircleDetail GET /circle/detail/:id
//
// 访客可读：登录时回填 is_joined/member 字段；匿名（userID==uuid.Nil）时 service 的
// memberRepo.GetMember 找不到记录，IsJoined 自然降级为 false（service.go:459-470）。
func (h *Handler) GetCircleDetail(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	circleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.BadRequest(c, "Invalid circle id")
		return
	}

	vo, err := h.svc.GetCircleDetail(c, userID, circleID)
	if err != nil {
		if err == domain.ErrCircleNotFound {
			httputil.NotFound(c, "Circle not found")
			return
		}
		logger.Log.Error("Failed to get circle: " + err.Error())
		httputil.InternalError(c, "Failed to get circle")
		return
	}
	httputil.Success(c, vo)
}

// JoinCircleRequest 加入圈子请求。
type JoinCircleRequest struct {
	CircleID uuid.UUID `json:"circle_id" binding:"required"`
}

// JoinCircle POST /circle/join
func (h *Handler) JoinCircle(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req JoinCircleRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	joined, pending, err := h.svc.JoinCircle(c, userID, req.CircleID)
	if err != nil {
		writeCircleError(c, err)
		return
	}

	if pending {
		httputil.SuccessWithMessage(c, "Join request submitted, awaiting approval", nil)
		return
	}
	_ = joined
	httputil.SuccessWithMessage(c, "Successfully joined the circle", nil)
}

// LeaveCircleRequest 退出圈子请求。
type LeaveCircleRequest struct {
	CircleID uuid.UUID `json:"circle_id" binding:"required"`
}

// LeaveCircle POST /circle/leave
func (h *Handler) LeaveCircle(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req LeaveCircleRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.LeaveCircle(c, userID, req.CircleID); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Successfully left the circle", nil)
}

// GetMyCirclesRequest 我加入圈子列表请求。
type GetMyCirclesRequest struct {
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetMyCircles GET /circle/my
func (h *Handler) GetMyCircles(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req GetMyCirclesRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	size := normalizeSize(req.Size)

	result, err := h.svc.GetMyCircles(c, userID, req.Keyword, size, req.SearchAfter)
	if err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.Success(c, result)
}

// GetUserCirclesRequest 任意用户加入圈子列表请求。
type GetUserCirclesRequest struct {
	UserID      string `query:"user_id"`
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetUserCircles GET /circle/user —— 查看任意用户加入的圈子（分页）。
func (h *Handler) GetUserCircles(c appctx.AppContext) {
	var req GetUserCirclesRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	targetUserID, err := uuid.Parse(req.UserID)
	if err != nil {
		httputil.BadRequest(c, "Invalid user_id")
		return
	}

	size := normalizeSize(req.Size)

	result, err := h.svc.GetUserCircles(c, targetUserID, req.Keyword, size, req.SearchAfter)
	if err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.Success(c, result)
}

// GetCirclePostsRequest 圈内帖子列表请求。
type GetCirclePostsRequest struct {
	CircleID    string `query:"circle_id" binding:"required,uuid"`
	Type        int    `query:"type" binding:"required,min=1,max=3"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetCirclePosts GET /circle/posts
func (h *Handler) GetCirclePosts(c appctx.AppContext) {
	var req GetCirclePostsRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle_id")
		return
	}

	size := normalizeSize(req.Size)
	searchAfter, ok := parseSearchAfter(c, req.SearchAfter)
	if !ok {
		return
	}

	result, err := h.svc.GetCirclePosts(c, circleID, req.Type, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search circle posts: " + err.Error())
		httputil.SearchError(c, err, "Failed to search circle posts")
		return
	}
	httputil.Success(c, result)
}

// GetActiveCirclesRequest 近期活跃圈子列表请求。
type GetActiveCirclesRequest struct {
	Size   int `query:"size"`
	Offset int `query:"offset"`
}

// GetActiveCircles GET /circle/active —— 近期活跃圈子分页列表（按近 7 天发帖数排序）。
func (h *Handler) GetActiveCircles(c appctx.AppContext) {
	var req GetActiveCirclesRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	size := normalizeSize(req.Size)
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	result, err := h.svc.ListActiveCircles(c, size, offset)
	if err != nil {
		logger.Log.Error("Failed to list active circles: " + err.Error())
		httputil.SearchError(c, err, "Failed to list active circles")
		return
	}
	httputil.Success(c, result)
}

// GetRandomCirclesRequest 随机圈子列表请求。
type GetRandomCirclesRequest struct {
	Size int `query:"size"`
}

// GetRandomCircles GET /circle/random —— 随机圈子列表（侧栏推荐，每次结果不同，不分页）。
func (h *Handler) GetRandomCircles(c appctx.AppContext) {
	var req GetRandomCirclesRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	size := normalizeSize(req.Size)

	result, err := h.svc.ListRandomCircles(c, size)
	if err != nil {
		logger.Log.Error("Failed to list random circles: " + err.Error())
		httputil.SearchError(c, err, "Failed to list random circles")
		return
	}
	httputil.Success(c, result)
}

// ===== 圈子管理（owner/admin，权限矩阵在 service 层校验）=====

// listManagedCirclesReq 可管理圈子列表查询参数（hertz BindQuery 只认 query tag）。
type listManagedCirclesReq struct {
	Page    int    `query:"page"`
	Size    int    `query:"size"`
	Keyword string `query:"keyword"`
}

// ListManagedCircles GET /circle/manage/list —— 我可管理的圈子列表（owner/admin）。
// 登录即可调用：查询本身即权限过滤，service 层不做角色门槛；page/size 规整在 service。
func (h *Handler) ListManagedCircles(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req listManagedCirclesReq
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid query parameters")
		return
	}

	result, err := h.svc.ListManagedCircles(c, userID, req.Keyword, req.Page, req.Size)
	if err != nil {
		logger.Log.Error("Failed to list managed circles: " + err.Error())
		httputil.InternalError(c, "Failed to list managed circles")
		return
	}
	httputil.Pagination(c, result.Data, result.Total, result.Page, result.Size)
}

// GetCircleMembersRequest 成员列表请求（管理端）。
// role/status 为字符串参数：空或 "-1" 表示不过滤（status=0 待审是合法过滤值，不能用零值默认）。
// keyword 非空时按用户名/邮箱搜索过滤（翻页须带同一 keyword）。
type GetCircleMembersRequest struct {
	CircleID string `query:"circle_id" binding:"required,uuid"`
	Role     string `query:"role"`
	Status   string `query:"status"`
	Keyword  string `query:"keyword"`
	Cursor   string `query:"cursor"`
	Size     int    `query:"size"`
}

// GetCircleMembers GET /circle/members —— 管理端成员列表（admin+，keyset 分页）。
func (h *Handler) GetCircleMembers(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req GetCircleMembersRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle_id")
		return
	}
	role, ok := parseMemberFilterParam(c, req.Role, "role")
	if !ok {
		return
	}
	status, ok := parseMemberFilterParam(c, req.Status, "status")
	if !ok {
		return
	}

	result, err := h.svc.ListCircleMembers(c, userID, circleID, role, status, req.Keyword, req.Cursor, normalizeSize(req.Size))
	if err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.Success(c, result)
}

// ManageRoleRequest 角色变更请求（仅圈主）。
type ManageRoleRequest struct {
	CircleID     uuid.UUID `json:"circle_id" binding:"required"`
	TargetUserID uuid.UUID `json:"target_user_id" binding:"required"`
	Role         int16     `json:"role" binding:"required"`
}

// ManageRole POST /circle/manage/role —— 设为/取消管理员。
func (h *Handler) ManageRole(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req ManageRoleRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.SetMemberRole(c, userID, req.CircleID, req.TargetUserID, req.Role); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Member role updated", nil)
}

// TransferOwnerRequest 转让圈主请求（仅圈主）。
type TransferOwnerRequest struct {
	CircleID     uuid.UUID `json:"circle_id" binding:"required"`
	TargetUserID uuid.UUID `json:"target_user_id" binding:"required"`
}

// ManageTransfer POST /circle/manage/transfer —— 转让圈主。
func (h *Handler) ManageTransfer(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req TransferOwnerRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.TransferOwner(c, userID, req.CircleID, req.TargetUserID); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Circle ownership transferred", nil)
}

// MuteMemberRequest 禁言请求（admin+）。
type MuteMemberRequest struct {
	CircleID      uuid.UUID `json:"circle_id" binding:"required"`
	TargetUserID  uuid.UUID `json:"target_user_id" binding:"required"`
	DurationHours int       `json:"duration_hours" binding:"required"`
}

// ManageMute POST /circle/manage/mute —— 禁言成员。
func (h *Handler) ManageMute(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req MuteMemberRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.MuteMember(c, userID, req.CircleID, req.TargetUserID, req.DurationHours); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Member muted", nil)
}

// UnmuteMemberRequest 解禁请求（admin+）。
type UnmuteMemberRequest struct {
	CircleID     uuid.UUID `json:"circle_id" binding:"required"`
	TargetUserID uuid.UUID `json:"target_user_id" binding:"required"`
}

// ManageUnmute POST /circle/manage/unmute —— 解除禁言。
func (h *Handler) ManageUnmute(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req UnmuteMemberRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.UnmuteMember(c, userID, req.CircleID, req.TargetUserID); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Member unmuted", nil)
}

// BanMemberRequest 拉黑请求（admin+）。
type BanMemberRequest struct {
	CircleID     uuid.UUID `json:"circle_id" binding:"required"`
	TargetUserID uuid.UUID `json:"target_user_id" binding:"required"`
}

// ManageBan POST /circle/manage/ban —— 拉黑/踢出成员。
func (h *Handler) ManageBan(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req BanMemberRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.BanMember(c, userID, req.CircleID, req.TargetUserID); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Member banned", nil)
}

// UnbanMemberRequest 解除拉黑请求（admin+）。
type UnbanMemberRequest struct {
	CircleID     uuid.UUID `json:"circle_id" binding:"required"`
	TargetUserID uuid.UUID `json:"target_user_id" binding:"required"`
}

// ManageUnban POST /circle/manage/unban —— 解除拉黑（成员需重新申请加入）。
func (h *Handler) ManageUnban(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req UnbanMemberRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.UnbanMember(c, userID, req.CircleID, req.TargetUserID); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Member unbanned", nil)
}

// ReviewMemberRequest 入圈审核请求（admin+）。
type ReviewMemberRequest struct {
	CircleID     uuid.UUID `json:"circle_id" binding:"required"`
	TargetUserID uuid.UUID `json:"target_user_id" binding:"required"`
	Approve      bool      `json:"approve"`
}

// ManageReview POST /circle/manage/review —— 审核入圈申请。
func (h *Handler) ManageReview(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req ReviewMemberRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.ReviewJoinRequest(c, userID, req.CircleID, req.TargetUserID, req.Approve); err != nil {
		writeCircleError(c, err)
		return
	}
	if req.Approve {
		httputil.SuccessWithMessage(c, "Join request approved", nil)
		return
	}
	httputil.SuccessWithMessage(c, "Join request rejected", nil)
}

// UpdateCircleRequest 编辑圈子资料请求（分字段权限：name/slug/join_type/category_id 仅圈主）。
// 指针字段 nil = 不更新；slug 传空串清除；category_id 传全零 UUID 清除分类。
type UpdateCircleRequest struct {
	CircleID    uuid.UUID  `json:"circle_id" binding:"required"`
	Name        *string    `json:"name"`
	Slug        *string    `json:"slug"`
	AvatarURL   *string    `json:"avatar_url"`
	CoverURL    *string    `json:"cover_url"`
	Description *string    `json:"description"`
	Rule        *string    `json:"rule"`
	CategoryID  *uuid.UUID `json:"category_id"`
	JoinType    *int16     `json:"join_type"`
}

// UpdateCircleProfile PUT /circle/update —— 编辑圈子资料。
func (h *Handler) UpdateCircleProfile(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req UpdateCircleRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.UpdateCircleProfile(c, userID, req.CircleID, application.UpdateCircleProfileInput{
		Name: req.Name, Slug: req.Slug, AvatarURL: req.AvatarURL, CoverURL: req.CoverURL,
		Description: req.Description, Rule: req.Rule, CategoryID: req.CategoryID, JoinType: req.JoinType,
	}); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Circle updated", nil)
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

// requireUserIDAllowAnon 尝试返回 userID，但允许匿名（未登录返回 uuid.Nil, true）。
//
// 用于"圈子详情"这类访客可读接口：登录时回填 is_joined/member 字段；匿名时降级为 false。
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

func normalizeSize(size int) int {
	if size <= 0 || size > 100 {
		return 20
	}
	return size
}

// parseMemberFilterParam 解析成员列表 role/status 过滤参数（字符串形式，
// 空或 "-1" → -1 表示不过滤）。非法时写入 BadRequest 并返回 ok=false。
// 用字符串而非数值绑定：status=0（待审）是合法过滤值，无法与"未传"区分。
func parseMemberFilterParam(c appctx.AppContext, raw, name string) (int16, bool) {
	if raw == "" || raw == "-1" {
		return -1, true
	}
	v, err := strconv.ParseInt(raw, 10, 16)
	if err != nil || v < -1 {
		httputil.BadRequest(c, "Invalid "+name+" filter parameter")
		return 0, false
	}
	return int16(v), true
}

// parseSearchAfter 解析 search_after 参数。非法时写入 BadRequest 并返回 ok=false。
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

// writeCircleError 把 service 层错误映射到 HTTP 响应。
func writeCircleError(c appctx.AppContext, err error) {
	switch {
	case application.IsInvalidCursorErr(err):
		httputil.BadRequest(c, "Invalid search_after parameter")
	case application.IsInvalidJoinTypeErr(err):
		httputil.BadRequest(c, "join_type must be 0 (direct), 1 (approval), or 2 (private)")
	case application.IsCircleNameExistsErr(err):
		httputil.Conflict(c, "Circle name already exists")
	case application.IsCircleSlugExistsErr(err):
		httputil.Conflict(c, "Circle slug already exists")
	case application.IsCircleNotAvailableErr(err):
		httputil.Forbidden(c, "This circle is not available for joining")
	case application.IsAlreadyMemberErr(err):
		httputil.Conflict(c, "Already a member of this circle")
	case application.IsBannedFromCircleErr(err):
		httputil.Forbidden(c, "User is banned from this circle")
	case application.IsPrivateCircleErr(err):
		httputil.Forbidden(c, "This circle is private and requires invitation")
	case application.IsOwnerCannotLeaveErr(err):
		httputil.Forbidden(c, "Circle owner cannot leave the circle")
	case application.IsNotMemberErr(err):
		httputil.NotFound(c, "Not a member of this circle")
	case application.IsInvalidMemberRoleErr(err):
		httputil.BadRequest(c, "role must be 10 (member) or 20 (admin); use /circle/manage/transfer to transfer ownership")
	case application.IsInvalidMuteDurationErr(err):
		httputil.BadRequest(c, "duration_hours must be between 1 and 720")
	case application.IsInvalidMemberFilterErr(err):
		httputil.BadRequest(c, "Invalid role or status filter parameter")
	case application.IsNoCircleUpdateFieldErr(err):
		httputil.BadRequest(c, "At least one field is required to update")
	case application.IsInvalidCircleProfileErr(err):
		httputil.BadRequest(c, "Invalid circle profile fields (name 1-50, slug ≤60, description 1-2000, rule ≤2000, url ≤500 chars)")
	case application.IsUserSearchUnavailableErr(err):
		httputil.ServiceUnavailable(c, "User search service unavailable, please retry later")
	case application.IsNotCircleAdminErr(err):
		httputil.Forbidden(c, "Circle admin privileges required")
	case application.IsNotCircleOwnerErr(err):
		httputil.Forbidden(c, "Circle owner privileges required")
	case application.IsCannotManageTargetErr(err):
		httputil.Forbidden(c, "Cannot manage a member with equal or higher role")
	case errors.Is(err, domain.ErrMemberStateConflict):
		httputil.Conflict(c, "Member state conflict, please refresh and retry")
	case errors.Is(err, domain.ErrInvalidCursor):
		httputil.BadRequest(c, "Invalid cursor parameter")
	case err == domain.ErrCircleNotFound:
		httputil.NotFound(c, "Circle not found")
	case err == domain.ErrMemberNotFound:
		httputil.NotFound(c, "Not a member of this circle")
	default:
		logger.Log.Error("circle service error: " + err.Error())
		httputil.InternalError(c, "Internal error")
	}
}
