// Package hertzadapter 提供 AppContext 接口的 hertz 实现。
//
// 它把 *app.RequestContext 包装成 appctx.AppContext，让业务层只依赖 AppContext，
// 不直接 import hertz。这是 gin→hertz 迁移后的唯一 AppContext 实现。
//
// 注意 hertz 的若干 API 与 gin 不同：
//   - Method/Path/GetHeader 返回 []byte，这里统一转成 string；
//   - RequestContext 是值类型，用指针持有；
//   - 业务上下文用 c.Set/c.Get（hertz 支持，跨 handler 存活）。
package hertzadapter

import (
	"context"
	"mime/multipart"

	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// hertzAppContext 把 *app.RequestContext 适配为 appctx.AppContext。
type hertzAppContext struct {
	c *app.RequestContext
	// 内嵌 context：用请求的 ctx 作为标准库 context.Context 的来源
	context.Context
}

// New 把 *app.RequestContext 包装为 AppContext。
//
// ctx 是 hertz handler 签名里的 context.Context（请求级，含 trace/超时）。
func New(ctx context.Context, c *app.RequestContext) appctx.AppContext {
	return &hertzAppContext{
		c:       c,
		Context: ctx,
	}
}

// ---- 请求信息 ----

func (h *hertzAppContext) Method() string              { return string(h.c.Method()) }
func (h *hertzAppContext) Path() string                { return string(h.c.Request.URI().Path()) }
func (h *hertzAppContext) Param(name string) string    { return h.c.Param(name) }
func (h *hertzAppContext) Query(name string) string    { return string(h.c.Query(name)) }
func (h *hertzAppContext) Header(name string) string   { return string(h.c.GetHeader(name)) }
func (h *hertzAppContext) PostForm(name string) string { return string(h.c.PostForm(name)) }
func (h *hertzAppContext) ClientIP() string            { return h.c.ClientIP() }

// FormFile 读取 multipart 上传的单个文件，映射到 RequestContext.FormFile。
func (h *hertzAppContext) FormFile(name string) (*multipart.FileHeader, error) {
	fh, err := h.c.FormFile(name)
	if err != nil {
		// 临时诊断：gin→hertz 迁移后 /upload/image 报 400，真实 err 被上层吞掉。
		// 打印 Content-Type / Content-Length / method / 真实错误，并枚举客户端实际发送的字段名，定位根因。
		fileFields, valueFields := []string{}, []string{}
		if form, ferr := h.c.MultipartForm(); ferr == nil && form != nil {
			for k := range form.File {
				fileFields = append(fileFields, k)
			}
			for k := range form.Value {
				valueFields = append(valueFields, k)
			}
		}
		logger.Log.Error("FormFile diag",
			zap.String("field", name),
			zap.String("content-type", string(h.c.GetHeader("Content-Type"))),
			zap.Int("content-length", h.c.Request.Header.ContentLength()),
			zap.String("method", string(h.c.Method())),
			zap.Strings("file-fields-sent", fileFields),
			zap.Strings("value-fields-sent", valueFields),
			zap.Error(err),
		)
	}
	return fh, err
}

// MultipartForm 读取整个 multipart 表单，映射到 RequestContext.MultipartForm。
func (h *hertzAppContext) MultipartForm() (*multipart.Form, error) {
	return h.c.MultipartForm()
}

// ---- 请求体绑定 ----

func (h *hertzAppContext) BindJSON(v any) error {
	return h.c.BindJSON(v)
}

// BindQuery 只绑定 URL query string 到 v（依赖 `query` tag）。
//
// 用 hertz 原生 BindQuery：tag="query" != "" 时跳过 preBindBody，绝不碰请求体，
// 且只认 `query` tag（不认 `form`/`header`/`path`）。语义等价 gin ShouldBindQuery。
//
// 注意：调用方的 Request struct 字段必须打 `query:"..."` tag，否则字段绑不到值。
// （gin 时代用的是 `form:` tag，迁移后需统一改为 `query:`。）
func (h *hertzAppContext) BindQuery(v any) error {
	return h.c.BindQuery(v)
}

// ---- 响应 ----

func (h *hertzAppContext) JSON(code int, v any) {
	h.c.JSON(code, v)
}

// Redirect 发送 HTTP 重定向。hertz 的 Redirect 第二参是 []byte。
func (h *hertzAppContext) Redirect(code int, url string) {
	h.c.Redirect(code, []byte(url))
}

func (h *hertzAppContext) SetHeader(key, value string) {
	h.c.Header(key, value)
}

// Abort 终止后续中间件执行，映射到 RequestContext.Abort()。
func (h *hertzAppContext) Abort() {
	h.c.Abort()
}

// ---- 业务上下文 ----

func (h *hertzAppContext) UserID() (uuid.UUID, bool) {
	if v, ok := h.c.Get(userIDKey); ok {
		if id, ok := v.(uuid.UUID); ok && id != uuid.Nil {
			return id, true
		}
	}
	return uuid.Nil, false
}

func (h *hertzAppContext) SetUserID(id uuid.UUID) {
	h.c.Set(userIDKey, id)
}

func (h *hertzAppContext) LoginID() (string, bool) {
	if v, ok := h.c.Get(loginIDKey); ok {
		if id, ok := v.(string); ok && id != "" {
			return id, true
		}
	}
	return "", false
}

func (h *hertzAppContext) SetLoginID(id string) {
	h.c.Set(loginIDKey, id)
}

func (h *hertzAppContext) Device() string {
	if v, ok := h.c.Get(deviceKey); ok {
		if d, ok := v.(string); ok {
			return d
		}
	}
	return ""
}

func (h *hertzAppContext) SetDevice(device string) {
	h.c.Set(deviceKey, device)
}

// hertz RequestContext 里存取业务值的 key。
// 用字符串常量，与 hertz 的 Keys map[string]interface{} 对齐。
const (
	userIDKey  = "appctx:user_id"
	loginIDKey = "appctx:login_id"
	deviceKey  = "appctx:device"
)

// compile-time guard: 确保 hertzAppContext 实现了 AppContext 接口。
var _ appctx.AppContext = (*hertzAppContext)(nil)
