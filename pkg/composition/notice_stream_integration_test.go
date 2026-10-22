//go:build integration

// notice_stream_integration_test.go GET /notice/stream 真实 TCP 连接回归测试（#127，make test-integration）：
// 客户端断开后立即释放名额（不等心跳写失败）、立即重连成功；超限时踢最旧连接。
// 依赖开发栈 Redis（sa-token 登录态存储），hub 的未读数读取器用桩函数。
package composition

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/composition/middleware"
	"interestBar/pkg/conf"
	noticeapp "interestBar/pkg/domains/notice/application"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/google/uuid"
	sahertz "github.com/sa-tokens/sa-token-go/integrations/hertz"
	saredis "github.com/sa-tokens/sa-token-go/storage/redis"
	"github.com/sa-tokens/sa-token-go/stputil"
)

var (
	streamSaTokenOnce   sync.Once
	streamSaTokenPrefix string
)

// initStreamSaToken 用 testkit 的 Redis（独立测试库）初始化 sa-token 全局 manager（进程内一次）。
func initStreamSaToken(t *testing.T) {
	t.Helper()
	testkit.Redis(t)
	streamSaTokenOnce.Do(func() {
		cfg := sahertz.DefaultConfig()
		cfg.IsConcurrent = true
		cfg.IsShare = false
		cfg.IsPrintBanner = false
		cfg.AutoRenew = false // 避免异步续期在清理后把 key 写回
		cfg.Timeout = 600
		streamSaTokenPrefix = cfg.KeyPrefix
		sahertz.SetManager(sahertz.NewManager(saredis.NewStorageFromClient(redispkg.Client), cfg))
	})
}

// removeRecorder 记录 handler 退出时的 Remove 调用（含被踢连接的 no-op Remove），用于观察名额释放时机。
type removeRecorder struct {
	noticeapp.StreamHub
	removed chan uint64
}

func (r *removeRecorder) Remove(userID uuid.UUID, connID uint64) {
	r.StreamHub.Remove(userID, connID)
	r.removed <- connID
}

// startNoticeStreamServer 按 router.InitRouter 的方式（断开感知 + DetachClientCancel）起一个只挂 SSE 路由的 hertz。
func startNoticeStreamServer(t *testing.T, hub noticeapp.StreamHub) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	h := server.New(
		server.WithListener(ln),
		server.WithSenseClientDisconnection(true),
		server.WithExitWaitTime(time.Second),
		server.WithDisablePrintRoute(true),
	)
	h.Use(middleware.DetachClientCancel())
	h.GET("/notice/stream", ServeNoticeStream(hub))
	go func() { _ = h.Run() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = h.Shutdown(ctx)
	})
	return "http://" + ln.Addr().String()
}

// sseStream 一条客户端 SSE 连接：逐行读响应体，EOF 时关闭 lines。
type sseStream struct {
	cancel context.CancelFunc
	lines  chan string
}

// openStream 建立 SSE 连接，断言 200 并读到首个 unread-count 事件（此时服务端已完成 hub.Add）。
func openStream(t *testing.T, client *http.Client, base, token string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/notice/stream?"+conf.Config.SaToken.TokenName+"="+token, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		_ = resp.Body.Close()
		t.Fatalf("open stream status = %d, want 200", resp.StatusCode)
	}
	s := &sseStream{cancel: cancel, lines: make(chan string, 64)}
	go func() {
		defer close(s.lines)
		defer func() { _ = resp.Body.Close() }()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
	}()
	t.Cleanup(cancel)
	if !s.waitEvent("unread-count", 3*time.Second) {
		t.Fatal("first unread-count event not received")
	}
	return s
}

// waitEvent 在 timeout 内读到 `event: <name>` 返回 true；流结束或超时返回 false。
func (s *sseStream) waitEvent(name string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return false
			}
			if strings.TrimSpace(line) == "event: "+name {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// waitClosed 在 timeout 内服务端结束响应流返回 true。
func (s *sseStream) waitClosed(timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestNoticeStreamDisconnectReleasesSlot(t *testing.T) {
	t.Parallel()
	initStreamSaToken(t)

	// 断开检测须明显快于心跳写失败：心跳周期至少 10s，下面的等待上限都远小于它。
	heartbeat := time.Duration(conf.Config.NoticeStream.HeartbeatSec) * time.Second
	if heartbeat > 0 && heartbeat < 10*time.Second {
		t.Skipf("heartbeat %s too short to distinguish disconnect detection", heartbeat)
	}
	const maxConns = 5
	const released = 2 * time.Second

	inner := noticeapp.NewStreamHub(maxConns, 60*60*1000) // sweeper 不打拍，只验证连接生命周期
	inner.SetCountReader(func(context.Context, uuid.UUID) (int64, error) { return 0, nil })
	t.Cleanup(inner.Stop)
	hub := &removeRecorder{StreamHub: inner, removed: make(chan uint64, 64)}
	base := startNoticeStreamServer(t, hub)

	userID := sharedomain.NewID()
	loginID := userID.String()
	token, err := stputil.Login(loginID, "pc")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	p := streamSaTokenPrefix
	testkit.CleanupKeys(t,
		p+"token:"+token, p+"token-session:"+token, p+"renew:"+token,
		p+"account:"+loginID+":pc", p+"session:"+loginID,
	)

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	streams := make([]*sseStream, maxConns)
	for i := range streams {
		streams[i] = openStream(t, client, base, token)
	}

	// 1. 客户端断开 → 服务端立即感知并 Remove（远早于下次心跳写失败）。
	streams[0].cancel()
	select {
	case <-hub.removed:
	case <-time.After(released):
		t.Fatalf("slot not released within %s after client disconnect", released)
	}

	// 2. 立即重连：200 + 首事件，且没有踢掉任何存活连接（名额是断开释放的，不是挤出来的）。
	streams[0] = openStream(t, client, base, token)
	select {
	case id := <-hub.removed:
		t.Fatalf("reconnect within limit should not evict anyone, got Remove(%d)", id)
	case <-time.After(300 * time.Millisecond):
	}
	for i, s := range streams[1:] {
		if s.waitEvent("conn-evicted", 50*time.Millisecond) {
			t.Fatalf("stream %d should not be evicted on reconnect", i+1)
		}
	}

	// 3. 第 6 条连接：照常建立；最旧的存活连接（streams[1]）收到 conn-evicted 后被服务端断开。
	sixth := openStream(t, client, base, token)
	if !streams[1].waitEvent("conn-evicted", released) {
		t.Fatal("oldest stream should receive conn-evicted")
	}
	if !streams[1].waitClosed(released) {
		t.Fatal("evicted stream should be closed by server")
	}
	select {
	case <-hub.removed:
	case <-time.After(released):
		t.Fatal("evicted handler should exit")
	}
	for i, s := range append(append([]*sseStream{streams[0]}, streams[2:]...), sixth) {
		if s.waitEvent("conn-evicted", 50*time.Millisecond) {
			t.Fatalf("stream #%d (non-oldest) should not be evicted", i)
		}
	}
}
