// Package httputil 提供与 Web 框架无关的 HTTP 响应工具。
//
// 它依赖 appctx.AppContext（而非具体框架的 Context），这样业务代码
// （领域 handler / service）在响应时不绑定 hertz，实现 DDD"领域代码框架无关"。
//
// 响应结构与错误码稳定，迁移 hertz 时本包零改动。
package httputil

import (
	"errors"
	"net/http"

	"interestBar/pkg/shared/appctx"
	sharedomain "interestBar/pkg/shared/domain"
)

// ResponseCode 定义业务响应码（与原 response.ResponseCode 一致）。
type ResponseCode int

const (
	// Success codes
	CodeSuccess ResponseCode = 200 + iota

	// Client error codes (4xx)
	CodeBadRequest
	CodeUnauthorized
	CodeForbidden
	CodeNotFound
	CodeMethodNotAllowed
	CodeRequestTimeout
	CodeConflict
	CodeTooManyRequests
	CodeValidationError

	// Server error codes (5xx)
	CodeInternalError
	CodeNotImplemented
	CodeServiceUnavailable
)

// Response 表示标准 API 响应结构。
type Response struct {
	Code    ResponseCode `json:"code"`
	Message string       `json:"message"`
	Data    interface{}  `json:"data,omitempty"`
}

// PaginationResponse 表示分页响应结构。
type PaginationResponse struct {
	Code    ResponseCode `json:"code"`
	Message string       `json:"message"`
	Data    interface{}  `json:"data,omitempty"`
	Total   int64        `json:"total,omitempty"`
	Page    int          `json:"page,omitempty"`
	PerPage int          `json:"per_page,omitempty"`
}

// 预定义错误消息（与原 response 包完全一致）。
const (
	// Success messages
	MsgSuccess = "Success"
	MsgCreated = "Created successfully"
	MsgUpdated = "Updated successfully"
	MsgDeleted = "Deleted successfully"

	// Error messages
	MsgBadRequest          = "Bad request"
	MsgUnauthorized        = "Authentication required"
	MsgInvalidToken        = "Invalid or expired token"
	MsgForbidden           = "Access forbidden"
	MsgNotFound            = "Resource not found"
	MsgMethodNotAllowed    = "Method not allowed"
	MsgRequestTimeout      = "Request timeout"
	MsgConflict            = "Resource conflict"
	MsgTooManyRequests     = "Too many requests"
	MsgValidationError     = "Validation failed"
	MsgInternalError       = "Internal server error"
	MsgNotImplemented      = "Feature not implemented"
	MsgServiceUnavailable  = "Service unavailable"
	MsgDatabaseError       = "Database error"
	MsgRedisError          = "Cache error"
	MsgInvalidCredentials  = "Invalid credentials"
	MsgUserNotFound        = "User not found"
	MsgUserExists          = "User already exists"
	MsgInvalidEmail        = "Invalid email format"
	MsgInvalidPassword     = "Invalid password"
	MsgEmailAlreadyExists  = "Email already registered"
	MsgTokenRequired       = "Token is required"
	MsgInvalidOTP          = "Invalid verification code"
	MsgOTPExpired          = "Verification code expired"
	MsgAccountDisabled     = "Account has been disabled"
	MsgInsufficientBalance = "Insufficient balance"
	MsgInvalidParameter    = "Invalid parameter"
	MsgMissingParameter    = "Missing required parameter"
	MsgInvalidFormat       = "Invalid format"
	MsgRateLimitExceeded   = "Rate limit exceeded"
	MsgCSRFTokenRequired   = "CSRF token is required"
	MsgInvalidCSRFToken    = "Invalid CSRF token"
	MsgOriginNotAllowed    = "Origin not allowed"
	MsgSessionExpired      = "Session has expired"
	MsgLoginRequired       = "Please login first"
	MsgPermissionDenied    = "Permission denied"
	MsgOperationFailed     = "Operation failed"
)

// codeMessage 把响应码映射到默认消息。
var codeMessage = map[ResponseCode]string{
	CodeSuccess:            MsgSuccess,
	CodeBadRequest:         MsgBadRequest,
	CodeUnauthorized:       MsgUnauthorized,
	CodeForbidden:          MsgForbidden,
	CodeNotFound:           MsgNotFound,
	CodeMethodNotAllowed:   MsgMethodNotAllowed,
	CodeRequestTimeout:     MsgRequestTimeout,
	CodeConflict:           MsgConflict,
	CodeTooManyRequests:    MsgTooManyRequests,
	CodeValidationError:    MsgValidationError,
	CodeInternalError:      MsgInternalError,
	CodeNotImplemented:     MsgNotImplemented,
	CodeServiceUnavailable: MsgServiceUnavailable,
}

// httpStatusMap 把响应码映射到 HTTP 状态码。
var httpStatusMap = map[ResponseCode]int{
	CodeSuccess:            http.StatusOK,
	CodeBadRequest:         http.StatusBadRequest,
	CodeUnauthorized:       http.StatusUnauthorized,
	CodeForbidden:          http.StatusForbidden,
	CodeNotFound:           http.StatusNotFound,
	CodeMethodNotAllowed:   http.StatusMethodNotAllowed,
	CodeRequestTimeout:     http.StatusRequestTimeout,
	CodeConflict:           http.StatusConflict,
	CodeTooManyRequests:    http.StatusTooManyRequests,
	CodeValidationError:    http.StatusBadRequest,
	CodeInternalError:      http.StatusInternalServerError,
	CodeNotImplemented:     http.StatusNotImplemented,
	CodeServiceUnavailable: http.StatusServiceUnavailable,
}

// GetHTTPStatus 返回响应码对应的 HTTP 状态码。
func GetHTTPStatus(code ResponseCode) int {
	if status, ok := httpStatusMap[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// GetMessage 返回响应码的默认消息。
func GetMessage(code ResponseCode) string {
	if msg, ok := codeMessage[code]; ok {
		return msg
	}
	return "Unknown error"
}

// ============ 响应助手函数（接收 appctx.AppContext） ============

// Success 发送成功响应。
func Success(c appctx.AppContext, data interface{}) {
	c.JSON(GetHTTPStatus(CodeSuccess), Response{
		Code:    CodeSuccess,
		Message: GetMessage(CodeSuccess),
		Data:    data,
	})
}

// SuccessWithMessage 发送带自定义消息的成功响应。
func SuccessWithMessage(c appctx.AppContext, message string, data interface{}) {
	c.JSON(GetHTTPStatus(CodeSuccess), Response{
		Code:    CodeSuccess,
		Message: message,
		Data:    data,
	})
}

// Created 发送 201 Created 响应。
func Created(c appctx.AppContext, data interface{}) {
	c.JSON(GetHTTPStatus(CodeSuccess), Response{
		Code:    CodeSuccess,
		Message: MsgCreated,
		Data:    data,
	})
}

// Error 发送错误响应。
//
// 写完响应后会调用 c.Abort()，与 ErrorWithMessage / ErrorWithData 保持一致，
// 避免后续中间件/handler 继续写入造成重复响应。
func Error(c appctx.AppContext, code ResponseCode) {
	c.JSON(GetHTTPStatus(code), Response{
		Code:    code,
		Message: GetMessage(code),
	})
	c.Abort()
}

// ErrorWithMessage 发送带自定义消息的错误响应。
//
// 写完响应后会调用 c.Abort()，终止后续中间件/handler 执行。
// 这一点与旧 response 包（依赖 gin 中间件链）行为一致：错误响应后
// 不应继续执行业务 handler。
func ErrorWithMessage(c appctx.AppContext, code ResponseCode, message string) {
	c.JSON(GetHTTPStatus(code), Response{
		Code:    code,
		Message: message,
	})
	c.Abort()
}

// ErrorWithData 发送带数据的错误响应。写完后调用 c.Abort()。
func ErrorWithData(c appctx.AppContext, code ResponseCode, message string, data interface{}) {
	c.JSON(GetHTTPStatus(code), Response{
		Code:    code,
		Message: message,
		Data:    data,
	})
	c.Abort()
}

// BadRequest 发送 400 Bad Request 响应。
func BadRequest(c appctx.AppContext, message ...string) {
	msg := MsgBadRequest
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeBadRequest, msg)
}

// Unauthorized 发送 401 Unauthorized 响应。
func Unauthorized(c appctx.AppContext, message ...string) {
	msg := MsgUnauthorized
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeUnauthorized, msg)
}

// Forbidden 发送 403 Forbidden 响应。
func Forbidden(c appctx.AppContext, message ...string) {
	msg := MsgForbidden
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeForbidden, msg)
}

// NotFound 发送 404 Not Found 响应。
func NotFound(c appctx.AppContext, message ...string) {
	msg := MsgNotFound
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeNotFound, msg)
}

// ValidationError 发送校验错误响应。
func ValidationError(c appctx.AppContext, message ...string) {
	msg := MsgValidationError
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeValidationError, msg)
}

// InternalError 发送 500 Internal Server Error 响应。
func InternalError(c appctx.AppContext, message ...string) {
	msg := MsgInternalError
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeInternalError, msg)
}

// SearchError 搜索类接口的统一错误出口：搜索后端过载（暂时错误）返回 503，其余 500。
func SearchError(c appctx.AppContext, err error, internalMsg string) {
	if errors.Is(err, sharedomain.ErrSearchOverloaded) {
		ServiceUnavailable(c, "Search service is busy, please retry later")
		return
	}
	InternalError(c, internalMsg)
}

// ServiceUnavailable 发送 503 Service Unavailable 响应。
func ServiceUnavailable(c appctx.AppContext, message ...string) {
	msg := MsgServiceUnavailable
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeServiceUnavailable, msg)
}

// Conflict 发送 409 Conflict 响应。
func Conflict(c appctx.AppContext, message ...string) {
	msg := MsgConflict
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeConflict, msg)
}

// TooManyRequests 发送 429 Too Many Requests 响应。
func TooManyRequests(c appctx.AppContext, message ...string) {
	msg := MsgTooManyRequests
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeTooManyRequests, msg)
}

// Pagination 发送分页响应。
func Pagination(c appctx.AppContext, data interface{}, total int64, page int, perPage int) {
	c.JSON(GetHTTPStatus(CodeSuccess), PaginationResponse{
		Code:    CodeSuccess,
		Message: GetMessage(CodeSuccess),
		Data:    data,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}

// PaginationWithMessage 发送带自定义消息的分页响应。
func PaginationWithMessage(c appctx.AppContext, message string, data interface{}, total int64, page int, perPage int) {
	c.JSON(GetHTTPStatus(CodeSuccess), PaginationResponse{
		Code:    CodeSuccess,
		Message: message,
		Data:    data,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}
