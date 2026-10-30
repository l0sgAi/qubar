package http

import (
	"errors"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// CreateInviteLinkRequest 创建邀请链接请求。expire_hours 不传 / 0 取默认 7 天；max_uses 不传表示不限次数。
type CreateInviteLinkRequest struct {
	CircleID    uuid.UUID `json:"circle_id" binding:"required"`
	ExpireHours int       `json:"expire_hours"`
	MaxUses     *int      `json:"max_uses"`
}

// CreateInviteLink POST /circle/invite/link/create —— 生成邀请链接（admin+，仅私密圈）。
func (h *Handler) CreateInviteLink(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req CreateInviteLinkRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	result, err := h.svc.CreateInviteLink(c, userID, req.CircleID, application.CreateInviteLinkInput{
		ExpireHours: req.ExpireHours, MaxUses: req.MaxUses,
	})
	if err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.Success(c, result)
}

// ListInviteLinksRequest 邀请链接列表查询参数。
type ListInviteLinksRequest struct {
	CircleID string `query:"circle_id" binding:"required,uuid"`
	Page     int    `query:"page"`
	Size     int    `query:"size"`
}

// ListInviteLinks GET /circle/invite/link/list —— 圈内邀请链接列表（admin+，不返回 token）。
func (h *Handler) ListInviteLinks(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req ListInviteLinksRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle_id")
		return
	}
	result, err := h.svc.ListInviteLinks(c, userID, circleID, req.Page, req.Size)
	if err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.Pagination(c, result.Data, result.Total, result.Page, result.Size)
}

// RevokeInviteLinkRequest 撤销邀请链接请求。
type RevokeInviteLinkRequest struct {
	LinkID uuid.UUID `json:"link_id" binding:"required"`
}

// RevokeInviteLink POST /circle/invite/link/revoke —— 撤销邀请链接（admin+，幂等）。
func (h *Handler) RevokeInviteLink(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req RevokeInviteLinkRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	if err := h.svc.RevokeInviteLink(c, userID, req.LinkID); err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Invite link revoked", nil)
}

// PreviewInviteRequest 邀请预览查询参数。
type PreviewInviteRequest struct {
	Token string `query:"token"`
}

// PreviewInvite GET /circle/invite/preview?token= —— 邀请预览（访客可读；无效统一 404）。
func (h *Handler) PreviewInvite(c appctx.AppContext) {
	var req PreviewInviteRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	result, err := h.svc.PreviewInvite(c, req.Token)
	if err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.Success(c, result)
}

// AcceptInviteRequest 接受邀请请求。
type AcceptInviteRequest struct {
	Token string `json:"token" binding:"required"`
}

// AcceptInvite POST /circle/invite/accept —— 凭邀请链接加入（已是成员幂等成功）。
func (h *Handler) AcceptInvite(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req AcceptInviteRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	result, err := h.svc.AcceptInvite(c, userID, req.Token)
	if err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.Success(c, result)
}

// writeInviteError 邀请专属错误映射，其余交给 writeCircleError（权限 / 拉黑 / 圈子不存在等）。
func writeInviteError(c appctx.AppContext, err error) {
	switch {
	case application.IsInviteInvalidErr(err):
		httputil.NotFound(c, "Invite not found or no longer valid")
	case application.IsInviteRequiresPrivateErr(err):
		httputil.BadRequest(c, "Invite links are only available for private circles")
	case application.IsInvalidInviteExpireErr(err):
		httputil.BadRequest(c, "expire_hours must be between 1 and 720")
	case application.IsInvalidInviteMaxUsesErr(err):
		httputil.BadRequest(c, "max_uses must be between 1 and 10000")
	case application.IsInvalidInviteEmailsErr(err):
		httputil.BadRequest(c, "emails must contain 1 to 50 entries")
	case application.IsInvalidInviteEmailStatusErr(err):
		httputil.BadRequest(c, "status filter must be -1 or 0-3")
	case errors.Is(err, domain.ErrInviteLinkNotFound):
		httputil.NotFound(c, "Invite link not found")
	case errors.Is(err, domain.ErrInviteEmailNotFound):
		httputil.NotFound(c, "Email invite not found")
	case errors.Is(err, domain.ErrInviteEmailStateConflict):
		httputil.Conflict(c, "Email invite is already accepted or rejected")
	default:
		writeCircleError(c, err)
	}
}

// AddInviteEmailsRequest 添加 email 邀请请求。
type AddInviteEmailsRequest struct {
	CircleID uuid.UUID `json:"circle_id" binding:"required"`
	Emails   []string  `json:"emails"`
}

// AddInviteEmails POST /circle/invite/email/add —— 批量添加 email 邀请（admin+，仅私密圈，单次 ≤50）。
func (h *Handler) AddInviteEmails(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req AddInviteEmailsRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	result, err := h.svc.AddInviteEmails(c, userID, req.CircleID, req.Emails)
	if err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.Success(c, result)
}

// ListInviteEmailsRequest email 邀请列表查询参数。status 为字符串：空或 "-1" 不过滤（0 是合法过滤值）。
type ListInviteEmailsRequest struct {
	CircleID string `query:"circle_id" binding:"required,uuid"`
	Status   string `query:"status"`
	Page     int    `query:"page"`
	Size     int    `query:"size"`
}

// ListInviteEmails GET /circle/invite/email/list —— 圈内 email 邀请列表（admin+）。
func (h *Handler) ListInviteEmails(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req ListInviteEmailsRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle_id")
		return
	}
	status, ok := parseMemberFilterParam(c, req.Status, "status")
	if !ok {
		return
	}
	result, err := h.svc.ListInviteEmails(c, userID, circleID, status, req.Page, req.Size)
	if err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.Pagination(c, result.Data, result.Total, result.Page, result.Size)
}

// RevokeInviteEmailRequest 撤销 email 邀请请求。
type RevokeInviteEmailRequest struct {
	InviteID uuid.UUID `json:"invite_id" binding:"required"`
}

// RevokeInviteEmail POST /circle/invite/email/revoke —— 撤销待接受的 email 邀请（admin+，幂等）。
func (h *Handler) RevokeInviteEmail(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req RevokeInviteEmailRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	if err := h.svc.RevokeInviteEmail(c, userID, req.InviteID); err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Email invite revoked", nil)
}

// ListMyInvitesRequest 我的邀请查询参数。
type ListMyInvitesRequest struct {
	Page int `query:"page"`
	Size int `query:"size"`
}

// ListMyInvites GET /circle/invite/my —— 当前用户 email 收到的待接受邀请。
func (h *Handler) ListMyInvites(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req ListMyInvitesRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	result, err := h.svc.ListMyInvites(c, userID, req.Page, req.Size)
	if err != nil {
		writeInviteError(c, err)
		return
	}
	httputil.Pagination(c, result.Data, result.Total, result.Page, result.Size)
}

// RespondInviteEmailRequest 接受 / 拒绝 email 邀请请求。Accept 用指针区分未传与 false。
type RespondInviteEmailRequest struct {
	InviteID uuid.UUID `json:"invite_id" binding:"required"`
	Accept   *bool     `json:"accept"`
}

// RespondInviteEmail POST /circle/invite/email/respond —— 接受或拒绝发给自己 email 的邀请。
func (h *Handler) RespondInviteEmail(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req RespondInviteEmailRequest
	if err := c.BindJSON(&req); err != nil || req.Accept == nil || req.InviteID == uuid.Nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	result, err := h.svc.RespondInviteEmail(c, userID, req.InviteID, *req.Accept)
	if err != nil {
		writeInviteError(c, err)
		return
	}
	if !*req.Accept {
		httputil.SuccessWithMessage(c, "Invite rejected", nil)
		return
	}
	httputil.Success(c, result)
}
