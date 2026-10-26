//go:build integration

// auth_integration_test.go 请求鉴权中间件（RequireLogin / OptionalLogin / authenticate）集成测试（#131，make test-integration）：
//   - 稳态每请求只有 GET + TTL 两条 Redis 命令，剩余有效期低于阈值才续期，续期覆盖 token / 账号映射 / 账号 session；
//   - 登出、踢下线（单终端 / 全部终端）、被顶号后立即 401，其他终端不受影响（#106 / #109 语义不变）。
//
// 依赖开发栈 Redis（testkit 独立测试库）。
package composition

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/conf"
	authinfra "interestBar/pkg/domains/auth/infrastructure"
	serverauth "interestBar/pkg/server/auth"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/appctx/hertzadapter"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/cloudwego/hertz/pkg/app"
	sahertz "github.com/sa-tokens/sa-token-go/integrations/hertz"
	saredis "github.com/sa-tokens/sa-token-go/storage/redis"
	"github.com/sa-tokens/sa-token-go/stputil"
)

// 与生产一致的有效期与续期间隔（configs/config.example.yaml）。
const (
	authTestTimeout       = 259200
	authTestRenewInterval = 600
)

// countingStorage 记录经过 sa-token Storage 的每次调用（"OP key"），用来断言每请求的 Redis 命令。
type countingStorage struct {
	sahertz.Storage
	mu  sync.Mutex
	ops []string
}

func (s *countingStorage) record(op, key string) {
	s.mu.Lock()
	s.ops = append(s.ops, op+" "+key)
	s.mu.Unlock()
}

// take 取出并清空已记录的调用。
func (s *countingStorage) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ops := s.ops
	s.ops = nil
	return ops
}

func (s *countingStorage) Set(key string, value any, expiration time.Duration) error {
	s.record("SET", key)
	return s.Storage.Set(key, value, expiration)
}

func (s *countingStorage) SetKeepTTL(key string, value any) error {
	s.record("SETKEEPTTL", key)
	return s.Storage.SetKeepTTL(key, value)
}

func (s *countingStorage) Get(key string) (any, error) {
	s.record("GET", key)
	return s.Storage.Get(key)
}

func (s *countingStorage) Delete(keys ...string) error {
	s.record("DEL", strings.Join(keys, ","))
	return s.Storage.Delete(keys...)
}

func (s *countingStorage) Exists(key string) bool {
	s.record("EXISTS", key)
	return s.Storage.Exists(key)
}

func (s *countingStorage) Expire(key string, expiration time.Duration) error {
	s.record("EXPIRE", key)
	return s.Storage.Expire(key, expiration)
}

func (s *countingStorage) TTL(key string) (time.Duration, error) {
	s.record("TTL", key)
	return s.Storage.TTL(key)
}

// newCountingManager 构造与 InitSaToken 同配置（有效期 3 天、续期阈值 Timeout - 600）的独立 manager，
// 不动全局 stputil manager，可与本包其他测试并行。
func newCountingManager(t *testing.T, autoRenew bool) (*sahertz.Manager, *countingStorage) {
	t.Helper()
	testkit.Redis(t)
	store := &countingStorage{Storage: saredis.NewStorageFromClient(redispkg.Client)}
	cfg := sahertz.DefaultConfig()
	cfg.Timeout = authTestTimeout
	cfg.MaxRefresh = serverauth.RenewMaxRefresh(authTestTimeout, authTestRenewInterval)
	cfg.AutoRenew = autoRenew
	cfg.IsConcurrent = true
	cfg.IsShare = false
	cfg.MaxLoginCount = -1
	cfg.IsPrintBanner = false
	return sahertz.NewManager(store, cfg), store
}

// saKeys 一个终端登录涉及的 sa-token key（含前缀）。
type saKeys struct{ token, account, session string }

func saKeysOf(prefix, loginID, device, token string) saKeys {
	return saKeys{
		token:   prefix + "token:" + token,
		account: prefix + "account:" + loginID + ":" + device,
		session: prefix + "session:" + loginID,
	}
}

// loginWith 在 mgr 上登录并写 session（与 POST /auth/login 一样会有账号 session），注册全部 key 的清理。
func loginWith(t *testing.T, mgr *sahertz.Manager, loginID, device string) (string, saKeys) {
	t.Helper()
	token, err := mgr.Login(loginID, device)
	if err != nil {
		t.Fatalf("login %s/%s: %v", loginID, device, err)
	}
	sess, err := mgr.GetSession(loginID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if err := sess.Set(authinfra.SessionKeyForUser, map[string]string{"id": loginID}); err != nil {
		t.Fatalf("set session: %v", err)
	}
	prefix := mgr.GetConfig().KeyPrefix
	keys := saKeysOf(prefix, loginID, device, token)
	testkit.CleanupKeys(t, keys.token, keys.account, keys.session, prefix+"token-session:"+token, prefix+"renew:"+token)
	return token, keys
}

func ttlOf(t *testing.T, key string) time.Duration {
	t.Helper()
	d, err := redispkg.Client.TTL(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("TTL %s: %v", key, err)
	}
	return d
}

func expireKeys(t *testing.T, ttl time.Duration, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if err := redispkg.Client.Expire(context.Background(), k, ttl).Err(); err != nil {
			t.Fatalf("EXPIRE %s: %v", k, err)
		}
	}
}

// TestAuthenticate_SteadyStateTwoRedisOps 稳态（未到续期阈值）每请求只有 GET token + TTL token，且不续期。
func TestAuthenticate_SteadyStateTwoRedisOps(t *testing.T) {
	t.Parallel()
	mgr, store := newCountingManager(t, true)
	loginID := sharedomain.NewID().String()
	token, keys := loginWith(t, mgr, loginID, "web")
	before := ttlOf(t, keys.token)
	store.take()

	for i := range 5 {
		got, ok := authenticate(mgr, token)
		if !ok || got != loginID {
			t.Fatalf("request %d: authenticate = (%q, %v), want (%q, true)", i, got, ok, loginID)
		}
		ops := store.take()
		want := []string{"GET " + keys.token, "TTL " + keys.token}
		if !slices.Equal(ops, want) {
			t.Fatalf("request %d: redis ops = %v, want %v", i, ops, want)
		}
	}
	if after := ttlOf(t, keys.token); after > before {
		t.Fatalf("token TTL grew %v -> %v: renewed before reaching threshold", before, after)
	}
}

// TestAuthenticate_RenewsOnlyBelowThreshold 剩余有效期低于 Timeout - renew_interval 才续期，
// 续期把 token / 账号映射 / 账号 session 的 TTL 都重置为 Timeout，之后回到稳态两条命令。
func TestAuthenticate_RenewsOnlyBelowThreshold(t *testing.T) {
	t.Parallel()
	mgr, store := newCountingManager(t, true)
	loginID := sharedomain.NewID().String()
	token, keys := loginWith(t, mgr, loginID, "web")
	threshold := time.Duration(mgr.GetConfig().MaxRefresh) * time.Second

	// 刚过阈值之上：不续期。
	expireKeys(t, threshold+30*time.Second, keys.token)
	store.take()
	if _, ok := authenticate(mgr, token); !ok {
		t.Fatal("authenticate above threshold: want ok")
	}
	if ops := store.take(); len(ops) != 2 {
		t.Fatalf("above threshold: redis ops = %v, want only GET + TTL", ops)
	}
	if d := ttlOf(t, keys.token); d > threshold+30*time.Second {
		t.Fatalf("above threshold: token TTL %v, want not renewed", d)
	}

	// 跌到阈值以下（模拟距上次续期已超过 renew_interval）：续期。
	expireKeys(t, time.Hour, keys.token, keys.account, keys.session)
	if _, ok := authenticate(mgr, token); !ok {
		t.Fatal("authenticate below threshold: want ok")
	}
	ops := store.take()
	for _, k := range []string{keys.token, keys.account, keys.session} {
		if !slices.Contains(ops, "EXPIRE "+k) {
			t.Fatalf("below threshold: ops %v missing EXPIRE %s", ops, k)
		}
		if d := ttlOf(t, k); d < threshold {
			t.Fatalf("below threshold: %s TTL %v, want renewed to ~%ds", k, d, authTestTimeout)
		}
	}
	if slices.ContainsFunc(ops, func(op string) bool { return strings.HasPrefix(op, "SET") }) {
		t.Fatalf("renew must not rewrite token value, ops = %v", ops)
	}

	// 续期后回到稳态。
	if _, ok := authenticate(mgr, token); !ok {
		t.Fatal("authenticate after renew: want ok")
	}
	if ops := store.take(); len(ops) != 2 {
		t.Fatalf("after renew: redis ops = %v, want only GET + TTL", ops)
	}
}

// TestAuthenticate_AutoRenewDisabled AutoRenew=false 时即使低于阈值也不续期。
func TestAuthenticate_AutoRenewDisabled(t *testing.T) {
	t.Parallel()
	mgr, store := newCountingManager(t, false)
	loginID := sharedomain.NewID().String()
	token, keys := loginWith(t, mgr, loginID, "web")
	expireKeys(t, time.Hour, keys.token)
	store.take()
	if _, ok := authenticate(mgr, token); !ok {
		t.Fatal("authenticate: want ok")
	}
	if ops := store.take(); !slices.Equal(ops, []string{"GET " + keys.token}) {
		t.Fatalf("redis ops = %v, want only GET", ops)
	}
	if d := ttlOf(t, keys.token); d > time.Hour {
		t.Fatalf("token TTL %v, want not renewed", d)
	}
}

// TestAuthenticate_KickedTokenNotRenewed 被踢下线的 token 即使低于续期阈值也不通过、不续期，KICK_OUT 标记不被覆盖。
func TestAuthenticate_KickedTokenNotRenewed(t *testing.T) {
	t.Parallel()
	mgr, store := newCountingManager(t, true)
	loginID := sharedomain.NewID().String()
	token, keys := loginWith(t, mgr, loginID, "web")
	expireKeys(t, time.Hour, keys.token)
	if err := mgr.KickoutByToken(token); err != nil {
		t.Fatalf("kickout: %v", err)
	}
	store.take()
	if _, ok := authenticate(mgr, token); ok {
		t.Fatal("kicked token: want rejected")
	}
	if ops := store.take(); !slices.Equal(ops, []string{"GET " + keys.token}) {
		t.Fatalf("kicked token: redis ops = %v, want only GET", ops)
	}
	val, err := redispkg.Client.Get(context.Background(), keys.token).Result()
	if err == nil && val != "KICK_OUT" {
		t.Fatalf("kicked token value = %q, want KICK_OUT", val)
	}
}

// ---- 中间件语义（全局 stputil manager，与 notice_stream 测试共用 initStreamSaToken）----

// runMiddleware 带 token 头调一次中间件，返回是否被中止（401）与写入的 loginID。
func runMiddleware(mw func(appctx.AppContext), token string) (aborted bool, loginID string) {
	rc := app.NewContext(0)
	if token != "" {
		rc.Request.Header.Set(conf.Config.SaToken.TokenName, token)
	}
	c := hertzadapter.New(context.Background(), rc)
	mw(c)
	loginID, _ = c.LoginID()
	return rc.IsAborted(), loginID
}

// globalLogin 在全局 manager 上登录（与 auth 领域同一套 stputil），注册 key 清理。
func globalLogin(t *testing.T, loginID, device string) string {
	t.Helper()
	token, err := stputil.Login(loginID, device)
	if err != nil {
		t.Fatalf("login %s/%s: %v", loginID, device, err)
	}
	p := streamSaTokenPrefix
	testkit.CleanupKeys(t,
		p+"token:"+token, p+"token-session:"+token, p+"renew:"+token,
		p+"account:"+loginID+":"+device, p+"session:"+loginID,
	)
	return token
}

// assertLoggedIn 断言 token 在 RequireLogin / OptionalLogin 下都解析为 wantID。
func assertLoggedIn(t *testing.T, token, wantID string) {
	t.Helper()
	if aborted, got := runMiddleware(RequireLogin, token); aborted || got != wantID {
		t.Fatalf("RequireLogin(%s) = (aborted=%v, loginID=%q), want pass as %q", token, aborted, got, wantID)
	}
	if aborted, got := runMiddleware(OptionalLogin, token); aborted || got != wantID {
		t.Fatalf("OptionalLogin(%s) = (aborted=%v, loginID=%q), want %q", token, aborted, got, wantID)
	}
}

// assertRejected 断言 token 被 RequireLogin 拒绝（401），OptionalLogin 按匿名放行。
func assertRejected(t *testing.T, token string) {
	t.Helper()
	if aborted, got := runMiddleware(RequireLogin, token); !aborted || got != "" {
		t.Fatalf("RequireLogin(%q) = (aborted=%v, loginID=%q), want 401", token, aborted, got)
	}
	if aborted, got := runMiddleware(OptionalLogin, token); aborted || got != "" {
		t.Fatalf("OptionalLogin(%q) = (aborted=%v, loginID=%q), want anonymous pass", token, aborted, got)
	}
}

func TestLoginMiddleware_MissingOrUnknownToken(t *testing.T) {
	t.Parallel()
	initStreamSaToken(t)
	assertRejected(t, "")
	assertRejected(t, sharedomain.NewID().String())
}

// TestLoginMiddleware_LogoutOneTerminal 多端在线时登出一个终端，只影响该终端（#106）。
func TestLoginMiddleware_LogoutOneTerminal(t *testing.T) {
	t.Parallel()
	initStreamSaToken(t)
	sess := authinfra.NewSaTokenSession()
	loginID := sharedomain.NewID().String()
	web := globalLogin(t, loginID, "web")
	mobile := globalLogin(t, loginID, "mobile")
	assertLoggedIn(t, web, loginID)
	assertLoggedIn(t, mobile, loginID)

	if err := sess.LogoutByToken(web); err != nil {
		t.Fatalf("logout web: %v", err)
	}
	assertRejected(t, web)
	assertLoggedIn(t, mobile, loginID)
}

// TestLoginMiddleware_KickoutOneTerminal 踢掉单个终端（超出终端上限 / 管理终端，#106）：被踢的 401，其余照常。
func TestLoginMiddleware_KickoutOneTerminal(t *testing.T) {
	t.Parallel()
	initStreamSaToken(t)
	sess := authinfra.NewSaTokenSession()
	loginID := sharedomain.NewID().String()
	web := globalLogin(t, loginID, "web")
	mobile := globalLogin(t, loginID, "mobile")

	if err := sess.KickoutByToken(web); err != nil {
		t.Fatalf("kickout web: %v", err)
	}
	assertRejected(t, web)
	assertLoggedIn(t, mobile, loginID)
}

// TestLoginMiddleware_KickoutAllTerminals 找回密码后踢下线全部终端（#109），不影响其他账号。
func TestLoginMiddleware_KickoutAllTerminals(t *testing.T) {
	t.Parallel()
	initStreamSaToken(t)
	sess := authinfra.NewSaTokenSession()
	loginID := sharedomain.NewID().String()
	web := globalLogin(t, loginID, "web")
	mobile := globalLogin(t, loginID, "mobile")
	otherID := sharedomain.NewID().String()
	other := globalLogin(t, otherID, "web")

	if err := sess.Kickout(loginID); err != nil {
		t.Fatalf("kickout all: %v", err)
	}
	assertRejected(t, web)
	assertRejected(t, mobile)
	assertLoggedIn(t, other, otherID)
}

// TestLoginMiddleware_ReplacedToken 被顶号的 token（BE_REPLACED）同样 401。
func TestLoginMiddleware_ReplacedToken(t *testing.T) {
	t.Parallel()
	initStreamSaToken(t)
	loginID := sharedomain.NewID().String()
	web := globalLogin(t, loginID, "web")
	if err := stputil.GetManager().ReplacedByToken(web); err != nil {
		t.Fatalf("replace: %v", err)
	}
	assertRejected(t, web)
}
