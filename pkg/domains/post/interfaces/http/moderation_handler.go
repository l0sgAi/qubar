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

// ===== 举报与复核（#153）=====

// ReportPostRequest 举报请求。
type ReportPostRequest struct {
	PostID     uuid.UUID `json:"post_id" binding:"required"`
	ReasonType int16     `json:"reason_type" binding:"required"` // 1 垃圾广告 2 色情低俗 3 辱骂骚扰 4 违法违规 5 暴力恐怖 6 泄露隐私 7 自伤轻生 8 其他
	Reason     string    `json:"reason" binding:"omitempty,max=200"`
}

// ReportPost POST /post/report
func (h *Handler) ReportPost(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req ReportPostRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	if err := h.svc.ReportPost(c, userID, application.ReportPostInput{
		PostID: req.PostID, ReasonType: req.ReasonType, Reason: req.Reason,
	}); err != nil {
		writeReportError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// ListReportedPosts GET /circle/moderation/reports
//
// 圈主 / 管理员查看本圈被举报（有待处理举报）的帖子，参数同待审列表。
func (h *Handler) ListReportedPosts(c appctx.AppContext) {
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

	result, err := h.svc.ListReportedPosts(c, userID, circleID, req.Size, req.Cursor)
	if err != nil {
		writeReportError(c, err)
		return
	}
	httputil.Success(c, result)
}

// ModerationSummaryRequest 待处理数量请求。
type ModerationSummaryRequest struct {
	CircleID string `query:"circle_id"`
}

// GetModerationSummary GET /circle/moderation/summary
//
// 圈主 / 管理员查看本圈待审帖子数与被举报帖子数（审核页角标，#156），鉴权同被举报列表。
func (h *Handler) GetModerationSummary(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req ModerationSummaryRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}
	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle_id")
		return
	}

	result, err := h.svc.GetModerationSummary(c, userID, circleID)
	if err != nil {
		writeReportError(c, err)
		return
	}
	httputil.Success(c, result)
}

// ResolveReportRequest 举报复核请求。
type ResolveReportRequest struct {
	CircleID uuid.UUID `json:"circle_id" binding:"required"`
	PostID   uuid.UUID `json:"post_id" binding:"required"`
	Action   string    `json:"action" binding:"required"` // dismiss | remove
	Reason   string    `json:"reason" binding:"omitempty,max=200"`
}

// ResolveReport POST /circle/moderation/reports/resolve
func (h *Handler) ResolveReport(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req ResolveReportRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.ResolveReport(c, userID, application.ResolveReportInput{
		CircleID: req.CircleID,
		PostID:   req.PostID,
		Action:   application.ReportAction(req.Action),
		Reason:   req.Reason,
	})
	if err != nil {
		writeReportError(c, err)
		return
	}
	httputil.Success(c, result)
}

// writeReportError 举报 / 复核错误映射。
func writeReportError(c appctx.AppContext, err error) {
	switch {
	case application.IsCircleIDRequiredErr(err):
		httputil.BadRequest(c, "circle_id is required")
	case application.IsInvalidReportReasonErr(err):
		httputil.BadRequest(c, "reason_type must be within 1-8")
	case application.IsCannotReportOwnPostErr(err):
		httputil.BadRequest(c, "You cannot report your own post")
	case application.IsInvalidReportActionErr(err):
		httputil.BadRequest(c, "action must be dismiss or remove")
	case application.IsInvalidReviewCursorErr(err):
		httputil.BadRequest(c, "Invalid cursor")
	case application.IsNotCircleModeratorErr(err):
		httputil.Forbidden(c, "Only circle owner or admin can handle reports")
	case application.IsReportPostNotFoundErr(err):
		httputil.NotFound(c, "Post not found or not published")
	case application.IsReportedPostNotFoundErr(err):
		httputil.NotFound(c, "Post not found or has no pending reports")
	case application.IsAlreadyReportedErr(err):
		httputil.Conflict(c, "You have already reported this post")
	case application.IsReportLimitExceededErr(err):
		httputil.TooManyRequests(c, "Report limit exceeded, please try again later")
	default:
		logger.Log.Error("post report error: " + err.Error())
		httputil.InternalError(c, "Failed to process post report")
	}
}
