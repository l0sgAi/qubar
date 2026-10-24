//go:build integration

// post_moderation_integration_test.go 帖子 AI 审核 worker 端到端集成测试（#117，make test-integration）：
// CreatePost（moderation 启用）→ PostgreSQL status=2 → Redpanda post_moderation → 审核 worker → ContentModerator（桩）
// → 分流 → 条件更新 → post_moderation_log → 发布副作用。
//
// 进程内只起一个 worker（全局单例），桩审核器按帖子标题分派结果，各测试用随机圈子 / 标题互相隔离、可并行。
package composition

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"interestBar/pkg/conf"
	postapp "interestBar/pkg/domains/post/application"
	postdomain "interestBar/pkg/domains/post/domain"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	"interestBar/pkg/server/storage/redpanda"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const moderationWait = 45 * time.Second // 含消费者组首次 join / rebalance

// scriptedModerator 按帖子标题返回预设结果，并记录每个标题的调用次数。
type scriptedModerator struct {
	mu      sync.Mutex
	results map[string]scriptedResult
	calls   map[string]int
}

type scriptedResult struct {
	a   *postdomain.Assessment
	err error
}

func (m *scriptedModerator) set(title string, a *postdomain.Assessment, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[title] = scriptedResult{a: a, err: err}
}

func (m *scriptedModerator) Assess(_ context.Context, in postdomain.ModerationInput) (*postdomain.Assessment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls[in.Title]++
	r, ok := m.results[in.Title]
	if !ok {
		return nil, fmt.Errorf("%w: no scripted result for %q", postdomain.ErrModerationRejected, in.Title)
	}
	return r.a, r.err
}

func (m *scriptedModerator) callCount(title string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[title]
}

// sideEffects 记录发布副作用（圈子计数按圈子、机器人触发按帖子）。
type sideEffects struct {
	mu      sync.Mutex
	counts  map[uuid.UUID]int
	trigger map[uuid.UUID]int
}

func (s *sideEffects) IncrPostCount(_ context.Context, circleID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[circleID]++
	return nil
}

func (s *sideEffects) OnPostMentioned(postID, _, _ uuid.UUID, _ []uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trigger[postID]++
}

func (s *sideEffects) get(circleID, postID uuid.UUID) (count, trigger int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[circleID], s.trigger[postID]
}

// anyUserFacade 任何 ID 都视为存在的普通用户（让 @提及 落库，验证发布时按落库名单触发）。
type anyUserFacade struct{}

func (anyUserFacade) GetBriefs(_ context.Context, ids []string) (map[string]postapp.UserBrief, error) {
	out := make(map[string]postapp.UserBrief, len(ids))
	for _, id := range ids {
		out[id] = postapp.UserBrief{ID: id, Username: "u"}
	}
	return out, nil
}

func (anyUserFacade) GetBrief(_ context.Context, id string) (*postapp.UserBrief, error) {
	return &postapp.UserBrief{ID: id, Username: "u"}, nil
}

func (anyUserFacade) GetAgentCircleIDs(context.Context, []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	return map[uuid.UUID]uuid.UUID{}, nil
}

type moderationEnv struct {
	db  *gorm.DB
	mod *scriptedModerator
	fx  *sideEffects
}

var (
	moderationEnvOnce sync.Once
	sharedModEnv      *moderationEnv
	sharedModSvc      postapp.PostService
)

func activeModeration(failOpen bool) conf.Moderation {
	return conf.Moderation{Enabled: true, APIKey: "integration-test-placeholder", FailOpen: &failOpen}
}

// newModerationService 用真实仓储 / 审核日志 / Redpanda 发布器装配 PostService，审核器与副作用端口用桩。
func newModerationService(db *gorm.DB, mod postdomain.ContentModerator, fx *sideEffects, failOpen bool) postapp.PostService {
	svc := postapp.NewPostService(postinfra.NewPostRepository(db), nil, nil, nil, nil, postinfra.NewPostEventPublisher())
	svc.SetUserFacade(anyUserFacade{})
	svc.SetPostCountPort(fx)
	svc.SetAgentTrigger(fx)
	svc.SetModeration(mod, postinfra.NewPostModerationLogRepository(db), activeModeration(failOpen))
	return svc
}

// setupModeration 启动进程内唯一的审核 worker（fail_open=true 的服务）。
func setupModeration(t *testing.T) *moderationEnv {
	t.Helper()
	db := testkit.PG(t)
	testkit.Redpanda(t)
	moderationEnvOnce.Do(func() {
		sharedModEnv = &moderationEnv{
			db:  db,
			mod: &scriptedModerator{results: map[string]scriptedResult{}, calls: map[string]int{}},
			fx:  &sideEffects{counts: map[uuid.UUID]int{}, trigger: map[uuid.UUID]int{}},
		}
		sharedModSvc = newModerationService(db, sharedModEnv.mod, sharedModEnv.fx, true)
		redpanda.StartPostModerationConsumer(sharedModSvc.ModeratePost)
	})
	return sharedModEnv
}

// createModeratedPost 发一篇带 @提及 的帖子并注册清理；返回 postID 与圈子 ID。
func createModeratedPost(t *testing.T, svc postapp.PostService, title string) (postID, circleID uuid.UUID) {
	t.Helper()
	circleID = sharedomain.NewID()
	postID, err := svc.CreatePost(t.Context(), sharedomain.NewID(), postapp.CreatePostInput{
		CircleID:       circleID,
		Title:          title,
		Content:        "审核集成测试正文",
		MentionUserIDs: []uuid.UUID{sharedomain.NewID()},
	})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	testkit.CleanupRows(t, "domains.post", "id", postID)
	testkit.CleanupRows(t, "domains.post_mention", "post_id", postID)
	testkit.CleanupRows(t, "domains.post_moderation_log", "post_id", postID)
	return postID, circleID
}

func postStatus(t *testing.T, db *gorm.DB, postID uuid.UUID) int16 {
	t.Helper()
	var status int16
	if err := db.Raw("SELECT status FROM domains.post WHERE id = ?", postID).Scan(&status).Error; err != nil {
		t.Fatalf("query status: %v", err)
	}
	return status
}

type moderationLogRow struct {
	Decision    string
	Hazards     string
	Severity    *float64
	Model       string
	InputTokens int
	ErrorMsg    *string
}

func moderationLogs(t *testing.T, db *gorm.DB, postID uuid.UUID) []moderationLogRow {
	t.Helper()
	var rows []moderationLogRow
	if err := db.Raw(`SELECT decision, hazards::text AS hazards, severity, model, input_tokens, error_msg
		FROM domains.post_moderation_log WHERE post_id = ? ORDER BY id`, postID).Scan(&rows).Error; err != nil {
		t.Fatalf("query moderation log: %v", err)
	}
	return rows
}

func waitModerationLogs(t *testing.T, db *gorm.DB, postID uuid.UUID) []moderationLogRow {
	t.Helper()
	var rows []moderationLogRow
	testkit.Eventually(t, moderationWait, 100*time.Millisecond, "moderation log written", func() bool {
		rows = moderationLogs(t, db, postID)
		return len(rows) > 0
	})
	return rows
}

func assessment(risk postdomain.Hazard, p, severity, confidence float64) *postdomain.Assessment {
	risks := make(map[postdomain.Hazard]float64, len(postdomain.Hazards))
	for _, h := range postdomain.Hazards {
		risks[h] = 0.01
	}
	if risk != "" {
		risks[risk] = p
	}
	return &postdomain.Assessment{
		Risks: risks, Severity: severity, SeverityConfidence: confidence,
		Model: "jev-test", InputTokens: 321, Latency: 15 * time.Millisecond, Attempts: 1,
	}
}

func uniqueTitle(kind string) string {
	return "moderation-" + kind + "-" + sharedomain.NewID().String()
}

// 发布：落库为审核中 → worker 放行 → status=1，副作用（计数 + 机器人）各一次，审计行完整。
// 随后重复投递同一事件：不再调审核器、不重复副作用、不多写审计行。
func TestModerationWorker_PublishAndDuplicate(t *testing.T) {
	t.Parallel()
	env := setupModeration(t)
	title := uniqueTitle("publish")
	env.mod.set(title, assessment("", 0, 0.1, 0.9), nil)

	postID, circleID := createModeratedPost(t, sharedModSvc, title)
	rows := waitModerationLogs(t, env.db, postID)

	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusPublished {
		t.Fatalf("status = %d, want published", got)
	}
	r := rows[0]
	if len(rows) != 1 || r.Decision != string(postdomain.ModerationPublish) || r.Model != "jev-test" ||
		r.InputTokens != 321 || r.Severity == nil || r.ErrorMsg != nil || r.Hazards == "{}" {
		t.Fatalf("unexpected log rows: %+v", rows)
	}
	if count, trigger := env.fx.get(circleID, postID); count != 1 || trigger != 1 {
		t.Fatalf("side effects count=%d trigger=%d, want 1/1", count, trigger)
	}

	// 重复投递两次
	for range 2 {
		if err := redpanda.PublishPostModerationEvent(t.Context(), postID); err != nil {
			t.Fatalf("republish: %v", err)
		}
	}
	// 用一条新帖子作为「worker 已处理到这里」的栅栏：同分区按序消费，栅栏处理完说明重复事件已消费过。
	fence := uniqueTitle("fence")
	env.mod.set(fence, assessment("", 0, 0.1, 0.9), nil)
	fenceID, _ := createModeratedPost(t, sharedModSvc, fence)
	waitModerationLogs(t, env.db, fenceID)

	if n := env.mod.callCount(title); n != 1 {
		t.Fatalf("moderator called %d times for duplicated event, want 1", n)
	}
	if count, trigger := env.fx.get(circleID, postID); count != 1 || trigger != 1 {
		t.Fatalf("duplicate event re-ran side effects: count=%d trigger=%d", count, trigger)
	}
	if rows := moderationLogs(t, env.db, postID); len(rows) != 1 {
		t.Fatalf("log rows after duplicate = %d, want 1", len(rows))
	}
}

// 人工待审 + 默认 review_action=publish（#153）：worker 直接发布（副作用一次），审计 decision=review_publish；
// 重复投递时帖子已发布，不再调审核器、不重复副作用。用生产首条实测分数（harassment 0.79 / severity 1.08）。
func TestModerationWorker_ReviewPublishes(t *testing.T) {
	t.Parallel()
	env := setupModeration(t)
	title := uniqueTitle("review-publish")
	env.mod.set(title, assessment(postdomain.HazardHarassment, 0.79, 1.08, 0.9), nil)

	postID, circleID := createModeratedPost(t, sharedModSvc, title)
	rows := waitModerationLogs(t, env.db, postID)

	if rows[0].Decision != string(postdomain.ModerationReviewPublish) || rows[0].Model != "jev-test" || rows[0].Severity == nil {
		t.Fatalf("unexpected log: %+v", rows[0])
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusPublished {
		t.Fatalf("status = %d, want published", got)
	}

	if err := redpanda.PublishPostModerationEvent(t.Context(), postID); err != nil {
		t.Fatalf("republish: %v", err)
	}
	fence := uniqueTitle("fence")
	env.mod.set(fence, assessment("", 0, 0.1, 0.9), nil)
	fenceID, _ := createModeratedPost(t, sharedModSvc, fence)
	waitModerationLogs(t, env.db, fenceID)

	if n := env.mod.callCount(title); n != 1 {
		t.Fatalf("moderator called %d times, want 1", n)
	}
	if rows := moderationLogs(t, env.db, postID); len(rows) != 1 {
		t.Fatalf("log rows = %d, want 1", len(rows))
	}
	if count, trigger := env.fx.get(circleID, postID); count != 1 || trigger != 1 {
		t.Fatalf("side effects count=%d trigger=%d, want 1/1", count, trigger)
	}
}

// newDirectModerationService 不发审核事件的服务（帖子不进共享 worker），由测试直接调用 ModeratePost。
func newDirectModerationService(db *gorm.DB, mod postdomain.ContentModerator, fx *sideEffects, cfg conf.Moderation) postapp.PostService {
	svc := postapp.NewPostService(postinfra.NewPostRepository(db), nil, nil, nil, nil,
		noEventPublisher{postinfra.NewPostEventPublisher()})
	svc.SetUserFacade(anyUserFacade{})
	svc.SetPostCountPort(fx)
	svc.SetAgentTrigger(fx)
	svc.SetModeration(mod, postinfra.NewPostModerationLogRepository(db), cfg)
	return svc
}

// review_action=hold：与 #118 一致，人工待审留在审核中、无副作用；重复投递靠审计行识别，不再调审核器。
func TestModeration_ReviewActionHold(t *testing.T) {
	t.Parallel()
	env := setupModeration(t)
	mod := &scriptedModerator{results: map[string]scriptedResult{}, calls: map[string]int{}}
	fx := &sideEffects{counts: map[uuid.UUID]int{}, trigger: map[uuid.UUID]int{}}
	cfg := activeModeration(true)
	cfg.ReviewAction = conf.ModerationReviewActionHold
	svc := newDirectModerationService(env.db, mod, fx, cfg)
	title := uniqueTitle("review-hold")
	mod.set(title, assessment(postdomain.HazardHarassment, 0.79, 1.08, 0.9), nil)

	postID, circleID := createModeratedPost(t, svc, title)
	for range 2 {
		if err := svc.ModeratePost(t.Context(), postID); err != nil {
			t.Fatalf("ModeratePost: %v", err)
		}
	}

	rows := moderationLogs(t, env.db, postID)
	if len(rows) != 1 || rows[0].Decision != string(postdomain.ModerationReview) {
		t.Fatalf("want exactly one review log, got %+v", rows)
	}
	if n := mod.callCount(title); n != 1 {
		t.Fatalf("moderator called %d times, want 1", n)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusReviewing {
		t.Fatalf("status = %d, want reviewing", got)
	}
	if count, trigger := fx.get(circleID, postID); count != 0 || trigger != 0 {
		t.Fatalf("hold must not run side effects: count=%d trigger=%d", count, trigger)
	}
}

// 驳回：status=3，无副作用。
func TestModerationWorker_Reject(t *testing.T) {
	t.Parallel()
	env := setupModeration(t)
	title := uniqueTitle("reject")
	env.mod.set(title, assessment(postdomain.HazardIllegal, 0.92, 2.2, 0.8), nil)

	postID, circleID := createModeratedPost(t, sharedModSvc, title)
	rows := waitModerationLogs(t, env.db, postID)

	if rows[0].Decision != string(postdomain.ModerationReject) {
		t.Fatalf("decision = %s, want reject", rows[0].Decision)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusRejected {
		t.Fatalf("status = %d, want rejected", got)
	}
	if count, trigger := env.fx.get(circleID, postID); count != 0 || trigger != 0 {
		t.Fatalf("reject must not run side effects: count=%d trigger=%d", count, trigger)
	}
}

// Jev 不可用 + fail_open=true（共享 worker 用默认 review_action=publish，即生产配置）：发布并记 fail_open 审计（含错误、无分数）。
func TestModerationWorker_FailOpen(t *testing.T) {
	t.Parallel()
	env := setupModeration(t)
	title := uniqueTitle("failopen")
	env.mod.set(title, nil, fmt.Errorf("%w: status 529 (attempts: 3)", postdomain.ErrModerationUnavailable))

	postID, circleID := createModeratedPost(t, sharedModSvc, title)
	rows := waitModerationLogs(t, env.db, postID)

	r := rows[0]
	if r.Decision != string(postdomain.ModerationFailOpen) || r.ErrorMsg == nil || r.Severity != nil || r.Hazards != "{}" {
		t.Fatalf("unexpected fail_open log: %+v", r)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusPublished {
		t.Fatalf("status = %d, want published", got)
	}
	if count, trigger := env.fx.get(circleID, postID); count != 1 || trigger != 1 {
		t.Fatalf("side effects count=%d trigger=%d, want 1/1", count, trigger)
	}
}

// noEventPublisher 不发审核事件（帖子不进共享 worker），由测试直接调用 ModeratePost。
type noEventPublisher struct{ postdomain.PostEventPublisher }

func (noEventPublisher) PublishModeration(context.Context, uuid.UUID) error { return nil }

// Jev 鉴权失败 + fail_open=false：留在审核中转人工，记 review 审计（含错误）（直接调用 ModeratePost，真实 PG）。
func TestModerationWorker_FailClosed(t *testing.T) {
	t.Parallel()
	env := setupModeration(t)
	mod := &scriptedModerator{results: map[string]scriptedResult{}, calls: map[string]int{}}
	fx := &sideEffects{counts: map[uuid.UUID]int{}, trigger: map[uuid.UUID]int{}}
	svc := postapp.NewPostService(postinfra.NewPostRepository(env.db), nil, nil, nil, nil,
		noEventPublisher{postinfra.NewPostEventPublisher()})
	svc.SetUserFacade(anyUserFacade{})
	svc.SetPostCountPort(fx)
	svc.SetAgentTrigger(fx)
	svc.SetModeration(mod, postinfra.NewPostModerationLogRepository(env.db), activeModeration(false))
	title := uniqueTitle("failclosed")
	mod.set(title, nil, fmt.Errorf("%w: status 401", postdomain.ErrModerationAuth))

	postID, circleID := createModeratedPost(t, svc, title)
	for range 2 { // 第二次模拟重复投递：已有审计行，跳过
		if err := svc.ModeratePost(t.Context(), postID); err != nil {
			t.Fatalf("ModeratePost: %v", err)
		}
	}

	rows := moderationLogs(t, env.db, postID)
	if len(rows) != 1 || rows[0].Decision != string(postdomain.ModerationReview) || rows[0].ErrorMsg == nil {
		t.Fatalf("want exactly one review log with error, got %+v", rows)
	}
	if n := mod.callCount(title); n != 1 {
		t.Fatalf("moderator called %d times, want 1", n)
	}
	if got := postStatus(t, env.db, postID); got != postdomain.PostStatusReviewing {
		t.Fatalf("status = %d, want reviewing", got)
	}
	if count, trigger := fx.get(circleID, postID); count != 0 || trigger != 0 {
		t.Fatalf("fail-closed must not run side effects: count=%d trigger=%d", count, trigger)
	}
}
