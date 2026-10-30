//go:build integration

// agent_auto_reply_integration_test.go 机器人自动回复（#99）端到端集成测试：生产装配
// （post / comment Service + agent 桥接器 + PG 仓储）对开发栈跑一轮扫描，LLM 用桩。
// 验证：达到门槛的帖子 / 评论被自动回复（帖子顶层评论、评论挂楼层）、机器人账号的内容与他圈内容不被选中、
// 重复扫描不再回复（日志表部分唯一索引兜底）。
package composition

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"interestBar/pkg/conf"
	agentapp "interestBar/pkg/domains/aiagent/application"
	agentdomain "interestBar/pkg/domains/aiagent/domain"
	agentinfra "interestBar/pkg/domains/aiagent/infrastructure"
	commentdomain "interestBar/pkg/domains/comment/domain"
	commentinfra "interestBar/pkg/domains/comment/infrastructure"
	postdomain "interestBar/pkg/domains/post/domain"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// stubLLM 固定输出的 LLM 桩（不出网）。
type stubLLM struct{ calls atomic.Int64 }

func (s *stubLLM) Generate(ctx context.Context, req agentapp.LLMRequest) (*agentapp.LLMResult, error) {
	s.calls.Add(1)
	return &agentapp.LLMResult{Content: "自动回复集成测试"}, nil
}

// onlyAgentRepo 把扫描范围收窄到本测试的机器人：开发栈是共享的，
// 不能让本测试去回复其他测试 / 开发者在库里留下的机器人配置。
type onlyAgentRepo struct {
	agentdomain.AgentRepository
	id uuid.UUID
}

func (r onlyAgentRepo) ListAutoReplyEnabled(ctx context.Context) ([]agentdomain.AiAgent, error) {
	all, err := r.AgentRepository.ListAutoReplyEnabled(ctx)
	var out []agentdomain.AiAgent
	for _, a := range all {
		if a.ID == r.id {
			out = append(out, a)
		}
	}
	return out, err
}

func insertTestUser(t *testing.T, db *gorm.DB, role int) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if err := db.Exec("INSERT INTO domains.users (id, username, email, role) VALUES (?, ?, ?, ?)",
		id, "it-user", fmt.Sprintf("%s@it.qubar.local", id), role).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	testkit.CleanupRows(t, "domains.users", "id", id)
	return id
}

func insertHotPost(t *testing.T, db *gorm.DB, circle, author uuid.UUID, hot int) uuid.UUID {
	t.Helper()
	p := &postdomain.Post{CircleID: circle, UserID: author, Title: "自动回复集成测试", Status: postdomain.PostStatusPublished, Hot: hot}
	if err := postinfra.NewPostRepository(db).Create(t.Context(), p); err != nil {
		t.Fatalf("create post: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", p.ID)
	testkit.CleanupKeys(t, redispkg.GetPostStatsKey(p.ID), redispkg.GetPostHotCapKey(p.ID))
	return p.ID
}

// 不加 t.Parallel：要改全局 conf.Config.AiAgent.Autonomy（包内其他测试不读它，串行段内改写无竞争）。
func TestAgentAutoReply_EndToEnd(t *testing.T) {
	db := testkit.PG(t)
	testkit.Redis(t)
	testkit.Redpanda(t)
	ctx := t.Context()

	prev := conf.Config.AiAgent.Autonomy
	conf.Config.AiAgent.Autonomy = conf.AiAgentAutonomy{Enabled: true}
	t.Cleanup(func() { conf.Config.AiAgent.Autonomy = prev })

	circle, otherCircle := sharedomain.NewID(), sharedomain.NewID()
	human := insertTestUser(t, db, 0)
	botUser := insertTestUser(t, db, 2)

	// 本圈机器人：帖子 hot>=100、评论点赞>=10，取消限频便于一轮内回复多个目标。
	agentRepo := agentinfra.NewAgentRepository(db)
	autoReply, _ := json.Marshal(agentdomain.AutoReplyConfig{
		Enabled: true, Targets: []string{"post", "comment"}, PostHotMin: 100, CommentLikeMin: 10, WindowHours: 24,
	})
	agent := &agentdomain.AiAgent{
		ID: sharedomain.NewID(), LinkedUserID: botUser, CircleID: &circle,
		APIProtocol: agentdomain.ProtocolOpenAI, Model: "it-model",
		TriggerMode: agentdomain.TriggerModeManual, Status: agentdomain.AgentStatusEnabled,
		AutoConfig: agentdomain.AutoConfigJSON{agentdomain.AutoConfigKeyAutoReply: autoReply},
	}
	agent.Name = "it-auto-" + agent.ID.String()[24:]
	if err := agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := agentRepo.UpdateFields(ctx, agent.ID, map[string]interface{}{"max_replies_per_hour": 0, "min_interval_sec": 0}); err != nil {
		t.Fatal(err)
	}
	testkit.CleanupRows(t, "domains.ai_agent", "id", agent.ID)
	testkit.CleanupRows(t, "domains.ai_agent_reply_log", "agent_id", agent.ID)
	testkit.CleanupRows(t, "domains.comment", "user_id", botUser) // 机器人发出的评论

	hotPost := insertHotPost(t, db, circle, human, 500)
	coldPost := insertHotPost(t, db, circle, human, 1)
	_ = insertHotPost(t, db, circle, botUser, 900)    // 机器人账号的帖子：不选
	_ = insertHotPost(t, db, otherCircle, human, 900) // 他圈：不选

	commentRepo := commentinfra.NewCommentRepository(db)
	hotComment := &commentdomain.Comment{PostID: coldPost, UserID: human, Content: "这条评论很热", ExtraData: []byte("{}"), LikeCount: 20, Status: commentdomain.CommentStatusNormal}
	if err := commentRepo.Create(ctx, hotComment); err != nil {
		t.Fatal(err)
	}
	testkit.CleanupRows(t, "domains.comment", "id", hotComment.ID)

	// 生产装配：post / comment Service + aiagent 桥接器。
	deps := NewDeps()
	postSvc := newPostService(deps)
	commentSvc := newCommentService(deps)
	commentSvc.SetPostLookup(&commentPostLookup{delegate: postSvc})
	llm := &stubLLM{}
	replySvc := agentapp.NewReplyService(onlyAgentRepo{AgentRepository: agentRepo, id: agent.ID},
		agentinfra.NewReplyLogRepository(db), llm)
	replySvc.SetPostReader(&agentPostReader{delegate: postSvc})
	replySvc.SetCommentCreator(&agentCommentCreator{delegate: commentSvc})
	replySvc.SetCandidateReader(&agentCandidateReader{post: postSvc, comment: commentSvc})

	replySvc.RunAutoReplyScan(ctx)

	var replies []commentdomain.Comment
	if err := db.Where("user_id = ? AND deleted = 0", botUser).Find(&replies).Error; err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Fatalf("bot replies = %d, want 2 (hot post + hot comment): %+v", len(replies), replies)
	}
	var onPost, onComment bool
	for _, c := range replies {
		switch {
		case c.PostID == hotPost && c.RootID == nil && c.ReplyToID == nil:
			onPost = true
		case c.PostID == coldPost && c.RootID != nil && *c.RootID == hotComment.ID &&
			c.ReplyToID != nil && *c.ReplyToID == hotComment.ID:
			onComment = true
		}
	}
	if !onPost || !onComment {
		t.Fatalf("want top-level reply on hot post and floor reply on hot comment, got %+v", replies)
	}

	var logs []agentdomain.ReplyLog
	if err := db.Where("agent_id = ?", agent.ID).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("reply logs = %d, want 2", len(logs))
	}
	for _, l := range logs {
		if l.TriggerSource != agentdomain.TriggerSourceAuto || l.Status != agentdomain.ReplyStatusOK || l.CommentID == nil {
			t.Fatalf("auto log row = %+v", l)
		}
		if l.PostID == coldPost && (l.TargetCommentID == nil || *l.TargetCommentID != hotComment.ID) {
			t.Fatalf("comment target not recorded: %+v", l)
		}
	}

	// 第二轮：同一目标不再回复。
	before := llm.calls.Load()
	replySvc.RunAutoReplyScan(ctx)
	var n int64
	db.Model(&commentdomain.Comment{}).Where("user_id = ?", botUser).Count(&n)
	if n != 2 || llm.calls.Load() != before {
		t.Fatalf("second scan must not reply again: comments=%d llm calls %d -> %d", n, before, llm.calls.Load())
	}
}
