// moderation_test.go 帖子 AI 审核流（#117）应用层单元测试。
//
// 覆盖：开关关闭 / 未配 api_key 时发帖行为不变；开启后落库为审核中、不执行发布副作用、发审核事件；
// 审核事件发不出去时按 fail_open 兜底；worker 关停取消不按 fail_open 放行。
// worker 的发布 / 待审 / 驳回 / fail_open / 重复消费见 composition 包的集成测试。
package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// fakeModerationRepo 内存帖子仓储：发帖 + 条件更新 + 读取。
type fakeModerationRepo struct {
	fakeCreatePostRepo

	updates    int                         // UpdateStatusFromReviewing / ResolveReview 成功次数
	reviewLogs []*domain.PostModerationLog // ResolveReview 写入的人工审计行
}

func (r *fakeModerationRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Post, error) {
	for _, p := range r.created {
		if p.ID == id {
			cp := *p
			return &cp, nil
		}
	}
	return nil, domain.ErrPostNotFound
}

func (r *fakeModerationRepo) UpdateStatusFromReviewing(_ context.Context, id uuid.UUID, status int16) (bool, error) {
	for _, p := range r.created {
		if p.ID == id && p.Status == domain.PostStatusReviewing {
			p.Status = status
			r.updates++
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeModerationRepo) GetMentionUserIDsByPostIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	out := make(map[uuid.UUID][]uuid.UUID)
	for _, id := range ids {
		if m, ok := r.mentions[id]; ok {
			out[id] = m
		}
	}
	return out, nil
}

// fakeModerationPublisher 记录 @提及 通知与审核事件。
type fakeModerationPublisher struct {
	fakeMentionPublisher

	moderation       []uuid.UUID
	reviewNotices    []reviewNotice
	moderatorNotices []moderatorNotice // #153 圈管通知（举报 / 自伤）
	removedNotices   []reviewNotice    // #153 举报下架通知
	err              error
}

func (p *fakeModerationPublisher) PublishModeration(_ context.Context, postID uuid.UUID) error {
	if p.err != nil {
		return p.err
	}
	p.moderation = append(p.moderation, postID)
	return nil
}

type fakeModerationLog struct{ rows []*domain.PostModerationLog }

func (l *fakeModerationLog) Create(_ context.Context, row *domain.PostModerationLog) error {
	l.rows = append(l.rows, row)
	return nil
}

func (l *fakeModerationLog) ExistsByPostID(_ context.Context, postID uuid.UUID) (bool, error) {
	for _, r := range l.rows {
		if r.PostID == postID {
			return true, nil
		}
	}
	return false, nil
}

func (l *fakeModerationLog) LatestAIByPostIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*domain.PostModerationLog, error) {
	out := make(map[uuid.UUID]*domain.PostModerationLog)
	for _, id := range ids {
		for _, r := range l.rows { // rows 按写入顺序，后写的覆盖先写的
			if r.PostID == id && r.Source != domain.ModerationSourceManual {
				out[id] = r
			}
		}
	}
	return out, nil
}

type fakeModerator struct {
	a     *domain.Assessment
	err   error
	calls int
}

func (m *fakeModerator) Assess(context.Context, domain.ModerationInput) (*domain.Assessment, error) {
	m.calls++
	return m.a, m.err
}

type moderationFixture struct {
	svc     PostService
	repo    *fakeModerationRepo
	pub     *fakeModerationPublisher
	log     *fakeModerationLog
	mod     *fakeModerator
	count   *fakePostCount
	trigger *fakeAgentTrigger
}

func moderationCfg(enabled bool, apiKey string, failOpen bool) conf.Moderation {
	return conf.Moderation{Enabled: enabled, APIKey: apiKey, FailOpen: &failOpen}
}

func newModerationFixture(cfg conf.Moderation) *moderationFixture {
	f := &moderationFixture{
		repo:    &fakeModerationRepo{},
		pub:     &fakeModerationPublisher{},
		log:     &fakeModerationLog{},
		mod:     &fakeModerator{},
		count:   &fakePostCount{},
		trigger: &fakeAgentTrigger{},
	}
	f.svc = NewPostService(f.repo, nil, nil, nil, nil, f.pub)
	f.svc.SetUserFacade(newMentionFacade())
	f.svc.SetMemberChecker(fakeNormalMember{})
	f.svc.SetStatusChecker(fakeAvailableCircle{})
	f.svc.SetPostCountPort(f.count)
	f.svc.SetAgentTrigger(f.trigger)
	f.svc.SetModeration(f.mod, f.log, cfg)
	return f
}

func (f *moderationFixture) createPost(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := f.svc.CreatePost(context.Background(), mentionActor, CreatePostInput{
		CircleID:       postCircleID,
		Title:          "标题",
		Content:        "正文 @普通用户",
		MentionUserIDs: []uuid.UUID{normalUserID},
	})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	return id
}

// assertPublishedOnce 发布副作用（计数 / 通知 / 机器人）各恰好执行一次。
func (f *moderationFixture) assertPublishedOnce(t *testing.T, postID uuid.UUID) {
	t.Helper()
	if f.count.incr[postCircleID] != 1 || len(f.pub.notices) != 1 || len(f.trigger.posts) != 1 {
		t.Fatalf("side effects: count=%v notices=%v trigger=%v, want exactly one each", f.count.incr, f.pub.notices, f.trigger.posts)
	}
	if f.pub.notices[0] != postID || f.trigger.posts[0] != postID {
		t.Fatalf("side effects for wrong post: notices=%v trigger=%v", f.pub.notices, f.trigger.posts)
	}
}

func (f *moderationFixture) assertNoSideEffects(t *testing.T) {
	t.Helper()
	if len(f.count.incr) != 0 || len(f.pub.notices) != 0 || len(f.trigger.posts) != 0 {
		t.Fatalf("unexpected side effects: count=%v notices=%v trigger=%v", f.count.incr, f.pub.notices, f.trigger.posts)
	}
}

// 开关关闭或未配置 api_key：与审核流上线前完全一致（直接发布，不发审核事件、不调 Jev）。
func TestCreatePost_ModerationInactivePublishesDirectly(t *testing.T) {
	for name, cfg := range map[string]conf.Moderation{
		"disabled":          moderationCfg(false, "key", true),
		"enabled no apikey": moderationCfg(true, "", true),
	} {
		t.Run(name, func(t *testing.T) {
			f := newModerationFixture(cfg)
			id := f.createPost(t)
			if got := f.repo.created[0].Status; got != domain.PostStatusPublished {
				t.Fatalf("status = %d, want published", got)
			}
			f.assertPublishedOnce(t, id)
			if len(f.pub.moderation) != 0 || f.mod.calls != 0 || len(f.log.rows) != 0 {
				t.Fatalf("moderation must not run: events=%v calls=%d logs=%d", f.pub.moderation, f.mod.calls, len(f.log.rows))
			}
		})
	}
}

// 开启后：落库为审核中、@提及照常落库、不执行发布副作用、发一条审核事件。
func TestCreatePost_ModerationActiveEnqueues(t *testing.T) {
	f := newModerationFixture(moderationCfg(true, "key", true))
	id := f.createPost(t)

	if got := f.repo.created[0].Status; got != domain.PostStatusReviewing {
		t.Fatalf("status = %d, want reviewing", got)
	}
	if got := f.repo.mentions[id]; len(got) != 1 || got[0] != normalUserID {
		t.Fatalf("mentions must still be saved for later publish, got %v", got)
	}
	if len(f.pub.moderation) != 1 || f.pub.moderation[0] != id {
		t.Fatalf("moderation events = %v, want [%s]", f.pub.moderation, id)
	}
	f.assertNoSideEffects(t)
	if f.mod.calls != 0 || len(f.log.rows) != 0 {
		t.Fatalf("CreatePost must not call the moderator synchronously: calls=%d logs=%d", f.mod.calls, len(f.log.rows))
	}
}

// 审核事件发不出去：fail_open=true 直接发布（副作用一次 + 审计 fail_open），false 留在审核中转人工。
func TestCreatePost_EnqueueFailureFallsBack(t *testing.T) {
	t.Run("fail_open", func(t *testing.T) {
		f := newModerationFixture(moderationCfg(true, "key", true))
		f.pub.err = errors.New("broker down")
		id := f.createPost(t)

		if got := f.repo.created[0].Status; got != domain.PostStatusPublished {
			t.Fatalf("status = %d, want published", got)
		}
		f.assertPublishedOnce(t, id)
		if len(f.log.rows) != 1 || f.log.rows[0].Decision != domain.ModerationFailOpen || f.log.rows[0].ErrorMsg == nil {
			t.Fatalf("want one fail_open log with error, got %+v", f.log.rows)
		}
	})
	t.Run("fail_closed", func(t *testing.T) {
		f := newModerationFixture(moderationCfg(true, "key", false))
		f.pub.err = errors.New("broker down")
		f.createPost(t)

		if got := f.repo.created[0].Status; got != domain.PostStatusReviewing {
			t.Fatalf("status = %d, want reviewing", got)
		}
		f.assertNoSideEffects(t)
		if len(f.log.rows) != 1 || f.log.rows[0].Decision != domain.ModerationReview {
			t.Fatalf("want one review log, got %+v", f.log.rows)
		}
	})
}

// worker 关停时 Assess 因 ctx 取消失败：不能当作「服务不可用」按 fail_open 放行，返回错误让事件重投。
func TestModeratePost_CanceledContextIsRetried(t *testing.T) {
	f := newModerationFixture(moderationCfg(true, "key", true))
	id := f.createPost(t)
	f.mod.err = domain.ErrModerationUnavailable

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.svc.ModeratePost(ctx, id); err == nil {
		t.Fatal("ModeratePost with canceled ctx must return an error so the event is redelivered")
	}
	if f.repo.updates != 0 || len(f.log.rows) != 0 {
		t.Fatalf("must not change status or log on shutdown: updates=%d logs=%d", f.repo.updates, len(f.log.rows))
	}
	f.assertNoSideEffects(t)
}

// Jev 鉴权 / 参数错误（不可重试）与不可用一样按 fail_open 处理。
func TestModeratePost_ConfigErrorsFollowFailOpen(t *testing.T) {
	for _, failOpen := range []bool{true, false} {
		f := newModerationFixture(moderationCfg(true, "key", failOpen))
		id := f.createPost(t)
		f.mod.err = domain.ErrModerationAuth

		if err := f.svc.ModeratePost(context.Background(), id); err != nil {
			t.Fatalf("fail_open=%v: ModeratePost: %v", failOpen, err)
		}
		want := int16(domain.PostStatusReviewing)
		if failOpen {
			want = domain.PostStatusPublished
		}
		if got := f.repo.created[0].Status; got != want {
			t.Fatalf("fail_open=%v: status = %d, want %d", failOpen, got, want)
		}
	}
}
