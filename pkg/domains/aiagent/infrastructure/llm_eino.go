package infrastructure

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"interestBar/pkg/conf"
	agentapp "interestBar/pkg/domains/aiagent/application"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/util/netguard"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// llmEino 基于 eino 的 LLMCaller 实现。
//
// 每次调用按 agent 配置临时实例化 ChatModel（调用频率受限频约束，不做缓存/池化）。
// 本期支持 openai / anthropic 协议（决策 A）；gemini / ollama 返回未实现错误
// （依赖已安装，P2 再实现）。
type llmEino struct{}

// NewLLMCaller 构造 LLMCaller。
func NewLLMCaller() agentapp.LLMCaller {
	return &llmEino{}
}

// Generate 非流式单轮生成。
func (l *llmEino) Generate(ctx context.Context, req agentapp.LLMRequest) (*agentapp.LLMResult, error) {
	// #48：库里可能有本修复之前保存的 base_url，调用前再做一次静态校验（不合规 = 本次不调用）。
	allowPrivate := conf.Config.AiAgent.AllowPrivateBaseURL
	if req.BaseURL != "" {
		if _, err := netguard.CheckURL(req.BaseURL, allowPrivate); err != nil {
			return nil, fmt.Errorf("base_url rejected: %w", err)
		}
	}
	httpClient := netguard.NewHTTPClient(llmTimeout(), allowPrivate)

	var (
		chatModel model.BaseChatModel
		err       error
	)
	switch req.Protocol {
	case domain.ProtocolOpenAI:
		chatModel, err = l.newOpenAIModel(req, httpClient)
	case domain.ProtocolAnthropic:
		chatModel, err = l.newClaudeModel(req, httpClient)
	default:
		return nil, fmt.Errorf("protocol %q not implemented (supported: openai/anthropic)", req.Protocol)
	}
	if err != nil {
		return nil, err
	}

	resp, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(req.SystemPrompt),
		schema.UserMessage(req.UserPrompt),
	})
	if err != nil {
		return nil, err
	}

	result := &agentapp.LLMResult{Content: resp.Content}
	if resp.ResponseMeta != nil {
		result.PromptTokens = resp.ResponseMeta.Usage.PromptTokens
		result.CompletionTokens = resp.ResponseMeta.Usage.CompletionTokens
	}
	return result, nil
}

// newOpenAIModel 实例化 OpenAI 兼容模型（DeepSeek/Qwen/Kimi/中转站等均可走 base_url）。
//
// httpClient 为 netguard 受限客户端（拨号时校验目标 IP，#48）；其 Timeout 即单次调用超时。
func (l *llmEino) newOpenAIModel(req agentapp.LLMRequest, httpClient *http.Client) (model.BaseChatModel, error) {
	cfg := &openai.ChatModelConfig{
		APIKey:     req.APIKey,
		BaseURL:    req.BaseURL,
		Model:      req.Model,
		HTTPClient: httpClient,
	}
	if v, ok := paramFloat(req.Params, "temperature"); ok {
		cfg.Temperature = float32Ptr(v)
	}
	if v, ok := paramFloat(req.Params, "top_p"); ok {
		cfg.TopP = float32Ptr(v)
	}
	if v, ok := paramInt(req.Params, "max_tokens"); ok {
		cfg.MaxCompletionTokens = intPtr(v)
	}
	if v, ok := paramFloat(req.Params, "presence_penalty"); ok {
		cfg.PresencePenalty = float32Ptr(v)
	}
	if v, ok := paramFloat(req.Params, "frequency_penalty"); ok {
		cfg.FrequencyPenalty = float32Ptr(v)
	}
	return openai.NewChatModel(context.Background(), cfg)
}

// newClaudeModel 实例化 Anthropic Claude 模型。
//
// claude 的 MaxTokens 为必填项：未配置 max_tokens 时用 1024 兜底；
// presence/frequency_penalty 为 OpenAI 特有参数，Claude 不支持，忽略。
func (l *llmEino) newClaudeModel(req agentapp.LLMRequest, httpClient *http.Client) (model.BaseChatModel, error) {
	cfg := &claude.Config{
		APIKey:         req.APIKey,
		Model:          req.Model,
		MaxTokens:      1024,
		RequestTimeout: llmTimeout(),
		HTTPClient:     httpClient,
	}
	if req.BaseURL != "" {
		baseURL := req.BaseURL
		cfg.BaseURL = &baseURL
	}
	if v, ok := paramInt(req.Params, "max_tokens"); ok {
		cfg.MaxTokens = v
	}
	if v, ok := paramFloat(req.Params, "temperature"); ok {
		cfg.Temperature = float32Ptr(v)
	}
	if v, ok := paramFloat(req.Params, "top_p"); ok {
		cfg.TopP = float32Ptr(v)
	}
	return claude.NewChatModel(context.Background(), cfg)
}

// llmTimeout 单次 LLM 调用超时（配置兜底，与 application.replyTimeout 同源）。
func llmTimeout() time.Duration {
	sec := conf.Config.AiAgent.TimeoutSec
	if sec <= 0 {
		sec = 30
	}
	return time.Duration(sec) * time.Second
}

// ===== llm_params 取值 helper（值为数字，白名单已在 CRUD 层校验）=====

func paramFloat(params map[string]interface{}, key string) (float64, bool) {
	v, ok := params[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

func paramInt(params map[string]interface{}, key string) (int, bool) {
	f, ok := paramFloat(params, key)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func float32Ptr(v float64) *float32 {
	f := float32(v)
	return &f
}

func intPtr(v int) *int {
	return &v
}
