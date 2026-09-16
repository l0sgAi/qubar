// Package http 提供 comment 领域的 HTTP 入站适配器。
package http

import (
	"encoding/json"
	"errors"

	"interestBar/pkg/domains/comment/application"
	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 comment 相关的 HTTP 请求。
type Handler struct {
	svc application.CommentService
}

// NewHandler 构造 comment Handler。
func NewHandler(svc application.CommentService) *Handler {
	return &Handler{svc: svc}
}

// CreateCommentRequest 发评论/回复的请求结构。
type CreateCommentRequest struct {
	PostID         uuid.UUID       `json:"post_id" binding:"required"`
	Content        string          `json:"content" binding:"required,min=1,max=10000"`
	ExtraData      json.RawMessage `json:"extra_data" binding:"omitempty"`
	RootID         *uuid.UUID      `json:"root_id" binding:"omitempty"`
	ReplyToID      *uuid.UUID      `json:"reply_to_id" binding:"omitempty"`
	MentionUserIDs []string        `json:"mention_user_ids" binding:"omitempty,max=50"` // @提及用户ID(uuid 字符串)
}

// CreateComment POST /comment/create
func (h *Handler) CreateComment(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req CreateCommentRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	mentionIDs, ok := parseMentionUserIDs(c, req.MentionUserIDs)
	if !ok {
		return
	}

	commentID, err := h.svc.CreateComment(c, userID, application.CreateCommentInput{
		PostID:         req.PostID,
		Content:        req.Content,
		ExtraData:      req.ExtraData,
		RootID:         req.RootID,
		ReplyToID:      req.ReplyToID,
		MentionUserIDs: mentionIDs,
	})
	if err != nil {
		writeCommentError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "评论成功", commentID)
}

// GetCommentsRequest 获取顶层评论列表的请求结构。
type GetCommentsRequest struct {
	PostID string `query:"post_id" binding:"required,uuid"`
	Sort   int    `query:"sort" binding:"omitempty,oneof=0 1"` // 0=点赞倒序(默认), 1=时间倒序
	Cursor string `query:"cursor"`
}

// GetComments GET /comment/list
func (h *Handler) GetComments(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	var req GetCommentsRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	postID, err := uuid.Parse(req.PostID)
	if err != nil {
		httputil.BadRequest(c, "Invalid post_id")
		return
	}

	result, err := h.svc.GetRootComments(c, userID, postID, req.Sort, req.Cursor)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCursor) {
			httputil.BadRequest(c, "Invalid cursor")
			return
		}
		logger.Log.Error("Failed to get comments: " + err.Error())
		httputil.InternalError(c, "Failed to get comments")
		return
	}
	httputil.Success(c, result)
}

// GetRepliesRequest 获取回复列表的请求结构。
type GetRepliesRequest struct {
	RootID string `query:"root_id" binding:"required,uuid"`
	Sort   int    `query:"sort" binding:"omitempty,oneof=0 1"` // 0=点赞倒序(最热), 1=时间倒序(最新)
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" binding:"omitempty,min=1,max=50"`
}

// GetReplies GET /comment/replies
func (h *Handler) GetReplies(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	var req GetRepliesRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	rootID, err := uuid.Parse(req.RootID)
	if err != nil {
		httputil.BadRequest(c, "Invalid root_id")
		return
	}

	result, err := h.svc.GetReplies(c, userID, rootID, req.Limit, req.Sort, req.Cursor)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCursor) {
			httputil.BadRequest(c, "Invalid cursor")
			return
		}
		if application.IsNotRootCommentErr(err) {
			httputil.BadRequest(c, "Not a root comment")
			return
		}
		if errors.Is(err, domain.ErrCommentNotFound) {
			httputil.NotFound(c, "Root comment not found")
			return
		}
		logger.Log.Error("Failed to get replies: " + err.Error())
		httputil.InternalError(c, "Failed to get replies")
		return
	}
	httputil.Success(c, result)
}

// LocateCommentRequest 评论定位请求。
type LocateCommentRequest struct {
	CommentID string `query:"comment_id" binding:"required,uuid"`
	Sort      int    `query:"sort" binding:"omitempty,oneof=0 1"` // 顶层列表排序：0=点赞倒序(默认), 1=时间倒序
}

// LocateComment GET /comment/locate
//
// 通知点击直达评论的定位接口（设计见 docs/comment-locate-design.md）。访客可读。
// reply_sort 手动解析（缺省=1 时间倒序，对齐前端回复列表当前「最新」用法）：
// 前端拉回复用哪个 sort，这里就须传哪个，否则回复游标无效。
// 回复页大小固定取服务端默认值（同 GetReplies 缺省 limit=10），前端不传 limit。
func (h *Handler) LocateComment(c appctx.AppContext) {
	var req LocateCommentRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	commentID, err := uuid.Parse(req.CommentID)
	if err != nil {
		httputil.BadRequest(c, "Invalid comment_id")
		return
	}

	replySort := 1
	switch raw := c.Query("reply_sort"); raw {
	case "":
		// 缺省：最新（时间倒序）
	case "0", "1":
		replySort = int(raw[0] - '0')
	default:
		httputil.BadRequest(c, "Invalid reply_sort")
		return
	}

	result, err := h.svc.LocateComment(c, commentID, req.Sort, replySort)
	if err != nil {
		if errors.Is(err, domain.ErrCommentNotFound) {
			httputil.NotFound(c, "评论不存在或已删除")
			return
		}
		logger.Log.Error("Failed to locate comment: " + err.Error())
		httputil.InternalError(c, "Failed to locate comment")
		return
	}
	httputil.Success(c, result)
}

// GetCommentDetail GET /comment/detail/:id
func (h *Handler) GetCommentDetail(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	commentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.BadRequest(c, "Invalid comment id")
		return
	}

	vo, err := h.svc.GetCommentDetail(c, userID, commentID)
	if err != nil {
		if errors.Is(err, domain.ErrCommentNotFound) {
			httputil.NotFound(c, "Comment not found")
			return
		}
		logger.Log.Error("Failed to get comment: " + err.Error())
		httputil.InternalError(c, "Failed to get comment")
		return
	}
	httputil.Success(c, vo)
}

// ===== 辅助函数 =====

// parseMentionUserIDs 解析 @提及 用户 ID 列表；任一非法写 400 并返回 ok=false。
func parseMentionUserIDs(c appctx.AppContext, raw []string) ([]uuid.UUID, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			httputil.BadRequest(c, "Invalid mention user id: "+s)
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

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

// requireUserIDAllowAnon 尝试返回 userID，但允许匿名（未登录返回 uuid.Nil, true）。
//
// 用于"评论列表/详情/回复"这类访客可读接口：登录时回填 is_liked 标记；匿名时 liked=false。
// 路由组挂 OptionalLogin（见 routes.go），故此处匿名是合法路径，不写 401。
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

// writeCommentError 把 service 层错误映射到 HTTP 响应。
func writeCommentError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrPostNotFound):
		httputil.NotFound(c, "Post not found")
	case errors.Is(err, domain.ErrPostNotCommentable):
		httputil.Forbidden(c, "Cannot comment on this post")
	case errors.Is(err, domain.ErrPostLocked):
		httputil.Forbidden(c, "This post is locked, comments are not allowed")
	case errors.Is(err, domain.ErrRootCommentNotFound):
		httputil.NotFound(c, "Root comment not found")
	case errors.Is(err, domain.ErrRootCommentMismatch):
		httputil.BadRequest(c, "Root comment does not belong to this post")
	case errors.Is(err, domain.ErrReplyTargetNotFound):
		httputil.NotFound(c, "Reply target comment not found")
	case errors.Is(err, domain.ErrReplyTargetNotInThread):
		httputil.BadRequest(c, "Reply target does not belong to the same thread")
	case errors.Is(err, domain.ErrEmptyContent):
		httputil.BadRequest(c, "Comment content is empty")
	default:
		logger.Log.Error("comment service error: " + err.Error())
		httputil.InternalError(c, "Failed to create comment")
	}
}
