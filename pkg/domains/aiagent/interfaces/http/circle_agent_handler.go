// circle_agent_handler.go 圈子级 AI 机器人管理的 HTTP 接口层。
//
// 鉴权：路由组挂 authCheck（登录）；圈内角色校验（admin+/owner 字段分级）在
// service 层统一做，handler 只 bind + 调 service + 映射错误（403/404/409）。
// api_key 明文/密文永不回显（同全局 agent，toVO 只回掩码）。
package http

import (
	"errors"

	"interestBar/pkg/domains/aiagent/application"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// CircleAgentHandler 圈子级 AI 机器人管理 HTTP 处理器。
type CircleAgentHandler struct {
	svc      application.CircleAgentService
	replySvc application.ReplyService
}

// NewCircleAgentHandler 构造 CircleAgentHandler（replySvc 供圈内手动触发回复）。
func NewCircleAgentHandler(svc application.CircleAgentService, replySvc application.ReplyService) *CircleAgentHandler {
	return &CircleAgentHandler{svc: svc, replySvc: replySvc}
}

// createCircleAgentReq 创建圈内机器人请求（字段定义同全局 createAgentReq，仅多 circle_id）。
type createCircleAgentReq struct {
	CircleID          string                 `json:"circle_id"`
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
	Status            *int                   `json:"status"`
}

// listCircleAgentsReq 列表查询参数（hertz BindQuery 只认 query tag）。
type listCircleAgentsReq struct {
	CircleID string `query:"circle_id"`
	Page     int    `query:"page"`
	Size     int    `query:"size"`
	Keyword  string `query:"keyword"`
}

// CreateCircleAgent POST /circle/agent 创建圈内机器人。
func (h *CircleAgentHandler) CreateCircleAgent(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req createCircleAgentReq
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request body")
		return
	}
	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle id")
		return
	}
	vo, err := h.svc.CreateCircleAgent(c, userID, circleID, application.CreateAgentInput{
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
		writeCircleAgentError(c, err)
		return
	}
	httputil.Created(c, vo)
}

// ListCircleAgents GET /circle/agent/list 圈内机器人列表（offset 分页，keyword 模糊过滤）。
func (h *CircleAgentHandler) ListCircleAgents(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	var req listCircleAgentsReq
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid query parameters")
		return
	}
	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle id")
		return
	}
	result, err := h.svc.ListCircleAgents(c, userID, circleID, req.Keyword, req.Page, req.Size)
	if err != nil {
		writeCircleAgentError(c, err)
		return
	}
	httputil.Pagination(c, result.Agents, result.Total, result.Page, result.Size)
}

// GetCircleAgent GET /circle/agent/:id 圈内机器人详情。
func (h *CircleAgentHandler) GetCircleAgent(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	agentID, ok := requireAgentID(c)
	if !ok {
		return
	}
	vo, err := h.svc.GetCircleAgent(c, userID, agentID)
	if err != nil {
		writeCircleAgentError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// UpdateCircleAgent PUT /circle/agent/:id 更新圈内机器人（凭据字段仅圈主，service 校验）。
func (h *CircleAgentHandler) UpdateCircleAgent(c appctx.AppContext) {
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
	vo, err := h.svc.UpdateCircleAgent(c, userID, agentID, application.UpdateAgentInput{
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
		writeCircleAgentError(c, err)
		return
	}
	httputil.Success(c, vo)
}

// DeleteCircleAgent DELETE /circle/agent/:id 软删圈内机器人（仅圈主，service 校验）。
func (h *CircleAgentHandler) DeleteCircleAgent(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}
	agentID, ok := requireAgentID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteCircleAgent(c, userID, agentID); err != nil {
		writeCircleAgentError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, httputil.MsgDeleted, nil)
}

// CircleManualReply POST /circle/agent/:id/reply/:postId 圈主手动触发圈内机器人回复。
//
// 仅该圈圈主 + trigger_mode=3 + 启用中的圈内机器人，且帖子必须属于机器人所在圈；
// 同步执行 LLM 调用，成功返回生成的评论 ID，失败返回错误（失败日志行已照常落库）。
func (h *CircleAgentHandler) CircleManualReply(c appctx.AppContext) {
	userID, ok := requireUserID(c)
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
	commentID, err := h.replySvc.CircleManualReply(c, userID, agentID, postID)
	if err != nil {
		writeReplyError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "回复成功", commentID)
}

// writeCircleAgentError 把圈内机器人管理的错误映射为 HTTP 响应。
func writeCircleAgentError(c appctx.AppContext, err error) {
	switch {
	case application.IsNotCircleAdminErr(err):
		httputil.Forbidden(c, "Circle admin privileges required")
	case application.IsNotCircleOwnerErr(err):
		httputil.Forbidden(c, "Circle owner privileges required")
	case application.IsAgentNotFoundErr(err), errors.Is(err, domain.ErrAgentNotFound):
		// 含跨作用域访问（全局链/圈内链互不暴露存在性）。
		httputil.NotFound(c, "Agent not found")
	case application.IsAgentNameExistsErr(err):
		httputil.Conflict(c, "Agent name already exists in this circle")
	case application.IsCircleAgentLimitErr(err), errors.Is(err, domain.ErrCircleAgentLimit):
		httputil.Conflict(c, "Circle agent limit reached (5)")
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
	case application.IsBaseURLNoDNSErr(err):
		// 保存时 DNS 临时故障（超时 / SERVFAIL），可重试；与「地址不合规」400 区分。
		httputil.ServiceUnavailable(c, err.Error())
	default:
		httputil.InternalError(c)
	}
}
