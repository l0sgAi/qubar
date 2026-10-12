package router

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/composition"
	"interestBar/pkg/composition/hertzadapter"
	"interestBar/pkg/composition/middleware"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/health"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/storage/elasticsearch"
	"interestBar/pkg/server/storage/redis"

	"github.com/cloudwego/hertz/pkg/app/server"
)

// InitRouter 创建并装配 hertz server，返回 *server.Hertz。
//
// 不在这里调用 Spin/Run（阻塞），由调用方决定运行方式，便于资源清理编排。
// server.Default() 已自带 Recovery；这里额外挂 Logger 和 CORS。
func InitRouter() *server.Hertz {
	h := server.Default(
		server.WithHostPorts(fmt.Sprintf(":%d", conf.Config.Server.Port)),
		// hertz 默认 MaxRequestBodySize=4MB，会截断图片上传请求体导致 FormFile 报错返 400。
		// 提到 50MB；具体文件大小仍由 service 层 ValidateFile 兜底校验。
		server.WithMaxRequestBodySize(50<<20),
	)

	// Middleware
	h.Use(middleware.Logger())
	h.Use(middleware.CORS())

	// 存活 / 就绪探针（#51）：无需登录，访问日志中间件跳过。
	health.Register(h, readinessChecker())

	// Register Domain Routes（所有领域已搬迁到 pkg/domains/）
	// 入口层做 engine→RouterGroup 的框架无关包装。
	streamHub := composition.RegisterDomainRoutes(hertzadapter.ForEngine(h))

	// SSE 未读数推流（设计 docs/design/sse-notification-design.md §四#6）：
	// 裸 hertz 路由（SSE 需 hijack writer，不走 AppContext 抽象）；
	// 挂全局 CORS 之后；鉴权在 handler 内自做（header→query 兜底），不走 RequireLogin。
	if conf.Config.NoticeStream.Enabled {
		h.GET("/notice/stream", composition.ServeNoticeStream(streamHub))
	}

	if logger.Log != nil {
		logger.Log.Info("router register success")
	}
	return h
}

// readinessChecker PG / Redis 为必需依赖（启动时即 fatal 的依赖）；ES 为可选（启动时即软依赖）。
func readinessChecker() *health.Checker {
	return &health.Checker{
		Required: map[string]health.Check{
			"postgres": func(ctx context.Context) error {
				if pgsql.DB == nil {
					return errors.New("not initialized")
				}
				sqlDB, err := pgsql.DB.DB()
				if err != nil {
					return err
				}
				return sqlDB.PingContext(ctx)
			},
			"redis": func(ctx context.Context) error {
				if redis.Client == nil {
					return errors.New("not initialized")
				}
				return redis.Client.Ping(ctx).Err()
			},
		},
		Optional: map[string]health.Check{
			"elasticsearch": func(ctx context.Context) error {
				if elasticsearch.Client == nil {
					return errors.New("not initialized")
				}
				res, err := elasticsearch.Client.Ping(elasticsearch.Client.Ping.WithContext(ctx))
				if err != nil {
					return err
				}
				defer res.Body.Close()
				if res.IsError() {
					return errors.New(res.Status())
				}
				return nil
			},
		},
	}
}
