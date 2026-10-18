// circle_service_test.go 圈子级 AI 机器人管理（CircleAgentService）单元测试。
//
// 覆盖设计文档 §八-8 的服务层可测项：权限矩阵（owner/admin/member/非成员/被禁言 owner
// × 各端点）、字段分级（admin 改凭据字段 → 403）、限额边界（超限 409 + 传给仓储的
// maxPerCircle）、跨作用域 404（两条链路互不暴露）、名称作用域（圈内冲突/跨圈与全局同名可建）、
// fail-closed（端口未注入一律拒绝）、ManualReply 圈内守卫。
// AgentRepository 用嵌入接口的最小 fake；行锁/唯一索引等 DB 兜底逻辑由
// CreateInCircle 的 PG 实现保证（无法在单测覆盖，见 fake 注释）。
package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TestMain 测试装配：api_key 加密路径依赖 conf.Config.Security.DataKey（生产由
// 配置加载写入），单测注入固定 32 字节测试密钥（SHA-256 派生后即 AES-256 密钥）；
// logger 注入 Nop 实现（fail-open 路径会打日志，避免 nil logger panic）。
func TestMain(m *testing.M) {
	conf.Config = &conf.AppConfig{}
	conf.Config.Security.DataKey = "unit-test-data-key-0123456789abcdef"
	logger.Log = zap.NewNop()
	m.Run()
}

// ---- 测试固定标识 ----

var (
	circleAID  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000aa")
	circleBID  = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000bb")
	ownerID    = uuid.MustParse("0192a0d0-0000-7000-8000-000000000001")
	adminID    = uuid.MustParse("0192a0d0-0000-7000-8000-000000000002")
	memberID   = uuid.MustParse("0192a0d0-0000-7000-8000-000000000003")
	outsiderID = uuid.MustParse("0192a0d0-0000-7000-8000-000000000004")
	mutedOwner = uuid.MustParse("0192a0d0-0000-7000-8000-000000000005") // role=30 但被禁言

	circleAgentID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000c1")
	globalAgentID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000c2")
)

// ---- fakes ----

// fakeAgentRepo 最小 AgentRepository fake：仅实现 CircleAgentService / 全局链守卫
// 触达的方法；CreateInCircle 的行锁+计数语义无法在内存 fake 中复现，
// 用 createErr 预设仓储返回（如 ErrCircleAgentLimit）。
type fakeAgentRepo struct {
	domain.AgentRepository // 未实现的方法不会被被测路径触达

	byID      map[uuid.UUID]*domain.AiAgent
	nameTaken map[uuid.UUID]map[string]bool // 作用域桶(circleID 或 uuid.Nil) → 已占名称

	createErr error // CreateInCircle 预设错误
	gotMax    int   // CreateInCircle 收到的 maxPerCircle
	updated   map[uuid.UUID]map[string]interface{}
	deleted   []uuid.UUID

	// scopeFilterEnabled 模拟 ListEnabledForCircle 的 SQL 圈子收口语义；
	// 置 false 模拟候选集语义漂移（返回全部启用机器人），用于验证触发链
	// agentInScope 第二道防线（circle-agent-reply 双保险）。
	scopeFilterEnabled bool
}

func newFakeAgentRepo() *fakeAgentRepo {
	return &fakeAgentRepo{
		byID:               map[uuid.UUID]*domain.AiAgent{},
		nameTaken:          map[uuid.UUID]map[string]bool{},
		updated:            map[uuid.UUID]map[string]interface{}{},
		scopeFilterEnabled: true,
	}
}

func (f *fakeAgentRepo) seed(a *domain.AiAgent) {
	f.byID[a.ID] = a
	bucket := uuid.Nil
	if a.CircleID != nil {
		bucket = *a.CircleID
	}
	if f.nameTaken[bucket] == nil {
		f.nameTaken[bucket] = map[string]bool{}
	}
	f.nameTaken[bucket][a.Name] = true
}

func (f *fakeAgentRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.AiAgent, error) {
	if a, ok := f.byID[id]; ok {
		cp := *a
		return &cp, nil
	}
	return nil, domain.ErrAgentNotFound
}

func (f *fakeAgentRepo) ExistsByNameInScope(ctx context.Context, circleID uuid.UUID, name string, excludeID uuid.UUID) (bool, error) {
	if excludeID != uuid.Nil {
		if a, ok := f.byID[excludeID]; ok && a.Name == name {
			return false, nil
		}
	}
	return f.nameTaken[circleID][name], nil
}

func (f *fakeAgentRepo) CreateInCircle(ctx context.Context, a *domain.AiAgent, maxPerCircle int) error {
	f.gotMax = maxPerCircle
	if f.createErr != nil {
		return f.createErr
	}
	cp := *a
	f.byID[a.ID] = &cp
	return nil
}

func (f *fakeAgentRepo) Create(ctx context.Context, a *domain.AiAgent) error {
	cp := *a
	f.byID[a.ID] = &cp
	return nil
}

func (f *fakeAgentRepo) UpdateFields(ctx context.Context, id uuid.UUID, fields map[string]interface{}) error {
	a, ok := f.byID[id]
	if !ok {
		return domain.ErrAgentNotFound
	}
	f.updated[id] = fields
	// 应用到存储实体（模拟 PG 行更新），供更新后 GetByID 回读断言。
	for k, v := range fields {
		switch k {
		case "name":
			a.Name = v.(string)
		case "avatar_url":
			a.AvatarURL = v.(string)
		case "api_protocol":
			a.APIProtocol = v.(string)
		case "base_url":
			a.BaseURL = v.(string)
		case "api_key":
			a.APIKeyEnc = v.(string)
		case "model":
			a.Model = v.(string)
		case "system_prompt":
			a.SystemPrompt = v.(string)
		case "filter_prompt":
			a.FilterPrompt = v.(string)
		case "trigger_mode":
			a.TriggerMode = domain.TriggerMode(v.(int))
		case "max_replies_per_hour":
			a.MaxRepliesPerHour = v.(int)
		case "min_interval_sec":
			a.MinIntervalSec = v.(int)
		case "status":
			a.Status = int16(v.(int))
		}
	}
	return nil
}

func (f *fakeAgentRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

// ListEnabledForCircle 模拟 PG 的圈子收口语义：全局机器人 + circleID 圈内机器人
// （circleID=Nil 仅全局）；scopeFilterEnabled=false 时返回全部启用机器人（漂移模拟）。
func (f *fakeAgentRepo) ListEnabledForCircle(ctx context.Context, circleID uuid.UUID) ([]domain.AiAgent, error) {
	var out []domain.AiAgent
	for _, a := range f.byID {
		if a.Status != domain.AgentStatusEnabled {
			continue
		}
		inScope := a.CircleID == nil || (circleID != uuid.Nil && *a.CircleID == circleID)
		if !f.scopeFilterEnabled || inScope {
			out = append(out, *a)
		}
	}
	return out, nil
}

// ExistsByLinkedUserID 防回环口径：linked_user_id 命中任意未删除机器人即 true。
func (f *fakeAgentRepo) ExistsByLinkedUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	for _, a := range f.byID {
		if a.LinkedUserID == userID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeAgentRepo) ListByCircle(ctx context.Context, circleID uuid.UUID, keyword string, offset, limit int) ([]domain.AiAgent, int64, error) {
	var out []domain.AiAgent
	for _, a := range f.byID {
		if a.CircleID != nil && *a.CircleID == circleID {
			out = append(out, *a)
		}
	}
	return out, int64(len(out)), nil
}

// fakeCircleRoles 圈内角色 fake：circleID → userID → {role, status}。
type fakeCircleRoles struct {
	members map[uuid.UUID]map[uuid.UUID][2]int16
}

func newFakeCircleRoles() *fakeCircleRoles {
	r := &fakeCircleRoles{members: map[uuid.UUID]map[uuid.UUID][2]int16{}}
	for _, cid := range []uuid.UUID{circleAID, circleBID} {
		r.members[cid] = map[uuid.UUID][2]int16{
			ownerID:    {30, 1}, // owner, normal
			adminID:    {20, 1}, // admin, normal
			memberID:   {10, 1}, // member, normal
			mutedOwner: {30, 2}, // owner, muted（管理权暂停）
		}
	}
	return r
}

func (f *fakeCircleRoles) GetCircleMembership(ctx context.Context, circleID, userID uuid.UUID) (int16, int16, bool, error) {
	users := f.members[circleID]
	if users == nil {
		return 0, 0, false, nil
	}
	m, ok := users[userID]
	if !ok {
		return 0, 0, false, nil
	}
	return m[0], m[1], true, nil
}

// fakeBotUserCreator 记录调用与圈子绑定参数的机器人账号创建 fake。
type fakeBotUserCreator struct {
	created   int
	gotCircle *uuid.UUID // 最后一次 CreateBotUser 收到的 circleID（nil=全局链路）
}

func (f *fakeBotUserCreator) CreateBotUser(ctx context.Context, username, email, avatarURL string, circleID *uuid.UUID) (uuid.UUID, error) {
	f.created++
	f.gotCircle = circleID
	return uuid.New(), nil
}

// fakeBotUserScopeCleaner 记录清列调用的机器人圈子绑定清理 fake。
type fakeBotUserScopeCleaner struct {
	cleared  []uuid.UUID
	clearErr error // 非 nil 时模拟清列失败（fail-open 断言用）
}

func (f *fakeBotUserScopeCleaner) ClearBotCircleScope(ctx context.Context, userID uuid.UUID) error {
	f.cleared = append(f.cleared, userID)
	return f.clearErr
}

// fakeUserRoleReader 全局 role 读取 fake（全局链守卫测试用，模拟平台超管 role=1）。
type fakeUserRoleReader struct {
	role int
	ok   bool
}

func (f *fakeUserRoleReader) GetUserRole(ctx context.Context, userID uuid.UUID) (int, bool, error) {
	return f.role, f.ok, nil
}

// ---- 构造助手 ----

func newCircleAgentSvc(repo *fakeAgentRepo, roles *fakeCircleRoles) CircleAgentService {
	svc := NewCircleAgentService(repo)
	svc.SetCircleRoleReader(roles)
	creator := &fakeBotUserCreator{}
	svc.SetBotUserCreator(creator)
	return svc
}

// newGlobalAgentSvc 构造全局 AgentService（role=1 超管），用于跨作用域守卫测试。
func newGlobalAgentSvc(repo *fakeAgentRepo) AgentService {
	svc := NewAgentService(repo)
	svc.SetRoleReader(&fakeUserRoleReader{role: 1, ok: true})
	return svc
}

// seedCircleAgent 在 repo 中放入一个圈内机器人。
func seedCircleAgent(repo *fakeAgentRepo) *domain.AiAgent {
	a := &domain.AiAgent{
		ID: circleAgentID, Name: "圈助理", CircleID: &circleAID,
		APIProtocol: "openai", Model: "test-model", Status: domain.AgentStatusEnabled,
	}
	repo.seed(a)
	return a
}

// seedGlobalAgent 在 repo 中放入一个平台全局机器人（CircleID=nil）。
func seedGlobalAgent(repo *fakeAgentRepo) *domain.AiAgent {
	a := &domain.AiAgent{
		ID: globalAgentID, Name: "全局助理", CircleID: nil,
		APIProtocol: "openai", Model: "test-model", Status: domain.AgentStatusEnabled,
	}
	repo.seed(a)
	return a
}

func createInput() CreateAgentInput {
	return CreateAgentInput{
		Name: "小助手", APIProtocol: "openai", Model: "test-model",
	}
}

// ---- 权限矩阵 ----

// TestCircleAgent_PermissionMatrix owner/admin 可读可写；member/非成员/被禁言 owner 一律 403。
// 管理权暂停语义：mutedOwner 有 owner 角色但 status≠normal → errNotCircleAdmin。
func TestCircleAgent_PermissionMatrix(t *testing.T) {
	cases := []struct {
		name     string
		operator uuid.UUID
		wantErr  error
	}{
		{"owner allowed", ownerID, nil},
		{"admin allowed", adminID, nil},
		{"member denied", memberID, errNotCircleAdmin},
		{"non-member denied", outsiderID, errNotCircleAdmin},
		{"muted owner denied", mutedOwner, errNotCircleAdmin},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/list", func(t *testing.T) {
			repo := newFakeAgentRepo()
			svc := newCircleAgentSvc(repo, newFakeCircleRoles())
			_, err := svc.ListCircleAgents(context.Background(), tc.operator, circleAID, "", 1, 20)
			assertErr(t, err, tc.wantErr)
		})
		t.Run(tc.name+"/get", func(t *testing.T) {
			repo := newFakeAgentRepo()
			seedCircleAgent(repo)
			svc := newCircleAgentSvc(repo, newFakeCircleRoles())
			_, err := svc.GetCircleAgent(context.Background(), tc.operator, circleAgentID)
			assertErr(t, err, tc.wantErr)
		})
		t.Run(tc.name+"/create", func(t *testing.T) {
			repo := newFakeAgentRepo()
			svc := newCircleAgentSvc(repo, newFakeCircleRoles())
			_, err := svc.CreateCircleAgent(context.Background(), tc.operator, circleAID, createInput())
			assertErr(t, err, tc.wantErr)
		})
		t.Run(tc.name+"/update", func(t *testing.T) {
			repo := newFakeAgentRepo()
			seedCircleAgent(repo)
			svc := newCircleAgentSvc(repo, newFakeCircleRoles())
			name := "新名字"
			_, err := svc.UpdateCircleAgent(context.Background(), tc.operator, circleAgentID,
				UpdateAgentInput{Name: &name})
			assertErr(t, err, tc.wantErr)
		})
	}
}

// TestCircleAgent_DeleteOwnerOnly 删除是破坏性操作，仅圈主：admin → 403，owner → 成功。
func TestCircleAgent_DeleteOwnerOnly(t *testing.T) {
	repo := newFakeAgentRepo()
	seedCircleAgent(repo)
	svc := newCircleAgentSvc(repo, newFakeCircleRoles())

	if err := svc.DeleteCircleAgent(context.Background(), adminID, circleAgentID); !errors.Is(err, errNotCircleOwner) {
		t.Fatalf("admin delete err = %v, want errNotCircleOwner", err)
	}
	if err := svc.DeleteCircleAgent(context.Background(), ownerID, circleAgentID); err != nil {
		t.Fatalf("owner delete failed: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != circleAgentID {
		t.Fatalf("SoftDelete not called on %s: %v", circleAgentID, repo.deleted)
	}
}

// TestCircleAgentUpdate_CredentialFieldsOwnerOnly 字段分级：凭据字段（api_protocol/
// base_url/api_key）仅圈主可改；运营字段 admin 可改。
func TestCircleAgentUpdate_CredentialFieldsOwnerOnly(t *testing.T) {
	cases := []struct {
		name    string
		input   UpdateAgentInput
		wantErr error
	}{
		{"admin+api_key", UpdateAgentInput{APIKey: strPtr("sk-new")}, errNotCircleOwner},
		{"admin+base_url", UpdateAgentInput{BaseURL: strPtr("https://x.test")}, errNotCircleOwner},
		{"admin+api_protocol", UpdateAgentInput{APIProtocol: strPtr("anthropic")}, errNotCircleOwner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeAgentRepo()
			seedCircleAgent(repo)
			svc := newCircleAgentSvc(repo, newFakeCircleRoles())
			_, err := svc.UpdateCircleAgent(context.Background(), adminID, circleAgentID, tc.input)
			assertErr(t, err, tc.wantErr)
		})
	}

	t.Run("owner+api_key allowed", func(t *testing.T) {
		repo := newFakeAgentRepo()
		seedCircleAgent(repo)
		svc := newCircleAgentSvc(repo, newFakeCircleRoles())
		vo, err := svc.UpdateCircleAgent(context.Background(), ownerID, circleAgentID,
			UpdateAgentInput{APIKey: strPtr("sk-new")})
		if err != nil {
			t.Fatalf("owner update api_key failed: %v", err)
		}
		if !vo.HasAPIKey {
			t.Fatal("vo.HasAPIKey = false, want true")
		}
	})
	t.Run("admin+name allowed", func(t *testing.T) {
		repo := newFakeAgentRepo()
		seedCircleAgent(repo)
		svc := newCircleAgentSvc(repo, newFakeCircleRoles())
		_, err := svc.UpdateCircleAgent(context.Background(), adminID, circleAgentID,
			UpdateAgentInput{Name: strPtr("改名后")})
		if err != nil {
			t.Fatalf("admin update name failed: %v", err)
		}
		if repo.updated[circleAgentID]["name"] != "改名后" {
			t.Fatalf("fields[name] = %v, want 改名后", repo.updated[circleAgentID]["name"])
		}
	})
}

// TestCircleAgentUpdate_MixedCredentialByAdmin 管理字段与凭据字段混提：任一凭据字段出现
// 即整体要求圈主（admin 即使只改 name 也一起被拒）。
func TestCircleAgentUpdate_MixedCredentialByAdmin(t *testing.T) {
	repo := newFakeAgentRepo()
	seedCircleAgent(repo)
	svc := newCircleAgentSvc(repo, newFakeCircleRoles())

	_, err := svc.UpdateCircleAgent(context.Background(), adminID, circleAgentID,
		UpdateAgentInput{Name: strPtr("新名"), BaseURL: strPtr("https://x.test")})
	if !errors.Is(err, errNotCircleOwner) {
		t.Fatalf("mixed update by admin err = %v, want errNotCircleOwner", err)
	}
}

// ---- 限额 ----

// TestCircleAgentCreate_QuotaExceeded 仓储返回 ErrCircleAgentLimit → 应用层 errCircleAgentLimit
// （handler 映射 409）；且传给仓储的上限是 domain.MaxAgentsPerCircle。
func TestCircleAgentCreate_QuotaExceeded(t *testing.T) {
	repo := newFakeAgentRepo()
	repo.createErr = domain.ErrCircleAgentLimit
	svc := newCircleAgentSvc(repo, newFakeCircleRoles())

	_, err := svc.CreateCircleAgent(context.Background(), ownerID, circleAID, createInput())
	if !errors.Is(err, errCircleAgentLimit) {
		t.Fatalf("err = %v, want errCircleAgentLimit", err)
	}
	if repo.gotMax != domain.MaxAgentsPerCircle {
		t.Fatalf("maxPerCircle passed to repo = %d, want %d", repo.gotMax, domain.MaxAgentsPerCircle)
	}
}

// ---- 名称作用域 ----

// TestCircleAgentCreate_NameScope 同圈同名 409；跨圈同名可建；全局+圈内同名可建（作用域隔离）。
func TestCircleAgentCreate_NameScope(t *testing.T) {
	t.Run("same circle same name conflict", func(t *testing.T) {
		repo := newFakeAgentRepo()
		seedCircleAgent(repo) // 圈 A 已有「圈助理」
		svc := newCircleAgentSvc(repo, newFakeCircleRoles())

		input := createInput()
		input.Name = "圈助理"
		_, err := svc.CreateCircleAgent(context.Background(), ownerID, circleAID, input)
		if !errors.Is(err, errAgentNameExists) {
			t.Fatalf("err = %v, want errAgentNameExists", err)
		}
	})
	t.Run("other circle same name ok", func(t *testing.T) {
		repo := newFakeAgentRepo()
		seedCircleAgent(repo) // 圈 A 有「圈助理」
		svc := newCircleAgentSvc(repo, newFakeCircleRoles())

		input := createInput()
		input.Name = "圈助理"
		vo, err := svc.CreateCircleAgent(context.Background(), ownerID, circleBID, input)
		if err != nil {
			t.Fatalf("create same name in other circle failed: %v", err)
		}
		if vo.CircleID == nil || *vo.CircleID != circleBID {
			t.Fatalf("vo.CircleID = %v, want circleBID", vo.CircleID)
		}
	})
	t.Run("global same name ok", func(t *testing.T) {
		repo := newFakeAgentRepo()
		repo.nameTaken[uuid.Nil] = map[string]bool{"小助手": true} // 全局桶已占
		svc := newCircleAgentSvc(repo, newFakeCircleRoles())

		_, err := svc.CreateCircleAgent(context.Background(), ownerID, circleAID, createInput())
		if err != nil {
			t.Fatalf("create same name as global agent failed: %v", err)
		}
	})
}

// TestCircleAgentCreate_RenamesBotUser 创建成功时同步创建机器人系统用户（role=2）。
func TestCircleAgentCreate_RenamesBotUser(t *testing.T) {
	repo := newFakeAgentRepo()
	svc := NewCircleAgentService(repo)
	svc.SetCircleRoleReader(newFakeCircleRoles())
	creator := &fakeBotUserCreator{}
	svc.SetBotUserCreator(creator)

	vo, err := svc.CreateCircleAgent(context.Background(), ownerID, circleAID, createInput())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if creator.created != 1 {
		t.Fatalf("CreateBotUser called %d times, want 1", creator.created)
	}
	if vo.CircleID == nil || *vo.CircleID != circleAID {
		t.Fatalf("vo.CircleID = %v, want circleAID（圈内机器人必须回显 circle_id）", vo.CircleID)
	}
	stored := repo.byID[vo.ID]
	if stored.CreatorID != ownerID {
		t.Fatalf("CreatorID = %s, want operator %s（审计字段必写）", stored.CreatorID, ownerID)
	}
	if stored.LinkedUserID == uuid.Nil {
		t.Fatal("LinkedUserID not set")
	}
}

// ---- 跨作用域 404 ----

// TestCircleAgent_CrossScopeAndUnknown404 圈内链：未知 ID / 全局机器人 ID → 404；
// 且 404 先于 403（member 查不存在的机器人报 404 而非 403，不泄露归属）。
func TestCircleAgent_CrossScopeAndUnknown404(t *testing.T) {
	repo := newFakeAgentRepo()
	seedGlobalAgent(repo)
	svc := newCircleAgentSvc(repo, newFakeCircleRoles())

	for _, id := range []uuid.UUID{globalAgentID, uuid.MustParse("0192a0d0-0000-7000-8000-0000000000ff")} {
		if _, err := svc.GetCircleAgent(context.Background(), ownerID, id); !errors.Is(err, errAgentNotFound) {
			t.Fatalf("GetCircleAgent(%s) err = %v, want errAgentNotFound", id, err)
		}
	}
	// member（无权限）访问未知机器人：先 404。
	if _, err := svc.GetCircleAgent(context.Background(), memberID, globalAgentID); !errors.Is(err, errAgentNotFound) {
		t.Fatalf("member cross-scope err = %v, want errAgentNotFound（404 先于 403）", err)
	}
}

// TestGlobalAgent_CrossScopeCircleAgent404 全局链：圈内机器人 ID 查询/更新/删除 → 404
// （平台超管对圈内机器人无读写特权，也不暴露其存在性）。
func TestGlobalAgent_CrossScopeCircleAgent404(t *testing.T) {
	repo := newFakeAgentRepo()
	seedCircleAgent(repo)
	seedGlobalAgent(repo)
	svc := newGlobalAgentSvc(repo)
	ctx := context.Background()

	if _, err := svc.GetAgent(ctx, ownerID, circleAgentID); !errors.Is(err, errAgentNotFound) {
		t.Fatalf("GetAgent(circle agent) err = %v, want errAgentNotFound", err)
	}
	name := "x"
	if _, err := svc.UpdateAgent(ctx, ownerID, circleAgentID, UpdateAgentInput{Name: &name}); !errors.Is(err, errAgentNotFound) {
		t.Fatalf("UpdateAgent(circle agent) err = %v, want errAgentNotFound", err)
	}
	if err := svc.DeleteAgent(ctx, ownerID, circleAgentID); !errors.Is(err, errAgentNotFound) {
		t.Fatalf("DeleteAgent(circle agent) err = %v, want errAgentNotFound", err)
	}
	// 全局自己的机器人不受影响。
	if _, err := svc.GetAgent(ctx, ownerID, globalAgentID); err != nil {
		t.Fatalf("GetAgent(global agent) failed: %v", err)
	}
}

// ---- fail-closed ----

// TestCircleAgent_FailClosed_CircleRoleReaderNotInjected CircleRoleReader 未注入一律拒绝。
func TestCircleAgent_FailClosed_CircleRoleReaderNotInjected(t *testing.T) {
	repo := newFakeAgentRepo()
	seedCircleAgent(repo)
	svc := NewCircleAgentService(repo)
	svc.SetBotUserCreator(&fakeBotUserCreator{})
	ctx := context.Background()

	if _, err := svc.ListCircleAgents(ctx, ownerID, circleAID, "", 1, 20); !errors.Is(err, errNotCircleAdmin) {
		t.Fatalf("list err = %v, want errNotCircleAdmin", err)
	}
	if _, err := svc.GetCircleAgent(ctx, ownerID, circleAgentID); !errors.Is(err, errNotCircleAdmin) {
		t.Fatalf("get err = %v, want errNotCircleAdmin", err)
	}
	if _, err := svc.CreateCircleAgent(ctx, ownerID, circleAID, createInput()); !errors.Is(err, errNotCircleAdmin) {
		t.Fatalf("create err = %v, want errNotCircleAdmin", err)
	}
}

// ---- ManualReply 404 对齐 ----

// TestManualReply_CircleAgentNotFound 圈内机器人走全局手动入口 → errAgentNotFound
// （跨作用域不可见，404 不泄露存在性；原 errCircleReplyUnsupported 403 已废）；
// 全局机器人不受影响（走到后续校验：非手动模式 → errNotManualMode）。
func TestManualReply_CircleAgentNotFound(t *testing.T) {
	repo := newFakeAgentRepo()
	seedCircleAgent(repo)
	seedGlobalAgent(repo)
	// 全局机器人改为非手动模式：验证守卫之后的既有校验仍生效。
	repo.byID[globalAgentID].TriggerMode = domain.TriggerModeKeyword

	replySvc := NewReplyService(repo, nil, nil)
	replySvc.SetRoleReader(&fakeUserRoleReader{role: 1, ok: true})
	ctx := context.Background()

	_, err := replySvc.ManualReply(ctx, ownerID, circleAgentID, uuid.New())
	if !errors.Is(err, errAgentNotFound) {
		t.Fatalf("ManualReply(circle agent) err = %v, want errAgentNotFound", err)
	}
	// 全局机器人走到后续校验（非手动模式 → errNotManualMode），说明守卫未误伤全局链。
	_, err = replySvc.ManualReply(ctx, ownerID, globalAgentID, uuid.New())
	if !errors.Is(err, errNotManualMode) {
		t.Fatalf("ManualReply(global agent) err = %v, want errNotManualMode", err)
	}
}

// ---- 助手 ----

func strPtr(s string) *string { return &s }

func assertErr(t *testing.T, got, want error) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("err = %v, want nil", got)
		}
		return
	}
	if !errors.Is(got, want) {
		t.Fatalf("err = %v, want %v", got, want)
	}
}
