//go:build integration

// agent_mention_visibility_integration_test.go 机器人 @提及 可见性端到端集成测试（#171，make test-integration）：
// 真实 aiagent Service + composition 桥接器 + user Service（PG / Redis 缓存）——
// 创建 / 停用 / 启用 / 删除机器人时 users.agent_mention_disabled 跟随 ai_agent 状态，users.status 恒为 1，
// 历史作者信息（GetBriefs）照常返回；删除圈内机器人保留 agent_circle_id；
// 发帖 @提及 经真实 user facade 剔除停用 / 已删除 / 越圈机器人：不落 post_mention、不触发机器人。
package composition

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"

	agentapp "interestBar/pkg/domains/aiagent/application"
	agentinfra "interestBar/pkg/domains/aiagent/infrastructure"
	postapp "interestBar/pkg/domains/post/application"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	userapp "interestBar/pkg/domains/user/application"
	userinfra "interestBar/pkg/domains/user/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// adminRoleReader 任何人都是平台超管（全局机器人链路鉴权桩）。
type adminRoleReader struct{}

func (adminRoleReader) GetUserRole(context.Context, uuid.UUID) (int, bool, error) {
	return 1, true, nil
}

// ownerCircleRoles 任何人都是任意圈的正常状态圈主（圈内机器人链路鉴权桩）。
type ownerCircleRoles struct{}

func (ownerCircleRoles) GetCircleMembership(context.Context, uuid.UUID, uuid.UUID) (int16, int16, bool, error) {
	return 30, 1, true, nil
}

// mentionTriggerRecorder 记录发帖 @提及 触发机器人时的最终名单（按帖子）。
type mentionTriggerRecorder struct {
	mu  sync.Mutex
	ids map[uuid.UUID][]uuid.UUID
}

func (r *mentionTriggerRecorder) OnPostMentioned(postID, _, _ uuid.UUID, mentionUserIDs []uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids[postID] = append([]uuid.UUID(nil), mentionUserIDs...)
}

func (r *mentionTriggerRecorder) get(postID uuid.UUID) []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ids[postID]
}

type mentionVisibilityEnv struct {
	db        *gorm.DB
	userSvc   userapp.UserService
	agentSvc  agentapp.AgentService
	circleSvc agentapp.CircleAgentService
}

// setupMentionVisibility 用真实仓储与 server.go 同款桥接器装配 aiagent / user Service。
func setupMentionVisibility(t *testing.T) *mentionVisibilityEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redis(t)

	userSvc := userapp.NewUserService(userinfra.NewUserRepository(db), userinfra.NewUserCache(), userinfra.NewUserSearcher())
	agentRepo := agentinfra.NewAgentRepository(db)

	agentSvc := agentapp.NewAgentService(agentRepo)
	agentSvc.SetRoleReader(adminRoleReader{})
	agentSvc.SetBotUserCreator(&agentBotUserCreator{db: db})
	agentSvc.SetBotUserMentionSyncer(&agentBotUserMentionSyncer{delegate: userSvc})

	circleSvc := agentapp.NewCircleAgentService(agentRepo)
	circleSvc.SetCircleRoleReader(ownerCircleRoles{})
	circleSvc.SetBotUserCreator(&agentBotUserCreator{db: db})
	circleSvc.SetBotUserMentionSyncer(&agentBotUserMentionSyncer{delegate: userSvc})

	return &mentionVisibilityEnv{db: db, userSvc: userSvc, agentSvc: agentSvc, circleSvc: circleSvc}
}

// uniqueName 机器人 / 圈子名称（ai_agent 名称在作用域桶内唯一，varchar(50)）。
func uniqueName(kind string) string {
	return "it171-" + kind + "-" + strings.ReplaceAll(sharedomain.NewID().String(), "-", "")[20:]
}

// registerAgentCleanup 删除测试机器人及其关联系统用户。
func registerAgentCleanup(t *testing.T, vo *agentapp.AgentVO) {
	t.Helper()
	testkit.CleanupRows(t, "domains.ai_agent", "id", vo.ID)
	testkit.CleanupRows(t, "domains.users", "id", vo.LinkedUserID)
	testkit.CleanupKeys(t, redispkg.GetUserInfoKey(vo.LinkedUserID))
}

func agentInput(kind string) agentapp.CreateAgentInput {
	return agentapp.CreateAgentInput{Name: uniqueName(kind), APIProtocol: "openai", Model: "it-model"}
}

func intPtr(v int) *int { return &v }

// botUserRow 机器人关联系统用户的可见性相关列。
type botUserRow struct {
	Status               int
	Deleted              int
	AgentCircleID        *uuid.UUID
	AgentMentionDisabled int16
}

func loadBotUser(t *testing.T, db *gorm.DB, userID uuid.UUID) botUserRow {
	t.Helper()
	var row botUserRow
	if err := db.Raw(`SELECT status, deleted, agent_circle_id, agent_mention_disabled FROM domains.users WHERE id = ?`, userID).
		Scan(&row).Error; err != nil {
		t.Fatalf("load bot user %s: %v", userID, err)
	}
	return row
}

// assertBotUser 断言可见性投影；status / deleted 必须恒为正常（历史作者信息依赖它）。
func assertBotUser(t *testing.T, env *mentionVisibilityEnv, userID uuid.UUID, step string, wantDisabled int16, wantCircle *uuid.UUID) {
	t.Helper()
	row := loadBotUser(t, env.db, userID)
	if row.AgentMentionDisabled != wantDisabled {
		t.Fatalf("%s: agent_mention_disabled = %d, want %d", step, row.AgentMentionDisabled, wantDisabled)
	}
	if row.Status != 1 || row.Deleted != 0 {
		t.Fatalf("%s: users.status/deleted = %d/%d, want 1/0（改 status 会让历史评论丢失作者信息）", step, row.Status, row.Deleted)
	}
	switch {
	case wantCircle == nil && row.AgentCircleID != nil:
		t.Fatalf("%s: agent_circle_id = %s, want NULL", step, row.AgentCircleID)
	case wantCircle != nil && (row.AgentCircleID == nil || *row.AgentCircleID != *wantCircle):
		t.Fatalf("%s: agent_circle_id = %v, want %s", step, row.AgentCircleID, wantCircle)
	}
	// 回归：GetBriefs 跳过非正常用户；机器人停用 / 删除后作者信息仍要能取到。
	briefs, err := userapp.NewUserFacade(env.userSvc).GetBriefs(context.Background(), []string{userID.String()})
	if err != nil {
		t.Fatalf("%s: GetBriefs: %v", step, err)
	}
	if _, ok := briefs[userID.String()]; !ok {
		t.Fatalf("%s: GetBriefs dropped bot user %s（机器人历史评论会丢失作者信息）", step, userID)
	}
}

// insertCircle 插入一个测试圈子（圈内机器人创建需要行锁圈子行）并注册清理。
func insertCircle(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if err := db.Exec(`INSERT INTO domains.circle (id, name, description, rule, creator_id) VALUES (?, ?, '', '', ?)`,
		id, uniqueName("circle"), sharedomain.NewID()).Error; err != nil {
		t.Fatalf("insert circle: %v", err)
	}
	testkit.CleanupRows(t, "domains.circle", "id", id)
	return id
}

// insertNormalUser 插入一个普通用户并注册清理。
func insertNormalUser(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := sharedomain.NewID()
	if err := db.Exec(`INSERT INTO domains.users (id, username, email) VALUES (?, 'it171 normal', ?)`,
		id, id.String()+"@it.qubar.local").Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	testkit.CleanupRows(t, "domains.users", "id", id)
	testkit.CleanupKeys(t, redispkg.GetUserInfoKey(id))
	return id
}

// 全局机器人：创建可 @ → 停用不可 @ → 启用恢复 → 删除不可 @；全程 status=1、作者信息可取。
func TestAgentMentionVisibility_GlobalLifecycle(t *testing.T) {
	t.Parallel()
	env := setupMentionVisibility(t)
	ctx := context.Background()
	admin := sharedomain.NewID()

	vo, err := env.agentSvc.CreateAgent(ctx, admin, agentInput("global"))
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	registerAgentCleanup(t, vo)
	assertBotUser(t, env, vo.LinkedUserID, "created", 0, nil)

	if _, err := env.agentSvc.UpdateAgent(ctx, admin, vo.ID, agentapp.UpdateAgentInput{Status: intPtr(0)}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	assertBotUser(t, env, vo.LinkedUserID, "disabled", 1, nil)

	if _, err := env.agentSvc.UpdateAgent(ctx, admin, vo.ID, agentapp.UpdateAgentInput{Status: intPtr(1)}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	assertBotUser(t, env, vo.LinkedUserID, "re-enabled", 0, nil)

	if err := env.agentSvc.DeleteAgent(ctx, admin, vo.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	assertBotUser(t, env, vo.LinkedUserID, "deleted", 1, nil)
}

// 创建即停用：建号时就写不可 @（不依赖后续投影）。
func TestAgentMentionVisibility_CreateDisabled(t *testing.T) {
	t.Parallel()
	env := setupMentionVisibility(t)
	in := agentInput("created-disabled")
	in.Status = intPtr(0)

	vo, err := env.agentSvc.CreateAgent(context.Background(), sharedomain.NewID(), in)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	registerAgentCleanup(t, vo)
	assertBotUser(t, env, vo.LinkedUserID, "created disabled", 1, nil)
}

// 圈内机器人：停用 / 启用跟随；删除后不可 @ 且保留 agent_circle_id（不会变成全站可见）。
func TestAgentMentionVisibility_CircleLifecycle(t *testing.T) {
	t.Parallel()
	env := setupMentionVisibility(t)
	ctx := context.Background()
	owner := sharedomain.NewID()
	circleID := insertCircle(t, env.db)

	vo, err := env.circleSvc.CreateCircleAgent(ctx, owner, circleID, agentInput("circle"))
	if err != nil {
		t.Fatalf("CreateCircleAgent: %v", err)
	}
	registerAgentCleanup(t, vo)
	assertBotUser(t, env, vo.LinkedUserID, "created", 0, &circleID)

	if _, err := env.circleSvc.UpdateCircleAgent(ctx, owner, vo.ID, agentapp.UpdateAgentInput{Status: intPtr(0)}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	assertBotUser(t, env, vo.LinkedUserID, "disabled", 1, &circleID)

	if _, err := env.circleSvc.UpdateCircleAgent(ctx, owner, vo.ID, agentapp.UpdateAgentInput{Status: intPtr(1)}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	assertBotUser(t, env, vo.LinkedUserID, "re-enabled", 0, &circleID)

	if err := env.circleSvc.DeleteCircleAgent(ctx, owner, vo.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	assertBotUser(t, env, vo.LinkedUserID, "deleted", 1, &circleID)
}

// 发帖 @提及：停用 / 已删除的全局与圈内机器人、越圈机器人被剔除，不落 post_mention、不触发机器人；
// 普通用户、启用的全局机器人、本圈启用机器人保留。走真实 user facade（PG 投影列）。
func TestAgentMentionVisibility_PostMentionStripping(t *testing.T) {
	t.Parallel()
	env := setupMentionVisibility(t)
	ctx := context.Background()
	operator := sharedomain.NewID()
	circleA, circleB := insertCircle(t, env.db), insertCircle(t, env.db)

	create := func(kind string, circleID uuid.UUID) *agentapp.AgentVO {
		t.Helper()
		var (
			vo  *agentapp.AgentVO
			err error
		)
		if circleID == uuid.Nil {
			vo, err = env.agentSvc.CreateAgent(ctx, operator, agentInput(kind))
		} else {
			vo, err = env.circleSvc.CreateCircleAgent(ctx, operator, circleID, agentInput(kind))
		}
		if err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
		registerAgentCleanup(t, vo)
		return vo
	}
	normal := insertNormalUser(t, env.db)
	globalBot := create("global", uuid.Nil)
	disabledGlobal := create("global-disabled", uuid.Nil)
	deletedGlobal := create("global-deleted", uuid.Nil)
	ownBot := create("own", circleA)
	disabledOwn := create("own-disabled", circleA)
	deletedOwn := create("own-deleted", circleA)
	otherBot := create("other", circleB)

	if _, err := env.agentSvc.UpdateAgent(ctx, operator, disabledGlobal.ID, agentapp.UpdateAgentInput{Status: intPtr(0)}); err != nil {
		t.Fatalf("disable global: %v", err)
	}
	if err := env.agentSvc.DeleteAgent(ctx, operator, deletedGlobal.ID); err != nil {
		t.Fatalf("delete global: %v", err)
	}
	if _, err := env.circleSvc.UpdateCircleAgent(ctx, operator, disabledOwn.ID, agentapp.UpdateAgentInput{Status: intPtr(0)}); err != nil {
		t.Fatalf("disable own: %v", err)
	}
	if err := env.circleSvc.DeleteCircleAgent(ctx, operator, deletedOwn.ID); err != nil {
		t.Fatalf("delete own: %v", err)
	}

	trigger := &mentionTriggerRecorder{ids: map[uuid.UUID][]uuid.UUID{}}
	postSvc := postapp.NewPostService(postinfra.NewPostRepository(env.db), nil, nil, nil, nil, nil)
	postSvc.SetUserFacade(&postUserFacade{delegate: userapp.NewUserFacade(env.userSvc)})
	postSvc.SetAgentTrigger(trigger)

	postID, err := postSvc.CreatePost(ctx, sharedomain.NewID(), postapp.CreatePostInput{
		CircleID: circleA,
		Title:    uniqueName("post"),
		Content:  "机器人 @提及 可见性集成测试正文",
		MentionUserIDs: []uuid.UUID{
			normal, globalBot.LinkedUserID, disabledGlobal.LinkedUserID, deletedGlobal.LinkedUserID,
			ownBot.LinkedUserID, disabledOwn.LinkedUserID, deletedOwn.LinkedUserID, otherBot.LinkedUserID,
		},
	})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", postID)
	testkit.CleanupRows(t, "domains.post_mention", "post_id", postID)

	want := sortedIDs([]uuid.UUID{normal, globalBot.LinkedUserID, ownBot.LinkedUserID})

	var stored []uuid.UUID
	if err := env.db.Raw(`SELECT user_id FROM domains.post_mention WHERE post_id = ?`, postID).Scan(&stored).Error; err != nil {
		t.Fatalf("query post_mention: %v", err)
	}
	if got := sortedIDs(stored); !equalIDs(got, want) {
		t.Fatalf("post_mention = %v, want %v", got, want)
	}
	if got := sortedIDs(trigger.get(postID)); !equalIDs(got, want) {
		t.Fatalf("agent trigger mentions = %v, want %v", got, want)
	}
}

func sortedIDs(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	sort.Strings(out)
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
