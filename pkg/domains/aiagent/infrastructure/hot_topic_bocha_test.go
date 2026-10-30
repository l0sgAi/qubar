package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
)

const testSearchKey = "<bocha-test-key>"

// fakeQuota 内存配额：记录 Consume 调用，超过 limit 返回 ErrSearchQuotaExhausted。
type fakeQuota struct {
	mu     sync.Mutex
	used   map[uuid.UUID]int
	limits []int
}

func (q *fakeQuota) Consume(_ context.Context, agentID uuid.UUID, limit int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.used == nil {
		q.used = map[uuid.UUID]int{}
	}
	q.used[agentID]++
	q.limits = append(q.limits, limit)
	if q.used[agentID] > limit {
		return domain.ErrSearchQuotaExhausted
	}
	return nil
}

// fakeBocha 模拟博查服务；handler 为空时返回 body。
func fakeBocha(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func newTestSource(t *testing.T, endpoint string, timeoutSec int, quota domain.SearchQuota) domain.HotTopicSource {
	t.Helper()
	if quota == nil {
		quota = &fakeQuota{}
	}
	src := NewHotTopicSource(conf.AiAgentSearch{
		Provider:   "bocha",
		Endpoint:   endpoint,
		APIKey:     testSearchKey,
		TimeoutSec: timeoutSec,
	}, quota, true)
	if _, ok := src.(*quotaHotTopicSource); !ok {
		t.Fatalf("NewHotTopicSource = %T, want enabled source", src)
	}
	return src
}

func agentOpts() domain.SearchOptions { return domain.SearchOptions{AgentID: uuid.New()} }

func assertNoKey(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), testSearchKey) {
		t.Fatalf("error leaks api key: %v", err)
	}
}

// 正常响应：请求带 Bearer key / freshness / count；字段映射正确。
func TestBochaSearch_OK(t *testing.T) {
	t.Parallel()
	var gotAuth string
	var gotReq bochaRequest
	srv := fakeBocha(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		jsonHandler(200, `{"code":200,"log_id":"x","msg":null,"data":{"_type":"SearchResponse","webPages":{"value":[
			{"name":"  热点 一 ","url":"https://news.example.com/a?id=1","snippet":"摘要一","summary":"长摘要","siteName":"示例新闻","datePublished":"2026-10-22T08:00:00+08:00"},
			{"name":"热点二","url":"http://b.example.org/x","snippet":"","summary":"只有长摘要","siteName":""}
		]}}}`)(w, r)
	})

	items, err := newTestSource(t, srv.URL, 5, nil).Search(context.Background(), " AI 新闻 ", domain.SearchOptions{
		AgentID: uuid.New(), MaxResults: 5, FreshnessHours: 72,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotAuth != "Bearer "+testSearchKey {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotReq.Query != "AI 新闻" || gotReq.Count != 5 || gotReq.Freshness != "oneWeek" {
		t.Errorf("request = %+v", gotReq)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	want0 := domain.TopicItem{Title: "热点 一", Snippet: "摘要一", URL: "https://news.example.com/a?id=1", Source: "示例新闻"}
	got0 := items[0]
	got0.PublishedAt = time.Time{}
	if got0 != want0 {
		t.Errorf("items[0] = %+v, want %+v", got0, want0)
	}
	if !items[0].PublishedAt.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("PublishedAt = %v", items[0].PublishedAt)
	}
	// snippet 缺省回落 summary；siteName 缺省取 host；无发布时间为零值
	if items[1].Snippet != "只有长摘要" || items[1].Source != "b.example.org" || !items[1].PublishedAt.IsZero() {
		t.Errorf("items[1] = %+v", items[1])
	}
}

// 超时：服务商不响应时按 timeout_sec 返回错误。
func TestBochaSearch_Timeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := fakeBocha(t, func(_ http.ResponseWriter, r *http.Request) {
		select { // 测试结束前一直不响应
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) }) // 先于 srv.Close 执行（Cleanup 后进先出）
	src, err := newBochaSource(srv.URL, testSearchKey, 100*time.Millisecond, true)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = src.Search(context.Background(), "q", domain.SearchOptions{MaxResults: 10, FreshnessHours: 24})
	if err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("timeout not enforced: %v", time.Since(start))
	}
	assertNoKey(t, err)
}

// 非 2xx 与业务错误码：返回错误、不回显响应体与 key。
func TestBochaSearch_ErrorResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		status  int
		body    string
		wantMsg string
	}{
		{"unauthorized", 401, `{"code":401,"msg":"invalid key ` + testSearchKey + `"}`, "http status 401"},
		{"server error", 502, `bad gateway`, "http status 502"},
		{"api code", 200, `{"code":403,"msg":"余额不足 ` + testSearchKey + `","data":null}`, "api code 403"},
		{"bad json", 200, `{"code":200,"data":`, "decode response"},
		{"missing data", 200, `{"code":200,"msg":null}`, "missing data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := fakeBocha(t, jsonHandler(tc.status, tc.body))
			_, err := newTestSource(t, srv.URL, 5, nil).Search(context.Background(), "q", agentOpts())
			if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want contains %q", err, tc.wantMsg)
			}
			assertNoKey(t, err)
		})
	}
}

// 字段缺失：无 webPages = 空结果；无标题 / 无链接 / 非 http(s) 链接的条目丢弃。
func TestBochaSearch_MissingFields(t *testing.T) {
	t.Parallel()
	srv := fakeBocha(t, jsonHandler(200, `{"code":200,"data":{}}`))
	items, err := newTestSource(t, srv.URL, 5, nil).Search(context.Background(), "q", agentOpts())
	if err != nil || len(items) != 0 {
		t.Fatalf("no webPages: items=%v err=%v", items, err)
	}

	srv = fakeBocha(t, jsonHandler(200, `{"code":200,"data":{"webPages":{"value":[
		{"url":"https://a.example.com/"},
		{"name":"无链接"},
		{"name":"js","url":"javascript:alert(1)"},
		{"name":"ftp","url":"ftp://a.example.com/f"},
		{"name":"无 host","url":"https:///path"},
		{"name":"带凭据","url":"https://u:p@a.example.com/"},
		{"name":"   ","url":"https://a.example.com/blank"},
		{"name":"保留","url":"HTTPS://ok.example.com/p","datePublished":"not-a-date"}
	]}}}`))
	items, err = newTestSource(t, srv.URL, 5, nil).Search(context.Background(), "q", agentOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "保留" || items[0].Source != "ok.example.com" || !items[0].PublishedAt.IsZero() {
		t.Fatalf("items = %+v", items)
	}
}

// 超长截断：标题 / 摘要按 rune 截断，控制字符清洗；超长 URL 整条丢弃；条数按 MaxResults 截断。
func TestBochaSearch_Truncation(t *testing.T) {
	t.Parallel()
	longTitle := strings.Repeat("热", maxTopicTitleRunes+50)
	longSnippet := strings.Repeat("a\x00b\n", maxTopicSnippetRunes)
	longURL := "https://a.example.com/" + strings.Repeat("x", maxTopicURLBytes)
	pages := []bochaWebPage{
		{Name: longTitle, URL: "https://a.example.com/1", Snippet: longSnippet},
		{Name: "超长链接", URL: longURL},
	}
	for i := 0; i < 5; i++ {
		pages = append(pages, bochaWebPage{Name: "条目", URL: "https://a.example.com/n"})
	}
	body, _ := json.Marshal(map[string]any{"code": 200, "data": map[string]any{"webPages": map[string]any{"value": pages}}})
	srv := fakeBocha(t, jsonHandler(200, string(body)))

	items, err := newTestSource(t, srv.URL, 5, nil).Search(context.Background(), "q", domain.SearchOptions{AgentID: uuid.New(), MaxResults: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (MaxResults)", len(items))
	}
	if n := len([]rune(items[0].Title)); n != maxTopicTitleRunes {
		t.Errorf("title runes = %d", n)
	}
	if n := len([]rune(items[0].Snippet)); n != maxTopicSnippetRunes {
		t.Errorf("snippet runes = %d", n)
	}
	if strings.ContainsAny(items[0].Snippet, "\x00\n") {
		t.Errorf("snippet not cleaned: %q", items[0].Snippet[:20])
	}
	for _, it := range items {
		if it.Title == "超长链接" {
			t.Errorf("overlong URL item kept")
		}
	}
}

// MaxResults 超过博查上限时按 50 请求。
func TestBochaSearch_MaxResultsCapped(t *testing.T) {
	t.Parallel()
	var gotReq bochaRequest
	srv := fakeBocha(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		jsonHandler(200, `{"code":200,"data":{}}`)(w, r)
	})
	opts := agentOpts()
	opts.MaxResults = 500
	if _, err := newTestSource(t, srv.URL, 5, nil).Search(context.Background(), "q", opts); err != nil {
		t.Fatal(err)
	}
	if gotReq.Count != maxSearchMaxResult || gotReq.Freshness != "oneDay" {
		t.Errorf("request = %+v", gotReq)
	}
}

// 没配 key / provider 不支持 / endpoint 非法：返回"未启用"，不发请求。
func TestNewHotTopicSource_Disabled(t *testing.T) {
	t.Parallel()
	hit := false
	srv := fakeBocha(t, func(http.ResponseWriter, *http.Request) { hit = true })
	cases := map[string]struct {
		cfg          conf.AiAgentSearch
		allowPrivate bool
	}{
		"no key":          {conf.AiAgentSearch{Provider: "bocha", Endpoint: srv.URL, APIKey: "  "}, true},
		"unknown":         {conf.AiAgentSearch{Provider: "google", Endpoint: srv.URL, APIKey: testSearchKey}, true},
		"empty provider":  {conf.AiAgentSearch{Endpoint: srv.URL, APIKey: testSearchKey}, true},
		"private blocked": {conf.AiAgentSearch{Provider: "bocha", Endpoint: srv.URL, APIKey: testSearchKey}, false},
	}
	for name, tc := range cases {
		q := &fakeQuota{}
		_, err := NewHotTopicSource(tc.cfg, q, tc.allowPrivate).Search(context.Background(), "q", agentOpts())
		if !errors.Is(err, domain.ErrSearchDisabled) {
			t.Errorf("%s: err = %v, want ErrSearchDisabled", name, err)
		}
		if len(q.limits) != 0 {
			t.Errorf("%s: quota consumed while disabled", name)
		}
	}
	if hit {
		t.Error("disabled source sent a request")
	}
}

// 配额：默认取平台配置；用尽后返回 ErrSearchQuotaExhausted 且不再请求服务商；参数错误不扣配额。
func TestHotTopicSource_Quota(t *testing.T) {
	t.Parallel()
	calls := 0
	var mu sync.Mutex
	srv := fakeBocha(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		jsonHandler(200, `{"code":200,"data":{}}`)(w, r)
	})
	q := &fakeQuota{}
	src := NewHotTopicSource(conf.AiAgentSearch{Provider: "Bocha", Endpoint: srv.URL, APIKey: testSearchKey, DailyQuota: 2}, q, true)
	ctx := context.Background()

	if _, err := src.Search(ctx, "  ", agentOpts()); !errors.Is(err, errEmptySearchQuery) {
		t.Fatalf("empty query err = %v", err)
	}
	if _, err := src.Search(ctx, "q", domain.SearchOptions{}); !errors.Is(err, errSearchNoAgent) {
		t.Fatalf("no agent err = %v", err)
	}
	if len(q.limits) != 0 {
		t.Fatal("invalid args consumed quota")
	}

	opts := agentOpts()
	for i := 0; i < 2; i++ {
		if _, err := src.Search(ctx, "q", opts); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if _, err := src.Search(ctx, "q", opts); !errors.Is(err, domain.ErrSearchQuotaExhausted) {
		t.Fatalf("3rd call err = %v, want ErrSearchQuotaExhausted", err)
	}
	if calls != 2 {
		t.Errorf("provider calls = %d, want 2", calls)
	}
	// 机器人级覆盖优先于平台默认
	opts.DailyQuota = 7
	_, _ = src.Search(ctx, "q", opts)
	if got := q.limits[len(q.limits)-1]; got != 7 {
		t.Errorf("limit = %d, want 7", got)
	}
}

func TestBochaFreshness(t *testing.T) {
	t.Parallel()
	for hours, want := range map[int]string{1: "oneDay", 24: "oneDay", 25: "oneWeek", 168: "oneWeek", 720: "oneMonth", 2000: "oneYear", 100000: "noLimit"} {
		if got := bochaFreshness(hours); got != want {
			t.Errorf("bochaFreshness(%d) = %s, want %s", hours, got, want)
		}
	}
}

// 自然日按 Asia/Shanghai：UTC 16:00 已是次日；过期时刻 = 次日零点 + 1h。
func TestQuotaDay(t *testing.T) {
	t.Parallel()
	day, exp := quotaDay(time.Date(2026, 10, 22, 16, 30, 0, 0, time.UTC))
	if day != "20261023" {
		t.Errorf("day = %s", day)
	}
	if want := time.Date(2026, 10, 23, 17, 0, 0, 0, time.UTC); !exp.Equal(want) {
		t.Errorf("expireAt = %v, want %v", exp, want)
	}
}
