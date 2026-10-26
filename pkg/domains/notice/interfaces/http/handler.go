// Package http 提供 notice 领域的 HTTP 接口层。
package http

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"interestBar/pkg/domains/notice/application"
	"interestBar/pkg/domains/notice/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler notice 领域 HTTP 处理器。
type Handler struct {
	svc application.NoticeService
}

// NewHandler 构造 Handler。
func NewHandler(svc application.NoticeService) *Handler {
	return &Handler{svc: svc}
}

// ListNoticesRequest 通知列表请求。
type ListNoticesRequest struct {
	Type   string `query:"type"`   // 空/"0"=全部; 单值"1"-"6"; 或逗号分隔多值"1,2"(分类 tab 聚合)
	Size   int    `query:"size"`   // 每页条数(<=0 或 >100 回落 20)
	Cursor string `query:"cursor"` // 上一页返回的游标, 空=第一页
}

// ListNotices GET /notice/list
func (h *Handler) ListNotices(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req ListNoticesRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	noticeTypes, err := parseNoticeTypes(req.Type)
	if err != nil {
		httputil.BadRequest(c, err.Error())
		return
	}

	result, err := h.svc.ListNotifications(c, userID, noticeTypes, normalizeSize(req.Size), req.Cursor)
	if err != nil {
		writeNoticeError(c, err)
		return
	}
	httputil.Success(c, result)
}

// GetUnreadCount GET /notice/unread-count
func (h *Handler) GetUnreadCount(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	count, err := h.svc.GetUnreadCount(c, userID)
	if err != nil {
		writeNoticeError(c, err)
		return
	}
	httputil.Success(c, map[string]int64{"unread_count": count})
}

// MarkReadRequest 批量已读请求。
type MarkReadRequest struct {
	IDs []string `json:"ids" binding:"required,min=1,max=100"`
}

// MarkRead POST /notice/read
func (h *Handler) MarkRead(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req MarkReadRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, raw := range req.IDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			httputil.BadRequest(c, "Invalid notice id: "+raw)
			return
		}
		ids = append(ids, id)
	}

	if err := h.svc.MarkRead(c, userID, ids); err != nil {
		writeNoticeError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// MarkAllRead POST /notice/read-all
func (h *Handler) MarkAllRead(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	if err := h.svc.MarkAllRead(c, userID); err != nil {
		writeNoticeError(c, err)
		return
	}
	httputil.Success(c, nil)
}

// ===== 辅助函数 =====

// requireUserID 要求登录并返回 userID。未登录返回 false（已写 401）。
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

// normalizeSize 分页大小规整：<=0 或 >100 回落 20。
func normalizeSize(size int) int {
	if size <= 0 || size > 100 {
		return 20
	}
	return size
}

// parseNoticeTypes 解析 type 查询参数为类型集合。
// 空串或 "0" = 全部（nil）；否则按逗号拆分，每段须为 1-11 的整数。
func parseNoticeTypes(raw string) ([]int16, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	types := make([]int16, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		v, err := strconv.Atoi(p)
		if err != nil || v < int(domain.NoticeTypeLikePost) || v > int(domain.NoticeTypeMax) {
			return nil, fmt.Errorf("invalid notice type: %s", p)
		}
		types = append(types, int16(v))
	}
	return types, nil
}

// writeNoticeError 把 service 层错误映射到 HTTP 响应。
func writeNoticeError(c appctx.AppContext, err error) {
	switch {
	case application.IsInvalidNoticeTypeErr(err), application.IsEmptyNoticeIDsErr(err):
		httputil.BadRequest(c, err.Error())
	case errors.Is(err, domain.ErrInvalidCursor):
		httputil.BadRequest(c, "Invalid cursor")
	default:
		logger.Log.Error("notice service error: " + err.Error())
		httputil.InternalError(c)
	}
}
