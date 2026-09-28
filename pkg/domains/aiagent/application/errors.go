package application

import (
	"errors"
)

// 领域层可识别错误（handler 经谓词映射为 HTTP 响应）。
var (
	errNotAdmin         = errors.New("admin role required")
	errAgentNotFound    = errors.New("agent not found")
	errAgentNameExists  = errors.New("agent name already exists")
	errInvalidName      = errors.New("agent name must be 1-50 chars")
	errInvalidProtocol  = errors.New("api_protocol must be openai/anthropic")
	errInvalidBaseURL   = errors.New("base_url must be a public https URL")
	errInvalidModel     = errors.New("model must be 1-100 chars")
	errInvalidTrigger   = errors.New("trigger_mode must be 1/2/3; mode 2 requires keywords")
	errInvalidLLMParams = errors.New("llm_params has invalid key/value")
	errInvalidRateLimit = errors.New("rate limit values must be >= 0")
	errInvalidStatus    = errors.New("status must be 0 or 1")
	errInvalidFilter    = errors.New("filter_prompt must be <= 2000 chars")
	errAPIKeyNotSet     = errors.New("data key not configured (security.data_key)")
	errNoFieldsToUpdate = errors.New("at least one field to update")
)

// 回复执行链路错误（reply_service）。
var (
	errAgentDisabled    = errors.New("agent is disabled")
	errNotManualMode    = errors.New("agent is not in manual trigger mode")
	errPostNotReplyable = errors.New("post not replyable (not published or locked)")
	errRateLimited      = errors.New("agent reply rate limited")
	errSkippedByFilter  = errors.New("reply skipped by classifier")
	errLLMCall          = errors.New("llm call failed")
	// errPostNotInAgentCircle 圈内手动触发的帖子不属于机器人所在圈（跨圈防刷：
	// 不允许拿本圈机器人的计费凭据去回它圈帖子）。handler 映射 404，
	// 不暴露它圈帖子与机器人的存在性。
	errPostNotInAgentCircle = errors.New("post does not belong to the agent's circle")
)

// 圈内机器人管理（CircleAgentService）的错误。
var (
	errNotCircleAdmin   = errors.New("circle admin privileges required")
	errNotCircleOwner   = errors.New("circle owner privileges required")
	errCircleAgentLimit = errors.New("circle agent limit exceeded")
)

// 错误判断谓词（供 handler 层使用）。
func IsNotAdminErr(err error) bool         { return errors.Is(err, errNotAdmin) }
func IsAgentNotFoundErr(err error) bool    { return errors.Is(err, errAgentNotFound) }
func IsAgentNameExistsErr(err error) bool  { return errors.Is(err, errAgentNameExists) }
func IsInvalidNameErr(err error) bool      { return errors.Is(err, errInvalidName) }
func IsInvalidProtocolErr(err error) bool  { return errors.Is(err, errInvalidProtocol) }
func IsInvalidBaseURLErr(err error) bool   { return errors.Is(err, errInvalidBaseURL) }
func IsInvalidModelErr(err error) bool     { return errors.Is(err, errInvalidModel) }
func IsInvalidTriggerErr(err error) bool   { return errors.Is(err, errInvalidTrigger) }
func IsInvalidLLMParamsErr(err error) bool { return errors.Is(err, errInvalidLLMParams) }
func IsInvalidRateLimitErr(err error) bool { return errors.Is(err, errInvalidRateLimit) }
func IsInvalidStatusErr(err error) bool    { return errors.Is(err, errInvalidStatus) }
func IsInvalidFilterErr(err error) bool    { return errors.Is(err, errInvalidFilter) }
func IsAPIKeyNotSetErr(err error) bool     { return errors.Is(err, errAPIKeyNotSet) }
func IsNoFieldsToUpdateErr(err error) bool { return errors.Is(err, errNoFieldsToUpdate) }

// 回复执行链路错误谓词（供 handler 映射手动触发接口的响应）。
func IsAgentDisabledErr(err error) bool    { return errors.Is(err, errAgentDisabled) }
func IsNotManualModeErr(err error) bool    { return errors.Is(err, errNotManualMode) }
func IsPostNotReplyableErr(err error) bool { return errors.Is(err, errPostNotReplyable) }
func IsRateLimitedErr(err error) bool      { return errors.Is(err, errRateLimited) }
func IsSkippedByFilterErr(err error) bool  { return errors.Is(err, errSkippedByFilter) }
func IsLLMCallErr(err error) bool          { return errors.Is(err, errLLMCall) }

// IsPostNotInAgentCircleErr 圈内手动触发的帖子不属于机器人所在圈（handler 映射 404）。
func IsPostNotInAgentCircleErr(err error) bool { return errors.Is(err, errPostNotInAgentCircle) }

// 圈内机器人管理错误谓词（供 handler 层使用）。
func IsNotCircleAdminErr(err error) bool   { return errors.Is(err, errNotCircleAdmin) }
func IsNotCircleOwnerErr(err error) bool   { return errors.Is(err, errNotCircleOwner) }
func IsCircleAgentLimitErr(err error) bool { return errors.Is(err, errCircleAgentLimit) }
