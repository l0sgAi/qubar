// oauth_test.go OAuth code 透传 + POST 换 token 的单元测试。
//
// 覆盖方案 A 核心行为：
//   - buildOAuthRedirectURL：query/hash 拼接正确性；
//   - OAuthCallback（GET）：只透传 code，绝不消耗（不调用 Exchange）；
//   - OAuthExchange（POST）：正常链路 / invalid_grant 400 / provider 不可达 503 /
//     GetTokenTimeout 失败回落配置超时。
package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/auth/domain"
	"interestBar/pkg/logger"

	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	conf.Config = &conf.AppConfig{}
	conf.Config.SaToken.Timeout = 259200
	logger.Log = zap.NewNop()
	m.Run()
}

// ============ fakes ============

type fakeOAuthRegistry struct {
	providers map[string]domain.OAuthProvider
}

func (r *fakeOAuthRegistry) Get(name string) domain.OAuthProvider { return r.providers[name] }

type fakeOAuthProvider struct {
	frontendURL    string
	exchangeCalled bool
	exchangeErr    error
	fetchErr       error
}

func (p *fakeOAuthProvider) Name() string { return "google" }
func (p *fakeOAuthProvider) AuthCodeURL(state string) string {
	return "https://accounts.google.com/o/oauth2/auth?state=" + state
}
func (p *fakeOAuthProvider) UserLookupField() string     { return "google_id" }
func (p *fakeOAuthProvider) FrontendRedirectURL() string { return p.frontendURL }
func (p *fakeOAuthProvider) Exchange(ctx context.Context, code string) (interface{}, error) {
	p.exchangeCalled = true
	if p.exchangeErr != nil {
		return nil, p.exchangeErr
	}
	return "provider-token", nil
}
func (p *fakeOAuthProvider) FetchUser(ctx context.Context, token interface{}) (*domain.OAuthUserInfo, error) {
	if p.fetchErr != nil {
		return nil, p.fetchErr
	}
	return &domain.OAuthUserInfo{
		ProviderID: "g-123",
		Email:      "u@example.com",
		Name:       "u",
	}, nil
}

type fakeSession struct {
	timeoutErr error
}

func (s *fakeSession) Login(loginID, device string) (string, error) { return "tok-abc", nil }
func (s *fakeSession) LogoutByToken(token string) error             { return nil }
func (s *fakeSession) Logout(loginID, device string) error          { return nil }
func (s *fakeSession) Kickout(loginID string) error                 { return nil }
func (s *fakeSession) SetSessionUser(loginID string, user domain.SessionUser) error {
	return nil
}
func (s *fakeSession) GetTokenTimeout(token string) (int64, error) {
	if s.timeoutErr != nil {
		return 0, s.timeoutErr
	}
	return 259199, nil
}

type fakeUserStore struct{}

func (f *fakeUserStore) GetByEmail(email string) (*domain.LoginUser, error) { return nil, nil }
func (f *fakeUserStore) Create(input domain.CreateUserInput) (*domain.LoginUser, error) {
	return nil, nil
}
func (f *fakeUserStore) FindOrCreateForOAuth(lookup domain.OAuthUserLookup) (*domain.LoginUser, error) {
	return &domain.LoginUser{ID: "0192a0d0-0000-7000-8000-000000000001", Email: lookup.Email}, nil
}
func (f *fakeUserStore) UpdatePassword(userID, newHash string) error { return nil }

func newTestService(p domain.OAuthProvider, session domain.SaTokenSession) AuthService {
	reg := &fakeOAuthRegistry{providers: map[string]domain.OAuthProvider{}}
	if p != nil {
		reg.providers[p.Name()] = p
	}
	return NewAuthService(session, &fakeUserStore{}, nil, nil, reg, nil)
}

// ============ buildOAuthRedirectURL ============

func TestBuildOAuthRedirectURL(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		code     string
		device   string
		provider string
		expected string
	}{
		{"plain", "https://app.com/success", "abc", "web", "google", "https://app.com/success?code=abc&device=web&provider=google"},
		{"base has query", "https://app.com/success?from=oauth", "abc", "web", "github", "https://app.com/success?from=oauth&code=abc&device=web&provider=github"},
		{"hash route", "https://app.com/#/success", "abc", "web", "azure", "https://app.com/?code=abc&device=web&provider=azure#/success"},
		{"code escaped", "https://app.com/success", "a/b+c=d", "mobile", "google", "https://app.com/success?code=a%2Fb%2Bc%3Dd&device=mobile&provider=google"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOAuthRedirectURL(tc.base, tc.code, tc.device, tc.provider)
			if got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// ============ OAuthCallback（GET 透传） ============

func TestOAuthCallbackPassthrough(t *testing.T) {
	p := &fakeOAuthProvider{frontendURL: "https://app.com/success"}
	svc := newTestService(p, &fakeSession{})

	u, err := svc.OAuthCallback(context.Background(), "google", "one-time-code", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://app.com/success?code=one-time-code&device=web&provider=google"
	if u != want {
		t.Fatalf("got %q, want %q", u, want)
	}
	if p.exchangeCalled {
		t.Fatal("GET callback must not exchange the one-time code")
	}
}

func TestOAuthCallbackUnknownProvider(t *testing.T) {
	svc := newTestService(nil, &fakeSession{})
	if _, err := svc.OAuthCallback(context.Background(), "noop", "c", "web"); !IsUnknownOAuthProviderErr(err) {
		t.Fatalf("want unknown provider err, got %v", err)
	}
}

func TestOAuthCallbackRedirectNotConfigured(t *testing.T) {
	p := &fakeOAuthProvider{frontendURL: ""}
	svc := newTestService(p, &fakeSession{})
	if _, err := svc.OAuthCallback(context.Background(), "google", "c", "web"); !IsFrontendRedirectNotConfiguredErr(err) {
		t.Fatalf("want redirect-not-configured err, got %v", err)
	}
}

// ============ OAuthExchange（POST 换 token） ============

func TestOAuthExchangeHappyPath(t *testing.T) {
	p := &fakeOAuthProvider{frontendURL: "https://app.com/success"}
	svc := newTestService(p, &fakeSession{})

	res, err := svc.OAuthExchange(context.Background(), "google", "one-time-code", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Token != "tok-abc" {
		t.Fatalf("token = %q", res.Token)
	}
	if res.Expire != 259199 {
		t.Fatalf("expire = %d", res.Expire)
	}
	if res.Email != "u@example.com" {
		t.Fatalf("email = %q", res.Email)
	}
	if !p.exchangeCalled {
		t.Fatal("POST exchange must consume the code")
	}
}

func TestOAuthExchangeInvalidGrant(t *testing.T) {
	p := &fakeOAuthProvider{
		frontendURL: "https://app.com/success",
		exchangeErr: fmt.Errorf("%w: Code was already redeemed", domain.ErrOAuthInvalidGrant),
	}
	svc := newTestService(p, &fakeSession{})

	_, err := svc.OAuthExchange(context.Background(), "google", "used-code", "web")
	if !IsOAuthInvalidGrantErr(err) {
		t.Fatalf("want invalid grant err, got %v", err)
	}
}

func TestOAuthExchangeProviderUnavailable(t *testing.T) {
	p := &fakeOAuthProvider{
		frontendURL: "https://app.com/success",
		exchangeErr: context.DeadlineExceeded,
	}
	svc := newTestService(p, &fakeSession{})

	_, err := svc.OAuthExchange(context.Background(), "google", "c", "web")
	if !IsOAuthProviderUnavailableErr(err) {
		t.Fatalf("want provider-unavailable err, got %v", err)
	}
}

func TestOAuthExchangeExpireFallback(t *testing.T) {
	p := &fakeOAuthProvider{frontendURL: "https://app.com/success"}
	svc := newTestService(p, &fakeSession{timeoutErr: errors.New("redis down")})

	res, err := svc.OAuthExchange(context.Background(), "google", "c", "web")
	if err != nil {
		t.Fatalf("expire read failure must not block login: %v", err)
	}
	if res.Expire != 259200 {
		t.Fatalf("expire fallback = %d, want configured 259200", res.Expire)
	}
}
