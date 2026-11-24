package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/follow/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// 发布失败分支会写日志；单元测试里没有初始化全局 logger。
func init() { logger.Log = zap.NewNop() }

// fakeEvents 记录被关注通知事件的发布调用（pair.follower = 关注者 / actor，pair.followee = 被关注者 / 接收人）。
type fakeEvents struct {
	published []pair
	err       error
}

func (e *fakeEvents) PublishFollowed(_ context.Context, followerID, followeeID uuid.UUID) error {
	e.published = append(e.published, pair{followerID, followeeID})
	return e.err
}

func newSvcWithEvents(limit int) (FollowService, *fakeRepo, *fakeUsers, *fakeEvents) {
	repo := newFakeRepo()
	users := &fakeUsers{missing: map[uuid.UUID]bool{}}
	events := &fakeEvents{}
	return NewFollowService(repo, repo, users, func() int { return limit }, nil, ListDeps{}, events), repo, users, events
}

// 只有关系从「未关注」变为「已关注」才发布：重复关注不发，取关不发，取关后再关注再发一条。
func TestFollow_PublishesOnlyWhenRelationChanges(t *testing.T) {
	svc, _, _, events := newSvcWithEvents(10)
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()

	steps := []struct {
		name string
		do   func() (*RelationVO, error)
		want int // 这一步之后累计发布的事件数
	}{
		{"first follow", func() (*RelationVO, error) { return svc.Follow(ctx, a, b) }, 1},
		{"repeat follow", func() (*RelationVO, error) { return svc.Follow(ctx, a, b) }, 1},
		{"unfollow", func() (*RelationVO, error) { return svc.Unfollow(ctx, a, b) }, 1},
		{"repeat unfollow", func() (*RelationVO, error) { return svc.Unfollow(ctx, a, b) }, 1},
		{"follow again", func() (*RelationVO, error) { return svc.Follow(ctx, a, b) }, 2},
		{"follow back", func() (*RelationVO, error) { return svc.Follow(ctx, b, a) }, 3},
	}
	for _, step := range steps {
		if _, err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if len(events.published) != step.want {
			t.Fatalf("%s: published %d events, want %d", step.name, len(events.published), step.want)
		}
	}

	// actor = 关注者，接收人 = 被关注者
	want := []pair{{a, b}, {a, b}, {b, a}}
	for i, got := range events.published {
		if got != want[i] {
			t.Errorf("event %d = follower %s / followee %s, want %s / %s", i, got.follower, got.followee, want[i].follower, want[i].followee)
		}
	}
}

// 发布失败只记日志：关注照常成功、关系与计数照常生效。
func TestFollow_PublishFailureDoesNotAffectResult(t *testing.T) {
	svc, repo, _, events := newSvcWithEvents(10)
	events.err = errBoom
	a, b := uuid.New(), uuid.New()

	vo, err := svc.Follow(context.Background(), a, b)
	if err != nil || vo == nil || vo.Relation != domain.RelationFollowing {
		t.Fatalf("follow with failing publisher: vo=%+v err=%v, want relation 1 and no error", vo, err)
	}
	if len(events.published) != 1 {
		t.Errorf("publish attempts = %d, want 1", len(events.published))
	}
	if repo.stat(a).FollowingCount != 1 || repo.stat(b).FollowerCount != 1 {
		t.Errorf("counts = %d/%d, want 1/1", repo.stat(a).FollowingCount, repo.stat(b).FollowerCount)
	}
}

// 关注被拒绝或写库失败时关系没有变化，不发布。
func TestFollow_NoPublishWhenFollowFails(t *testing.T) {
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()

	for _, tc := range []struct {
		name     string
		follower uuid.UUID
		limit    int
		arrange  func(repo *fakeRepo, users *fakeUsers)
	}{
		{"self", b, 10, func(*fakeRepo, *fakeUsers) {}},
		{"target missing or disabled", a, 10, func(_ *fakeRepo, users *fakeUsers) { users.missing[b] = true }},
		{"limit reached", a, 0, func(*fakeRepo, *fakeUsers) {}},
		{"repo error", a, 10, func(repo *fakeRepo, _ *fakeUsers) { repo.followErr = errBoom }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, users, events := newSvcWithEvents(tc.limit)
			tc.arrange(repo, users)
			if _, err := svc.Follow(ctx, tc.follower, b); err == nil {
				t.Fatal("follow succeeded, want an error")
			}
			if len(events.published) != 0 {
				t.Errorf("published %d events, want 0", len(events.published))
			}
		})
	}
}

// 关系已经提交、只是回读关系失败：接口返回错误，但对方确实被关注了，通知照发。
func TestFollow_PublishesEvenIfRelationReadFails(t *testing.T) {
	svc, repo, _, events := newSvcWithEvents(10)
	repo.relationErr = errBoom
	a, b := uuid.New(), uuid.New()

	if _, err := svc.Follow(context.Background(), a, b); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(events.published) != 1 || events.published[0] != (pair{a, b}) {
		t.Errorf("published = %+v, want one event a -> b", events.published)
	}
}
