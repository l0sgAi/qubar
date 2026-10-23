package composition

import (
	"interestBar/pkg/conf"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	sahertz "github.com/sa-tokens/sa-token-go/integrations/hertz"
	"github.com/sa-tokens/sa-token-go/stputil"
)

// authenticate 校验 token 并返回 loginID，供 RequireLogin / OptionalLogin 共用（#131）。
//
// 旧实现先 stputil.IsLogin 再 stputil.GetLoginID：GetLoginID 内部又调一次 IsLogin，
// 每次 IsLogin 还会异步续期（GET + SET KEEPTTL + 3 次 EXPIRE + session GET），
// 一个登录态请求约 19 条 Redis 命令。现在稳态只有 2 条：
//
//   - GetTokenInfo：1 次 GET 完成校验并取 loginID。与 GetLoginID 用同一个检查，
//     不存在（过期 / 登出）返回 key not found，被踢（KICK_OUT）/ 被顶（BE_REPLACED）返回状态错误；
//   - renewIfDue：1 次 TTL 判断是否到续期阈值，到了才续期。
//
// 每个终端的 token 各自独立校验 / 续期，登出、踢人、多端在线语义不变（#106 / #109）。
func authenticate(mgr *sahertz.Manager, token string) (string, bool) {
	if mgr == nil || token == "" {
		return "", false
	}
	info, err := mgr.GetTokenInfo(token)
	if err != nil || info == nil || info.LoginID == "" {
		return "", false
	}
	renewIfDue(mgr, token)
	return info.LoginID, true
}

// renewIfDue 剩余有效期 <= MaxRefresh 时把 token、账号映射、账号 session 的 TTL 重置为 Timeout。
//
// MaxRefresh 由 InitSaToken 设为 Timeout - sa_token.renew_interval，即同一 token 距上次登录 / 续期
// 满 renew_interval 秒才续一次（与 sa-token IsLogin 内置续期用同一个阈值）。
// 续期用 RenewTimeout（只做 EXPIRE，不回写 token 值）：不会把并发踢下线写入的 KICK_OUT 覆盖回有效 token。
// 同步执行：一个 token 每 renew_interval 才轮到一次，省掉 goroutine 也便于测试。
// 续期失败不影响本次请求（token 刚校验通过），下次请求 TTL 仍低于阈值会再续。
func renewIfDue(mgr *sahertz.Manager, token string) {
	cfg := mgr.GetConfig()
	if !cfg.AutoRenew || cfg.Timeout <= 0 {
		return
	}
	ttl, err := mgr.GetTokenTimeout(token)
	if err != nil || ttl <= 0 { // -1 永不过期 / -2 不存在
		return
	}
	if cfg.MaxRefresh > 0 && ttl > cfg.MaxRefresh {
		return
	}
	_ = mgr.RenewTimeout(token, cfg.Timeout)
}

// RequireLogin 是一个框架无关的"需要登录"中间件（routing.HandlerFunc）。
//
// 它替代旧的 sagin.CheckLogin() gin 中间件，但实现方式不同：
//   - sagin.CheckLogin() 依赖 gin 中间件链（c.Next/c.Abort）
//   - RequireLogin 直接用 sa-token manager 校验 token（authenticate），把 loginID 写入 AppContext
//
// 两者行为等价：无 token / token 无效 → 返回 401；有效 → 放行并填充用户信息。
// 区别在于 RequireLogin 不依赖任何 Web 框架的中间件机制，可跨 gin/hertz 复用。
//
// 设计权衡：为何不直接复用 sagin.CheckLogin？
//   - sagin.CheckLogin 返回 gin.HandlerFunc，强绑定 gin；
//   - 把它包成 routing.HandlerFunc 会破坏 c.Next() 语义（它需要作为中间件
//     挂在 gin 路由上，而不是作为普通 handler 被调用）；
//   - 因此在过渡期，我们用 stputil（与框架无关）重新实现等价逻辑。
func RequireLogin(c appctx.AppContext) {
	tokenName := conf.Config.SaToken.TokenName
	token := c.Header(tokenName)

	// 兼容：token 也可能在 cookie 里（与 sagin 行为一致）
	// AppContext 暂未暴露 Cookie 读取，这里仅从 header 取。
	// 若未来需要 cookie 兜底，可在 AppContext 增加 Cookie() 方法。

	if token == "" {
		httputil.Unauthorized(c, "Token not found")
		return
	}

	// 校验 token 并取 loginID（一次读取，见 authenticate）
	loginID, ok := authenticate(stputil.GetManager(), token)
	if !ok {
		httputil.Unauthorized(c, "Invalid or expired token")
		return
	}

	// 写入 AppContext，供后续 handler 使用
	c.SetLoginID(loginID)
	// 注意：不在这里 parse userID，由 handler 按需调用 appctx 或工具函数解析。
	// 这样保持中间件的轻量，也避免引入 uuid.Parse 失败的分支。
}

// RequireLoginWith sagin.CheckLogin 的框架无关等价物（别名，语义更清晰）。
// 保持为一个 routing.HandlerFunc 变量，便于在路由注册时直接传入。
//
// 用法：
//
//	cat := rg.Group("/category", RequireLogin)
var RequireLoginFn = RequireLogin

// OptionalLogin 是"可选登录"中间件：有 token 则解析并写入 loginID，无 token/坏 token 静默放行。
//
// 用于"登录态可选"的读接口（如发现页 GET /discover）：
//   - 登录用户 → 反气泡个性化（推气泡外内容）
//   - 匿名用户 → 纯随机退化（新用户落地页场景）
//
// 与 RequireLogin 的区别：不会因缺/坏 token 返回 401；handler 用 requireUserIDAllowAnon 取 userID。
func OptionalLogin(c appctx.AppContext) {
	tokenName := conf.Config.SaToken.TokenName
	token := c.Header(tokenName)
	if token == "" {
		return // 匿名放行
	}

	// 有 token 但无效（过期/伪造/被踢）→ 也按匿名处理，不 401（发现页容忍匿名）。
	loginID, ok := authenticate(stputil.GetManager(), token)
	if !ok {
		return
	}
	c.SetLoginID(loginID)
}

// OptionalLoginFn 别名，语义更清晰，便于在路由注册时直接传入。
var OptionalLoginFn = OptionalLogin
