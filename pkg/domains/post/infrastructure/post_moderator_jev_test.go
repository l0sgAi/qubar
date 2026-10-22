package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/post/domain"
)

const testJevKey = "<jev-test-key>"

// jevOKResponse 数值取自 #45 实测（jev-1.13.0、中文题目、spam_ad 样本），legend / probabilities 按 OpenAPI 结构补全。
const jevOKResponse = `{
  "model": "jev-1.13.0",
  "answers": {
    "sexual": {"type": "noul", "noul": 0.02},
    "violence_terror": {"type": "noul", "noul": 0.01},
    "harassment": {"type": "noul", "noul": 0.02},
    "illegal": {"type": "noul", "noul": 0.88},
    "spam_ad": {"type": "noul", "noul": 0.99},
    "privacy": {"type": "noul", "noul": 0.06},
    "self_harm": {"type": "noul", "noul": 0.01},
    "manipulation": {"type": "noul", "noul": 0.03},
    "severity": {
      "type": "score", "score": 1.97, "confidence": 0.93,
      "legend": {"0": "无", "1": "轻微", "2": "严重", "3": "极端"},
      "probabilities": {"0": 0.01, "1": 0.03, "2": 0.94, "3": 0.02}
    }
  },
  "usage": {"input_tokens": 1116, "output_tokens": 18}
}`

// jevServer 按顺序返回 handlers 里的响应（用完后重复最后一个），并记录请求。
type jevServer struct {
	*httptest.Server
	mu       sync.Mutex
	calls    int32
	requests []*recordedReq
}

type recordedReq struct {
	method, path, auth, contentType string
	body                            []byte
}

func newJevServer(t *testing.T, handlers ...http.HandlerFunc) *jevServer {
	t.Helper()
	s := &jevServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := atomic.AddInt32(&s.calls, 1)
		s.mu.Lock()
		s.requests = append(s.requests, &recordedReq{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
		s.mu.Unlock()
		handlers[min(int(n), len(handlers))-1](w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jevServer) callCount() int { return int(atomic.LoadInt32(&s.calls)) }

func (s *jevServer) request(i int) *recordedReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[i]
}

func respond(code int, body string, headers ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

// fakeSleep 记录退避时长，不真正等待。
type fakeSleep struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (f *fakeSleep) sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	f.waits = append(f.waits, d)
	f.mu.Unlock()
	return ctx.Err()
}

func newTestModerator(t *testing.T, baseURL string, tweak func(*conf.Moderation)) (*jevModerator, *fakeSleep) {
	t.Helper()
	cfg := conf.Moderation{Enabled: true, APIKey: testJevKey, BaseURL: baseURL, MaxRetries: 2}
	if tweak != nil {
		tweak(&cfg)
	}
	m := newJevModerator(cfg, &http.Client{})
	fs := &fakeSleep{}
	m.sleep = fs.sleep
	m.timeout = 2 * time.Second
	return m, fs
}

var testInput = domain.ModerationInput{
	CircleName: "数码爱好者", CircleCategory: "数码",
	Title:   "低价出售全新手机，加微信详聊",
	Content: "全新未拆封，官网价五折！加微信 abc12345 领取优惠券。",
}

// assertNoKey 任何错误信息都不能带出 api_key。
func assertNoKey(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), testJevKey) {
		t.Fatalf("error leaks api key: %v", err)
	}
}

func TestJevModerator_OK(t *testing.T) {
	t.Parallel()
	srv := newJevServer(t, respond(http.StatusOK, jevOKResponse))
	m, _ := newTestModerator(t, srv.URL+"/", nil) // 尾部斜杠不影响路径

	a, err := m.Assess(context.Background(), testInput)
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if a.Model != "jev-1.13.0" || a.InputTokens != 1116 || a.Attempts != 1 || a.Latency <= 0 {
		t.Fatalf("unexpected meta: %+v", a)
	}
	if a.Severity != 1.97 || a.SeverityConfidence != 0.93 {
		t.Fatalf("severity = %v / %v", a.Severity, a.SeverityConfidence)
	}
	if len(a.Risks) != len(domain.Hazards) || a.Risks[domain.HazardSpamAd] != 0.99 || a.Risks[domain.HazardIllegal] != 0.88 {
		t.Fatalf("risks = %v", a.Risks)
	}

	req := srv.request(0)
	if req.method != http.MethodPost || req.path != "/v1/systemone" {
		t.Fatalf("request = %s %s", req.method, req.path)
	}
	if req.auth != "Bearer "+testJevKey || req.contentType != "application/json" {
		t.Fatalf("headers: auth=%q content-type=%q", req.auth, req.contentType)
	}

	var body struct {
		Model string `json:"model"`
		State struct {
			Circle map[string]string `json:"circle"`
			Post   map[string]string `json:"post"`
		} `json:"state"`
		Questions map[string]struct {
			Type         string          `json:"type"`
			Instructions string          `json:"instructions"`
			Criteria     json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if body.Model != conf.DefaultModerationModel {
		t.Fatalf("model = %q", body.Model)
	}
	if body.State.Circle["name"] != testInput.CircleName || body.State.Circle["category"] != testInput.CircleCategory ||
		body.State.Post["title"] != testInput.Title || body.State.Post["content"] != testInput.Content {
		t.Fatalf("state = %+v", body.State)
	}
	if len(body.Questions) != len(domain.Hazards)+1 {
		t.Fatalf("want %d questions, got %d", len(domain.Hazards)+1, len(body.Questions))
	}
	for _, h := range domain.Hazards {
		q := body.Questions[string(h)]
		var crit map[string]string
		if q.Type != "noul" || q.Instructions == "" || json.Unmarshal(q.Criteria, &crit) != nil || crit["true"] == "" || crit["false"] == "" {
			t.Fatalf("question %q malformed: %+v", h, q)
		}
	}
	sev := body.Questions["severity"]
	var levels []string
	if sev.Type != "score" || json.Unmarshal(sev.Criteria, &levels) != nil || len(levels) != 4 {
		t.Fatalf("severity question malformed: %+v", sev)
	}
}

func TestJevModerator_TimeoutRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	hang := func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }
	srv := newJevServer(t, hang, respond(http.StatusOK, jevOKResponse))
	m, fs := newTestModerator(t, srv.URL, nil)
	m.timeout = 50 * time.Millisecond

	a, err := m.Assess(context.Background(), testInput)
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if a.Attempts != 2 || srv.callCount() != 2 || len(fs.waits) != 1 {
		t.Fatalf("attempts=%d calls=%d waits=%v", a.Attempts, srv.callCount(), fs.waits)
	}
}

func TestJevModerator_TimeoutExhausted(t *testing.T) {
	t.Parallel()
	hang := func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }
	srv := newJevServer(t, hang)
	m, _ := newTestModerator(t, srv.URL, func(c *conf.Moderation) { c.MaxRetries = 1 })
	m.timeout = 50 * time.Millisecond

	_, err := m.Assess(context.Background(), testInput)
	if !errors.Is(err, domain.ErrModerationUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want unavailable + deadline exceeded, got %v", err)
	}
	if srv.callCount() != 2 {
		t.Fatalf("want 2 calls (1 retry), got %d", srv.callCount())
	}
	assertNoKey(t, err)
}

func TestJevModerator_429HonorsRetryAfter(t *testing.T) {
	t.Parallel()
	srv := newJevServer(t,
		respond(http.StatusTooManyRequests, `{"detail":"rate limited"}`, "Retry-After", "3"),
		respond(http.StatusOK, jevOKResponse))
	m, fs := newTestModerator(t, srv.URL, nil)

	a, err := m.Assess(context.Background(), testInput)
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if a.Attempts != 2 || len(fs.waits) != 1 || fs.waits[0] != 3*time.Second {
		t.Fatalf("attempts=%d waits=%v, want one 3s wait", a.Attempts, fs.waits)
	}
}

func TestJevModerator_529ExponentialBackoffThenGivesUp(t *testing.T) {
	t.Parallel()
	srv := newJevServer(t, respond(529, `{"detail":"overloaded"}`))
	m, fs := newTestModerator(t, srv.URL, nil) // max_retries=2

	_, err := m.Assess(context.Background(), testInput)
	if !errors.Is(err, domain.ErrModerationUnavailable) || !strings.Contains(err.Error(), "http 529") {
		t.Fatalf("want unavailable 529, got %v", err)
	}
	if srv.callCount() != 3 || len(fs.waits) != 2 {
		t.Fatalf("calls=%d waits=%v, want 3 calls / 2 waits", srv.callCount(), fs.waits)
	}
	// 0.5s、1s 两档，各带 [d/2, d] 抖动
	for i, base := range []time.Duration{500 * time.Millisecond, time.Second} {
		if w := fs.waits[i]; w < base/2 || w > base {
			t.Fatalf("wait[%d]=%v out of [%v, %v]", i, w, base/2, base)
		}
	}
}

func TestJevModerator_CanceledWhileWaitingToRetry(t *testing.T) {
	t.Parallel()
	srv := newJevServer(t, respond(http.StatusServiceUnavailable, `{}`))
	m, _ := newTestModerator(t, srv.URL, func(c *conf.Moderation) { c.MaxRetries = 5 })
	ctx, cancel := context.WithCancel(context.Background())
	m.sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }

	_, err := m.Assess(ctx, testInput)
	if !errors.Is(err, domain.ErrModerationUnavailable) || srv.callCount() != 1 {
		t.Fatalf("calls=%d err=%v", srv.callCount(), err)
	}
}

func TestJevModerator_NonRetryableStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code int
		body string
		want error
	}{
		// 401 响应体回显了 Authorization，错误里必须被抹掉
		{"401", http.StatusUnauthorized, `{"detail":"invalid api key: Bearer ` + testJevKey + `"}`, domain.ErrModerationAuth},
		{"400", http.StatusBadRequest, `{"detail":"max_tokens_exceeded"}`, domain.ErrModerationRejected},
		{"413", http.StatusRequestEntityTooLarge, `{"detail":"too large"}`, domain.ErrModerationRejected},
		{"422", http.StatusUnprocessableEntity, `{"detail":[{"loc":["body","state"],"msg":"Field required","type":"missing"}]}`, domain.ErrModerationRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newJevServer(t, respond(tc.code, tc.body))
			m, fs := newTestModerator(t, srv.URL, nil)

			_, err := m.Assess(context.Background(), testInput)
			if !errors.Is(err, tc.want) || errors.Is(err, domain.ErrModerationUnavailable) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), "http "+tc.name) {
				t.Fatalf("error should carry status: %v", err)
			}
			if srv.callCount() != 1 || len(fs.waits) != 0 {
				t.Fatalf("must not retry: calls=%d waits=%v", srv.callCount(), fs.waits)
			}
			assertNoKey(t, err)
		})
	}
}

func TestJevModerator_BadResponse(t *testing.T) {
	t.Parallel()
	mutate := func(f func(r map[string]any)) string {
		var r map[string]any
		_ = json.Unmarshal([]byte(jevOKResponse), &r)
		f(r)
		b, _ := json.Marshal(r)
		return string(b)
	}
	answers := func(r map[string]any) map[string]any { return r["answers"].(map[string]any) }
	cases := map[string]string{
		"not json":            `<html>oops</html>`,
		"missing model":       mutate(func(r map[string]any) { delete(r, "model") }),
		"missing usage":       mutate(func(r map[string]any) { delete(r, "usage") }),
		"missing tokens":      mutate(func(r map[string]any) { r["usage"] = map[string]any{"output_tokens": 1} }),
		"missing hazard":      mutate(func(r map[string]any) { delete(answers(r), "privacy") }),
		"missing noul":        mutate(func(r map[string]any) { answers(r)["sexual"] = map[string]any{"type": "noul"} }),
		"noul out of range":   mutate(func(r map[string]any) { answers(r)["spam_ad"] = map[string]any{"type": "noul", "noul": 1.5} }),
		"missing severity":    mutate(func(r map[string]any) { delete(answers(r), "severity") }),
		"missing confidence":  mutate(func(r map[string]any) { delete(answers(r)["severity"].(map[string]any), "confidence") }),
		"severity over range": mutate(func(r map[string]any) { answers(r)["severity"].(map[string]any)["score"] = 3.5 }),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := newJevServer(t, respond(http.StatusOK, body))
			m, _ := newTestModerator(t, srv.URL, nil)
			_, err := m.Assess(context.Background(), testInput)
			if !errors.Is(err, domain.ErrModerationBadResponse) {
				t.Fatalf("want bad response, got %v", err)
			}
			if srv.callCount() != 1 {
				t.Fatalf("must not retry, calls=%d", srv.callCount())
			}
		})
	}
}

func TestJevModerator_TruncatesLongContentKeepingHeadAndTail(t *testing.T) {
	t.Parallel()
	srv := newJevServer(t, respond(http.StatusOK, jevOKResponse))
	m, _ := newTestModerator(t, srv.URL, func(c *conf.Moderation) { c.ContentMaxRunes = 100 })

	in := testInput
	in.Content = "开头内容" + strings.Repeat("正常文字", 3000) + "加微信abc123"
	if _, err := m.Assess(context.Background(), in); err != nil {
		t.Fatalf("Assess: %v", err)
	}
	var body struct {
		State struct {
			Post struct {
				Content string `json:"content"`
			} `json:"post"`
		} `json:"state"`
	}
	if err := json.Unmarshal(srv.request(0).body, &body); err != nil {
		t.Fatal(err)
	}
	got := body.State.Post.Content
	if !strings.HasPrefix(got, "开头内容") || !strings.HasSuffix(got, "加微信abc123") || !strings.Contains(got, "中间省略") {
		t.Fatalf("truncated content lost head/tail: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 100+30 {
		t.Fatalf("truncated content too long: %d runes", n)
	}
}

func TestTruncateMiddle(t *testing.T) {
	t.Parallel()
	if got := truncateMiddle("短文本", 10); got != "短文本" {
		t.Fatalf("short text changed: %q", got)
	}
	got := truncateMiddle("一二三四五六七八九十", 4)
	if got != "一二\n……（中间省略 6 字）……\n九十" {
		t.Fatalf("got %q", got)
	}
}

func TestJevModerator_Disabled(t *testing.T) {
	t.Parallel()
	srv := newJevServer(t, respond(http.StatusOK, jevOKResponse))
	cases := map[string]conf.Moderation{
		"switch off":       {Enabled: false, APIKey: testJevKey, BaseURL: srv.URL},
		"no api key":       {Enabled: true, BaseURL: srv.URL},
		"invalid base_url": {Enabled: true, APIKey: testJevKey, BaseURL: "ftp://example.com"},
	}
	for name, cfg := range cases {
		m := NewJevModerator(cfg)
		_, err := m.Assess(context.Background(), testInput)
		if !errors.Is(err, domain.ErrModerationDisabled) {
			t.Fatalf("%s: want disabled, got %v", name, err)
		}
		assertNoKey(t, err)
	}
	if srv.callCount() != 0 {
		t.Fatalf("disabled moderator must not call the API, calls=%d", srv.callCount())
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	cases := map[string]time.Duration{
		"":      0,
		"2":     2 * time.Second,
		"0.5":   500 * time.Millisecond,
		"-1":    0,
		"NaN":   0,
		"1e300": jevRetryAfterMax,
		"junk":  0,
	}
	for in, want := range cases {
		if got := parseRetryAfter(in); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
	if got := parseRetryAfter(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)); got != 0 {
		t.Errorf("past date = %v, want 0", got)
	}
	if got := parseRetryAfter(time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)); got < 59*time.Minute {
		t.Errorf("future date = %v", got)
	}
	if got := backoff(1, time.Hour); got != jevRetryAfterMax {
		t.Errorf("retry-after should be capped, got %v", got)
	}
}
