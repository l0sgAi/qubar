// auto_post_test.go 全局机器人 hot 圈自动发帖（#101）单元测试。
//
// 模拟搜索 + LLM 覆盖：到期发帖（作者 / 圈子 / 来源链接 / AI 标识）、interval_min / daily_max、
// 同一话题不重复发、不活跃时段不发、引用不存在的来源记失败不发帖、搜索为空 / 去重后为空不调 LLM、
// 未配置 hot_circle_id 不执行；auto_post 配置校验与管理接口读写。唯一约束与内部发帖见集成测试。
package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
)

// ---- fakes ----

// autoPostAgentRepo 只实现 ListAutoPostEnabled。
type autoPostAgentRepo struct {
	domain.AgentRepository
	agents []domain.AiAgent
}

func (r *autoPostAgentRepo) ListAutoPostEnabled(context.Context) ([]domain.AiAgent, error) {
	return r.agents, nil
}

// memPostLogRepo 内存发帖日志，模拟唯一约束 (agent_id, topic_key)。
type memPostLogRepo struct {
	logs []*domain.PostLog
	now  func() time.Time
}

func (r *memPostLogRepo) CreatePlaceholder(_ context.Context, l *domain.PostLog) (bool, error) {
	for _, x := range r.logs {
		if x.AgentID == l.AgentID && x.TopicKey == l.TopicKey {
			return false, nil
		}
	}
	cp := *l
	cp.CreateTime = r.now()
	r.logs = append(r.logs, &cp)
	return true, nil
}

func (r *memPostLogRepo) Finish(_ context.Context, l *domain.PostLog) error {
	for _, x := range r.logs {
		if x.ID == l.ID {
			x.Status, x.PostID, x.Sources, x.ErrorMsg = l.Status, l.PostID, l.Sources, l.ErrorMsg
		}
	}
	return nil
}

func (r *memPostLogRepo) CountSinceByAgent(_ context.Context, agentID uuid.UUID, since time.Time) (int64, error) {
	var n int64
	for _, x := range r.logs {
		if x.AgentID == agentID && !x.CreateTime.Before(since) {
			n++
		}
	}
	return n, nil
}

func (r *memPostLogRepo) GetLastTimeByAgent(_ context.Context, agentID uuid.UUID) (*time.Time, error) {
	var last *time.Time
	for _, x := range r.logs {
		if x.AgentID == agentID && (last == nil || x.CreateTime.After(*last)) {
			t := x.CreateTime
			last = &t
		}
	}
	return last, nil
}

func (r *memPostLogRepo) ListSourceURLsSince(_ context.Context, agentID uuid.UUID, since time.Time) ([]string, error) {
	var out []string
	for _, x := range r.logs {
		if x.AgentID == agentID && !x.CreateTime.Before(since) {
			for _, src := range x.Sources {
				out = append(out, src.URL)
			}
		}
	}
	return out, nil
}

type fakeTopicSource struct {
	items   []domain.TopicItem
	err     error
	queries []string
}

func (f *fakeTopicSource) Search(_ context.Context, q string, _ domain.SearchOptions) ([]domain.TopicItem, error) {
	f.queries = append(f.queries, q)
	return f.items, f.err
}

type scriptedLLM struct {
	content string
	calls   int
	last    LLMRequest
}

func (f *scriptedLLM) Generate(_ context.Context, req LLMRequest) (*LLMResult, error) {
	f.calls++
	f.last = req
	return &LLMResult{Content: f.content, PromptTokens: 10, CompletionTokens: 20}, nil
}

type fakePostCreator struct {
	userIDs []uuid.UUID
	inputs  []AutoPostInput
}

func (f *fakePostCreator) CreateAgentPost(_ context.Context, userID uuid.UUID, in AutoPostInput) (uuid.UUID, error) {
	f.userIDs = append(f.userIDs, userID)
	f.inputs = append(f.inputs, in)
	return uuid.New(), nil
}

// ---- fixture ----

var (
	hotCircleID    = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000f1")
	autoPostBotID  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000f2")
	autoPostLinked = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000f3")
)

type autoPostFixture struct {
	svc     *autoPostServiceImpl
	logs    *memPostLogRepo
	search  *fakeTopicSource
	llm     *scriptedLLM
	creator *fakePostCreator
	clock   time.Time
}

func (f *autoPostFixture) run(t *testing.T) { t.Helper(); f.svc.RunAutoPostScan(context.Background()) }

func (f *autoPostFixture) advance(d time.Duration) { f.clock = f.clock.Add(d) }

var defaultAutoPostCfg = domain.AutoPostConfig{Enabled: true, IntervalMin: 60, DailyMax: 5, Topics: []string{"AI"}}

func twoItems() []domain.TopicItem {
	return []domain.TopicItem{
		{Title: "新模型发布", Snippet: "某公司发布新模型", URL: "https://news.example.com/a?utm_source=x", Source: "example"},
		{Title: "芯片 [快讯]", Snippet: "芯片产能提升", URL: "https://news.example.com/b", Source: "example"},
	}
}

func newAutoPostFixture(t *testing.T, cfg domain.AutoPostConfig) *autoPostFixture {
	t.Helper()
	prev := conf.Config.AiAgent.Autonomy
	conf.Config.AiAgent.Autonomy = conf.AiAgentAutonomy{Enabled: true, HotCircleID: hotCircleID.String()}
	t.Cleanup(func() { conf.Config.AiAgent.Autonomy = prev })

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	agent := domain.AiAgent{
		ID: autoPostBotID, LinkedUserID: autoPostLinked, APIProtocol: "openai", Model: "m",
		Status: domain.AgentStatusEnabled, AutoConfig: domain.AutoConfigJSON{domain.AutoConfigKeyAutoPost: raw},
	}
	f := &autoPostFixture{
		search:  &fakeTopicSource{items: twoItems()},
		llm:     &scriptedLLM{content: "```json\n{\"title\":\"新模型来了\",\"content\":\"正文\",\"used_sources\":[1,2]}\n```"},
		creator: &fakePostCreator{},
		clock:   time.Date(2026, 10, 31, 12, 0, 0, 0, autoPostZone),
	}
	f.logs = &memPostLogRepo{now: func() time.Time { return f.clock }}
	svc := NewAutoPostService(&autoPostAgentRepo{agents: []domain.AiAgent{agent}}, f.logs, f.llm, f.search).(*autoPostServiceImpl)
	svc.now = func() time.Time { return f.clock }
	svc.SetPostCreator(f.creator)
	f.svc = svc
	return f
}

// ---- 发帖流程 ----

func TestAutoPost_PublishesToHotCircle(t *testing.T) {
	f := newAutoPostFixture(t, defaultAutoPostCfg)
	f.run(t)

	if len(f.creator.inputs) != 1 {
		t.Fatalf("posts = %d, want 1", len(f.creator.inputs))
	}
	in := f.creator.inputs[0]
	if f.creator.userIDs[0] != autoPostLinked || in.CircleID != hotCircleID || in.Title != "新模型来了" {
		t.Fatalf("author=%s circle=%s title=%q", f.creator.userIDs[0], in.CircleID, in.Title)
	}
	if !strings.HasPrefix(in.Content, "正文\n\n---\n\n来源：\n") ||
		!strings.Contains(in.Content, "- [新模型发布](https://news.example.com/a?utm_source=x)") ||
		!strings.Contains(in.Content, `- [芯片 \[快讯\]](https://news.example.com/b)`) ||
		!strings.HasSuffix(in.Content, autoPostAIDisclosure) {
		t.Fatalf("content = %q", in.Content)
	}
	if !strings.Contains(f.llm.last.SystemPrompt, autoPostInstruction) || strings.Contains(f.llm.last.UserPrompt, "https://") {
		t.Fatal("prompt must carry the generation rules and must not leak URLs to the LLM")
	}
	l := f.logs.logs[0]
	if l.Status != domain.PostLogStatusOK || l.PostID == nil || l.CircleID != hotCircleID || len(l.Sources) != 2 ||
		l.SearchQuery != "AI" || l.TopicKey != topicKey("https://news.example.com/a") {
		t.Fatalf("log = %+v", l)
	}
}

func TestAutoPost_IntervalAndDailyMax(t *testing.T) {
	cfg := defaultAutoPostCfg
	cfg.DailyMax = 2
	f := newAutoPostFixture(t, cfg)
	f.run(t) // 12:00 发第 1 帖

	f.advance(30 * time.Minute)
	f.search.items = []domain.TopicItem{{Title: "t3", URL: "https://x.example.com/3"}}
	f.run(t) // 未到 interval_min
	if len(f.search.queries) != 1 {
		t.Fatalf("searched before interval_min elapsed: %d", len(f.search.queries))
	}

	f.advance(30 * time.Minute)
	f.llm.content = `{"title":"t","content":"c","used_sources":[1]}`
	f.run(t) // 13:00 发第 2 帖
	f.advance(2 * time.Hour)
	f.search.items = []domain.TopicItem{{Title: "t4", URL: "https://x.example.com/4"}}
	f.run(t) // 达到 daily_max
	if len(f.creator.inputs) != 2 || len(f.search.queries) != 2 {
		t.Fatalf("posts=%d searches=%d, want 2/2", len(f.creator.inputs), len(f.search.queries))
	}

	f.advance(12 * time.Hour) // 次日 03:00，日计数重置
	f.run(t)
	if len(f.creator.inputs) != 3 {
		t.Fatalf("posts after day rollover = %d, want 3", len(f.creator.inputs))
	}
}

func TestAutoPost_SameTopicNotRepeated(t *testing.T) {
	f := newAutoPostFixture(t, defaultAutoPostCfg)
	f.run(t)
	f.advance(2 * time.Hour)
	// 同一首条来源（仅追踪参数 / 尾斜杠 / 大小写不同）+ 同一第二条：去重后全部已发过 → 不调 LLM。
	f.search.items = []domain.TopicItem{
		{Title: "x", URL: "https://WWW.news.example.com/a/?utm_medium=y"},
		{Title: "y", URL: "https://news.example.com/b"},
	}
	f.run(t)
	if f.llm.calls != 1 || len(f.creator.inputs) != 1 {
		t.Fatalf("llm calls=%d posts=%d, want 1/1", f.llm.calls, len(f.creator.inputs))
	}
}

func TestAutoPost_InactiveHours(t *testing.T) {
	cfg := defaultAutoPostCfg
	cfg.ActiveHours = []int{22, 6} // 跨零点
	f := newAutoPostFixture(t, cfg)
	f.run(t) // 12:00 不活跃
	if len(f.search.queries) != 0 {
		t.Fatal("must not search outside active hours")
	}
	f.advance(11 * time.Hour) // 23:00
	f.run(t)
	if len(f.creator.inputs) != 1 {
		t.Fatalf("posts = %d, want 1 inside active hours", len(f.creator.inputs))
	}
}

func TestAutoPost_UnknownSourceFails(t *testing.T) {
	f := newAutoPostFixture(t, defaultAutoPostCfg)
	f.llm.content = `{"title":"t","content":"c","used_sources":[1,3]}`
	f.run(t)
	if len(f.creator.inputs) != 0 {
		t.Fatal("must not post when LLM cites a non-existent source")
	}
	l := f.logs.logs[0]
	if l.Status != domain.PostLogStatusFailed || l.PostID != nil || !strings.Contains(l.ErrorMsg, "unknown material 3") {
		t.Fatalf("log = %+v", l)
	}
}

func TestAutoPost_EmptySearchSkipsLLM(t *testing.T) {
	f := newAutoPostFixture(t, defaultAutoPostCfg)
	f.search.items = nil
	f.run(t)
	if f.llm.calls != 0 || len(f.logs.logs) != 0 {
		t.Fatalf("llm calls=%d logs=%d, want 0/0", f.llm.calls, len(f.logs.logs))
	}
}

func TestAutoPost_DisabledWithoutHotCircle(t *testing.T) {
	f := newAutoPostFixture(t, defaultAutoPostCfg)
	conf.Config.AiAgent.Autonomy.HotCircleID = ""
	f.run(t)
	if len(f.search.queries) != 0 {
		t.Fatal("auto post must be disabled when hot_circle_id is not configured")
	}
}

func TestAutoPost_DefaultTopicsAndRotation(t *testing.T) {
	cfg := defaultAutoPostCfg
	cfg.Topics = nil
	f := newAutoPostFixture(t, cfg)
	conf.Config.AiAgent.Autonomy.DefaultHotTopics = []string{"热点A", "热点B"}
	f.search.items = nil
	f.run(t)
	f.run(t)
	f.run(t)
	if strings.Join(f.search.queries, ",") != "热点A,热点B,热点A" {
		t.Fatalf("queries = %v", f.search.queries)
	}
}

// ---- 解析 / 规范化 ----

func TestParseAutoPostDraft(t *testing.T) {
	items := twoItems()
	_, used, err := parseAutoPostDraft(`{"title":"t","content":"c","used_sources":[2,2]}`, items)
	if err != nil || len(used) != 1 || used[0].URL != items[1].URL {
		t.Fatalf("used=%v err=%v", used, err)
	}
	for name, raw := range map[string]string{
		"not json":      "抱歉，我无法完成",
		"empty title":   `{"title":"","content":"c","used_sources":[1]}`,
		"no sources":    `{"title":"t","content":"c","used_sources":[]}`,
		"out of range":  `{"title":"t","content":"c","used_sources":[0]}`,
		"string source": `{"title":"t","content":"c","used_sources":["https://x"]}`,
	} {
		if _, _, err := parseAutoPostDraft(raw, items); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestTopicKeyNormalization(t *testing.T) {
	a := topicKey("https://news.example.com/a?id=1&utm_source=x#top")
	b := topicKey("HTTPS://www.News.Example.com/a/?utm_campaign=y&id=1")
	if a != b || a == topicKey("https://news.example.com/a?id=2") {
		t.Fatal("topic key normalization mismatch")
	}
}

// ---- 配置校验 / 管理接口 ----

func TestBuildAutoPost_Validation(t *testing.T) {
	ok := []domain.AutoPostConfig{
		{},
		{Enabled: true, IntervalMin: 10, DailyMax: 1},
		{Enabled: true, IntervalMin: 60, DailyMax: 48, ActiveHours: []int{8, 24}, Topics: []string{" AI ", "AI"}},
		{ActiveHours: []int{22, 6}},
	}
	for i, cfg := range ok {
		if _, err := buildAutoPost(cfg); err != nil {
			t.Errorf("ok[%d]: %v", i, err)
		}
	}
	bad := map[string]domain.AutoPostConfig{
		"interval too small": {Enabled: true, IntervalMin: 5, DailyMax: 1},
		"daily zero":         {Enabled: true, IntervalMin: 60},
		"daily too big":      {DailyMax: 49},
		"negative interval":  {IntervalMin: -1},
		"hours one value":    {ActiveHours: []int{8}},
		"hours equal":        {ActiveHours: []int{8, 8}},
		"hours out of range": {ActiveHours: []int{8, 25}},
		"empty topic":        {Topics: []string{" "}},
		"topic too long":     {Topics: []string{strings.Repeat("长", 51)}},
		"require review":     {RequireReview: true},
	}
	for name, cfg := range bad {
		if _, err := buildAutoPost(cfg); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestUpdateAgent_AutoPost(t *testing.T) {
	repo := newFakeAgentRepo()
	seedGlobalAgent(repo)
	svc := NewAgentService(repo)
	svc.SetRoleReader(&fakeUserRoleReader{role: roleAdmin, ok: true})
	ctx := context.Background()

	cfg := domain.AutoPostConfig{Enabled: true, IntervalMin: 120, DailyMax: 3, ActiveHours: []int{8, 23}}
	vo, err := svc.UpdateAgent(ctx, adminID, globalAgentID, UpdateAgentInput{AutoConfig: &AutoConfigInput{AutoPost: &cfg}})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	ap := vo.AutoConfig.AutoPost
	if !ap.Enabled || ap.IntervalMin != 120 || ap.DailyMax != 3 || ap.Topics == nil || ap.RequireReview {
		t.Fatalf("vo auto_post = %+v", ap)
	}
	patch := repo.updated[globalAgentID]["auto_config"].(domain.AutoConfigPatch)
	if _, ok := patch[domain.AutoConfigKeyAutoReply]; ok || len(patch) != 1 {
		t.Fatalf("patch must only carry auto_post, got %v", patch)
	}

	bad := domain.AutoPostConfig{Enabled: true, IntervalMin: 1, DailyMax: 1}
	_, err = svc.UpdateAgent(ctx, adminID, globalAgentID, UpdateAgentInput{AutoConfig: &AutoConfigInput{AutoPost: &bad}})
	if !IsInvalidAutoConfigErr(err) {
		t.Fatalf("err = %v, want invalid auto_config", err)
	}
}

func TestCircleAgent_AutoPostRejected(t *testing.T) {
	repo := newFakeAgentRepo()
	svc := NewCircleAgentService(repo)
	svc.SetCircleRoleReader(newFakeCircleRoles())
	svc.SetBotUserCreator(&fakeBotUserCreator{})
	cfg := domain.AutoPostConfig{}
	_, err := svc.CreateCircleAgent(context.Background(), adminID, circleAID, CreateAgentInput{
		Name: "圈内发帖", APIProtocol: "openai", Model: "m", AutoConfig: &AutoConfigInput{AutoPost: &cfg},
	})
	assertErr(t, err, errAutoPostGlobalOnly)
}
