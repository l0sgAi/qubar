//go:build integration

// post_log_integration_test.go 机器人自动发帖（#101）仓储对开发栈 PostgreSQL 的集成测试：
// 唯一约束 uk_ai_agent_post_log_topic（同一机器人同一话题只能占位一次，并发占位只有一个成功）、
// 占位行终态更新、限频统计 / 最近时间 / 来源 URL 去重查询；ListAutoPostEnabled 只加载启用的全局机器人。
package infrastructure_test

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/domains/aiagent/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

// newPostLog 构造一条自动发帖占位行（随机 ID，测试结束删除）。
func newPostLog(t *testing.T, agentID uuid.UUID, topicKey, url string) *domain.PostLog {
	t.Helper()
	l := &domain.PostLog{
		ID:          sharedomain.NewID(),
		AgentID:     agentID,
		CircleID:    sharedomain.NewID(),
		TopicKey:    topicKey,
		Status:      domain.PostLogStatusPending,
		SearchQuery: "it",
		Sources:     domain.PostSourcesJSON{{Title: "t", URL: url}},
	}
	testkit.CleanupRows(t, "domains.ai_agent_post_log", "id", l.ID)
	return l
}

// TestPostLog_TopicUniqueConstraint 同一机器人同一话题只能占位一次；其他机器人 / 其他话题互不冲突；
// 并发占位只有一个成功；直接 INSERT 被数据库唯一约束拦下。
func TestPostLog_TopicUniqueConstraint(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostLogRepository(db)
	ctx := t.Context()
	agentA, agentB := sharedomain.NewID(), sharedomain.NewID()
	key := sharedomain.NewID().String()

	claim := func(l *domain.PostLog, want bool) {
		t.Helper()
		ok, err := repo.CreatePlaceholder(ctx, l)
		if err != nil || ok != want {
			t.Fatalf("CreatePlaceholder = %v, %v; want %v", ok, err, want)
		}
	}
	claim(newPostLog(t, agentA, key, "https://a"), true)
	claim(newPostLog(t, agentA, key, "https://a"), false)
	claim(newPostLog(t, agentB, key, "https://a"), true)
	claim(newPostLog(t, agentA, key+"-2", "https://b"), true)

	if err := db.Create(newPostLog(t, agentA, key, "https://a")).Error; err == nil {
		t.Fatal("duplicate (agent_id, topic_key) must violate uk_ai_agent_post_log_topic")
	}

	// 并发：同一 (agent, topic) 8 个占位只有 1 个成功。
	agentC := sharedomain.NewID()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		l := newPostLog(t, agentC, key, "https://c")
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := repo.CreatePlaceholder(ctx, l); err == nil && ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("concurrent placeholders won = %d, want 1", wins.Load())
	}
}

// TestPostLog_FinishAndQueries 终态更新只作用于占位行；CountSinceByAgent / GetLastTimeByAgent /
// ListSourceURLsSince 口径。
func TestPostLog_FinishAndQueries(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostLogRepository(db)
	ctx := t.Context()
	agent := sharedomain.NewID()

	if last, err := repo.GetLastTimeByAgent(ctx, agent); err != nil || last != nil {
		t.Fatalf("GetLastTimeByAgent on empty = %v, %v", last, err)
	}

	ok := newPostLog(t, agent, "k1", "https://x.example.com/1")
	failed := newPostLog(t, agent, "k2", "https://x.example.com/2")
	for _, l := range []*domain.PostLog{ok, failed} {
		if _, err := repo.CreatePlaceholder(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	postID := sharedomain.NewID()
	ok.Status, ok.PostID, ok.PromptTokens = domain.PostLogStatusOK, &postID, 11
	ok.Sources = domain.PostSourcesJSON{{Title: "used", URL: "https://x.example.com/used"}}
	if err := repo.Finish(ctx, ok); err != nil {
		t.Fatal(err)
	}
	failed.Status, failed.ErrorMsg = domain.PostLogStatusFailed, "invalid llm output"
	if err := repo.Finish(ctx, failed); err != nil {
		t.Fatal(err)
	}
	// 终态不可再改。
	ok.Status = domain.PostLogStatusFailed
	if err := repo.Finish(ctx, ok); err != nil {
		t.Fatal(err)
	}

	var got domain.PostLog
	if err := db.Where("id = ?", ok.ID).Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.PostLogStatusOK || got.PostID == nil || *got.PostID != postID ||
		got.PromptTokens != 11 || len(got.Sources) != 1 || got.Sources[0].Title != "used" {
		t.Fatalf("finished row = %+v", got)
	}

	since := time.Now().Add(-time.Hour)
	if n, err := repo.CountSinceByAgent(ctx, agent, since); err != nil || n != 2 {
		t.Fatalf("CountSinceByAgent = %d, %v; want 2", n, err)
	}
	if n, _ := repo.CountSinceByAgent(ctx, agent, time.Now().Add(time.Minute)); n != 0 {
		t.Fatalf("CountSinceByAgent(future) = %d, want 0", n)
	}
	if last, err := repo.GetLastTimeByAgent(ctx, agent); err != nil || last == nil || last.Before(since) {
		t.Fatalf("GetLastTimeByAgent = %v, %v", last, err)
	}
	urls, err := repo.ListSourceURLsSince(ctx, agent, since)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, u := range urls {
		set[u] = true
	}
	if len(urls) != 2 || !set["https://x.example.com/used"] || !set["https://x.example.com/2"] {
		t.Fatalf("ListSourceURLsSince = %v", urls)
	}
}

// TestAgentRepo_ListAutoPostEnabled 只返回启用、未删除、全局且 auto_post.enabled=true 的机器人。
func TestAgentRepo_ListAutoPostEnabled(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewAgentRepository(db)
	on := json.RawMessage(`{"enabled":true,"interval_min":60,"daily_max":1}`)

	global := newAgent(t, db, repo, func(a *domain.AiAgent) {
		a.AutoConfig = domain.AutoConfigJSON{domain.AutoConfigKeyAutoPost: on}
	})
	scope := sharedomain.NewID()
	inCircle := newAgent(t, db, repo, func(a *domain.AiAgent) {
		a.CircleID = &scope
		a.AutoConfig = domain.AutoConfigJSON{domain.AutoConfigKeyAutoPost: on}
	})
	disabled := newAgent(t, db, repo, func(a *domain.AiAgent) {
		a.AutoConfig = domain.AutoConfigJSON{domain.AutoConfigKeyAutoPost: on}
	})
	if err := repo.UpdateFields(t.Context(), disabled.ID, map[string]interface{}{"status": domain.AgentStatusDisabled}); err != nil {
		t.Fatal(err)
	}
	replyOnly := newAgent(t, db, repo, func(a *domain.AiAgent) {
		a.AutoConfig = domain.AutoConfigJSON{domain.AutoConfigKeyAutoReply: on}
	})

	list, err := repo.ListAutoPostEnabled(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]bool{}
	for _, a := range list {
		seen[a.ID] = true
	}
	if !seen[global.ID] || seen[inCircle.ID] || seen[disabled.ID] || seen[replyOnly.ID] {
		t.Fatalf("ListAutoPostEnabled: global=%v inCircle=%v disabled=%v replyOnly=%v",
			seen[global.ID], seen[inCircle.ID], seen[disabled.ID], seen[replyOnly.ID])
	}
}
