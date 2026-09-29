// Package health 提供存活 / 就绪探针（l0sgAi/qubar#51）。
//
//	GET /healthz  进程存活即 200，不检查依赖（供重启判定，依赖抖动不应触发重启）。
//	GET /readyz   必需依赖（PG / Redis）全部可用才 200，否则 503（供负载均衡摘流 / 发布等待）；
//	              可选依赖（ES 等）只报告状态，不影响结果（它们在启动时本就是软依赖）。
//
// 响应体只给 ok/fail，不回显错误详情（可能含内网地址），详情写日志。
package health

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"interestBar/pkg/logger"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
)

const (
	PathLive  = "/healthz"
	PathReady = "/readyz"

	defaultTimeout = time.Second
)

// Check 单项依赖检查；返回 nil 表示可用。
type Check func(ctx context.Context) error

// Checker 就绪检查集合。
type Checker struct {
	Timeout  time.Duration    // 每项检查超时，<=0 用 1s
	Required map[string]Check // 任一失败 → 503
	Optional map[string]Check // 仅报告
}

// Report /readyz 响应体。
type Report struct {
	Status string            `json:"status"` // ok | unavailable
	Checks map[string]string `json:"checks"` // name -> ok | fail
}

// Ready 并行执行全部检查。
func (c *Checker) Ready(ctx context.Context) (bool, Report) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	type result struct {
		name     string
		required bool
		err      error
	}
	var (
		wg      sync.WaitGroup
		results = make(chan result, len(c.Required)+len(c.Optional))
	)
	run := func(name string, required bool, check Check) {
		defer wg.Done()
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		results <- result{name: name, required: required, err: check(cctx)}
	}
	for name, check := range c.Required {
		wg.Add(1)
		go run(name, true, check)
	}
	for name, check := range c.Optional {
		wg.Add(1)
		go run(name, false, check)
	}
	wg.Wait()
	close(results)

	ok := true
	rep := Report{Status: "ok", Checks: make(map[string]string)}
	var failed []result
	for r := range results {
		if r.err == nil {
			rep.Checks[r.name] = "ok"
			continue
		}
		rep.Checks[r.name] = "fail"
		failed = append(failed, r)
		if r.required {
			ok = false
		}
	}
	if !ok {
		rep.Status = "unavailable"
	}
	// 必需依赖失败记 WARN；可选依赖失败记 DEBUG（探针高频，ES 长期不可用时避免刷屏）。
	if logger.Log != nil {
		sort.Slice(failed, func(i, j int) bool { return failed[i].name < failed[j].name })
		for _, r := range failed {
			msg := "readiness check failed: " + r.name + ": " + r.err.Error()
			if r.required {
				logger.Log.Warn(msg)
			} else {
				logger.Log.Debug(msg)
			}
		}
	}
	return ok, rep
}

// Register 在 hertz engine 上挂载 /healthz 与 /readyz（无需登录）。
func Register(h *server.Hertz, c *Checker) {
	h.GET(PathLive, func(_ context.Context, rc *app.RequestContext) {
		rc.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	h.GET(PathReady, func(ctx context.Context, rc *app.RequestContext) {
		ok, rep := c.Ready(ctx)
		code := http.StatusOK
		if !ok {
			code = http.StatusServiceUnavailable
		}
		rc.JSON(code, rep)
	})
}

// IsProbePath 探针路径（访问日志中间件据此跳过，避免刷屏）。
func IsProbePath(path string) bool {
	return path == PathLive || path == PathReady
}
