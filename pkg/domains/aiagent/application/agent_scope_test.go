// agent_scope_test.go 机器人 @提及 可见范围投影（users.agent_circle_id / agent_mention_disabled）写路径单元测试。
//
// 覆盖：创建时圈内机器人写入圈子绑定 / 全局机器人不写绑定、创建即停用写不可 @；
// 全局 / 圈内两条链路的停用、启用、删除同步可 @ 状态（#171），删除不再清圈子绑定；
// 投影失败 / 端口未注入 fail-open（不回滚状态变更与软删），失败后异步重试按 ai_agent 当前状态重新推导。
package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
)

var botLinkedUserID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000b1")

// newScopedAgentSvc 构造注入了 creator + mentionSyncer 的圈内 Service。
func newScopedAgentSvc(repo *fakeAgentRepo, roles *fakeCircleRoles, creator *fakeBotUserCreator, syncer *fakeBotUserMentionSyncer) CircleAgentService {
	svc := NewCircleAgentService(repo)
	svc.SetCircleRoleReader(roles)
	svc.SetBotUserCreator(creator)
	svc.SetBotUserMentionSyncer(syncer)
	return svc
}

// newScopedGlobalSvc 构造注入了超管 role + creator + mentionSyncer 的全局 Service。
func newScopedGlobalSvc(repo *fakeAgentRepo, creator *fakeBotUserCreator, syncer *fakeBotUserMentionSyncer) AgentService {
	svc := NewAgentService(repo)
	svc.SetRoleReader(&fakeUserRoleReader{role: 1, ok: true})
	svc.SetBotUserCreator(creator)
	svc.SetBotUserMentionSyncer(syncer)
	return svc
}

// statusPtr 返回 status 指针（UpdateAgentInput / CreateAgentInput 用）。
func statusPtr(v int) *int { return &v }

// wantMentionCalls 断言投影调用序列（userID 恒为 botLinkedUserID）。
func wantMentionCalls(t *testing.T, got []mentionCall, want ...bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("SetBotUserMentionable calls = %v, want mentionable sequence %v", got, want)
	}
	for i, c := range got {
		if c.userID != botLinkedUserID || c.mentionable != want[i] {
			t.Fatalf("call[%d] = %+v, want {user=botLinkedUserID mentionable=%v}", i, c, want[i])
		}
	}
}

// TestCreateCircleAgent_ScopesBotUser 圈内创建：CreateBotUser 收到本圈 circleID（投影列写入），默认可 @。
func TestCreateCircleAgent_ScopesBotUser(t *testing.T) {
	repo := newFakeAgentRepo()
	creator := &fakeBotUserCreator{}
	svc := newScopedAgentSvc(repo, newFakeCircleRoles(), creator, &fakeBotUserMentionSyncer{})

	vo, err := svc.CreateCircleAgent(context.Background(), ownerID, circleAID, createInput())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if creator.gotCircle == nil || *creator.gotCircle != circleAID {
		t.Fatalf("CreateBotUser circleID = %v, want circleAID（圈内机器人必须写 users.agent_circle_id）", creator.gotCircle)
	}
	if !creator.gotMentionable {
		t.Fatal("CreateBotUser mentionable = false, want true（默认启用的机器人可被 @）")
	}
	if vo.LinkedUserID == uuid.Nil {
		t.Fatal("LinkedUserID not set")
	}
}

// TestCreateGlobalAgent_NotScoped 全局创建：CreateBotUser 收到 nil circleID（全站可见语义）。
func TestCreateGlobalAgent_NotScoped(t *testing.T) {
	repo := newFakeAgentRepo()
	creator := &fakeBotUserCreator{}
	svc := newScopedGlobalSvc(repo, creator, &fakeBotUserMentionSyncer{})

	if _, err := svc.CreateAgent(context.Background(), ownerID, createInput()); err != nil {
		t.Fatalf("global create failed: %v", err)
	}
	if creator.gotCircle != nil {
		t.Fatalf("CreateBotUser circleID = %v, want nil（全局机器人不得写绑定列）", *creator.gotCircle)
	}
	if !creator.gotMentionable {
		t.Fatal("CreateBotUser mentionable = false, want true")
	}
}

// TestCreateAgent_DisabledNotMentionable 创建即停用（status=0）：两条链路都把不可 @ 随建号写入。
func TestCreateAgent_DisabledNotMentionable(t *testing.T) {
	in := createInput()
	in.Status = statusPtr(0)

	globalCreator := &fakeBotUserCreator{}
	if _, err := newScopedGlobalSvc(newFakeAgentRepo(), globalCreator, &fakeBotUserMentionSyncer{}).
		CreateAgent(context.Background(), ownerID, in); err != nil {
		t.Fatalf("global create failed: %v", err)
	}
	if globalCreator.gotMentionable {
		t.Fatal("global: CreateBotUser mentionable = true, want false（创建即停用）")
	}

	circleCreator := &fakeBotUserCreator{}
	if _, err := newScopedAgentSvc(newFakeAgentRepo(), newFakeCircleRoles(), circleCreator, &fakeBotUserMentionSyncer{}).
		CreateCircleAgent(context.Background(), ownerID, circleAID, in); err != nil {
		t.Fatalf("circle create failed: %v", err)
	}
	if circleCreator.gotMentionable {
		t.Fatal("circle: CreateBotUser mentionable = true, want false（创建即停用）")
	}
}

// TestUpdateAgent_StatusSyncsMentionable 全局链路：停用 → 不可 @，启用 → 可 @；不改 status 不投影。
func TestUpdateAgent_StatusSyncsMentionable(t *testing.T) {
	repo := newFakeAgentRepo()
	seedGlobalAgent(repo).LinkedUserID = botLinkedUserID
	syncer := &fakeBotUserMentionSyncer{}
	svc := newScopedGlobalSvc(repo, &fakeBotUserCreator{}, syncer)
	ctx := context.Background()

	if _, err := svc.UpdateAgent(ctx, ownerID, globalAgentID, UpdateAgentInput{Status: statusPtr(0)}); err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	if _, err := svc.UpdateAgent(ctx, ownerID, globalAgentID, UpdateAgentInput{Status: statusPtr(1)}); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	prompt := "新的系统提示词"
	if _, err := svc.UpdateAgent(ctx, ownerID, globalAgentID, UpdateAgentInput{SystemPrompt: &prompt}); err != nil {
		t.Fatalf("update prompt failed: %v", err)
	}
	wantMentionCalls(t, syncer.snapshot(), false, true)
}

// TestUpdateCircleAgent_StatusSyncsMentionable 圈内链路同构：停用 → 不可 @，启用 → 可 @。
func TestUpdateCircleAgent_StatusSyncsMentionable(t *testing.T) {
	repo := newFakeAgentRepo()
	seedCircleAgent(repo).LinkedUserID = botLinkedUserID
	syncer := &fakeBotUserMentionSyncer{}
	svc := newScopedAgentSvc(repo, newFakeCircleRoles(), &fakeBotUserCreator{}, syncer)
	ctx := context.Background()

	if _, err := svc.UpdateCircleAgent(ctx, adminID, circleAgentID, UpdateAgentInput{Status: statusPtr(0)}); err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	if _, err := svc.UpdateCircleAgent(ctx, adminID, circleAgentID, UpdateAgentInput{Status: statusPtr(1)}); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	wantMentionCalls(t, syncer.snapshot(), false, true)
}

// TestDeleteAgent_NotMentionable 全局删除：软删成功后把机器人账号投影为不可 @。
func TestDeleteAgent_NotMentionable(t *testing.T) {
	repo := newFakeAgentRepo()
	seedGlobalAgent(repo).LinkedUserID = botLinkedUserID
	syncer := &fakeBotUserMentionSyncer{}
	svc := newScopedGlobalSvc(repo, &fakeBotUserCreator{}, syncer)

	if err := svc.DeleteAgent(context.Background(), ownerID, globalAgentID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != globalAgentID {
		t.Fatalf("SoftDelete not applied: %v", repo.deleted)
	}
	wantMentionCalls(t, syncer.snapshot(), false)
}

// TestDeleteCircleAgent_NotMentionable 圈内删除：软删成功后投影为不可 @（圈子绑定保留，不再清空）。
func TestDeleteCircleAgent_NotMentionable(t *testing.T) {
	repo := newFakeAgentRepo()
	seedCircleAgent(repo).LinkedUserID = botLinkedUserID
	syncer := &fakeBotUserMentionSyncer{}
	svc := newScopedAgentSvc(repo, newFakeCircleRoles(), &fakeBotUserCreator{}, syncer)

	if err := svc.DeleteCircleAgent(context.Background(), ownerID, circleAgentID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != circleAgentID {
		t.Fatalf("SoftDelete not applied: %v", repo.deleted)
	}
	wantMentionCalls(t, syncer.snapshot(), false)
}

// shortenMentionRetry 把异步重试退避缩到毫秒级，测试结束后恢复。
func shortenMentionRetry(t *testing.T) {
	t.Helper()
	old := botMentionSyncBackoff
	botMentionSyncBackoff = time.Millisecond
	t.Cleanup(func() { botMentionSyncBackoff = old })
}

// waitMentionCalls 等到投影调用数达到 n（异步重试在后台 goroutine 完成）。
func waitMentionCalls(t *testing.T, syncer *fakeBotUserMentionSyncer, n int) []mentionCall {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		calls := syncer.snapshot()
		if len(calls) >= n {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d SetBotUserMentionable calls, got %v", n, calls)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestDeleteCircleAgent_FailOpenAndRetry 投影失败不回滚删除（软删已生效）：删除仍返回 nil；
// 后台重试按 ai_agent 当前状态（已软删 → 不可 @）补写成功。
func TestDeleteCircleAgent_FailOpenAndRetry(t *testing.T) {
	shortenMentionRetry(t)
	repo := newFakeAgentRepo()
	seedCircleAgent(repo).LinkedUserID = botLinkedUserID
	syncer := &fakeBotUserMentionSyncer{errs: []error{errors.New("pg down")}}
	svc := newScopedAgentSvc(repo, newFakeCircleRoles(), &fakeBotUserCreator{}, syncer)

	if err := svc.DeleteCircleAgent(context.Background(), ownerID, circleAgentID); err != nil {
		t.Fatalf("delete must succeed even when syncing fails, got: %v", err)
	}
	if len(repo.deleted) != 1 {
		t.Fatalf("SoftDelete not applied: %v", repo.deleted)
	}
	wantMentionCalls(t, waitMentionCalls(t, syncer, 2), false, false)
}

// TestUpdateAgent_RetryRederivesFromAgent 重试不重放旧值：停用投影失败后机器人又被启用，
// 重试按 ai_agent 当前状态（已启用）写可 @，不会把它覆盖回不可 @。
func TestUpdateAgent_RetryRederivesFromAgent(t *testing.T) {
	base := newFakeAgentRepo()
	seedGlobalAgent(base).LinkedUserID = botLinkedUserID
	repo := &lockedAgentRepo{fakeAgentRepo: base}
	syncer := &fakeBotUserMentionSyncer{errs: []error{errors.New("pg down")}}
	svc := NewAgentService(repo)
	svc.SetRoleReader(&fakeUserRoleReader{role: 1, ok: true})
	svc.SetBotUserMentionSyncer(syncer)
	ctx := context.Background()

	// 退避放长：保证两次更新都在重试前完成，重试时 ai_agent 已是启用状态。
	old := botMentionSyncBackoff
	botMentionSyncBackoff = 50 * time.Millisecond
	t.Cleanup(func() { botMentionSyncBackoff = old })

	if _, err := svc.UpdateAgent(ctx, ownerID, globalAgentID, UpdateAgentInput{Status: statusPtr(0)}); err != nil {
		t.Fatalf("disable must succeed even when syncing fails, got: %v", err)
	}
	if _, err := svc.UpdateAgent(ctx, ownerID, globalAgentID, UpdateAgentInput{Status: statusPtr(1)}); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	if got, _ := repo.GetByID(ctx, globalAgentID); got.Status != domain.AgentStatusEnabled {
		t.Fatalf("agent status = %d, want enabled", got.Status)
	}
	wantMentionCalls(t, waitMentionCalls(t, syncer, 3), false, true, true)
}

// lockedAgentRepo 给 fakeAgentRepo 的读写加锁：异步重试在后台 goroutine 读 GetByID，
// 与测试 goroutine 的 UpdateFields 并发。
type lockedAgentRepo struct {
	mu sync.Mutex
	*fakeAgentRepo
}

func (r *lockedAgentRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.AiAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fakeAgentRepo.GetByID(ctx, id)
}

func (r *lockedAgentRepo) UpdateFields(ctx context.Context, id uuid.UUID, fields map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fakeAgentRepo.UpdateFields(ctx, id, fields)
}

// TestDeleteCircleAgent_FailOpen_NoSyncer 端口未注入（装配遗漏）：跳过投影不 panic，
// 删除主流程照常成功（与 botUserUpdater 的 nil 处理范式一致）。
func TestDeleteCircleAgent_FailOpen_NoSyncer(t *testing.T) {
	repo := newFakeAgentRepo()
	seedCircleAgent(repo).LinkedUserID = botLinkedUserID

	svc := newCircleAgentSvc(repo, newFakeCircleRoles()) // 未 SetBotUserMentionSyncer
	if err := svc.DeleteCircleAgent(context.Background(), ownerID, circleAgentID); err != nil {
		t.Fatalf("delete failed without syncer injected: %v", err)
	}
	if len(repo.deleted) != 1 {
		t.Fatalf("SoftDelete not applied: %v", repo.deleted)
	}
}
