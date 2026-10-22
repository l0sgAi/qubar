//go:build integration

// multi_device_integration_test.go 多端同时在线（#106）端到端集成测试：
// application.AuthService + 真实 sa-token + 真实会话索引，连开发栈 Redis（make test-integration）。
package infrastructure_test

import (
	"context"
	"testing"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/auth/application"
	"interestBar/pkg/domains/auth/domain"
	"interestBar/pkg/domains/auth/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"
	"interestBar/pkg/util/password"

	"github.com/sa-tokens/sa-token-go/stputil"
)

const e2ePassword = "secret-password"

// e2eUserStore 只认一个用户的 UserSessionStore。
type e2eUserStore struct{ user *domain.LoginUser }

func (s *e2eUserStore) GetByEmail(email string) (*domain.LoginUser, error) {
	if email == s.user.Email {
		return s.user, nil
	}
	return nil, nil
}
func (s *e2eUserStore) Create(domain.CreateUserInput) (*domain.LoginUser, error) { return nil, nil }
func (s *e2eUserStore) FindOrCreateForOAuth(domain.OAuthUserLookup) (*domain.LoginUser, error) {
	return s.user, nil
}
func (s *e2eUserStore) UpdatePassword(userID, newHash string) error {
	s.user.Pwd = newHash
	return nil
}

// e2ePwdReset 已通过找回密码验证码校验。
type e2ePwdReset struct{ domain.PasswordResetStore }

func (e2ePwdReset) IsVerified(string) (bool, error) { return true, nil }
func (e2ePwdReset) DeleteVerified(string) error     { return nil }

type e2eEnv struct {
	svc    application.AuthService
	sess   domain.SaTokenSession
	userID string
	email  string
}

// newE2E 用随机用户构造 AuthService，并注册该用户的会话索引 key 清理。
func newE2E(t *testing.T) *e2eEnv {
	t.Helper()
	initSaToken(t)
	hash, err := password.Hash(e2ePassword)
	if err != nil {
		t.Fatal(err)
	}
	userID := sharedomain.NewID().String()
	user := &domain.LoginUser{ID: userID, Email: userID + "@example.test", Pwd: hash, Status: domain.UserStatusActive}
	sess := infrastructure.NewSaTokenSession()
	svc := application.NewAuthService(sess, &e2eUserStore{user: user}, nil, nil, nil, e2ePwdReset{}, infrastructure.NewSessionIndex())
	testkit.CleanupKeys(t, redispkg.GetAuthSessionsKey(userID), redispkg.GetAuthSessionMetaKey(userID), saTokenPrefix+"session:"+userID)
	return &e2eEnv{svc: svc, sess: sess, userID: userID, email: user.Email}
}

// login 走 POST /auth/login 的 service 路径，并注册该 token 的 sa-token key 清理。
func (e *e2eEnv) login(t *testing.T, device, deviceID string) *application.LoginResult {
	t.Helper()
	res, err := e.svc.Login(context.Background(), application.LoginInput{
		Email: e.email, Password: e2ePassword,
		Terminal: application.TerminalInput{Device: device, DeviceID: deviceID, IP: "127.0.0.1", UserAgent: "integration-test"},
	})
	if err != nil {
		t.Fatalf("Login(%s, %s): %v", device, deviceID, err)
	}
	if device == "" {
		device = "web"
	}
	p := saTokenPrefix
	testkit.CleanupKeys(t,
		p+"token:"+res.Token, p+"token-session:"+res.Token, p+"renew:"+res.Token,
		p+"account:"+e.userID+":"+domain.TerminalDevice(device, res.DeviceID))
	return res
}

func (e *e2eEnv) indexed(t *testing.T) []string {
	t.Helper()
	return listIndex(t, infrastructure.NewSessionIndex(), e.userID)
}

// 两个都是 web、device_id 不同：两个 token 都有效；同一 device_id 重新登录只替换它自己的旧 token。
func TestMultiDevice_TerminalsCoexistAndSameTerminalReplaces(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	devA, devB := sharedomain.NewID().String(), sharedomain.NewID().String()

	a := e.login(t, "web", devA)
	b := e.login(t, "web", devB)
	if a.DeviceID != devA || b.DeviceID != devB {
		t.Fatalf("device ids = (%s, %s), want (%s, %s)", a.DeviceID, b.DeviceID, devA, devB)
	}
	if !stputil.IsLogin(a.Token) || !stputil.IsLogin(b.Token) {
		t.Fatal("two web terminals with different device_id must both be logged in")
	}

	a2 := e.login(t, "web", devA)
	if stputil.IsLogin(a.Token) {
		t.Error("old token of the re-logged-in terminal should be invalid")
	}
	if !stputil.IsLogin(b.Token) || !stputil.IsLogin(a2.Token) {
		t.Error("other terminal and the new token must stay logged in")
	}
	if got := e.indexed(t); len(got) != 2 || got[0] != b.Token || got[1] != a2.Token {
		t.Errorf("index = %v, want [%s %s]", got, b.Token, a2.Token)
	}
}

// 不传 device_id / 传非 UUID：两次登录互不踢，响应返回服务端生成的 device_id。
func TestMultiDevice_WithoutDeviceID(t *testing.T) {
	t.Parallel()
	e := newE2E(t)

	a := e.login(t, "", "")
	b := e.login(t, "", "web:injected")
	if a.DeviceID == "" || b.DeviceID == "" || a.DeviceID == b.DeviceID {
		t.Fatalf("generated device ids = (%q, %q)", a.DeviceID, b.DeviceID)
	}
	if !stputil.IsLogin(a.Token) || !stputil.IsLogin(b.Token) {
		t.Fatal("logins without a valid device_id must not kick each other")
	}
}

// 超过 sa_token.max_login_count：最早登录的终端被踢（得到被踢下线状态），其余不受影响。
func TestMultiDevice_MaxLoginCountKicksOldest(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	limit := conf.Config.SaToken.MaxLoginCount
	if limit <= 0 {
		limit = 10
	}

	var results []*application.LoginResult
	for range limit + 1 {
		results = append(results, e.login(t, "web", sharedomain.NewID().String()))
	}
	if _, err := stputil.GetManager().CheckLoginWithState(results[0].Token); err == nil {
		t.Error("oldest terminal should be kicked out")
	}
	for _, r := range results[1:] {
		if !stputil.IsLogin(r.Token) {
			t.Errorf("token %s should stay logged in", r.Token)
		}
	}
	if got := e.indexed(t); len(got) != limit || got[0] != results[1].Token {
		t.Errorf("index size = %d (first %v), want %d starting at the second login", len(got), got, limit)
	}
}

// POST /auth/logout：只注销当前 token，并从索引删除。
func TestMultiDevice_LogoutOnlyCurrent(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	a := e.login(t, "web", sharedomain.NewID().String())
	b := e.login(t, "mobile", sharedomain.NewID().String())

	if err := e.svc.LogoutByToken(context.Background(), e.userID, a.Token); err != nil {
		t.Fatalf("LogoutByToken: %v", err)
	}
	if stputil.IsLogin(a.Token) || !stputil.IsLogin(b.Token) {
		t.Fatal("logout must only invalidate the current token")
	}
	if got := e.indexed(t); len(got) != 1 || got[0] != b.Token {
		t.Errorf("index = %v, want [%s]", got, b.Token)
	}
}

// 回归：重置密码后所有终端失效，包括上线前按 web / mobile 签发、不在索引里的旧 token。
func TestMultiDevice_ResetPasswordKicksOutEverything(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	a := e.login(t, "web", sharedomain.NewID().String())
	b := e.login(t, "mobile", sharedomain.NewID().String())
	legacyWeb := login(t, e.sess, e.userID, "web")
	legacyMobile := login(t, e.sess, e.userID, "mobile")
	other := newE2E(t)
	otherTok := other.login(t, "web", a.DeviceID) // 另一个用户恰好用同一个 device_id

	if err := e.svc.ResetPassword(context.Background(), application.ResetPasswordInput{Email: e.email, NewPassword: "new-password-1"}); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	for name, tk := range map[string]string{"web terminal": a.Token, "mobile terminal": b.Token, "legacy web": legacyWeb, "legacy mobile": legacyMobile} {
		if stputil.IsLogin(tk) {
			t.Errorf("%s token still logged in after password reset", name)
		}
	}
	if n := tokenCount(t, e.userID); n != 0 {
		t.Errorf("account mappings after reset = %d, want 0", n)
	}
	if got := e.indexed(t); len(got) != 0 {
		t.Errorf("index after reset = %v, want empty", got)
	}
	if !stputil.IsLogin(otherTok.Token) {
		t.Error("other user's token must not be affected")
	}
}
