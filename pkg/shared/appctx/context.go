// Package appctx 定义与 Web 框架无关的应用请求上下文抽象。
//
// 设计目的：
//   - 业务层（领域 service / handler）只依赖 AppContext 接口，不直接 import
//     gin/hertz，实现"框架无关"，为后续 DDD 领域拆分和 hertz 迁移铺路。
//   - AppContext 同时承载"HTTP 请求/响应"与"业务上下文"（当前登录用户 ID 等），
//     避免把 *gin.Context 这类框架类型泄漏到业务层。
//
// 实现方：pkg/shared/appctx/hertzadapter 提供 hertz 版实现。
package appctx

import (
	"context"
	"mime/multipart"

	"github.com/google/uuid"
)

// AppContext 是一次 HTTP 请求对应的业务上下文。
//
// 它同时实现了标准库 context.Context，可在业务代码中直接作为 ctx 透传。
// 框架无关：不暴露任何 gin/hertz 类型。
type AppContext interface {
	context.Context

	// ---- 请求信息 ----

	// Method 返回 HTTP 方法（GET/POST/...）。
	Method() string
	// Path 返回请求路径（不含 query）。
	Path() string
	// Param 读取路由路径参数，如 /circle/detail/:id 中的 :id。
	Param(name string) string
	// Query 读取 URL query 参数。
	Query(name string) string
	// Header 读取请求头。
	Header(name string) string
	// ClientIP 返回客户端 IP（框架按 X-Forwarded-For / X-Real-IP / 远端地址解析）。
	ClientIP() string
	// PostForm 读取表单字段（multipart 或 application/x-www-form-urlencoded）。
	PostForm(name string) string
	// FormFile 读取 multipart 上传的单个文件。
	// 返回的是标准库 *multipart.FileHeader，不绑定任何 Web 框架类型。
	FormFile(name string) (*multipart.FileHeader, error)
	// MultipartForm 读取整个 multipart 表单（含多文件）。
	MultipartForm() (*multipart.Form, error)

	// ---- 请求体绑定 ----

	// BindJSON 将 JSON body 绑定到 v，并执行 validator 校验。
	BindJSON(v any) error
	// BindQuery 将 URL query 绑定到 v（使用 `form` tag）。
	BindQuery(v any) error

	// ---- 响应 ----

	// JSON 写入 HTTP 响应：状态码 + JSON 序列化的 v。
	JSON(code int, v any)
	// Redirect 发送 HTTP 重定向（302/307 等）到给定 URL。
	// 用于 OAuth 回调等"需要跳转前端"的场景。
	Redirect(code int, url string)
	// SetHeader 设置响应头。
	SetHeader(key, value string)
	// Abort 终止后续中间件/handler 的执行。
	//
	// 通常在鉴权中间件写完错误响应后调用，防止链上后续 handler 再次写响应。
	// hertz 实现映射到 RequestContext.Abort()。
	Abort()

	// ---- 业务上下文（由鉴权中间件填充）----

	// UserID 返回当前登录用户 ID（UUIDv7）。第二个返回值表示是否已登录。
	// 未登录时返回 uuid.Nil, false。
	UserID() (uuid.UUID, bool)
	// SetUserID 由鉴权中间件调用，写入当前登录用户 ID。
	SetUserID(id uuid.UUID)
	// LoginID 返回当前登录主体的字符串 ID（sa-token 的 loginID）。
	LoginID() (string, bool)
	// SetLoginID 由鉴权中间件调用，写入 loginID。
	SetLoginID(id string)
	// Device 返回登录设备标识，未设置时返回空串。
	Device() string
	// SetDevice 设置登录设备标识。
	SetDevice(device string)
}
