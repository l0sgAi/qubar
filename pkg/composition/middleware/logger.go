package middleware

import (
	"context"
	"time"

	"interestBar/pkg/logger"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"
)

// Logger 访问日志中间件。
//
// 从 pkg/server/router/middleware/log.go 迁移（gin→hertz）。
// 差异：hertz 无 c.Errors 等价物，本字段省略（如需可后续接入 hlog）。
func Logger() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		path := string(c.Request.URI().Path())
		if isProbePath(path) {
			c.Next(ctx) // 探针高频调用，不记访问日志
			return
		}
		query := string(c.Request.URI().QueryString())

		c.Next(ctx)

		cost := time.Since(start)
		logger.Log.Info(path,
			zap.Int("status", c.Response.StatusCode()),
			zap.String("method", string(c.Method())),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
			zap.String("user-agent", string(c.Request.Header.UserAgent())),
			zap.Duration("cost", cost),
		)
	}
}

// isProbePath 存活 / 就绪探针路径（与 pkg/server/health 保持一致；
// middleware 不反向依赖 server 包，故此处重复声明）。
func isProbePath(path string) bool {
	return path == "/healthz" || path == "/readyz"
}
