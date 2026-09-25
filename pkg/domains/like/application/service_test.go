package application

import (
	"context"
	"errors"
	"os"
	"testing"

	"interestBar/pkg/domains/like/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

// fakeLikeCache 模拟 Lua 设值语义：只在状态变化时返回 ±1。
type fakeLikeCache struct {
	liked   map[uuid.UUID]bool
	setArgs []bool
	err     error
}

func newFakeLikeCache() *fakeLikeCache { return &fakeLikeCache{liked: map[uuid.UUID]bool{}} }

func (c *fakeLikeCache) set(id uuid.UUID, liked bool) (domain.ToggleResult, error) {
	c.setArgs = append(c.setArgs, liked)
	if c.err != nil {
		return 0, c.err
	}
	if c.liked[id] == liked {
		return domain.ToggleResultUnchanged, nil
	}
	c.liked[id] = liked
	if liked {
		return domain.ToggleResultLiked, nil
	}
	return domain.ToggleResultUnliked, nil
}

type fakePostCache struct{ *fakeLikeCache }

func (c fakePostCache) Set(_ context.Context, _, postID uuid.UUID, liked bool) (domain.ToggleResult, error) {
	return c.set(postID, liked)
}
func (c fakePostCache) StatsExists(context.Context, uuid.UUID) (bool, error) { return true, nil }

type fakeCommentCache struct{ *fakeLikeCache }

func (c fakeCommentCache) Set(_ context.Context, _, commentID uuid.UUID, liked bool) (domain.ToggleResult, error) {
	return c.set(commentID, liked)
}
func (c fakeCommentCache) StatsExists(context.Context, uuid.UUID) (bool, error) { return true, nil }

type publishedEvent struct {
	target uuid.UUID
	amount int64
}

type fakePublisher struct{ events []publishedEvent }

func (p *fakePublisher) PublishPostLike(_ context.Context, _, postID uuid.UUID, amount int64) error {
	p.events = append(p.events, publishedEvent{postID, amount})
	return nil
}
func (p *fakePublisher) PublishCommentLike(_ context.Context, _, commentID, _ uuid.UUID, amount int64) error {
	p.events = append(p.events, publishedEvent{commentID, amount})
	return nil
}

// fakeTarget 同时实现 PostTarget 与 CommentTarget；dbLiked 表示 DB 中的真实状态。
type fakeTarget struct {
	dbLiked bool
	dbErr   error
}

func (t *fakeTarget) Exists(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (t *fakeTarget) ExistsWithPostID(context.Context, uuid.UUID) (*uuid.UUID, bool, error) {
	p := uuid.New()
	return &p, true, nil
}
func (t *fakeTarget) RestoreStats(context.Context, uuid.UUID) error { return nil }
func (t *fakeTarget) IsLiked(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return t.dbLiked, t.dbErr
}

func newTestService(cache *fakeLikeCache, target *fakeTarget, pub *fakePublisher) LikeService {
	svc := NewLikeService(fakePostCache{cache}, fakeCommentCache{cache}, pub)
	svc.SetPostTarget(target)
	svc.SetCommentTarget(target)
	return svc
}

// 回归 #46：用户 ZSET 已过期（缓存无记录），DB 显示已赞，此时点击应为取消，而不是再赞一次。
func TestToggle_CacheMissButLikedInDB_Unlikes(t *testing.T) {
	for _, typ := range []string{"post", "comment"} {
		t.Run(typ, func(t *testing.T) {
			cache, pub := newFakeLikeCache(), &fakePublisher{}
			target := &fakeTarget{dbLiked: true}
			id := uuid.New()
			cache.liked[id] = true // IsLiked 回源后已回填缓存

			res, err := newTestService(cache, target, pub).Toggle(context.Background(), uuid.New(), ToggleInput{Type: typ, TargetID: id})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsLiked {
				t.Fatal("want unliked")
			}
			if len(cache.setArgs) != 1 || cache.setArgs[0] {
				t.Fatalf("want Set(false), got %v", cache.setArgs)
			}
			if len(pub.events) != 1 || pub.events[0].amount != -1 {
				t.Fatalf("want one -1 event, got %+v", pub.events)
			}
		})
	}
}

func TestToggle_NotLiked_Likes(t *testing.T) {
	cache, pub := newFakeLikeCache(), &fakePublisher{}
	id := uuid.New()

	res, err := newTestService(cache, &fakeTarget{}, pub).Toggle(context.Background(), uuid.New(), ToggleInput{Type: "post", TargetID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsLiked || len(pub.events) != 1 || pub.events[0].amount != 1 {
		t.Fatalf("want liked with one +1 event, got %+v / %+v", res, pub.events)
	}
}

// 缓存已处于期望态（例如并发请求已先完成）时不发布任何事件。
func TestToggle_UnchangedPublishesNothing(t *testing.T) {
	cache, pub := newFakeLikeCache(), &fakePublisher{}
	id := uuid.New()
	cache.liked[id] = true // 缓存已赞，但 target 解析为未赞 → want=true → no-op

	res, err := newTestService(cache, &fakeTarget{dbLiked: false}, pub).Toggle(context.Background(), uuid.New(), ToggleInput{Type: "post", TargetID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsLiked {
		t.Fatal("want liked")
	}
	if len(pub.events) != 0 {
		t.Fatalf("unchanged state must not publish, got %+v", pub.events)
	}
}

func TestToggle_StateResolveErrorDoesNotGuess(t *testing.T) {
	cache, pub := newFakeLikeCache(), &fakePublisher{}
	target := &fakeTarget{dbErr: errors.New("db down")}

	_, err := newTestService(cache, target, pub).Toggle(context.Background(), uuid.New(), ToggleInput{Type: "comment", TargetID: uuid.New()})
	if err == nil {
		t.Fatal("want error")
	}
	if len(cache.setArgs) != 0 || len(pub.events) != 0 {
		t.Fatal("must not touch cache or publish when state cannot be resolved")
	}
}

func TestToggle_InvalidType(t *testing.T) {
	_, err := newTestService(newFakeLikeCache(), &fakeTarget{}, &fakePublisher{}).
		Toggle(context.Background(), uuid.New(), ToggleInput{Type: "share", TargetID: uuid.New()})
	if !errors.Is(err, domain.ErrInvalidTargetType) {
		t.Fatalf("want ErrInvalidTargetType, got %v", err)
	}
}

// 显式 action：客户端重试 / 双击同一动作只生效一次。
func TestToggle_ExplicitActionIsIdempotent(t *testing.T) {
	cache, pub := newFakeLikeCache(), &fakePublisher{}
	target := &fakeTarget{}
	svc := newTestService(cache, target, pub)
	id, user := uuid.New(), uuid.New()

	for i := 0; i < 2; i++ {
		res, err := svc.Toggle(context.Background(), user, ToggleInput{Type: "post", TargetID: id, Action: "like"})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsLiked {
			t.Fatalf("attempt %d: want liked", i)
		}
		target.dbLiked = true // 模拟首次成功后真实状态已为已赞
	}
	if len(pub.events) != 1 || pub.events[0].amount != 1 {
		t.Fatalf("want exactly one +1 event, got %+v", pub.events)
	}

	res, err := svc.Toggle(context.Background(), user, ToggleInput{Type: "post", TargetID: id, Action: "unlike"})
	if err != nil || res.IsLiked {
		t.Fatalf("want unliked, got %+v err=%v", res, err)
	}
	if len(pub.events) != 2 || pub.events[1].amount != -1 {
		t.Fatalf("want a -1 event after unlike, got %+v", pub.events)
	}
}

func TestToggle_InvalidAction(t *testing.T) {
	cache := newFakeLikeCache()
	_, err := newTestService(cache, &fakeTarget{}, &fakePublisher{}).
		Toggle(context.Background(), uuid.New(), ToggleInput{Type: "post", TargetID: uuid.New(), Action: "flip"})
	if !errors.Is(err, domain.ErrInvalidAction) {
		t.Fatalf("want ErrInvalidAction, got %v", err)
	}
	if len(cache.setArgs) != 0 {
		t.Fatal("invalid action must not touch the cache")
	}
}

func TestResolveWant(t *testing.T) {
	cases := []struct {
		current bool
		action  string
		want    bool
	}{
		{false, "", true}, {true, "", false},
		{false, "like", true}, {true, "like", true},
		{false, "unlike", false}, {true, "unlike", false},
	}
	for _, tc := range cases {
		got, err := domain.ResolveWant(tc.current, tc.action)
		if err != nil || got != tc.want {
			t.Fatalf("ResolveWant(%v, %q) = %v, %v; want %v", tc.current, tc.action, got, err, tc.want)
		}
	}
}
