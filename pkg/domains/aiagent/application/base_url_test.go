package application

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"interestBar/pkg/conf"
)

type stubResolver map[string][]netip.Addr

func (r stubResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if a, ok := r[host]; ok {
		return a, nil
	}
	return nil, errors.New("no such host")
}

func withBaseURLResolver(t *testing.T, r stubResolver) {
	t.Helper()
	prev := baseURLResolver
	baseURLResolver = r
	t.Cleanup(func() { baseURLResolver = prev })
}

// TestValidateBaseURL #48：base_url 只收公网 https，内网 / 环回 / 元数据地址与解析到内网的域名一律拒绝。
func TestValidateBaseURL(t *testing.T) {
	withBaseURLResolver(t, stubResolver{
		"api.example.com":      {netip.MustParseAddr("93.184.216.34")},
		"internal.example.com": {netip.MustParseAddr("10.0.0.8")},
	})
	ctx := context.Background()

	for _, ok := range []string{"", "https://api.example.com/v1", "https://1.1.1.1/v1"} {
		if err := validateBaseURL(ctx, ok); err != nil {
			t.Errorf("%q: unexpected %v", ok, err)
		}
	}
	for _, bad := range []string{
		"http://api.example.com/v1",
		"https://127.0.0.1:9200/pg.domains.users/_doc/x?",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/",
		"https://internal.example.com/v1",
		"https://nxdomain.example.com/v1",
		"https://u:p@api.example.com/v1",
		"gopher://api.example.com",
	} {
		if err := validateBaseURL(ctx, bad); !IsInvalidBaseURLErr(err) {
			t.Errorf("%q: want errInvalidBaseURL, got %v", bad, err)
		}
	}
}

type dnsErrResolver struct{ err error }

func (r dnsErrResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, r.err
}

// DNS 临时故障 → 可重试错误（503），不误报「必须是公网 https」；请求已取消 → 透传 ctx 错误。
func TestValidateBaseURL_DNSFailure(t *testing.T) {
	prev := baseURLResolver
	t.Cleanup(func() { baseURLResolver = prev })

	baseURLResolver = dnsErrResolver{&net.DNSError{Err: "i/o timeout", Name: "api.example.com", IsTimeout: true}}
	if err := validateBaseURL(context.Background(), "https://api.example.com/v1"); !IsBaseURLNoDNSErr(err) {
		t.Errorf("timeout: want errBaseURLNoDNS, got %v", err)
	}

	baseURLResolver = dnsErrResolver{&net.DNSError{Err: "no such host", Name: "api.example.com", IsNotFound: true}}
	if err := validateBaseURL(context.Background(), "https://api.example.com/v1"); !IsInvalidBaseURLErr(err) {
		t.Errorf("nxdomain: want errInvalidBaseURL, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	baseURLResolver = dnsErrResolver{context.Canceled}
	if err := validateBaseURL(ctx, "https://api.example.com/v1"); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: want context.Canceled, got %v", err)
	}
}

func TestValidateBaseURL_AllowPrivateFlag(t *testing.T) {
	withBaseURLResolver(t, stubResolver{})
	conf.Config.AiAgent.AllowPrivateBaseURL = true
	t.Cleanup(func() { conf.Config.AiAgent.AllowPrivateBaseURL = false })

	if err := validateBaseURL(context.Background(), "http://127.0.0.1:11434/v1"); err != nil {
		t.Fatalf("allow_private_base_url=true should accept local gateway, got %v", err)
	}
}

// TestCreateAndUpdate_RejectPrivateBaseURL 创建（validateAndBuildAgent）与更新（buildAgentUpdateFields）
// 两条入口都走 validateBaseURL（全局 / 圈内共用）。
func TestCreateAndUpdate_RejectPrivateBaseURL(t *testing.T) {
	withBaseURLResolver(t, stubResolver{})

	_, err := validateAndBuildAgent(context.Background(), CreateAgentInput{
		Name: "bot", APIProtocol: "openai", Model: "gpt-x", TriggerMode: 3,
		BaseURL: "https://169.254.169.254/",
	})
	if !IsInvalidBaseURLErr(err) {
		t.Fatalf("create: want errInvalidBaseURL, got %v", err)
	}

	repo := newFakeAgentRepo()
	seedCircleAgent(repo)
	svc := newCircleAgentSvc(repo, newFakeCircleRoles())
	_, err = svc.UpdateCircleAgent(context.Background(), ownerID, circleAgentID,
		UpdateAgentInput{BaseURL: strPtr("https://10.0.0.1/v1")})
	if !IsInvalidBaseURLErr(err) {
		t.Fatalf("update: want errInvalidBaseURL, got %v", err)
	}
}
