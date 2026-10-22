// terminal_test.go 多端同时在线（#106）的单元测试：device_id 校验、按终端签发 token、终端上限、注销全部终端、OAuth state。
package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/auth/domain"
	"interestBar/pkg/util/password"
)

const (
	testDeviceA = "0192a0d0-0000-7000-8000-00000000000a"
	testDeviceB = "0192a0d0-0000-7000-8000-00000000000b"
	testUserID  = "0192a0d0-0000-7000-8000-000000000001"
)

// ============ fakes：按 sa-token 语义模拟的内存会话 + 内存索引 ============

type memToken struct {
	loginID, device string
	kicked          bool
}

// memSession 模拟 sa-token：每个 (loginID, device) 一个账号映射；Logout 删除 token，Kickout 标记为被踢。
type memSession struct {
	seq      int
	tokens   map[string]*memToken
	accounts map[string]string // loginID + "|" + device → token
}

func newMemSession() *memSession {
	return &memSession{tokens: map[string]*memToken{}, accounts: map[string]string{}}
}

func (s *memSession) Login(loginID, device string) (string, error) {
	s.seq++
	tk := fmt.Sprintf("tok-%d", s.seq)
	s.tokens[tk] = &memToken{loginID: loginID, device: device}
	s.accounts[loginID+"|"+device] = tk
	return tk, nil
}

func (s *memSession) LogoutByToken(token string) error {
	if t, ok := s.tokens[token]; ok {
		delete(s.accounts, t.loginID+"|"+t.device)
		delete(s.tokens, token)
	}
	return nil
}

func (s *memSession) Logout(loginID, device string) error {
	if tk, ok := s.accounts[loginID+"|"+device]; ok {
		return s.LogoutByToken(tk)
	}
	return nil
}

func (s *memSession) Kickout(loginID string) error {
	for key, tk := range s.accounts {
		if strings.HasPrefix(key, loginID+"|") {
			if err := s.KickoutByToken(tk); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *memSession) KickoutByToken(token string) error {
	if !s.IsTokenAlive(token) {
		return errors.New("invalid token data")
	}
	t := s.tokens[token]
	t.kicked = true
	delete(s.accounts, t.loginID+"|"+t.device)
	return nil
}

func (s *memSession) IsTokenAlive(token string) bool {
	t, ok := s.tokens[token]
	return ok && !t.kicked
}

func (s *memSession) SetSessionUser(loginID string, user domain.SessionUser) error { return nil }
func (s *memSession) GetTokenTimeout(token string) (int64, error)                  { return 259199, nil }

// memIndex 内存版 SessionIndex：按登记顺序（即登录时间）排列。
type memIndex struct {
	tokens map[string][]string
	meta   map[string]domain.TerminalMeta
}

func newMemIndex() *memIndex {
	return &memIndex{tokens: map[string][]string{}, meta: map[string]domain.TerminalMeta{}}
}

func (x *memIndex) Add(_ context.Context, loginID, token string, meta domain.TerminalMeta, _ time.Duration) error {
	x.tokens[loginID] = append(slices.DeleteFunc(x.tokens[loginID], func(t string) bool { return t == token }), token)
	x.meta[token] = meta
	return nil
}

func (x *memIndex) List(_ context.Context, loginID string) ([]string, error) {
	return slices.Clone(x.tokens[loginID]), nil
}

func (x *memIndex) Remove(_ context.Context, loginID string, tokens ...string) error {
	x.tokens[loginID] = slices.DeleteFunc(x.tokens[loginID], func(t string) bool { return slices.Contains(tokens, t) })
	return nil
}

func (x *memIndex) Clear(_ context.Context, loginID string) error {
	delete(x.tokens, loginID)
	return nil
}

func newTerminalService(sess *memSession, idx *memIndex) *authServiceImpl {
	return NewAuthService(sess, &fakeUserStore{}, nil, nil, &fakeOAuthRegistry{}, nil, idx).(*authServiceImpl)
}

func setMaxLoginCount(t *testing.T, n int) {
	t.Helper()
	old := conf.Config.SaToken.MaxLoginCount
	conf.Config.SaToken.MaxLoginCount = n
	t.Cleanup(func() { conf.Config.SaToken.MaxLoginCount = old })
}

var testUser = &domain.LoginUser{ID: testUserID, Email: "u@example.com", Status: domain.UserStatusActive}

func issue(t *testing.T, svc *authServiceImpl, in TerminalInput) (token, deviceID string) {
	t.Helper()
	token, deviceID, err := svc.issueToken(context.Background(), testUser, in)
	if err != nil {
		t.Fatalf("issueToken(%+v): %v", in, err)
	}
	return token, deviceID
}

// ============ device_id 校验 ============

func TestNormalizeDeviceID(t *testing.T) {
	cases := []struct{ in, want string }{
		{testDeviceA, testDeviceA},
		{strings.ToUpper(testDeviceA), testDeviceA},
		{"", ""},
		{"not-a-uuid", ""},
		{"urn:uuid:" + testDeviceA, ""},
		{"{" + testDeviceA + "}", ""},
		{strings.ReplaceAll(testDeviceA, "-", ""), ""},
		{"00000000-0000-0000-0000-000000000000", ""},
		{"0192a0d0-0000-7000-8000-00000000000:", ""},
	}
	for _, tc := range cases {
		if got := normalizeDeviceID(tc.in); got != tc.want {
			t.Errorf("normalizeDeviceID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveTerminal(t *testing.T) {
	typ, id := resolveTerminal(TerminalInput{Device: "mobile", DeviceID: testDeviceA})
	if typ != "mobile" || id != testDeviceA {
		t.Fatalf("resolveTerminal = (%q, %q)", typ, id)
	}
	typ, id = resolveTerminal(TerminalInput{Device: "tv", DeviceID: "web:evil"})
	if typ != "web" {
		t.Fatalf("invalid device type should fall back to web, got %q", typ)
	}
	if normalizeDeviceID(id) != id || id == "" {
		t.Fatalf("invalid device_id should be replaced by a generated UUID, got %q", id)
	}
}

func TestTruncateUserAgent(t *testing.T) {
	short := "Mozilla/5.0"
	if got := truncateUserAgent(short); got != short {
		t.Fatalf("short UA changed: %q", got)
	}
	long := strings.Repeat("a", maxUserAgentBytes-1) + "中文"
	got := truncateUserAgent(long)
	if len(got) > maxUserAgentBytes || !strings.HasPrefix(long, got) || !utf8.ValidString(got) {
		t.Fatalf("truncateUserAgent cut badly: len=%d", len(got))
	}
}

// ============ 按终端签发 token ============

// 两个都是 web、device_id 不同的终端：两个 token 都有效。
func TestIssueToken_DifferentTerminalsCoexist(t *testing.T) {
	sess, idx := newMemSession(), newMemIndex()
	svc := newTerminalService(sess, idx)

	a, idA := issue(t, svc, TerminalInput{Device: "web", DeviceID: testDeviceA, IP: "1.2.3.4", UserAgent: "ua-a"})
	b, idB := issue(t, svc, TerminalInput{Device: "web", DeviceID: testDeviceB})
	if !sess.IsTokenAlive(a) || !sess.IsTokenAlive(b) {
		t.Fatal("tokens of different terminals must both stay valid")
	}
	if idA != testDeviceA || idB != testDeviceB {
		t.Fatalf("device ids = (%q, %q)", idA, idB)
	}
	if got := sess.tokens[a].device; got != "web:"+testDeviceA {
		t.Fatalf("sa-token device = %q, want web:<device_id>", got)
	}
	if m := idx.meta[a]; m.Type != "web" || m.DeviceID != testDeviceA || m.IP != "1.2.3.4" || m.UA != "ua-a" || m.LoginTime == 0 {
		t.Fatalf("terminal meta = %+v", m)
	}
	if got := idx.tokens[testUserID]; !slices.Equal(got, []string{a, b}) {
		t.Fatalf("index = %v, want [%s %s]", got, a, b)
	}
}

// 同一 device_id 重新登录：只让这个终端之前的 token 失效，并移出索引。
func TestIssueToken_SameTerminalReplacesOwnToken(t *testing.T) {
	sess, idx := newMemSession(), newMemIndex()
	svc := newTerminalService(sess, idx)

	old, _ := issue(t, svc, TerminalInput{DeviceID: testDeviceA})
	other, _ := issue(t, svc, TerminalInput{DeviceID: testDeviceB})
	renewed, _ := issue(t, svc, TerminalInput{DeviceID: testDeviceA})

	if sess.IsTokenAlive(old) {
		t.Error("previous token of the same terminal should be invalid")
	}
	if !sess.IsTokenAlive(other) || !sess.IsTokenAlive(renewed) {
		t.Error("other terminal and the new token must stay valid")
	}
	if got := idx.tokens[testUserID]; !slices.Equal(got, []string{other, renewed}) {
		t.Fatalf("index = %v, want [%s %s]", got, other, renewed)
	}
}

// 不传 device_id：每次登录都是新终端，互不踢；返回服务端生成的 device_id。
func TestIssueToken_WithoutDeviceIDNeverCollides(t *testing.T) {
	sess := newMemSession()
	svc := newTerminalService(sess, newMemIndex())

	a, idA := issue(t, svc, TerminalInput{})
	b, idB := issue(t, svc, TerminalInput{DeviceID: "garbage"})
	if !sess.IsTokenAlive(a) || !sess.IsTokenAlive(b) {
		t.Fatal("logins without device_id must not kick each other")
	}
	if idA == idB || normalizeDeviceID(idA) != idA || normalizeDeviceID(idB) != idB {
		t.Fatalf("generated device ids = (%q, %q)", idA, idB)
	}
}

// 超过 max_login_count：最早登录的终端被踢（kicked 而不是删除），其余不受影响。
func TestIssueToken_MaxLoginCountKicksOldest(t *testing.T) {
	setMaxLoginCount(t, 3)
	sess, idx := newMemSession(), newMemIndex()
	svc := newTerminalService(sess, idx)

	var tokens []string
	for range 4 {
		tk, _ := issue(t, svc, TerminalInput{})
		tokens = append(tokens, tk)
	}
	if sess.IsTokenAlive(tokens[0]) {
		t.Error("oldest terminal should be kicked out")
	}
	if tok := sess.tokens[tokens[0]]; tok == nil || !tok.kicked {
		t.Error("oldest terminal should be marked as kicked out, not deleted")
	}
	for _, tk := range tokens[1:] {
		if !sess.IsTokenAlive(tk) {
			t.Errorf("token %s should stay valid", tk)
		}
	}
	if got := idx.tokens[testUserID]; !slices.Equal(got, tokens[1:]) {
		t.Fatalf("index = %v, want %v", got, tokens[1:])
	}
}

// 未配置（<=0）时上限回落默认 10。
func TestIssueToken_DefaultMaxLoginCount(t *testing.T) {
	setMaxLoginCount(t, 0)
	sess, idx := newMemSession(), newMemIndex()
	svc := newTerminalService(sess, idx)

	var first string
	for i := range defaultMaxLoginCount {
		tk, _ := issue(t, svc, TerminalInput{})
		if i == 0 {
			first = tk
		}
	}
	if !sess.IsTokenAlive(first) {
		t.Fatal("first token should survive while within the default limit")
	}
	issue(t, svc, TerminalInput{})
	if sess.IsTokenAlive(first) {
		t.Fatal("first token should be kicked once the default limit is exceeded")
	}
	if n := len(idx.tokens[testUserID]); n != defaultMaxLoginCount {
		t.Fatalf("index size = %d, want %d", n, defaultMaxLoginCount)
	}
}

// 已失效的 member（例如在别处被注销）不占名额，登录时被清出索引。
func TestIssueToken_PrunesStaleMembers(t *testing.T) {
	setMaxLoginCount(t, 2)
	sess, idx := newMemSession(), newMemIndex()
	svc := newTerminalService(sess, idx)

	a, _ := issue(t, svc, TerminalInput{})
	b, _ := issue(t, svc, TerminalInput{})
	_ = sess.LogoutByToken(a) // 绕过索引直接注销，模拟过期
	c, _ := issue(t, svc, TerminalInput{})

	if !sess.IsTokenAlive(b) || !sess.IsTokenAlive(c) {
		t.Fatal("stale member must not count against the limit")
	}
	if got := idx.tokens[testUserID]; !slices.Equal(got, []string{b, c}) {
		t.Fatalf("index = %v, want [%s %s]", got, b, c)
	}
}

// ============ 登出 / 注销全部终端 ============

func TestLogoutByToken_OnlyCurrentTerminal(t *testing.T) {
	sess, idx := newMemSession(), newMemIndex()
	svc := newTerminalService(sess, idx)

	a, _ := issue(t, svc, TerminalInput{DeviceID: testDeviceA})
	b, _ := issue(t, svc, TerminalInput{DeviceID: testDeviceB})
	if err := svc.LogoutByToken(context.Background(), testUserID, a); err != nil {
		t.Fatalf("LogoutByToken: %v", err)
	}
	if sess.IsTokenAlive(a) || !sess.IsTokenAlive(b) {
		t.Fatal("logout must only invalidate the current token")
	}
	if got := idx.tokens[testUserID]; !slices.Equal(got, []string{b}) {
		t.Fatalf("index = %v, want [%s]", got, b)
	}
}

// 重置密码：索引里的终端、上线前按 web / mobile 签发且不在索引里的旧 token 全部失效，索引清空。
func TestResetPassword_KicksOutAllTerminals(t *testing.T) {
	sess, idx := newMemSession(), newMemIndex()
	pwdHash, err := password.Hash("old-password")
	if err != nil {
		t.Fatal(err)
	}
	store := &resetUserStore{user: &domain.LoginUser{ID: testUserID, Email: "u@example.com", Pwd: pwdHash, Status: domain.UserStatusActive}}
	svc := NewAuthService(sess, store, nil, nil, &fakeOAuthRegistry{}, &verifiedPwdReset{}, idx).(*authServiceImpl)

	a, _ := issue(t, svc, TerminalInput{DeviceID: testDeviceA})
	b, _ := issue(t, svc, TerminalInput{Device: "mobile", DeviceID: testDeviceB})
	legacyWeb, _ := sess.Login(testUserID, "web")
	legacyMobile, _ := sess.Login(testUserID, "mobile")
	otherUser, _ := sess.Login("0192a0d0-0000-7000-8000-000000000002", "web:"+testDeviceA)

	if err := svc.ResetPassword(context.Background(), ResetPasswordInput{Email: "u@example.com", NewPassword: "new-password"}); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	for name, tk := range map[string]string{"indexed web": a, "indexed mobile": b, "legacy web": legacyWeb, "legacy mobile": legacyMobile} {
		if sess.IsTokenAlive(tk) {
			t.Errorf("%s token still valid after password reset", name)
		}
	}
	if !sess.IsTokenAlive(otherUser) {
		t.Error("other user's token must not be affected")
	}
	if _, ok := idx.tokens[testUserID]; ok {
		t.Error("session index should be cleared")
	}
	if store.updatedHash == "" {
		t.Error("password hash not updated")
	}
}

type resetUserStore struct {
	fakeUserStore
	user        *domain.LoginUser
	updatedHash string
}

func (f *resetUserStore) GetByEmail(email string) (*domain.LoginUser, error) { return f.user, nil }
func (f *resetUserStore) UpdatePassword(userID, newHash string) error {
	f.updatedHash = newHash
	return nil
}

// verifiedPwdReset 已通过找回密码验证码校验的 PasswordResetStore。
type verifiedPwdReset struct{ domain.PasswordResetStore }

func (verifiedPwdReset) IsVerified(email string) (bool, error) { return true, nil }
func (verifiedPwdReset) DeleteVerified(email string) error     { return nil }

// ============ 邮箱登录返回 device_id ============

func TestLogin_ReturnsDeviceID(t *testing.T) {
	sess := newMemSession()
	pwdHash, err := password.Hash("secret-password")
	if err != nil {
		t.Fatal(err)
	}
	store := &resetUserStore{user: &domain.LoginUser{ID: testUserID, Email: "u@example.com", Pwd: pwdHash, Status: domain.UserStatusActive}}
	svc := NewAuthService(sess, store, nil, nil, &fakeOAuthRegistry{}, nil, newMemIndex())

	first, err := svc.Login(context.Background(), LoginInput{Email: "u@example.com", Password: "secret-password"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if normalizeDeviceID(first.DeviceID) != first.DeviceID || first.DeviceID == "" {
		t.Fatalf("generated device_id = %q", first.DeviceID)
	}
	second, err := svc.Login(context.Background(), LoginInput{Email: "u@example.com", Password: "secret-password"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !sess.IsTokenAlive(first.Token) || !sess.IsTokenAlive(second.Token) {
		t.Fatal("two logins without device_id must not kick each other")
	}
}

// ============ OAuth state ============

func TestOAuthLoginURL_EncodesDeviceID(t *testing.T) {
	p := &fakeOAuthProvider{frontendURL: "https://app.com/success"}
	svc := newTestService(p, &fakeSession{})

	out, err := svc.OAuthLoginURL(context.Background(), "google", "mobile", strings.ToUpper(testDeviceA))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(out.RedirectURL)
	state := u.Query().Get("state")
	if want := "device:mobile:google-token:" + testDeviceA; state != want {
		t.Fatalf("state = %q, want %q", state, want)
	}

	out, _ = svc.OAuthLoginURL(context.Background(), "google", "", "x:y")
	u, _ = url.Parse(out.RedirectURL)
	if state := u.Query().Get("state"); state != "device:web:google-token" {
		t.Fatalf("invalid device_id must not reach state, got %q", state)
	}
}

func TestParseOAuthState(t *testing.T) {
	cases := []struct{ state, device, deviceID string }{
		{"device:mobile:google-token:" + testDeviceA, "mobile", testDeviceA},
		{"device:web:github-token", "web", ""},                       // 上线前生成的旧 state
		{"device:web:google-token:not-a-uuid", "web", ""},            // 非法 device_id 忽略
		{"device:web:google-token:" + testDeviceA + ":x", "web", ""}, // 多余分隔符：整段不是 UUID
		{"device:tv:google-token", "web", ""},
		{"", "web", ""},
	}
	for _, tc := range cases {
		device, deviceID := ParseOAuthState(tc.state)
		if device != tc.device || deviceID != tc.deviceID {
			t.Errorf("ParseOAuthState(%q) = (%q, %q), want (%q, %q)", tc.state, device, deviceID, tc.device, tc.deviceID)
		}
	}
}

func TestOAuthCallback_PassesDeviceID(t *testing.T) {
	p := &fakeOAuthProvider{frontendURL: "https://app.com/success"}
	svc := newTestService(p, &fakeSession{})

	got, err := svc.OAuthCallback(context.Background(), "google", "c", "web", testDeviceA)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://app.com/success?code=c&device=web&device_id=" + testDeviceA + "&provider=google"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
