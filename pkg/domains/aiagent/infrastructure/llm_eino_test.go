package infrastructure

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"interestBar/pkg/conf"
	agentapp "interestBar/pkg/domains/aiagent/application"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/util/netguard"
)

func setAllowPrivate(t *testing.T, v bool) {
	t.Helper()
	conf.Config = &conf.AppConfig{}
	conf.Config.AiAgent.TimeoutSec = 5
	conf.Config.AiAgent.AllowPrivateBaseURL = v
}

// fakeOpenAI 最小 OpenAI 兼容服务（环回地址），记录是否被访问。
func fakeOpenAI(t *testing.T, hit *bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*hit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestGenerate_RejectsInternalBaseURL #48：历史数据中的内网 base_url 在调用时被拒，请求不会发出。
func TestGenerate_RejectsInternalBaseURL(t *testing.T) {
	setAllowPrivate(t, false)
	hit := false
	srv := fakeOpenAI(t, &hit)

	for _, proto := range []string{domain.ProtocolOpenAI, domain.ProtocolAnthropic} {
		_, err := NewLLMCaller().Generate(context.Background(), agentapp.LLMRequest{
			Protocol: proto, BaseURL: srv.URL, Model: "m", APIKey: "k", UserPrompt: "ping",
		})
		if !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("%s: want ErrBlocked, got %v", proto, err)
		}
	}
	if hit {
		t.Fatal("internal endpoint must not be reached")
	}
}

// TestGenerate_AllowPrivateUsesGuardedClient allow_private_base_url=true 时经受限客户端正常完成调用
// （验证 HTTPClient 注入后 openai 链路仍可用）。
func TestGenerate_AllowPrivateUsesGuardedClient(t *testing.T) {
	setAllowPrivate(t, true)
	hit := false
	srv := fakeOpenAI(t, &hit)

	res, err := NewLLMCaller().Generate(context.Background(), agentapp.LLMRequest{
		Protocol: domain.ProtocolOpenAI, BaseURL: srv.URL, Model: "m", APIKey: "k", UserPrompt: "ping",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !hit || res.Content != "pong" {
		t.Fatalf("hit=%v content=%q", hit, res.Content)
	}
}
