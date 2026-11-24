//go:build integration

// agent_auto_post_integration_test.go 全局机器人 hot 圈自动发帖（#101）集成测试：生产装配
// （post Service + 圈子成员 / 状态校验桥接 + agent 桥接器 + PG 仓储）对开发栈运行，搜索与 LLM 用桩。
// 验证：内部发帖接口跳过成员检查、不落 @提及；一轮扫描以机器人账号在 hot 圈发帖并带来源与 AI 标识、
// 写成功日志；机器人发的帖不进自动回复候选；再跑一轮不重复发。
package composition

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"interestBar/pkg/conf"
	agentapp "interestBar/pkg/domains/aiagent/application"
	agentdomain "interestBar/pkg/domains/aiagent/domain"
	agentinfra "interestBar/pkg/domains/aiagent/infrastructure"
	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ListAutoPostEnabled 同 ListAutoReplyEnabled：只扫本测试的机器人（开发栈共享）。
func (r onlyAgentRepo) ListAutoPostEnabled(ctx context.Context) ([]agentdomain.AiAgent, error) {
	all, err := r.AgentRepository.ListAutoPostEnabled(ctx)
	var out []agentdomain.AiAgent
	for _, a := range all {
		if a.ID == r.id {
			out = append(out, a)
		}
	}
	return out, err
}

// stubTopicSource 固定搜索结果（不出网）。
type stubTopicSource struct{ items []agentdomain.TopicItem }

func (s stubTopicSource) Search(context.Context, string, agentdomain.SearchOptions) ([]agentdomain.TopicItem, error) {
	return s.items, nil
}

// draftLLM 返回固定 JSON 草稿的 LLM 桩。
type draftLLM struct{ calls int }

func (d *draftLLM) Generate(context.Context, agentapp.LLMRequest) (*agentapp.LLMResult, error) {
	d.calls++
	return &agentapp.LLMResult{Content: `{"title":"自动发帖集成测试","content":"根据材料整理的正文","used_sources":[1]}`}, nil
}

// newAutoPostEnv 生产装配的 post Service（真实成员 / 圈子状态校验）+ 一个 hot 圈 + 一个机器人账号。
func newAutoPostEnv(t *testing.T) (db *gorm.DB, postSvc postapp.PostService, circle, botUser uuid.UUID) {
	t.Helper()
	db = testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	deps := NewDeps()
	_, circleRepo, memberRepo := newCircleService(deps)
	postSvc = newPostService(deps)
	postSvc.SetMemberChecker(&circleMemberCheckerForPost{memberRepo: memberRepo})
	postSvc.SetStatusChecker(&circleStatusCheckerForPost{circleRepo: circleRepo})
	circle = insertCircle(t, db)
	botUser = insertTestUser(t, db, 2)
	testkit.CleanupRows(t, "domains.post", "user_id", botUser)
	return db, postSvc, circle, botUser
}

// TestCreateAgentPost_SkipsMemberCheck 机器人账号不是 hot 圈成员：普通发帖被拒，内部发帖成功且不落 @提及。
func TestCreateAgentPost_SkipsMemberCheck(t *testing.T) {
	t.Parallel()
	db, postSvc, circle, botUser := newAutoPostEnv(t)
	ctx := t.Context()
	mentioned := insertTestUser(t, db, 0)
	input := postapp.CreatePostInput{CircleID: circle, Title: "内部发帖", Content: "正文 @someone", MentionUserIDs: []uuid.UUID{mentioned}}

	if _, err := postSvc.CreatePost(ctx, botUser, input); !postapp.IsNotMemberErr(err) {
		t.Fatalf("CreatePost by non-member = %v, want not-member error", err)
	}
	postID, err := postSvc.CreateAgentPost(ctx, botUser, input)
	if err != nil {
		t.Fatalf("CreateAgentPost: %v", err)
	}
	var p postdomain.Post
	if err := db.Where("id = ?", postID).Take(&p).Error; err != nil {
		t.Fatal(err)
	}
	wantStatus := int16(postdomain.PostStatusPublished)
	if conf.Config.Moderation.Active() {
		wantStatus = postdomain.PostStatusReviewing
	}
	if p.UserID != botUser || p.CircleID != circle || p.Status != wantStatus || p.Summary == "" {
		t.Fatalf("post = %+v", p)
	}
	var mentions int64
	db.Model(&postdomain.PostMention{}).Where("post_id = ?", postID).Count(&mentions)
	if mentions != 0 {
		t.Fatalf("agent post must not persist mentions, got %d", mentions)
	}

	if _, err := postSvc.CreateAgentPost(ctx, botUser, postapp.CreatePostInput{CircleID: sharedomain.NewID(), Title: "x"}); err == nil {
		t.Fatal("CreateAgentPost into a missing circle must fail")
	}
}

// 不加 t.Parallel：要改全局 conf.Config.AiAgent.Autonomy。
func TestAgentAutoPost_EndToEnd(t *testing.T) {
	db, postSvc, circle, botUser := newAutoPostEnv(t)
	ctx := t.Context()

	prev := conf.Config.AiAgent.Autonomy
	conf.Config.AiAgent.Autonomy = conf.AiAgentAutonomy{Enabled: true, HotCircleID: circle.String()}
	t.Cleanup(func() { conf.Config.AiAgent.Autonomy = prev })

	agentRepo := agentinfra.NewAgentRepository(db)
	autoPost, _ := json.Marshal(agentdomain.AutoPostConfig{Enabled: true, IntervalMin: 10, DailyMax: 3, Topics: []string{"集成测试"}})
	agent := &agentdomain.AiAgent{
		ID: sharedomain.NewID(), LinkedUserID: botUser, APIProtocol: agentdomain.ProtocolOpenAI, Model: "it-model",
		TriggerMode: agentdomain.TriggerModeManual, Status: agentdomain.AgentStatusEnabled,
		AutoConfig: agentdomain.AutoConfigJSON{agentdomain.AutoConfigKeyAutoPost: autoPost},
	}
	agent.Name = "it-autopost-" + agent.ID.String()[24:]
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	testkit.CleanupRows(t, "domains.ai_agent", "id", agent.ID)
	testkit.CleanupRows(t, "domains.ai_agent_post_log", "agent_id", agent.ID)

	source := "https://it.example.com/" + agent.ID.String()
	llm := &draftLLM{}
	svc := agentapp.NewAutoPostService(onlyAgentRepo{AgentRepository: agentRepo, id: agent.ID},
		agentinfra.NewPostLogRepository(db), llm,
		stubTopicSource{items: []agentdomain.TopicItem{{Title: "集成测试来源", Snippet: "摘要", URL: source, Source: "it"}}})
	svc.SetPostCreator(&agentPostCreator{delegate: postSvc})

	svc.RunAutoPostScan(ctx)

	var posts []postdomain.Post
	if err := db.Where("user_id = ?", botUser).Find(&posts).Error; err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("bot posts = %d, want 1", len(posts))
	}
	p := posts[0]
	if p.CircleID != circle || p.Title != "自动发帖集成测试" ||
		!strings.Contains(p.Content, "- [集成测试来源]("+source+")") || !strings.HasSuffix(p.Content, "本帖由 AI 根据网络公开信息生成") {
		t.Fatalf("post = %+v", p)
	}
	var log agentdomain.PostLog
	if err := db.Where("agent_id = ?", agent.ID).Take(&log).Error; err != nil {
		t.Fatal(err)
	}
	if log.Status != agentdomain.PostLogStatusOK || log.PostID == nil || *log.PostID != p.ID || log.CircleID != circle {
		t.Fatalf("post log = %+v", log)
	}

	// 机器人账号发的帖不作为自动回复候选（防循环，#99 / #105）。
	if err := db.Model(&postdomain.Post{}).Where("id = ?", p.ID).Updates(map[string]interface{}{"hot": 1000, "status": postdomain.PostStatusPublished}).Error; err != nil {
		t.Fatal(err)
	}
	cands, err := postSvc.ListAutoReplyCandidates(ctx, postapp.AutoReplyPostQuery{
		CircleID: circle, HotMin: 1, Since: time.Now().Add(-time.Hour), Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if c.ID == p.ID {
			t.Fatal("bot's auto post must not be an auto-reply candidate")
		}
	}

	// 第二轮：interval_min 未到，且同一来源已用过，不再发帖、不调 LLM。
	svc.RunAutoPostScan(ctx)
	var n int64
	db.Model(&postdomain.Post{}).Where("user_id = ?", botUser).Count(&n)
	if n != 1 || llm.calls != 1 {
		t.Fatalf("second scan: posts=%d llm calls=%d, want 1/1", n, llm.calls)
	}
}
