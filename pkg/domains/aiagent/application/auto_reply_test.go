// auto_reply_test.go 机器人自动回复（#99）单元测试。
//
// 覆盖：auto_config.auto_reply 校验（非法值 400）与读写（顶层 key 合并、VO 回显、圈内 admin 可改）；
// 扫描链路（总开关 / 候选作用域 / 帖子顶层回复 / 评论楼层回复 / 占位去重 / 每目标上限 /
// 自动回复每小时上限 / 分类器 / 限频截停）；排除名单计算。唯一约束本身见 infrastructure 集成测试。
package application

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
)

// ---- fakes ----

// fakeCandidateReader 自动回复候选 fake：记录查询入参，返回预设候选。
type fakeCandidateReader struct {
	mu           sync.Mutex
	posts        []AutoPostCandidate
	comments     []AutoCommentCandidate
	postQueries  []AutoPostQuery
	commentQuery []AutoCommentQuery
}

func (f *fakeCandidateReader) ListHotPosts(ctx context.Context, q AutoPostQuery) ([]AutoPostCandidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.postQueries = append(f.postQueries, q)
	return f.posts, nil
}

func (f *fakeCandidateReader) ListHotComments(ctx context.Context, q AutoCommentQuery) ([]AutoCommentCandidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commentQuery = append(f.commentQuery, q)
	return f.comments, nil
}

// judgeLLM 分类器感知的 LLM fake：判定请求返回预设 JSON，生成请求返回固定回复。
type judgeLLM struct {
	reply       bool
	judgeCalls  int
	genCalls    int
	judgePrompt string
	mu          sync.Mutex
}

func (f *judgeLLM) Generate(ctx context.Context, req LLMRequest) (*LLMResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.SystemPrompt == judgeSystemPrompt {
		f.judgeCalls++
		f.judgePrompt = req.UserPrompt
		if f.reply {
			return &LLMResult{Content: `{"reply":true,"reason":"相关"}`}, nil
		}
		return &LLMResult{Content: `{"reply":false,"reason":"无关"}`}, nil
	}
	f.genCalls++
	return &LLMResult{Content: "自动回复内容"}, nil
}

// ---- 助手 ----

var (
	autoCommentID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000001c1")
	autoRootID    = uuid.MustParse("0192a0d0-0000-7000-8000-0000000001c0")
)

// enableAutonomy 打开平台总开关（全局 conf，测试结束恢复）。
func enableAutonomy(t *testing.T, maxAgentsPerTarget int) {
	t.Helper()
	prev := conf.Config.AiAgent.Autonomy
	conf.Config.AiAgent.Autonomy = conf.AiAgentAutonomy{Enabled: true, MaxAgentsPerTarget: maxAgentsPerTarget}
	t.Cleanup(func() { conf.Config.AiAgent.Autonomy = prev })
}

// withAutoReply 给机器人写入 auto_reply 配置。
func withAutoReply(t *testing.T, a *domain.AiAgent, cfg domain.AutoReplyConfig) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a.AutoConfig = domain.AutoConfigJSON{domain.AutoConfigKeyAutoReply: raw}
}

// newAutoFixture 圈A内一个开启自动回复的机器人（模式为关键词，验证与 trigger_mode 互不影响）。
func newAutoFixture(t *testing.T, cfg domain.AutoReplyConfig) (*replyFixture, *fakeCandidateReader, *domain.AiAgent) {
	t.Helper()
	enableAutonomy(t, 1)
	f := newReplyFixture()
	bot := seedCircleBot(f.repo, circleABotID, circleABotLinked, circleAID, domain.TriggerModeKeyword, "x")
	withAutoReply(t, bot, cfg)
	cands := &fakeCandidateReader{}
	f.svc.SetCandidateReader(cands)
	return f, cands, bot
}

var postAutoCfg = domain.AutoReplyConfig{
	Enabled: true, Targets: []string{domain.AutoReplyTargetPost}, PostHotMin: 100, WindowHours: 24,
}

var commentAutoCfg = domain.AutoReplyConfig{
	Enabled: true, Targets: []string{domain.AutoReplyTargetComment}, CommentLikeMin: 10, WindowHours: 6,
}

// ---- 配置校验 ----

func TestBuildAutoReply_Validation(t *testing.T) {
	ok := []domain.AutoReplyConfig{
		{},                                    // 未开启的零值
		{Enabled: false, Targets: []string{}}, // 关闭
		postAutoCfg,
		commentAutoCfg,
		{Enabled: true, Targets: []string{"post", "comment"}, PostHotMin: 1, CommentReplyMin: 3, WindowHours: 168, MaxPerHour: 5},
	}
	for i, cfg := range ok {
		if _, err := buildAutoReply(cfg); err != nil {
			t.Fatalf("case %d: unexpected err %v", i, err)
		}
	}
	bad := map[string]domain.AutoReplyConfig{
		"unknown target":          {Targets: []string{"user"}},
		"duplicate target":        {Targets: []string{"post", "post"}},
		"negative threshold":      {PostHotMin: -1},
		"negative max per hour":   {MaxPerHour: -1},
		"window too large":        {WindowHours: 169},
		"enabled without targets": {Enabled: true, WindowHours: 24},
		"enabled without window":  {Enabled: true, Targets: []string{"post"}, PostHotMin: 10},
		"post without hot min":    {Enabled: true, Targets: []string{"post"}, WindowHours: 24},
		"comment without min":     {Enabled: true, Targets: []string{"comment"}, WindowHours: 24},
	}
	for name, cfg := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := buildAutoReply(cfg)
			assertErr(t, err, errInvalidAutoConfig)
		})
	}
}

// ---- 管理接口读写 ----

// TestUpdateAgent_AutoConfig 全局机器人更新 auto_reply：顶层 key 合并（保留兄弟键），
// VO 回显；非法值 errInvalidAutoConfig（handler 400）。
func TestUpdateAgent_AutoConfig(t *testing.T) {
	repo := newFakeAgentRepo()
	global := seedGlobalAgent(repo)
	global.AutoConfig = domain.AutoConfigJSON{"auto_post": json.RawMessage(`{"enabled":true}`)}
	svc := NewAgentService(repo)
	svc.SetRoleReader(&fakeUserRoleReader{role: roleAdmin, ok: true})
	ctx := context.Background()

	cfg := postAutoCfg
	vo, err := svc.UpdateAgent(ctx, adminID, globalAgentID, UpdateAgentInput{AutoConfig: &AutoConfigInput{AutoReply: &cfg}})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if !vo.AutoConfig.AutoReply.Enabled || vo.AutoConfig.AutoReply.PostHotMin != 100 {
		t.Fatalf("vo auto_reply = %+v", vo.AutoConfig.AutoReply)
	}
	if _, ok := repo.byID[globalAgentID].AutoConfig["auto_post"]; !ok {
		t.Fatal("sibling auto_post must be preserved by top-level merge")
	}
	if _, ok := repo.updated[globalAgentID]["auto_config"].(domain.AutoConfigPatch); !ok {
		t.Fatalf("auto_config field must be an AutoConfigPatch, got %T", repo.updated[globalAgentID]["auto_config"])
	}

	bad := domain.AutoReplyConfig{Enabled: true, Targets: []string{"post"}, WindowHours: 24}
	_, err = svc.UpdateAgent(ctx, adminID, globalAgentID, UpdateAgentInput{AutoConfig: &AutoConfigInput{AutoReply: &bad}})
	assertErr(t, err, errInvalidAutoConfig)

	// auto_config 不带 auto_reply 视为未改动。
	_, err = svc.UpdateAgent(ctx, adminID, globalAgentID, UpdateAgentInput{AutoConfig: &AutoConfigInput{}})
	assertErr(t, err, errNoFieldsToUpdate)
}

// TestCircleAgent_AutoConfig 圈内机器人：创建可带 auto_reply；admin（非圈主）可改（运营字段 admin+）。
func TestCircleAgent_AutoConfig(t *testing.T) {
	repo := newFakeAgentRepo()
	svc := NewCircleAgentService(repo)
	svc.SetCircleRoleReader(newFakeCircleRoles())
	svc.SetBotUserCreator(&fakeBotUserCreator{})
	ctx := context.Background()

	cfg := commentAutoCfg
	vo, err := svc.CreateCircleAgent(ctx, adminID, circleAID, CreateAgentInput{
		Name: "自动回复助手", APIProtocol: "openai", Model: "m",
		AutoConfig: &AutoConfigInput{AutoReply: &cfg},
	})
	if err != nil {
		t.Fatalf("CreateCircleAgent: %v", err)
	}
	if !vo.AutoConfig.AutoReply.Enabled || vo.AutoConfig.AutoReply.CommentLikeMin != 10 {
		t.Fatalf("created vo auto_reply = %+v", vo.AutoConfig.AutoReply)
	}

	off := domain.AutoReplyConfig{Enabled: false}
	vo, err = svc.UpdateCircleAgent(ctx, adminID, vo.ID, UpdateAgentInput{AutoConfig: &AutoConfigInput{AutoReply: &off}})
	if err != nil {
		t.Fatalf("admin UpdateCircleAgent auto_config: %v", err)
	}
	if vo.AutoConfig.AutoReply.Enabled {
		t.Fatal("auto_reply should be disabled after update")
	}
	if vo.AutoConfig.AutoReply.Targets == nil {
		t.Fatal("targets must echo as [] not null")
	}

	bad := domain.AutoReplyConfig{Targets: []string{"nope"}}
	_, err = svc.CreateCircleAgent(ctx, adminID, circleAID, CreateAgentInput{
		Name: "坏配置", APIProtocol: "openai", Model: "m",
		AutoConfig: &AutoConfigInput{AutoReply: &bad},
	})
	assertErr(t, err, errInvalidAutoConfig)
}

// ---- 扫描链路 ----

func TestRunAutoReplyScan_DisabledSwitch(t *testing.T) {
	f, cands, _ := newAutoFixture(t, postAutoCfg)
	conf.Config.AiAgent.Autonomy.Enabled = false
	cands.posts = []AutoPostCandidate{{ID: postInCircleA, CircleID: circleAID}}
	f.svc.RunAutoReplyScan(context.Background())
	if len(cands.postQueries) != 0 || f.comments.callCount() != 0 {
		t.Fatalf("switch off must not scan: queries=%d comments=%d", len(cands.postQueries), f.comments.callCount())
	}
}

func TestRunAutoReplyScan_DisabledAgentNotScanned(t *testing.T) {
	f, cands, bot := newAutoFixture(t, postAutoCfg)
	bot.Status = domain.AgentStatusDisabled
	f.svc.RunAutoReplyScan(context.Background())
	if len(cands.postQueries) != 0 {
		t.Fatalf("disabled agent must not be scanned, queries=%d", len(cands.postQueries))
	}
}

// TestRunAutoReplyScan_PostTarget 帖子候选：查询按机器人圈子收口 + 门槛 / 窗口透传；
// 回复为顶层评论；占位行 trigger_source=4 终态 status=1；重复扫描不再回复。
func TestRunAutoReplyScan_PostTarget(t *testing.T) {
	f, cands, bot := newAutoFixture(t, postAutoCfg)
	cands.posts = []AutoPostCandidate{{ID: postInCircleA, CircleID: circleAID, Hot: 500}}
	ctx := context.Background()

	f.svc.RunAutoReplyScan(ctx)
	if len(cands.postQueries) != 1 {
		t.Fatalf("post queries = %d, want 1", len(cands.postQueries))
	}
	q := cands.postQueries[0]
	if q.CircleID != circleAID || q.HotMin != 100 || q.Limit != conf.DefaultAutonomyMaxCandidatesPerRun {
		t.Fatalf("unexpected post query %+v", q)
	}
	if d := time.Since(q.Since); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("since should be ~24h ago, got %v", d)
	}
	if n := f.comments.callCount(); n != 1 {
		t.Fatalf("comments = %d, want 1", n)
	}
	if in := f.comments.calls[0]; in.RootID != nil || in.ReplyToID != nil || in.PostID != postInCircleA {
		t.Fatalf("post auto reply must be a top-level comment, got %+v", in)
	}
	row := f.logRepo.autoRow(bot.ID, postInCircleA, nil)
	if row == nil || row.TriggerSource != domain.TriggerSourceAuto || row.Status != domain.ReplyStatusOK || row.CommentID == nil {
		t.Fatalf("auto log row = %+v", row)
	}
	if len(f.logRepo.created) != 0 {
		t.Fatalf("auto reply must finish the placeholder, not append rows: %+v", f.logRepo.created)
	}

	// 第二轮：本机器人已触发的帖子进入排除名单；即使候选仍返回它，占位撞唯一约束也不会再回复。
	f.svc.RunAutoReplyScan(ctx)
	if q2 := cands.postQueries[1]; len(q2.ExcludeIDs) != 1 || q2.ExcludeIDs[0] != postInCircleA {
		t.Fatalf("second scan should exclude replied post, got %+v", q2.ExcludeIDs)
	}
	if n := f.comments.callCount(); n != 1 {
		t.Fatalf("same target must not be replied twice, comments = %d", n)
	}
}

// TestRunAutoReplyScan_CommentTarget 评论候选：回复挂在该评论楼层（root=评论的 root，reply_to=评论），
// 日志 target_comment_id=评论；prompt 带评论原文。
func TestRunAutoReplyScan_CommentTarget(t *testing.T) {
	f, cands, bot := newAutoFixture(t, commentAutoCfg)
	root := autoRootID
	cands.comments = []AutoCommentCandidate{{
		ID: autoCommentID, PostID: postInCircleA, PostCircleID: circleAID,
		UserID: memberID, RootID: &root, Content: "这条评论很热",
	}}
	f.svc.RunAutoReplyScan(context.Background())

	if len(cands.commentQuery) != 1 || cands.commentQuery[0].LikeMin != 10 || cands.commentQuery[0].CircleID != circleAID {
		t.Fatalf("unexpected comment query %+v", cands.commentQuery)
	}
	if n := f.comments.callCount(); n != 1 {
		t.Fatalf("comments = %d, want 1", n)
	}
	in := f.comments.calls[0]
	if in.RootID == nil || *in.RootID != autoRootID || in.ReplyToID == nil || *in.ReplyToID != autoCommentID {
		t.Fatalf("comment auto reply must hang in the floor, got %+v", in)
	}
	target := autoCommentID
	if row := f.logRepo.autoRow(bot.ID, postInCircleA, &target); row == nil || row.Status != domain.ReplyStatusOK {
		t.Fatalf("auto log row for comment target = %+v", row)
	}
}

// TestRunAutoReplyScan_CircleScope 圈内机器人：候选混入他圈内容（候选查询语义漂移）也不回复。
func TestRunAutoReplyScan_CircleScope(t *testing.T) {
	f, cands, _ := newAutoFixture(t, postAutoCfg)
	cands.posts = []AutoPostCandidate{{ID: postInCircleB, CircleID: circleBID, Hot: 999}}
	f.svc.RunAutoReplyScan(context.Background())
	if n := f.comments.callCount(); n != 0 {
		t.Fatalf("circle bot must not reply other circle content, comments = %d", n)
	}

	// 候选声称在本圈、但帖子实际在他圈：executeReply 按帖子真实圈子再挡一次。
	f2, cands2, _ := newAutoFixture(t, postAutoCfg)
	f2.svc.SetPostReader(&fakePostReader{post: &PostBrief{ID: postInCircleA, Status: 1, CircleID: circleBID}})
	cands2.posts = []AutoPostCandidate{{ID: postInCircleA, CircleID: circleAID}}
	f2.svc.RunAutoReplyScan(context.Background())
	if n := f2.comments.callCount(); n != 0 {
		t.Fatalf("executeReply must re-check post circle, comments = %d", n)
	}
}

// TestRunAutoReplyScan_MaxAgentsPerTarget 目标已有其他机器人自动回复（达上限）：
// 进入排除名单；即使候选仍返回，执行前计数也跳过，不调 LLM。
func TestRunAutoReplyScan_MaxAgentsPerTarget(t *testing.T) {
	f, cands, _ := newAutoFixture(t, postAutoCfg)
	other := uuid.MustParse("0192a0d0-0000-7000-8000-0000000001ff")
	f.logRepo.auto = map[string]*domain.ReplyLog{ // 其他机器人已对该帖自动回复成功
		autoKey(other, postInCircleA, nil): {AgentID: other, PostID: postInCircleA, Status: domain.ReplyStatusOK},
	}
	cands.posts = []AutoPostCandidate{{ID: postInCircleA, CircleID: circleAID}}

	f.svc.RunAutoReplyScan(context.Background())
	if q := cands.postQueries[0]; len(q.ExcludeIDs) != 1 {
		t.Fatalf("saturated target should be excluded, got %+v", q.ExcludeIDs)
	}
	if n := f.comments.callCount(); n != 0 || f.llm.calls.Load() != 0 {
		t.Fatalf("saturated target must be skipped before LLM: comments=%d llm=%d", n, f.llm.calls.Load())
	}

	// 上限放宽到 2：第二个机器人可以回复。
	conf.Config.AiAgent.Autonomy.MaxAgentsPerTarget = 2
	f.svc.RunAutoReplyScan(context.Background())
	if n := f.comments.callCount(); n != 1 {
		t.Fatalf("with max_agents_per_target=2 the second agent should reply, comments = %d", n)
	}
}

// TestRunAutoReplyScan_ClassifierRejectConsumesTarget 设置了 filter_prompt 时自动回复帖子也先走分类器
// （无评论原文不 panic）；判定不回复 → 占位行终态 status=2，下一轮不再判定该目标。
func TestRunAutoReplyScan_ClassifierRejectConsumesTarget(t *testing.T) {
	f, cands, bot := newAutoFixture(t, postAutoCfg)
	bot.FilterPrompt = "只回复骑行相关"
	llm := &judgeLLM{reply: false}
	f.svc = NewReplyService(f.repo, f.logRepo, llm)
	f.svc.SetPostReader(&fakePostReader{post: &PostBrief{ID: postInCircleA, Title: "热帖", Status: 1, CircleID: circleAID}})
	f.svc.SetCommentCreator(f.comments)
	f.svc.SetCandidateReader(cands)
	cands.posts = []AutoPostCandidate{{ID: postInCircleA, CircleID: circleAID}}

	f.svc.RunAutoReplyScan(context.Background())
	if llm.judgeCalls != 1 || llm.genCalls != 0 {
		t.Fatalf("judge=%d gen=%d, want 1/0", llm.judgeCalls, llm.genCalls)
	}
	if strings.Contains(llm.judgePrompt, "用户评论") {
		t.Fatalf("post auto reply judge prompt must not include comment section: %q", llm.judgePrompt)
	}
	if row := f.logRepo.autoRow(bot.ID, postInCircleA, nil); row == nil || row.Status != domain.ReplyStatusSkipped {
		t.Fatalf("placeholder should end as skipped, got %+v", row)
	}
	f.svc.RunAutoReplyScan(context.Background())
	if llm.judgeCalls != 1 {
		t.Fatalf("rejected target must not be judged again, judge calls = %d", llm.judgeCalls)
	}
}

// TestRunAutoReplyScan_RateLimits 自动回复每小时上限 / 机器人限频命中即停止本机器人本轮，不占位。
func TestRunAutoReplyScan_RateLimits(t *testing.T) {
	cfg := postAutoCfg
	cfg.MaxPerHour = 2
	f, cands, bot := newAutoFixture(t, cfg)
	f.logRepo.autoHourCount = 2
	cands.posts = []AutoPostCandidate{{ID: postInCircleA, CircleID: circleAID}}
	f.svc.RunAutoReplyScan(context.Background())
	if n := f.comments.callCount(); n != 0 || f.logRepo.autoRow(bot.ID, postInCircleA, nil) != nil {
		t.Fatalf("auto max_per_hour reached: comments=%d", n)
	}

	f2, cands2, bot2 := newAutoFixture(t, postAutoCfg)
	f2.logRepo.last = &domain.ReplyLog{CreateTime: time.Now()} // min_interval_sec=60 未到
	cands2.posts = []AutoPostCandidate{{ID: postInCircleA, CircleID: circleAID}}
	f2.svc.RunAutoReplyScan(context.Background())
	if n := f2.comments.callCount(); n != 0 || f2.logRepo.autoRow(bot2.ID, postInCircleA, nil) != nil {
		t.Fatalf("min interval must block before placeholder: comments=%d", n)
	}
}

// TestRunAutoReplyScan_GlobalAgentWholeSite 全局机器人候选查询不带圈子过滤（全站）。
func TestRunAutoReplyScan_GlobalAgentWholeSite(t *testing.T) {
	enableAutonomy(t, 1)
	f := newReplyFixture()
	global := seedGlobalAgent(f.repo)
	global.Status = domain.AgentStatusEnabled
	withAutoReply(t, global, postAutoCfg)
	cands := &fakeCandidateReader{posts: []AutoPostCandidate{{ID: postInCircleA, CircleID: circleAID}}}
	f.svc.SetCandidateReader(cands)

	f.svc.RunAutoReplyScan(context.Background())
	if len(cands.postQueries) != 1 || cands.postQueries[0].CircleID != uuid.Nil {
		t.Fatalf("global agent must query whole site, got %+v", cands.postQueries)
	}
	if n := f.comments.callCount(); n != 1 {
		t.Fatalf("global agent should reply, comments = %d", n)
	}
}

// ---- 排除名单 ----

func TestAutoReplyExclusions(t *testing.T) {
	me := uuid.MustParse("0192a0d0-0000-7000-8000-000000000a01")
	other := uuid.MustParse("0192a0d0-0000-7000-8000-000000000a02")
	p1 := uuid.MustParse("0192a0d0-0000-7000-8000-000000000b01")
	p2 := uuid.MustParse("0192a0d0-0000-7000-8000-000000000b02")
	p3 := uuid.MustParse("0192a0d0-0000-7000-8000-000000000b03")
	c1 := uuid.MustParse("0192a0d0-0000-7000-8000-000000000c01")
	records := []domain.AutoTargetRecord{
		{AgentID: me, PostID: p1, Status: domain.ReplyStatusSkipped},                     // 自己触发过（即使被分类器拒绝）
		{AgentID: other, PostID: p2, Status: domain.ReplyStatusOK},                       // 他人已回复，达上限 1
		{AgentID: other, PostID: p3, Status: domain.ReplyStatusSkipped},                  // 他人分类器拒绝，不占名额
		{AgentID: other, PostID: p1, TargetCommentID: &c1, Status: domain.ReplyStatusOK}, // 评论目标
	}
	posts, comments := autoReplyExclusions(records, me, 1)
	if !containsID(posts, p1) || !containsID(posts, p2) || containsID(posts, p3) {
		t.Fatalf("posts excluded = %v", posts)
	}
	if len(comments) != 1 || comments[0] != c1 {
		t.Fatalf("comments excluded = %v", comments)
	}
	posts, _ = autoReplyExclusions(records, me, 2)
	if containsID(posts, p2) {
		t.Fatalf("with max 2, p2 has only one agent and must not be excluded: %v", posts)
	}
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// TestPassiveTriggers_LogSource 被动触发日志行写入 trigger_source（手动=3）。
func TestPassiveTriggers_LogSource(t *testing.T) {
	f := newReplyFixture()
	seedManualCircleBot(f.repo)
	f.svc.SetCircleRoleReader(newFakeCircleRoles())
	if _, err := f.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleA); err != nil {
		t.Fatalf("CircleManualReply: %v", err)
	}
	f.logRepo.mu.Lock()
	defer f.logRepo.mu.Unlock()
	if len(f.logRepo.created) != 1 || f.logRepo.created[0].TriggerSource != domain.TriggerSourceManual ||
		f.logRepo.created[0].TargetCommentID != nil {
		t.Fatalf("manual log row = %+v", f.logRepo.created)
	}
}
