package http

import (
	"context"
	"errors"

	"interestBar/pkg/domains/follow/application"
	"interestBar/pkg/domains/follow/domain"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// ListRequest 列表接口的 query 参数。
type ListRequest struct {
	// Cursor 上一页返回的 next_cursor，原样回传；不传表示第一页。
	Cursor string `query:"cursor"`
	// Size 页大小：不传或 ≤0 取 20，超过 50 按 50。
	Size int `query:"size"`
}

// RelationsRequest 批量关系查询的请求体。
type RelationsRequest struct {
	UserIDs []string `json:"user_ids"`
}

// ListFollowing GET /follow/following/:user_id —— 该用户关注的人（访客可读，受列表隐私开关限制）。
func (h *Handler) ListFollowing(c appctx.AppContext) {
	h.listOf(c, h.svc.ListFollowing)
}

// ListFollowers GET /follow/followers/:user_id —— 关注该用户的人（访客可读，受列表隐私开关限制）。
func (h *Handler) ListFollowers(c appctx.AppContext) {
	h.listOf(c, h.svc.ListFollowers)
}

func (h *Handler) listOf(
	c appctx.AppContext,
	list func(ctx context.Context, viewerID, userID uuid.UUID, cursor string, size int) (*application.FollowListVO, error),
) {
	targetID, ok := targetUserID(c)
	if !ok {
		return
	}
	req, ok := bindListRequest(c)
	if !ok {
		return
	}
	vo, err := list(c, optionalUserID(c), targetID, req.Cursor, req.Size)
	if err != nil {
		writeListError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// ListMutual GET /follow/mutual —— 当前用户的互关列表（需登录，只能看自己的）。
func (h *Handler) ListMutual(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	req, ok := bindListRequest(c)
	if !ok {
		return
	}
	vo, err := h.svc.ListMutual(c, userID, req.Cursor, req.Size)
	if err != nil {
		writeListError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// GetRelations POST /follow/relations —— 批量查询当前用户与一组用户的关系（需登录）。
func (h *Handler) GetRelations(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req RelationsRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	targetIDs := make([]uuid.UUID, 0, len(req.UserIDs))
	for _, raw := range req.UserIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			httputil.BadRequest(c, "Invalid user id in user_ids")
			return
		}
		targetIDs = append(targetIDs, id)
	}
	vo, err := h.svc.GetRelations(c, userID, targetIDs)
	if err != nil {
		writeListError(c, err)
		return
	}
	httputil.Success(c, vo)
}

func bindListRequest(c appctx.AppContext) (ListRequest, bool) {
	var req ListRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return req, false
	}
	return req, true
}

// writeListError 列表 / 批量关系接口的错误映射；其余错误交给 writeFollowError。
func writeListError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCursor):
		httputil.BadRequest(c, "Invalid cursor")
	case errors.Is(err, domain.ErrFollowListPrivate):
		httputil.Forbidden(c, "This user's following and follower lists are private")
	case errors.Is(err, domain.ErrTooManyTargets):
		// message 带上限值，如 "too many user ids: at most 100 per request"
		httputil.BadRequest(c, err.Error())
	default:
		writeFollowError(c, err)
	}
}
