//go:build integration

// auto_reply_integration_test.go 机器人自动回复（#99）仓储对开发栈 PostgreSQL 的集成测试：
// 部分唯一索引 idx_ai_reply_auto_target（每机器人每目标最多一行，并发占位只有一个成功）、
// 占位行终态更新、每目标 / 每小时计数口径、窗口内目标列表；auto_config 顶层 key 合并与启用机器人加载。
package infrastructure_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/domains/aiagent/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TestMain 在同包的 search_quota_redis_integration_test.go（内部测试包，与本文件编进同一个测试二进制）。

// newPlaceholder 构造一条自动回复占位行（随机 ID，测试结束删除）。
func newPlaceholder(t *testing.T, agentID, postID uuid.UUID, target *uuid.UUID) *domain.ReplyLog {
	t.Helper()
	l := &domain.ReplyLog{
		ID:              sharedomain.NewID(),
		AgentID:         agentID,
		PostID:          postID,
		UserID:          sharedomain.NewID(),
		TriggerSource:   domain.TriggerSourceAuto,
		TargetCommentID: target,
		Status:          domain.ReplyStatusPending,
	}
	testkit.CleanupRows(t, "domains.ai_agent_reply_log", "id", l.ID)
	return l
}

func mustClaim(t *testing.T, repo domain.ReplyLogRepository, l *domain.ReplyLog, want bool) {
	t.Helper()
	ok, err := repo.CreateAutoPlaceholder(t.Context(), l)
	if err != nil {
		t.Fatalf("CreateAutoPlaceholder: %v", err)
	}
	if ok != want {
		t.Fatalf("CreateAutoPlaceholder = %v, want %v", ok, want)
	}
}

// TestReplyLog_AutoTargetUniqueConstraint 同一机器人对同一目标只能占位一次；
// 不同机器人 / 不同目标（帖子 vs 评论）互不冲突；被动触发行（trigger_source≠4）不受约束。
func TestReplyLog_AutoTargetUniqueConstraint(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewReplyLogRepository(db)
	agentA, agentB := sharedomain.NewID(), sharedomain.NewID()
	post, comment := sharedomain.NewID(), sharedomain.NewID()

	mustClaim(t, repo, newPlaceholder(t, agentA, post, nil), true)
	mustClaim(t, repo, newPlaceholder(t, agentA, post, nil), false)      // 重复：撞部分唯一索引
	mustClaim(t, repo, newPlaceholder(t, agentA, post, &comment), true)  // 同帖的评论目标是另一个目标
	mustClaim(t, repo, newPlaceholder(t, agentA, post, &comment), false) // 评论目标重复
	mustClaim(t, repo, newPlaceholder(t, agentB, post, nil), true)       // 其他机器人
	mustClaim(t, repo, newPlaceholder(t, agentA, sharedomain.NewID(), nil), true)

	// 直接 INSERT 绕过 ON CONFLICT：确认是数据库唯一约束在拦（而不是应用层判断）。
	dup := newPlaceholder(t, agentA, post, nil)
	if err := db.Create(dup).Error; err == nil {
		t.Fatal("duplicate auto row must violate idx_ai_reply_auto_target")
	}

	// 被动触发（关键词）可以对同一目标写多行。
	for i := 0; i < 2; i++ {
		l := newPlaceholder(t, agentA, post, &comment)
		l.TriggerSource = domain.TriggerSourceKeyword
		l.Status = domain.ReplyStatusOK
		if err := repo.Create(t.Context(), l); err != nil {
			t.Fatalf("keyword rows must not be constrained: %v", err)
		}
	}
}

// TestReplyLog_AutoPlaceholderConcurrent 多实例并发占位同一目标：恰好一个成功。
func TestReplyLog_AutoPlaceholderConcurrent(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewReplyLogRepository(db)
	agent, post := sharedomain.NewID(), sharedomain.NewID()

	const n = 8
	logs := make([]*domain.ReplyLog, n)
	for i := range logs {
		logs[i] = newPlaceholder(t, agent, post, nil)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, l := range logs {
		wg.Add(1)
		go func(l *domain.ReplyLog) {
			defer wg.Done()
			ok, err := repo.CreateAutoPlaceholder(t.Context(), l)
			if err != nil {
				t.Errorf("CreateAutoPlaceholder: %v", err)
				return
			}
			if ok {
				wins.Add(1)
			}
		}(l)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("concurrent placeholders won = %d, want exactly 1", wins.Load())
	}
}

// TestReplyLog_AutoFinishAndCounts 占位行原地更新终态；每目标计数排除 status=2；
// 每小时计数排除 status=2/3 且只算 trigger_source=4；窗口内目标列表。
func TestReplyLog_AutoFinishAndCounts(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewReplyLogRepository(db)
	ctx := t.Context()
	agentA, agentB := sharedomain.NewID(), sharedomain.NewID()
	post, comment := sharedomain.NewID(), sharedomain.NewID()

	pa := newPlaceholder(t, agentA, post, nil)
	mustClaim(t, repo, pa, true)
	pb := newPlaceholder(t, agentB, post, nil)
	mustClaim(t, repo, pb, true)
	pc := newPlaceholder(t, agentA, post, &comment)
	mustClaim(t, repo, pc, true)

	commentID := sharedomain.NewID()
	pa.Status, pa.CommentID, pa.LatencyMs, pa.PromptTokens = domain.ReplyStatusOK, &commentID, 123, 7
	if err := repo.FinishAutoPlaceholder(ctx, pa); err != nil {
		t.Fatalf("FinishAutoPlaceholder: %v", err)
	}
	pb.Status, pb.ErrorMsg = domain.ReplyStatusSkipped, "无关"
	if err := repo.FinishAutoPlaceholder(ctx, pb); err != nil {
		t.Fatalf("FinishAutoPlaceholder: %v", err)
	}
	var got domain.ReplyLog
	if err := db.First(&got, "id = ?", pa.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.ReplyStatusOK || got.CommentID == nil || *got.CommentID != commentID ||
		got.LatencyMs != 123 || got.PromptTokens != 7 || got.TriggerSource != domain.TriggerSourceAuto {
		t.Fatalf("finished row = %+v", got)
	}

	// 每目标：帖子目标 A 成功 + B 分类器拒绝 → 1；评论目标 pending → 1。
	if n, err := repo.CountAutoByTarget(ctx, post, nil); err != nil || n != 1 {
		t.Fatalf("CountAutoByTarget(post) = %d, %v; want 1", n, err)
	}
	if n, err := repo.CountAutoByTarget(ctx, post, &comment); err != nil || n != 1 {
		t.Fatalf("CountAutoByTarget(comment) = %d, %v; want 1", n, err)
	}

	// 每小时：A 有成功 + pending 两行；再加一条 A 的关键词行（不计入自动回复口径）。
	kw := newPlaceholder(t, agentA, post, nil)
	kw.TriggerSource, kw.Status = domain.TriggerSourceKeyword, domain.ReplyStatusOK
	if err := repo.Create(ctx, kw); err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-time.Hour)
	if n, err := repo.CountAutoSinceByAgent(ctx, agentA, since); err != nil || n != 2 {
		t.Fatalf("CountAutoSinceByAgent(A) = %d, %v; want 2", n, err)
	}
	if n, err := repo.CountAutoSinceByAgent(ctx, agentB, since); err != nil || n != 0 {
		t.Fatalf("CountAutoSinceByAgent(B) = %d, %v; want 0 (skipped excluded)", n, err)
	}

	// 窗口内目标：至少包含本测试的三行（其他并行测试的行也可能在内）。
	recs, err := repo.ListAutoTargetsSince(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("ListAutoTargetsSince: %v", err)
	}
	mine := 0
	for _, r := range recs {
		if r.PostID == post {
			mine++
			if r.AgentID == agentB && r.Status != domain.ReplyStatusSkipped {
				t.Fatalf("record status not returned: %+v", r)
			}
		}
	}
	if mine != 3 {
		t.Fatalf("ListAutoTargetsSince own records = %d, want 3 (keyword row excluded)", mine)
	}
	// 窗口起点之后才写入：未来时间的窗口不应返回本测试的行。
	recs, err = repo.ListAutoTargetsSince(ctx, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.PostID == post {
			t.Fatal("rows before window start must not be listed")
		}
	}
}

// newAgent 插入一个机器人（随机 ID / 名称，测试结束删除）。
func newAgent(t *testing.T, db *gorm.DB, repo domain.AgentRepository, mutate func(*domain.AiAgent)) *domain.AiAgent {
	t.Helper()
	a := &domain.AiAgent{
		ID:           sharedomain.NewID(),
		LinkedUserID: sharedomain.NewID(),
		APIProtocol:  domain.ProtocolOpenAI,
		Model:        "it-model",
		TriggerMode:  domain.TriggerModeManual,
		Status:       domain.AgentStatusEnabled,
	}
	a.Name = fmt.Sprintf("it-%s", a.ID.String()[24:])
	if mutate != nil {
		mutate(a)
	}
	if err := repo.Create(t.Context(), a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	testkit.CleanupRows(t, "domains.ai_agent", "id", a.ID)
	return a
}

// TestAgentRepo_AutoConfig auto_config 默认 {}；补丁按顶层 key 合并（保留兄弟键）；
// ListAutoReplyEnabled 只返回启用且 auto_reply.enabled=true 的机器人。
func TestAgentRepo_AutoConfig(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewAgentRepository(db)
	ctx := t.Context()

	plain := newAgent(t, db, repo, nil)
	got, err := repo.GetByID(ctx, plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AutoConfig) != 0 {
		t.Fatalf("default auto_config should be empty, got %v", got.AutoConfig)
	}

	// 绑定随机圈子：开发栈共享，若有开发中的应用在扫描，这个机器人也找不到任何候选。
	scope := sharedomain.NewID()
	on := newAgent(t, db, repo, func(a *domain.AiAgent) {
		a.CircleID = &scope
		a.AutoConfig = domain.AutoConfigJSON{"auto_post": json.RawMessage(`{"enabled":false,"x":1}`)}
	})
	patch := domain.AutoConfigPatch{domain.AutoConfigKeyAutoReply: json.RawMessage(
		`{"enabled":true,"targets":["post"],"post_hot_min":10,"window_hours":24}`)}
	if err := repo.UpdateFields(ctx, on.ID, map[string]interface{}{"auto_config": patch, "min_interval_sec": 0}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}
	got, err = repo.GetByID(ctx, on.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := got.AutoConfig.AutoReply()
	if err != nil || !cfg.Enabled || cfg.PostHotMin != 10 || !cfg.HasTarget(domain.AutoReplyTargetPost) {
		t.Fatalf("auto_reply after patch = %+v, %v", cfg, err)
	}
	if string(got.AutoConfig["auto_post"]) == "" || got.MinIntervalSec != 0 {
		t.Fatalf("sibling key must survive and other fields update: %+v", got)
	}

	disabled := newAgent(t, db, repo, func(a *domain.AiAgent) {
		a.CircleID = &scope
		a.AutoConfig = domain.AutoConfigJSON{domain.AutoConfigKeyAutoReply: json.RawMessage(`{"enabled":true}`)}
	})
	if err := repo.UpdateFields(ctx, disabled.ID, map[string]interface{}{"status": domain.AgentStatusDisabled}); err != nil {
		t.Fatal(err)
	}
	junk := newAgent(t, db, repo, func(a *domain.AiAgent) {
		a.AutoConfig = domain.AutoConfigJSON{domain.AutoConfigKeyAutoReply: json.RawMessage(`{"enabled":"yes"}`)}
	})

	list, err := repo.ListAutoReplyEnabled(ctx)
	if err != nil {
		t.Fatalf("ListAutoReplyEnabled: %v", err)
	}
	seen := map[uuid.UUID]bool{}
	for _, a := range list {
		seen[a.ID] = true
	}
	if !seen[on.ID] || seen[plain.ID] || seen[disabled.ID] || seen[junk.ID] {
		t.Fatalf("ListAutoReplyEnabled: on=%v plain=%v disabled=%v junk=%v",
			seen[on.ID], seen[plain.ID], seen[disabled.ID], seen[junk.ID])
	}
}
