//go:build integration

// sa_token_session_integration_test.go saTokenSessionImpl 对开发栈 Redis 的集成测试（make test-integration）。
package infrastructure_test

import (
	"sync"
	"testing"

	"interestBar/pkg/domains/auth/domain"
	"interestBar/pkg/domains/auth/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	sahertz "github.com/sa-tokens/sa-token-go/integrations/hertz"
	saredis "github.com/sa-tokens/sa-token-go/storage/redis"
	"github.com/sa-tokens/sa-token-go/stputil"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var (
	saTokenOnce   sync.Once
	saTokenPrefix string
)

// initSaToken 用 testkit 的 Redis 连接（独立测试库）初始化 sa-token 全局 manager。
// stputil 的 manager 是包级单例，本测试进程只初始化一次；其他包的测试是独立进程，互不影响。
// 并发 / 共享配置与 pkg/server/auth/sa_token_init.go 一致（IsConcurrent=true、IsShare=false）。
func initSaToken(t *testing.T) {
	t.Helper()
	testkit.Redis(t)
	saTokenOnce.Do(func() {
		cfg := sahertz.DefaultConfig()
		cfg.IsConcurrent = true
		cfg.IsShare = false
		cfg.IsPrintBanner = false
		// 关闭自动续期：IsLogin 会起异步 goroutine 续期，可能在 CleanupKeys 之后把 token key 写回来
		cfg.AutoRenew = false
		// 测试数据短 TTL：即使清理失败也会很快过期
		cfg.Timeout = 600
		saTokenPrefix = cfg.KeyPrefix
		sahertz.SetManager(sahertz.NewManager(saredis.NewStorageFromClient(redispkg.Client), cfg))
	})
}

// login 以 loginID 在 device 上登录，并注册该会话涉及的全部 Redis key 的清理。
func login(t *testing.T, sess domain.SaTokenSession, loginID, device string) string {
	t.Helper()
	token, err := sess.Login(loginID, device)
	if err != nil {
		t.Fatalf("Login(%s, %s): %v", loginID, device, err)
	}
	p := saTokenPrefix
	testkit.CleanupKeys(t,
		p+"token:"+token, p+"token-session:"+token, p+"renew:"+token,
		p+"account:"+loginID+":"+device, p+"session:"+loginID,
	)
	return token
}

func tokenCount(t *testing.T, loginID string) int {
	t.Helper()
	tokens, err := stputil.GetTokenValueList(loginID)
	if err != nil {
		t.Fatalf("GetTokenValueList(%s): %v", loginID, err)
	}
	return len(tokens)
}

// 同一用户在 web、mobile 各有一个 token：Kickout 后两个都失效；另一个用户的 token 不受影响。
func TestSaTokenSession_KickoutAllDevices(t *testing.T) {
	t.Parallel()
	initSaToken(t)
	sess := infrastructure.NewSaTokenSession()
	victim, other := sharedomain.NewID().String(), sharedomain.NewID().String()

	webToken := login(t, sess, victim, "web")
	mobileToken := login(t, sess, victim, "mobile")
	otherToken := login(t, sess, other, "web")
	for _, tk := range []string{webToken, mobileToken, otherToken} {
		if !stputil.IsLogin(tk) {
			t.Fatalf("token should be logged in before Kickout")
		}
	}
	if n := tokenCount(t, victim); n != 2 {
		t.Fatalf("victim token count before Kickout = %d, want 2", n)
	}

	if err := sess.Kickout(victim); err != nil {
		t.Fatalf("Kickout: %v", err)
	}

	if stputil.IsLogin(webToken) {
		t.Errorf("web token still logged in after Kickout")
	}
	if stputil.IsLogin(mobileToken) {
		t.Errorf("mobile token still logged in after Kickout")
	}
	if n := tokenCount(t, victim); n != 0 {
		t.Errorf("victim token count after Kickout = %d, want 0", n)
	}
	if !stputil.IsLogin(otherToken) {
		t.Errorf("other user's token was kicked out")
	}
	if n := tokenCount(t, other); n != 1 {
		t.Errorf("other user token count after Kickout = %d, want 1", n)
	}
}

// 没有任何会话（从未登录 / 已被踢过）时 Kickout 返回 nil。
func TestSaTokenSession_KickoutWithoutSession(t *testing.T) {
	t.Parallel()
	initSaToken(t)
	sess := infrastructure.NewSaTokenSession()
	loginID := sharedomain.NewID().String()

	if err := sess.Kickout(loginID); err != nil {
		t.Fatalf("Kickout without session: %v", err)
	}

	token := login(t, sess, loginID, "web")
	if err := sess.Kickout(loginID); err != nil {
		t.Fatalf("first Kickout: %v", err)
	}
	if err := sess.Kickout(loginID); err != nil {
		t.Fatalf("repeated Kickout: %v", err)
	}
	if stputil.IsLogin(token) {
		t.Errorf("token still logged in after Kickout")
	}
}

// IsTokenAlive：有效 token 为 true；注销、被踢、从未存在的 token 为 false。KickoutByToken 只影响给定 token。
func TestSaTokenSession_IsTokenAliveAndKickoutByToken(t *testing.T) {
	t.Parallel()
	initSaToken(t)
	sess := infrastructure.NewSaTokenSession()
	loginID := sharedomain.NewID().String()

	a := login(t, sess, loginID, domain.TerminalDevice("web", sharedomain.NewID().String()))
	b := login(t, sess, loginID, domain.TerminalDevice("web", sharedomain.NewID().String()))
	c := login(t, sess, loginID, domain.TerminalDevice("mobile", sharedomain.NewID().String()))
	for _, tk := range []string{a, b, c} {
		if !sess.IsTokenAlive(tk) {
			t.Fatalf("fresh token should be alive")
		}
	}
	if sess.IsTokenAlive("") || sess.IsTokenAlive("never-issued-"+sharedomain.NewID().String()) {
		t.Fatal("unknown token should not be alive")
	}

	if err := sess.KickoutByToken(a); err != nil {
		t.Fatalf("KickoutByToken: %v", err)
	}
	if err := sess.LogoutByToken(b); err != nil {
		t.Fatalf("LogoutByToken: %v", err)
	}
	if sess.IsTokenAlive(a) || sess.IsTokenAlive(b) {
		t.Error("kicked / logged-out token should not be alive")
	}
	if !sess.IsTokenAlive(c) {
		t.Error("untouched token should stay alive")
	}
	// 被踢的 token 保留 KICK_OUT 标记，校验时得到"被踢下线"状态
	if _, err := stputil.GetManager().CheckLoginWithState(a); err == nil {
		t.Error("kicked token should report a kicked-out state")
	}
	if err := sess.KickoutByToken(a); err == nil {
		t.Error("kicking an already kicked token should return an error")
	}
}
