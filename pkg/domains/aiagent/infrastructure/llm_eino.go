package infrastructure

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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
	cfg.Temperature, cfg.TopP = claudeSampling(req.Model, req.Params)
	return claude.NewChatModel(context.Background(), cfg)
}

// claudeGenRe 从模型名提取 Claude 家族与版本号：claude-haiku-5-5 → (5, 5)、
// claude-opus-4-1-20250805 → (4, 1)、claude-sonnet-4-20250514 → (4, 无)。
// 未锚定开头，兼容 anthropic/claude-xxx 这类中转站前缀。
var claudeGenRe = regexp.MustCompile(`claude-(?:opus|sonnet|haiku)-(\d+)(?:-(\d{1,2}))?(?:$|[^0-9])`)

// claudeSampling 按模型代际决定发送的采样参数（#140）：
//   - 新一代（Opus 4.7+ / Sonnet、Haiku 5+ / Fable / Mythos）不接受 temperature、top_p，
//     传了即 400，一律不发；分类器覆盖的 temperature=0 也在此被丢弃；
//   - 旧一代不允许同时传两者（Opus 4.1 起），两者都配时只保留 temperature；
//   - 非 claude-* 模型名（第三方 Anthropic 兼容端点）同样适用上一条，其余原样透传。
func claudeSampling(modelName string, params map[string]interface{}) (temperature, topP *float32) {
	if !claudeAcceptsSampling(modelName) {
		return nil, nil
	}
	if v, ok := paramFloat(params, "temperature"); ok {
		return float32Ptr(v), nil
	}
	if v, ok := paramFloat(params, "top_p"); ok {
		return nil, float32Ptr(v)
	}
	return nil, nil
}

// claudeAcceptsSampling 模型是否仍接受 temperature / top_p。无法识别的模型名按接受处理
// （第三方兼容端点的自有模型），只拦截可确定不接受的 Claude 新一代模型。
func claudeAcceptsSampling(modelName string) bool {
	name := strings.ToLower(modelName)
	if strings.Contains(name, "claude-fable") || strings.Contains(name, "claude-mythos") {
		return false
	}
	m := claudeGenRe.FindStringSubmatch(name)
	if m == nil {
		return true
	}
	major, _ := strconv.Atoi(m[1])
	minor := 0
	if m[2] != "" {
		minor, _ = strconv.Atoi(m[2])
	}
	return major < 4 || (major == 4 && minor < 7)
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
