// circle_reply_test.go 圈内机器人回复触发与限流应用单元测试。
//
// 覆盖设计文档 P0-9（docs/circle-agent-reply-design.md）：
//   - agentInScope 四象限（全局全站 / 本圈命中 / 他圈不触发 / Nil fail-closed）；
//   - 触发链候选集收口 + agentInScope 第二道防线（候选集语义漂移仍不越圈）；
//   - CircleManualReply 权限矩阵（仅圈主 / fail-closed）+ 跨作用域 404 + 帖子门槛
//     （跨圈 404 / 未发布 / 锁定 / 停用 / 非手动模式）；
//   - 限流对圈内机器人原样生效（每小时上限 / 最小间隔，per-agent 口径）；
//   - ManualReply 对圈内机器人 404 对齐回归（errCircleReplyUnsupported 已废）。
package application

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
)

// ---- 触发链测试固定标识 ----

var (
	circleABotLinked = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000d1")
	circleBBotLinked = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000d2")
	circleABotID     = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000e1")
	circleBBotID     = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000e2")

	postInCircleA = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000f1")
	postInCircleB = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000f2")
)

// ---- fakes ----

// fakeReplyLogRepo 最小 ReplyLogRepository fake：限流计数/最新日志可预设。
type fakeReplyLogRepo struct {
	count    int64
	last     *domain.ReplyLog
	created  []domain.ReplyLog
	mu       sync.Mutex
	createOK bool // 记录是否至少落过一行 status=1
}

func (f *fakeReplyLogRepo) CountSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error) {
	return f.count, nil
}

func (f *fakeReplyLogRepo) GetLastByAgent(ctx context.Context, agentID uuid.UUID) (*domain.ReplyLog, error) {
	return f.last, nil
}

func (f *fakeReplyLogRepo) Create(ctx context.Context, log *domain.ReplyLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	log.ID = uuid.New()
	f.created = append(f.created, *log)
	if log.Status == domain.ReplyStatusOK {
		f.createOK = true
	}
	return nil
}

// fakePostReader PostReader fake：预设单篇帖子（nil=帖子不存在）。
type fakePostReader struct{ post *PostBrief }

func (f *fakePostReader) GetPostBrief(ctx context.Context, postID uuid.UUID) (*PostBrief, error) {
	if f.post == nil || f.post.ID != postID {
		return nil, nil
	}
	cp := *f.post
	return &cp, nil
}

// fakeCommentCreator CommentCreator fake：并发安全记录调用，channel 供异步测试等待。
type fakeCommentCreator struct {
	mu    sync.Mutex
	calls []CommentCreateInput
	users []uuid.UUID
	ch    chan uuid.UUID
}

func newFakeCommentCreator() *fakeCommentCreator {
	return &fakeCommentCreator{ch: make(chan uuid.UUID, 16)}
}

func (f *fakeCommentCreator) CreateComment(ctx context.Context, userID uuid.UUID, input CommentCreateInput) (uuid.UUID, error) {
	f.mu.Lock()
	f.calls = append(f.calls, input)
	f.users = append(f.users, userID)
	f.mu.Unlock()
	id := uuid.New()
	f.ch <- id
	return id, nil
}

func (f *fakeCommentCreator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeLLM LLMCaller fake：固定输出，原子计数调用次数（异步触发链并发调用）。
type fakeLLM struct{ calls atomic.Int64 }

func (f *fakeLLM) Generate(ctx context.Context, req LLMRequest) (*LLMResult, error) {
	f.calls.Add(1)
	return &LLMResult{Content: "这是机器人的回复", PromptTokens: 10, CompletionTokens: 5}, nil
}

// ---- 构造助手 ----

type replyFixture struct {
	repo     *fakeAgentRepo
	logRepo  *fakeReplyLogRepo
	llm      *fakeLLM
	comments *fakeCommentCreator
	svc      ReplyService
}

func newReplyFixture() *replyFixture {
	f := &replyFixture{
		repo:     newFakeAgentRepo(),
		logRepo:  &fakeReplyLogRepo{},
		llm:      &fakeLLM{},
		comments: newFakeCommentCreator(),
	}
	f.svc = NewReplyService(f.repo, f.logRepo, f.llm)
	f.svc.SetPostReader(&fakePostReader{post: &PostBrief{
		ID: postInCircleA, Title: "圈内好帖", Status: 1, CircleID: circleAID,
	}})
	f.svc.SetCommentCreator(f.comments)
	return f
}

// seedCircleBot 放入一个圈内机器人（mode/keywords 按用例调整）。
func seedCircleBot(repo *fakeAgentRepo, id uuid.UUID, linked uuid.UUID, circle uuid.UUID, mode domain.TriggerMode, keywords ...string) *domain.AiAgent {
	a := &domain.AiAgent{
		ID: id, Name: "圈机器人", CircleID: &circle, LinkedUserID: linked,
		APIProtocol: "openai", Model: "test-model", Status: domain.AgentStatusEnabled,
		TriggerMode:       mode,
		TriggerKeywords:   domain.KeywordsJSON(keywords),
		MaxRepliesPerHour: 30, MinIntervalSec: 60,
	}
	repo.byID[id] = a
	return a
}

// seedManualCircleBot 圈内手动模式机器人（CircleManualReply 主角）。
func seedManualCircleBot(repo *fakeAgentRepo) *domain.AiAgent {
	a := seedCircleBot(repo, circleAgentID, circleABotLinked, circleAID, domain.TriggerModeManual)
	return a
}

// waitComment 等待一次评论创建（异步触发链用），超时 fail。
func waitComment(t *testing.T, f *replyFixture) {
	t.Helper()
	select {
	case <-f.comments.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for agent comment")
	}
}

// assertNoComment 短暂等待后断言没有评论产生（异步链负路径用）。
func assertNoComment(t *testing.T, f *replyFixture) {
	t.Helper()
	time.Sleep(150 * time.Millisecond)
	if n := f.comments.callCount(); n != 0 {
		t.Fatalf("expected no comment, got %d", n)
	}
}

// ---- agentInScope 四象限 ----

func TestAgentInScope(t *testing.T) {
	global := &domain.AiAgent{CircleID: nil}
	inA := seedCircleBot(newFakeAgentRepo(), circleABotID, circleABotLinked, circleAID, domain.TriggerModeKeyword)

	cases := []struct {
		name       string
		agent      *domain.AiAgent
		postCircle uuid.UUID
		want       bool
	}{
		{"global bot in any circle", global, circleAID, true},
		{"global bot with unknown circle (Nil)", global, uuid.Nil, true},
		{"circle bot in own circle", inA, circleAID, true},
		{"circle bot in other circle", inA, circleBID, false},
		{"circle bot with unknown circle fail-closed", inA, uuid.Nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentInScope(tc.agent, tc.postCircle); got != tc.want {
				t.Fatalf("agentInScope = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---- 评论关键词触发：候选集收口 + 第二道防线 ----

// TestOnCommentCreated_CircleScope 本圈关键词机器人触发；他圈不触发；
// 候选集漂移时 agentInScope 兜底仍不越圈；Nil 圈子 fail-closed 仅全局机器人。
func TestOnCommentCreated_CircleScope(t *testing.T) {
	t.Run("own circle keyword hit", func(t *testing.T) {
		f := newReplyFixture()
		seedCircleBot(f.repo, circleABotID, circleABotLinked, circleAID, domain.TriggerModeKeyword, "骑行")
		f.svc.OnCommentCreated(CommentEvent{
			CommentID: uuid.New(), PostID: postInCircleA, PostCircleID: circleAID,
			UserID: memberID, Content: "周末骑行约吗",
		})
		waitComment(t, f)
		if f.comments.callCount() != 1 {
			t.Fatalf("comments = %d, want 1", f.comments.callCount())
		}
	})

	t.Run("other circle bot not triggered", func(t *testing.T) {
		f := newReplyFixture()
		seedCircleBot(f.repo, circleBBotID, circleBBotLinked, circleBID, domain.TriggerModeKeyword, "骑行")
		f.svc.OnCommentCreated(CommentEvent{
			CommentID: uuid.New(), PostID: postInCircleA, PostCircleID: circleAID,
			UserID: memberID, Content: "周末骑行约吗",
		})
		assertNoComment(t, f)
	})

	t.Run("double guard when candidate set drifts", func(t *testing.T) {
		f := newReplyFixture()
		f.repo.scopeFilterEnabled = false // 模拟候选集语义漂移：返回全部机器人
		seedCircleBot(f.repo, circleBBotID, circleBBotLinked, circleBID, domain.TriggerModeKeyword, "骑行")
		f.svc.OnCommentCreated(CommentEvent{
			CommentID: uuid.New(), PostID: postInCircleA, PostCircleID: circleAID,
			UserID: memberID, Content: "周末骑行约吗",
		})
		assertNoComment(t, f) // 圈B机器人即使进了候选集也被 agentInScope 挡下
	})

	t.Run("unknown circle only global bots", func(t *testing.T) {
		f := newReplyFixture()
		seedCircleBot(f.repo, circleABotID, circleABotLinked, circleAID, domain.TriggerModeKeyword, "骑行")
		f.svc.OnCommentCreated(CommentEvent{
			CommentID: uuid.New(), PostID: postInCircleA, PostCircleID: uuid.Nil,
			UserID: memberID, Content: "周末骑行约吗",
		})
		assertNoComment(t, f) // 圈内机器人 fail-closed 不触发
	})
}

// ---- 发帖 @提及 触发：scope 匹配第二道防线 ----

// TestOnPostMentioned_CircleScope 越圈机器人即使被 @ 且混进候选集也不触发
// （mention 兜底剔除是名单层第一道，此处验证触发层兜底）；本圈 + 全局机器人正常触发。
func TestOnPostMentioned_CircleScope(t *testing.T) {
	t.Run("cross-circle mention blocked by scope", func(t *testing.T) {
		f := newReplyFixture()
		f.repo.scopeFilterEnabled = false
		seedCircleBot(f.repo, circleBBotID, circleBBotLinked, circleBID, domain.TriggerModeKeyword, "x")
		f.svc.OnPostMentioned(PostMentionEvent{
			PostID: postInCircleA, PostCircleID: circleAID, UserID: memberID,
			MentionUserIDs: []uuid.UUID{circleBBotLinked},
		})
		assertNoComment(t, f)
	})

	t.Run("own circle and global bots trigger", func(t *testing.T) {
		f := newReplyFixture()
		own := seedCircleBot(f.repo, circleABotID, circleABotLinked, circleAID, domain.TriggerModeKeyword)
		global := &domain.AiAgent{
			ID: globalAgentID, Name: "全局助理", CircleID: nil, LinkedUserID: outsiderID,
			APIProtocol: "openai", Model: "test-model", Status: domain.AgentStatusEnabled,
			TriggerMode: domain.TriggerModeKeyword,
		}
		f.repo.byID[globalAgentID] = global
		_ = own
		f.svc.OnPostMentioned(PostMentionEvent{
			PostID: postInCircleA, PostCircleID: circleAID, UserID: memberID,
			MentionUserIDs: []uuid.UUID{circleABotLinked, outsiderID},
		})
		waitComment(t, f)
		waitComment(t, f)
		if n := f.comments.callCount(); n != 2 {
			t.Fatalf("comments = %d, want 2 (own circle + global)", n)
		}
	})
}

// ---- CircleManualReply 权限矩阵 ----

// TestCircleManualReply_PermissionMatrix 仅圈主可触发；admin/member/非成员 403；
// CircleRoleReader 未注入 fail-closed 拒绝。
func TestCircleManualReply_PermissionMatrix(t *testing.T) {
	cases := []struct {
		name     string
		operator uuid.UUID
		roles    *fakeCircleRoles
		wantErr  error
	}{
		{"owner allowed", ownerID, newFakeCircleRoles(), nil},
		{"admin forbidden", adminID, newFakeCircleRoles(), errNotCircleOwner},
		{"member forbidden", memberID, newFakeCircleRoles(), errNotCircleOwner},
		{"non-member forbidden", outsiderID, newFakeCircleRoles(), errNotCircleOwner},
		{"muted owner forbidden", mutedOwner, newFakeCircleRoles(), errNotCircleOwner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReplyFixture()
			seedManualCircleBot(f.repo)
			f.svc.SetCircleRoleReader(tc.roles)
			_, err := f.svc.CircleManualReply(context.Background(), tc.operator, circleAgentID, postInCircleA)
			assertErr(t, err, tc.wantErr)
			if tc.wantErr == nil {
				waitComment(t, f)
			}
		})
	}

	t.Run("fail-closed without circle role reader", func(t *testing.T) {
		f := newReplyFixture()
		seedManualCircleBot(f.repo)
		_, err := f.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleA)
		assertErr(t, err, errNotCircleOwner)
	})
}

// ---- CircleManualReply 校验链 ----

// TestCircleManualReply_Validation 跨作用域 404（全局机器人/未知/跨圈帖）、
// 帖子门槛（未发布/锁定）、停用、非手动模式。
func TestCircleManualReply_Validation(t *testing.T) {
	newManualSvc := func() *replyFixture {
		f := newReplyFixture()
		seedManualCircleBot(f.repo)
		f.svc.SetCircleRoleReader(newFakeCircleRoles())
		return f
	}

	t.Run("unknown and global agent 404", func(t *testing.T) {
		f := newManualSvc()
		seedGlobalAgent(f.repo)
		ctx := context.Background()
		_, err := f.svc.CircleManualReply(ctx, ownerID, circleBBotID, postInCircleA)
		assertErr(t, err, errAgentNotFound)
		_, err = f.svc.CircleManualReply(ctx, ownerID, globalAgentID, postInCircleA)
		assertErr(t, err, errAgentNotFound)
	})

	t.Run("cross-circle post 404", func(t *testing.T) {
		f := newManualSvc()
		// 帖子属于圈B：postReader 只认 postInCircleA，查圈B帖返回 nil → 先 errPostNotReplyable。
		_, err := f.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleB)
		assertErr(t, err, errPostNotReplyable)

		// 帖子存在但属于他圈 → errPostNotInAgentCircle（handler 映射 404）。
		f2 := newManualSvc()
		f2.svc.SetPostReader(&fakePostReader{post: &PostBrief{
			ID: postInCircleB, Status: 1, CircleID: circleBID,
		}})
		_, err = f2.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleB)
		if !errors.Is(err, errPostNotInAgentCircle) {
			t.Fatalf("err = %v, want errPostNotInAgentCircle", err)
		}
	})

	t.Run("post not replyable", func(t *testing.T) {
		f := newManualSvc()
		f.svc.SetPostReader(&fakePostReader{post: &PostBrief{ID: postInCircleA, Status: 2, CircleID: circleAID}})
		_, err := f.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleA)
		assertErr(t, err, errPostNotReplyable)
	})

	t.Run("disabled agent and non-manual mode", func(t *testing.T) {
		f := newReplyFixture()
		bot := seedManualCircleBot(f.repo)
		f.svc.SetCircleRoleReader(newFakeCircleRoles())
		bot.Status = domain.AgentStatusDisabled
		_, err := f.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleA)
		assertErr(t, err, errAgentDisabled)

		f2 := newReplyFixture()
		seedCircleBot(f2.repo, circleAgentID, circleABotLinked, circleAID, domain.TriggerModeKeyword, "x")
		f2.svc.SetCircleRoleReader(newFakeCircleRoles())
		_, err = f2.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleA)
		assertErr(t, err, errNotManualMode)
	})
}

// ---- 限流对圈内机器人生效（per-agent 口径原样应用）----

// TestCircleManualReply_RateLimitApplied 每小时上限与最小间隔两条限流路径
// 对圈内机器人照常拦截（配置即决策B缺省也可被预设计数命中）。
func TestCircleManualReply_RateLimitApplied(t *testing.T) {
	t.Run("hourly cap", func(t *testing.T) {
		f := newReplyFixture()
		bot := seedManualCircleBot(f.repo)
		f.svc.SetCircleRoleReader(newFakeCircleRoles())
		f.logRepo.count = int64(bot.MaxRepliesPerHour) // 最近 1h 已达上限
		_, err := f.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleA)
		assertErr(t, err, errRateLimited)
	})

	t.Run("min interval", func(t *testing.T) {
		f := newReplyFixture()
		seedManualCircleBot(f.repo)
		f.svc.SetCircleRoleReader(newFakeCircleRoles())
		f.logRepo.last = &domain.ReplyLog{CreateTime: time.Now()} // 刚回过，60s 间隔未到
		_, err := f.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleA)
		assertErr(t, err, errRateLimited)
	})

	t.Run("happy path writes success log", func(t *testing.T) {
		f := newReplyFixture()
		seedManualCircleBot(f.repo)
		f.svc.SetCircleRoleReader(newFakeCircleRoles())
		commentID, err := f.svc.CircleManualReply(context.Background(), ownerID, circleAgentID, postInCircleA)
		if err != nil {
			t.Fatalf("circle manual reply failed: %v", err)
		}
		if commentID == uuid.Nil {
			t.Fatal("comment id empty")
		}
		if f.llm.calls.Load() != 1 {
			t.Fatalf("llm calls = %d, want 1", f.llm.calls.Load())
		}
		if !f.logRepo.createOK {
			t.Fatal("success reply log not written")
		}
	})
}

// ---- 创建链决策B：圈内限流缺省 ----

// TestCircleAgentCreate_RateLimitDefaults 圈内创建未传限流字段 → 兜底 30/60
// （防圈主计费 key 裸奔）；全局链路不做兜底（0=不限语义保持）。
func TestCircleAgentCreate_RateLimitDefaults(t *testing.T) {
	repo := newFakeAgentRepo()
	svc := newCircleAgentSvc(repo, newFakeCircleRoles())
	vo, err := svc.CreateCircleAgent(context.Background(), ownerID, circleAID, createInput())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if vo.MaxRepliesPerHour != circleBotDefaultMaxRepliesPerHour {
		t.Fatalf("MaxRepliesPerHour = %d, want %d", vo.MaxRepliesPerHour, circleBotDefaultMaxRepliesPerHour)
	}
	if vo.MinIntervalSec != circleBotDefaultMinIntervalSec {
		t.Fatalf("MinIntervalSec = %d, want %d", vo.MinIntervalSec, circleBotDefaultMinIntervalSec)
	}

	// 全局链路：未传限流 → 保持 0=不限（决策B仅圈内）。
	gRepo := newFakeAgentRepo()
	gSvc := NewAgentService(gRepo)
	gSvc.SetRoleReader(&fakeUserRoleReader{role: 1, ok: true})
	gSvc.SetBotUserCreator(&fakeBotUserCreator{})
	gvo, err := gSvc.CreateAgent(context.Background(), ownerID, createInput())
	if err != nil {
		t.Fatalf("global create failed: %v", err)
	}
	if gvo.MaxRepliesPerHour != 0 || gvo.MinIntervalSec != 0 {
		t.Fatalf("global bot rate limit = %d/%d, want 0/0 (unlimited)",
			gvo.MaxRepliesPerHour, gvo.MinIntervalSec)
	}
}
