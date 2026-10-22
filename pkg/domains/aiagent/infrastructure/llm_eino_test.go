package infrastructure

import (
	"context"
	"encoding/json"
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

// TestClaudeAcceptsSampling #140：新一代 Claude 不接受采样参数，旧一代与非 claude 模型名接受。
func TestClaudeAcceptsSampling(t *testing.T) {
	cases := map[string]bool{
		"claude-haiku-5-5":           false,
		"claude-sonnet-5-5":          false,
		"claude-sonnet-5":            false,
		"claude-opus-5-5":            false,
		"claude-opus-4-8":            false,
		"claude-opus-4-7":            false,
		"claude-fable-5-1":           false,
		"claude-mythos-5-1":          false,
		"anthropic/claude-haiku-5-5": false,
		"Claude-Haiku-5-5":           false,
		"claude-opus-4-6":            true,
		"claude-sonnet-4-6":          true,
		"claude-haiku-4-5":           true,
		"claude-opus-4-1-20250805":   true,
		"claude-sonnet-4-20250514":   true,
		"claude-3-7-sonnet-20250219": true,
		"deepseek-chat":              true,
		"kimi-k2":                    true,
	}
	for name, want := range cases {
		if got := claudeAcceptsSampling(name); got != want {
			t.Errorf("%s: want %v, got %v", name, want, got)
		}
	}
}

// TestClaudeSampling_KeepsOnlyTemperature 旧一代两者都配时只发 temperature；只配 top_p 时发 top_p。
func TestClaudeSampling_KeepsOnlyTemperature(t *testing.T) {
	temp, topP := claudeSampling("claude-haiku-4-5", map[string]interface{}{"temperature": 0.7, "top_p": 1.0})
	if temp == nil || *temp != float32(0.7) || topP != nil {
		t.Fatalf("both set: temp=%v topP=%v", temp, topP)
	}
	temp, topP = claudeSampling("claude-haiku-4-5", map[string]interface{}{"top_p": 0.9})
	if temp != nil || topP == nil || *topP != float32(0.9) {
		t.Fatalf("top_p only: temp=%v topP=%v", temp, topP)
	}
}

// fakeAnthropic 最小 Anthropic Messages 服务（环回地址），记录最后一次请求体。
func fakeAnthropic(t *testing.T, body *map[string]interface{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestGenerate_AnthropicSamplingParams #140：断言实际发出的请求体——新一代模型不带
// temperature / top_p（含分类器覆盖的 temperature=0），旧一代只带 temperature。
func TestGenerate_AnthropicSamplingParams(t *testing.T) {
	setAllowPrivate(t, true)
	params := map[string]interface{}{"temperature": float64(0), "top_p": 1.0, "max_tokens": float64(512)}
	cases := []struct {
		model    string
		wantTemp bool
	}{
		{"claude-haiku-5-5", false},
		{"claude-haiku-4-5", true},
	}
	for _, tc := range cases {
		var body map[string]interface{}
		srv := fakeAnthropic(t, &body)
		res, err := NewLLMCaller().Generate(context.Background(), agentapp.LLMRequest{
			Protocol: domain.ProtocolAnthropic, BaseURL: srv.URL, Model: tc.model, APIKey: "k",
			Params: params, SystemPrompt: "sys", UserPrompt: "ping",
		})
		if err != nil {
			t.Fatalf("%s: Generate: %v", tc.model, err)
		}
		if res.Content != "pong" {
			t.Fatalf("%s: content=%q", tc.model, res.Content)
		}
		if _, ok := body["top_p"]; ok {
			t.Errorf("%s: top_p must not be sent, body=%v", tc.model, body)
		}
		if _, ok := body["temperature"]; ok != tc.wantTemp {
			t.Errorf("%s: temperature sent=%v, want %v", tc.model, ok, tc.wantTemp)
		}
		if body["max_tokens"] != float64(512) {
			t.Errorf("%s: max_tokens=%v", tc.model, body["max_tokens"])
		}
	}
}
