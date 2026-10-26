package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/util/netguard"
)

// defaultBochaEndpoint 博查 Web Search API（国内直连可达，#100）。
const defaultBochaEndpoint = "https://api.bochaai.com/v1/web-search"

// 结果字段截断上限（rune 数，URL 为字节数）。
const (
	maxTopicTitleRunes   = 200
	maxTopicSnippetRunes = 500
	maxTopicSourceRunes  = 100
	maxTopicURLBytes     = 2048
	maxBochaBodyBytes    = 2 << 20 // 响应体上限 2MB
	maxBochaErrMsgRunes  = 200
)

// bochaSource 博查 Web Search 适配器。只读取搜索结果的标题 / 摘要 / 链接 / 时间，
// 绝不请求结果里的 URL（SSRF，见 #100）。出站走 netguard 受限客户端：直连不走代理（#48）。
type bochaSource struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

func newBochaSource(endpoint, apiKey string, timeout time.Duration, allowPrivate bool) (*bochaSource, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = defaultBochaEndpoint
	}
	if _, err := netguard.CheckURL(endpoint, allowPrivate); err != nil {
		return nil, err
	}
	return &bochaSource{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   netguard.NewHTTPClient(timeout, allowPrivate),
	}, nil
}

type bochaRequest struct {
	Query     string `json:"query"`
	Freshness string `json:"freshness"`
	Summary   bool   `json:"summary"`
	Count     int    `json:"count"`
}

type bochaResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data *struct {
		WebPages *struct {
			Value []bochaWebPage `json:"value"`
		} `json:"webPages"`
	} `json:"data"`
}

type bochaWebPage struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Snippet       string `json:"snippet"`
	Summary       string `json:"summary"`
	SiteName      string `json:"siteName"`
	DatePublished string `json:"datePublished"`
}

// Search opts 已由 quotaHotTopicSource 补齐默认值。
func (b *bochaSource) Search(ctx context.Context, query string, opts domain.SearchOptions) ([]domain.TopicItem, error) {
	body, err := json.Marshal(bochaRequest{
		Query:     query,
		Freshness: bochaFreshness(opts.FreshnessHours),
		Summary:   true,
		Count:     opts.MaxResults,
	})
	if err != nil {
		return nil, fmt.Errorf("bocha search: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, b.redact(fmt.Errorf("bocha search: build request: %w", err))
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, b.redact(fmt.Errorf("bocha search: request: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 不回显响应体：服务商报错信息可能回带请求头片段
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBochaBodyBytes))
		return nil, fmt.Errorf("bocha search: http status %d", resp.StatusCode)
	}
	var out bochaResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBochaBodyBytes)).Decode(&out); err != nil {
		return nil, b.redact(fmt.Errorf("bocha search: decode response: %w", err))
	}
	if out.Code != 0 && out.Code != http.StatusOK {
		return nil, b.redact(fmt.Errorf("bocha search: api code %d: %s", out.Code, truncateRunes(out.Msg, maxBochaErrMsgRunes)))
	}
	if out.Data == nil {
		return nil, errors.New("bocha search: response missing data")
	}
	if out.Data.WebPages == nil {
		return []domain.TopicItem{}, nil // 无匹配结果
	}
	items := make([]domain.TopicItem, 0, min(len(out.Data.WebPages.Value), opts.MaxResults))
	for _, p := range out.Data.WebPages.Value {
		if len(items) >= opts.MaxResults {
			break
		}
		if item, ok := toTopicItem(p); ok {
			items = append(items, item)
		}
	}
	return items, nil
}

// redact 把错误信息中可能出现的 api_key 替换掉（key 只在请求头里，这里是兜底）。
func (b *bochaSource) redact(err error) error {
	if b.apiKey == "" || !strings.Contains(err.Error(), b.apiKey) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), b.apiKey, "[REDACTED]"))
}

// bochaFreshness 小时数 → 博查 freshness 枚举（向上取整到最近的档位）。
func bochaFreshness(hours int) string {
	switch {
	case hours <= 24:
		return "oneDay"
	case hours <= 24*7:
		return "oneWeek"
	case hours <= 24*31:
		return "oneMonth"
	case hours <= 24*366:
		return "oneYear"
	default:
		return "noLimit"
	}
}

// toTopicItem 清洗单条结果：标题为空或 URL 不合规的条目丢弃；各文本字段截断。
func toTopicItem(p bochaWebPage) (domain.TopicItem, bool) {
	title := truncateRunes(cleanText(p.Name), maxTopicTitleRunes)
	if title == "" {
		return domain.TopicItem{}, false
	}
	u, ok := safeResultURL(p.URL)
	if !ok {
		return domain.TopicItem{}, false
	}
	snippet := cleanText(p.Snippet)
	if snippet == "" {
		snippet = cleanText(p.Summary)
	}
	source := cleanText(p.SiteName)
	if source == "" {
		source = u.Hostname()
	}
	item := domain.TopicItem{
		Title:   title,
		Snippet: truncateRunes(snippet, maxTopicSnippetRunes),
		URL:     u.String(),
		Source:  truncateRunes(source, maxTopicSourceRunes),
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(p.DatePublished)); err == nil {
		item.PublishedAt = t
	}
	return item, true
}

// safeResultURL 结果链接只做文本校验，不发起任何请求：scheme 仅 http/https、host 非空。
// 超过 maxTopicURLBytes 的链接整条丢弃——截断后的链接已失效，不能作为来源链接展示。
func safeResultURL(raw string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxTopicURLBytes {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return nil, false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, false
	}
	return u, true
}

// cleanText 去掉非法 UTF-8 与控制字符（含 NUL，PG 文本不接受），压缩连续空白。
func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// truncateRunes 按 rune 截断到 limit 个字符。
func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}
