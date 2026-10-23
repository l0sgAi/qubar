package http

import (
	"interestBar/pkg/domains/post/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// ListReviewQueueRequest 待审帖子列表请求。
type ListReviewQueueRequest struct {
	CircleID string `query:"circle_id"`
	Size     int    `query:"size"`
	Cursor   string `query:"cursor"`
}

// ListReviewQueue GET /circle/moderation/posts
//
// 圈主 / 管理员查看本圈审核中的帖子（带 AI 审核结论），鉴权在 service 内统一做。
func (h *Handler) ListReviewQueue(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req ListReviewQueueRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle_id")
		return
	}

	result, err := h.svc.ListReviewQueue(c, userID, circleID, req.Size, req.Cursor)
	if err != nil {
		writeReviewError(c, err)
		return
	}
	httputil.Success(c, result)
}

// ReviewPostRequest 人工审核请求。
type ReviewPostRequest struct {
	CircleID uuid.UUID `json:"circle_id" binding:"required"`
	PostID   uuid.UUID `json:"post_id" binding:"required"`
	Action   string    `json:"action" binding:"required"` // approve | reject
	Reason   string    `json:"reason" binding:"omitempty,max=200"`
}

// ReviewPost POST /circle/moderation/review
func (h *Handler) ReviewPost(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req ReviewPostRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.ReviewPost(c, userID, application.ReviewPostInput{
		CircleID: req.CircleID,
		PostID:   req.PostID,
		Action:   application.ReviewAction(req.Action),
		Reason:   req.Reason,
	})
	if err != nil {
		writeReviewError(c, err)
		return
	}
	httputil.Success(c, result)
}

// writeReviewError 人工审核错误映射：非本圈管理员 403，帖子不在该圈 / 不是审核中 404。
func writeReviewError(c appctx.AppContext, err error) {
	switch {
	case application.IsCircleIDRequiredErr(err):
		httputil.BadRequest(c, "circle_id is required")
	case application.IsInvalidReviewActionErr(err):
		httputil.BadRequest(c, "action must be approve or reject")
	case application.IsInvalidReviewCursorErr(err):
		httputil.BadRequest(c, "Invalid cursor")
	case application.IsNotCircleModeratorErr(err):
		httputil.Forbidden(c, "Only circle owner or admin can review posts")
	case application.IsReviewPostNotFoundErr(err):
		httputil.NotFound(c, "Post not found or not under review")
	default:
		logger.Log.Error("post review error: " + err.Error())
		httputil.InternalError(c, "Failed to process post review")
	}
}
