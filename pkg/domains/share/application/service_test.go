package application

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"interestBar/pkg/domains/share/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

// fakeTarget 可分享的帖子集合：posts 之外的都不可分享（不存在 / 未发布 / 不可见）；hidden 里的对指定用户不可见。
type fakeTarget struct {
	posts  map[uuid.UUID]*domain.SharedPost
	hidden map[uuid.UUID]uuid.UUID // postID -> 看不到它的用户
	err    error
}

func (f *fakeTarget) GetShareable(_ context.Context, userID, postID uuid.UUID) (*domain.SharedPost, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.hidden[postID] == userID {
		return nil, nil
	}
	return f.posts[postID], nil
}

// fakeRepo 模拟 post_share：(user, post) 唯一，并发安全。
type fakeRepo struct {
	mu      sync.Mutex
	rows    map[[2]uuid.UUID]uuid.UUID // (user, post) -> share id
	deleted []uuid.UUID
	err     error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[[2]uuid.UUID]uuid.UUID{}} }

func (r *fakeRepo) Create(_ context.Context, s *domain.PostShare) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return false, r.err
	}
	k := [2]uuid.UUID{s.UserID, s.PostID}
	if _, ok := r.rows[k]; ok {
		return false, nil
	}
	r.rows[k] = s.ID
	return true, nil
}

func (r *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range r.rows {
		if v == id {
			delete(r.rows, k)
		}
	}
	r.deleted = append(r.deleted, id)
	return nil
}

func (r *fakeRepo) CountByPost(_ context.Context, postID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for k := range r.rows {
		if k[1] == postID {
			n++
		}
	}
	return n, nil
}

// fakeCache 模拟 post:sharecnt：不存在时 IncrIfCached 不写。
type fakeCache struct {
	mu     sync.Mutex
	counts map[uuid.UUID]int64
	err    error
}

func newFakeCache() *fakeCache { return &fakeCache{counts: map[uuid.UUID]int64{}} }

func (c *fakeCache) IncrIfCached(_ context.Context, postID uuid.UUID) (int64, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, false, c.err
	}
	v, ok := c.counts[postID]
	if !ok {
		return 0, false, nil
	}
	c.counts[postID] = v + 1
	return v + 1, true, nil
}

func (c *fakeCache) Get(_ context.Context, postID uuid.UUID) (int64, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, false, c.err
	}
	v, ok := c.counts[postID]
	return v, ok, nil
}

func (c *fakeCache) Seed(_ context.Context, postID uuid.UUID, n int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, c.err
	}
	if v, ok := c.counts[postID]; ok {
		return v, nil
	}
	c.counts[postID] = n
	return n, nil
}

// fakePublisher 记录发布的首次分享事件。
type fakePublisher struct {
	mu     sync.Mutex
	events []domain.SharedPost
	shares []domain.PostShare
	err    error
}

func (p *fakePublisher) PublishPostShare(_ context.Context, s *domain.PostShare, post *domain.SharedPost) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, *post)
	p.shares = append(p.shares, *s)
	return nil
}

func (p *fakePublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

type fixture struct {
	svc    ShareService
	target *fakeTarget
	repo   *fakeRepo
	cache  *fakeCache
	pub    *fakePublisher
	post   *domain.SharedPost
}

func newFixture() *fixture {
	post := &domain.SharedPost{ID: uuid.New(), AuthorID: uuid.New(), CircleID: uuid.New()}
	f := &fixture{
		target: &fakeTarget{posts: map[uuid.UUID]*domain.SharedPost{post.ID: post}, hidden: map[uuid.UUID]uuid.UUID{}},
		repo:   newFakeRepo(),
		cache:  newFakeCache(),
		pub:    &fakePublisher{},
		post:   post,
	}
	f.svc = NewShareService(f.repo, f.cache, f.pub)
	f.svc.SetPostTarget(f.target)
	return f
}

// 首次分享：计数 +1、发布一次事件（带作者 / 圈子）；重复分享幂等成功，不重复计数、不重复发事件。
func TestSharePost_FirstThenRepeat(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	user := uuid.New()

	got, err := f.svc.SharePost(ctx, user, f.post.ID)
	if err != nil || !got.FirstShare || got.ShareCount != 1 {
		t.Fatalf("first share = %+v, %v; want first_share=true share_count=1", got, err)
	}
	if f.pub.count() != 1 || f.pub.events[0] != *f.post {
		t.Fatalf("published events = %+v, want exactly the shared post", f.pub.events)
	}
	if s := f.pub.shares[0]; s.UserID != user || s.PostID != f.post.ID || s.ID == uuid.Nil {
		t.Fatalf("published share = %+v", s)
	}

	for range 3 {
		got, err = f.svc.SharePost(ctx, user, f.post.ID)
		if err != nil || got.FirstShare || got.ShareCount != 1 {
			t.Fatalf("repeat share = %+v, %v; want first_share=false share_count=1", got, err)
		}
	}
	if f.pub.count() != 1 {
		t.Fatalf("repeat share published %d events, want 1", f.pub.count())
	}

	// 另一个用户首次分享：计数累加到 2
	got, err = f.svc.SharePost(ctx, uuid.New(), f.post.ID)
	if err != nil || !got.FirstShare || got.ShareCount != 2 {
		t.Fatalf("second user share = %+v, %v; want share_count=2", got, err)
	}
}

// 缓存冷启动：首次分享按记录数回种（已含本次），不从 0 起算。
func TestSharePost_ColdCacheSeedsFromDB(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	for range 4 {
		if _, err := f.repo.Create(ctx, &domain.PostShare{ID: uuid.New(), UserID: uuid.New(), PostID: f.post.ID}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.svc.SharePost(ctx, uuid.New(), f.post.ID)
	if err != nil || got.ShareCount != 5 {
		t.Fatalf("share on cold cache = %+v, %v; want share_count=5", got, err)
	}
}

// 缓存故障：降级为 DB 记录数，不影响分享成功。
func TestSharePost_CacheDownFallsBackToDB(t *testing.T) {
	f := newFixture()
	f.cache.err = errors.New("redis down")
	got, err := f.svc.SharePost(context.Background(), uuid.New(), f.post.ID)
	if err != nil || !got.FirstShare || got.ShareCount != 1 {
		t.Fatalf("share with cache down = %+v, %v", got, err)
	}
}

// 不可分享（不存在 / 未发布 / 已删除 / 私密圈非成员）：404，不写记录、不发事件、不加计数。
func TestSharePost_NotShareable(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	outsider := uuid.New()
	f.target.hidden[f.post.ID] = outsider

	for name, tc := range map[string]struct {
		user, post uuid.UUID
	}{
		"missing post": {uuid.New(), uuid.New()},
		"invisible":    {outsider, f.post.ID},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.SharePost(ctx, tc.user, tc.post)
			if !errors.Is(err, domain.ErrPostNotFound) {
				t.Fatalf("err = %v, want ErrPostNotFound", err)
			}
		})
	}
	if len(f.repo.rows) != 0 || f.pub.count() != 0 || len(f.cache.counts) != 0 {
		t.Fatalf("rejected share left side effects: rows=%d events=%d cache=%v", len(f.repo.rows), f.pub.count(), f.cache.counts)
	}
}

// 可见性查询失败：原样返回错误（不猜测可见）。
func TestSharePost_TargetError(t *testing.T) {
	f := newFixture()
	f.target.err = errors.New("db down")
	if _, err := f.svc.SharePost(context.Background(), uuid.New(), f.post.ID); err == nil || errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("err = %v, want the target error", err)
	}
	if len(f.repo.rows) != 0 {
		t.Fatal("share row written despite target error")
	}
}

// 事件投递失败：删除刚插入的记录、返回可重试错误；重试时仍是首次分享。
func TestSharePost_PublishFailureRollsBack(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	user := uuid.New()
	f.pub.err = errors.New("broker down")

	_, err := f.svc.SharePost(ctx, user, f.post.ID)
	if !errors.Is(err, domain.ErrEventPublishFailed) {
		t.Fatalf("err = %v, want ErrEventPublishFailed", err)
	}
	if len(f.repo.rows) != 0 || len(f.repo.deleted) != 1 || len(f.cache.counts) != 0 {
		t.Fatalf("not rolled back: rows=%d deleted=%d cache=%v", len(f.repo.rows), len(f.repo.deleted), f.cache.counts)
	}

	f.pub.err = nil
	got, err := f.svc.SharePost(ctx, user, f.post.ID)
	if err != nil || !got.FirstShare || got.ShareCount != 1 {
		t.Fatalf("retry = %+v, %v; want first share", got, err)
	}
}

// 同一用户并发多次请求只计一次：只有一个请求 first_share=true，只发一次事件，计数为 1。
func TestSharePost_ConcurrentSameUser(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	user := uuid.New()

	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	firsts := 0
	for range n {
		wg.Go(func() {
			got, err := f.svc.SharePost(ctx, user, f.post.ID)
			if err != nil {
				t.Errorf("share: %v", err)
				return
			}
			if got.FirstShare {
				mu.Lock()
				firsts++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if firsts != 1 || f.pub.count() != 1 {
		t.Fatalf("first_share=%d events=%d, want 1 and 1", firsts, f.pub.count())
	}
	if c, _, _ := f.cache.Get(ctx, f.post.ID); c != 1 {
		t.Fatalf("share count = %d, want 1", c)
	}
}
