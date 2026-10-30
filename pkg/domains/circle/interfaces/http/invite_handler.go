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
	case errors.Is(err, domain.ErrInviteLinkNotFound):
		httputil.NotFound(c, "Invite link not found")
	default:
		writeCircleError(c, err)
	}
}
