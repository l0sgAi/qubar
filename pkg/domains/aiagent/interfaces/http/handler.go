// Package http 提供 aiagent 领域的 HTTP 接口层（handler + 路由注册）。
//
// 鉴权：路由组挂 authCheck（登录）；role=1 管理员校验在 service 层统一做，
// handler 只映射错误（403）。api_key 明文/密文永不回显（见 application.AgentVO）。
package http

import (
	"errors"

	"interestBar/pkg/domains/aiagent/application"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler aiagent 领域 HTTP 处理器。
type Handler struct {
	svc      application.AgentService
	replySvc application.ReplyService
}

// NewHandler 构造 Handler。
func NewHandler(svc application.AgentService, replySvc application.ReplyService) *Handler {
	return &Handler{svc: svc, replySvc: replySvc}
}

// createAgentReq 创建机器人请求。
type createAgentReq struct {
	Name              string                 `json:"name"`
	AvatarURL         string                 `json:"avatar_url"`
	APIProtocol       string                 `json:"api_protocol"`
	BaseURL           string                 `json:"base_url"`
	APIKey            string                 `json:"api_key"`
	Model             string                 `json:"model"`
	LLMParams         map[string]interface{} `json:"llm_params"`
	SystemPrompt      string                 `json:"system_prompt"`
	FilterPrompt      string                 `json:"filter_prompt"`
	TriggerMode       int                    `json:"trigger_mode"`
	TriggerKeywords   []string               `json:"trigger_keywords"`
	MaxRepliesPerHour int                    `json:"max_replies_per_hour"`
	MinIntervalSec    int                    `json:"min_interval_sec"`
	Status            *int                   `json:"status"` // nil=默认启用；显式 0=停用
}

// updateAgentReq 更新机器人请求（指针字段，部分更新）。
type updateAgentReq struct {
	Name              *string                `json:"name"`
	AvatarURL         *string                `json:"avatar_url"`
	APIProtocol       *string                `json:"api_protocol"`
	BaseURL           *string                `json:"base_url"`
	APIKey            *string                `json:"api_key"`
	Model             *string                `json:"model"`
	LLMParams         map[string]interface{} `json:"llm_params"`
	SystemPrompt      *string                `json:"system_prompt"`
	FilterPrompt      *string                `json:"filter_prompt"`
	TriggerMode       *int                   `json:"trigger_mode"`
	TriggerKeywords   []string               `json:"trigger_keywords"`
	MaxRepliesPerHour *int                   `json:"max_replies_per_hour"`
	MinIntervalSec    *int                   `json:"min_interval_sec"`
	Status            *int                   `json:"status"`
}

// listAgentsReq 列表查询参数（hertz BindQuery 只认 query tag）。
type listAgentsReq struct {
	Page    int    `query:"page"`
	Size    int    `query:"size"`
	Keyword string `query:"keyword"`
}

// CreateAgent POST /agent 创建机器人。
func (h *Handler) CreateAgent(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req createAgentReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request body")
		return
	}
	vo, err := h.svc.CreateAgent(c, userID, application.CreateAgentInput{
		Name:              req.Name,
		AvatarURL:         req.AvatarURL,
		APIProtocol:       req.APIProtocol,
		BaseURL:           req.BaseURL,
		APIKey:            req.APIKey,
		Model:             req.Model,
		LLMParams:         req.LLMParams,
		SystemPrompt:      req.SystemPrompt,
		FilterPrompt:      req.FilterPrompt,
		TriggerMode:       req.TriggerMode,
		TriggerKeywords:   req.TriggerKeywords,
		MaxRepliesPerHour: req.MaxRepliesPerHour,
		MinIntervalSec:    req.MinIntervalSec,
		Status:            req.Status,
	})
	if err != nil {
		writeAgentError(c, err)
		return
	}
	httputil.Created(c, vo)
}

// GetAgent GET /agent/:id 机器人详情。
func (h *Handler) GetAgent(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	agentID, ok := requireAgentID(c)
	if !ok {
		return
	}
	vo, err := h.svc.GetAgent(c, userID, agentID)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// ListAgents GET /agent/list 机器人列表（offset 分页，keyword 按 name 模糊过滤）。
func (h *Handler) ListAgents(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req listAgentsReq
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid query parameters")
		return
	}
	result, err := h.svc.ListAgents(c, userID, req.Keyword, req.Page, req.Size)
	if err != nil {
		writeAgentError(c, err)
		return
	}
	httputil.Pagination(c, result.Agents, result.Total, result.Page, result.Size)
}

// UpdateAgent PUT /agent/:id 更新机器人。
func (h *Handler) UpdateAgent(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	agentID, ok := requireAgentID(c)
	if !ok {
		return
	}
	var req updateAgentReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request body")
		return
	}
	vo, err := h.svc.UpdateAgent(c, userID, agentID, application.UpdateAgentInput{
		Name:              req.Name,
		AvatarURL:         req.AvatarURL,
		APIProtocol:       req.APIProtocol,
		BaseURL:           req.BaseURL,
		APIKey:            req.APIKey,
		Model:             req.Model,
		LLMParams:         req.LLMParams,
		SystemPrompt:      req.SystemPrompt,
		FilterPrompt:      req.FilterPrompt,
		TriggerMode:       req.TriggerMode,
		TriggerKeywords:   req.TriggerKeywords,
		MaxRepliesPerHour: req.MaxRepliesPerHour,
		MinIntervalSec:    req.MinIntervalSec,
		Status:            req.Status,
	})
	if err != nil {
		writeAgentError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// DeleteAgent DELETE /agent/:id 软删机器人。
func (h *Handler) DeleteAgent(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	agentID, ok := requireAgentID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteAgent(c, userID, agentID); err != nil {
		writeAgentError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, httputil.MsgDeleted, nil)
}

// ManualReply POST /agent/:id/reply/:postId 管理员手动触发机器人回复。
//
// 仅 trigger_mode=3（手动）且启用中的机器人可触发；同步执行 LLM 调用，
// 成功返回生成的评论 ID，失败返回错误（失败日志行已照常落库）。
func (h *Handler) ManualReply(c appctx.AppContext) {
	adminID, ok := requireUserID(c)
	if !ok {
		return
	}
	agentID, ok := requireAgentID(c)
	if !ok {
		return
	}
	postID, err := uuid.Parse(c.Param("postId"))
	if err != nil {
		httputil.BadRequest(c, "Invalid post id")
		return
	}
	commentID, err := h.replySvc.ManualReply(c, adminID, agentID, postID)
	if err != nil {
		writeReplyError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "回复成功", commentID)
}

// ---- 本地助手 ----

// requireUserID 读取当前登录用户 ID（已过 authCheck，理论上必成功）。
func requireUserID(c appctx.AppContext) (uuid.UUID, bool) {
	loginID, ok := c.LoginID()
	if !ok {
		httputil.Unauthorized(c, "Login required")
		return uuid.Nil, false
	}
	id, err := uuid.Parse(loginID)
	if err != nil {
		httputil.Unauthorized(c, "Invalid login identity")
		return uuid.Nil, false
	}
	return id, true
}

// requireAgentID 解析路径参数 :id 为 UUID。
func requireAgentID(c appctx.AppContext) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.BadRequest(c, "Invalid agent id")
		return uuid.Nil, false
	}
	return id, true
}

// writeReplyError 把手动触发回复的错误映射为 HTTP 响应（全局/圈内两条端点共用）。
func writeReplyError(c appctx.AppContext, err error) {
	switch {
	case application.IsNotAdminErr(err):
		httputil.Forbidden(c, "Admin role required")
	case application.IsNotCircleOwnerErr(err):
		httputil.Forbidden(c, "Circle owner privileges required")
	case application.IsAgentNotFoundErr(err), errors.Is(err, domain.ErrAgentNotFound),
		application.IsPostNotInAgentCircleErr(err):
		// 帖子不属于机器人所在圈同样 404：不暴露它圈帖子与机器人的存在性。
		httputil.NotFound(c, "Agent not found")
	case application.IsAgentDisabledErr(err),
		application.IsNotManualModeErr(err),
		application.IsPostNotReplyableErr(err):
		httputil.BadRequest(c, err.Error())
	case application.IsRateLimitedErr(err):
		httputil.TooManyRequests(c, "Agent reply rate limited")
	case application.IsLLMCallErr(err):
		// 手动触发同步反馈失败原因（含供应商错误摘要），日志行已落库。
		httputil.ServiceUnavailable(c, err.Error())
	default:
		httputil.InternalError(c)
	}
}

// writeAgentError 把 application/domain 错误映射为 HTTP 响应。
func writeAgentError(c appctx.AppContext, err error) {
	switch {
	case application.IsNotAdminErr(err):
		httputil.Forbidden(c, "Admin role required")
	case application.IsAgentNotFoundErr(err), errors.Is(err, domain.ErrAgentNotFound):
		httputil.NotFound(c, "Agent not found")
	case application.IsAgentNameExistsErr(err):
		httputil.Conflict(c, "Agent name already exists")
	case application.IsInvalidNameErr(err),
		application.IsInvalidProtocolErr(err),
		application.IsInvalidBaseURLErr(err),
		application.IsInvalidModelErr(err),
		application.IsInvalidTriggerErr(err),
		application.IsInvalidLLMParamsErr(err),
		application.IsInvalidRateLimitErr(err),
		application.IsInvalidStatusErr(err),
		application.IsInvalidFilterErr(err),
		application.IsNoFieldsToUpdateErr(err):
		httputil.BadRequest(c, err.Error())
	case application.IsAPIKeyNotSetErr(err):
		httputil.ServiceUnavailable(c, "Data key not configured")
	default:
		httputil.InternalError(c)
	}
}
