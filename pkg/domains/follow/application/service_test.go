package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"interestBar/pkg/domains/follow/domain"

	"github.com/google/uuid"
)

type pair struct{ follower, followee uuid.UUID }

// fakeRepo 内存版关系 + 计数仓储，语义与 PG 实现一致（幂等、只在变化时改计数、上限）。
type fakeRepo struct {
	following map[pair]bool
	stats     map[uuid.UUID]*domain.UserFollowStat

	followErr, unfollowErr, relationErr, statErr error
	gotLimit                                     int
	followCalls, unfollowCalls, relationCalls    int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{following: map[pair]bool{}, stats: map[uuid.UUID]*domain.UserFollowStat{}}
}

func (r *fakeRepo) stat(id uuid.UUID) *domain.UserFollowStat {
	if r.stats[id] == nil {
		r.stats[id] = &domain.UserFollowStat{UserID: id}
	}
	return r.stats[id]
}

func (r *fakeRepo) Follow(_ context.Context, follower, followee uuid.UUID, maxFollowing int) (bool, error) {
	r.followCalls++
	r.gotLimit = maxFollowing
	if r.followErr != nil {
		return false, r.followErr
	}
	if r.following[pair{follower, followee}] {
		return false, nil
	}
	if r.stat(follower).FollowingCount >= maxFollowing {
		return false, domain.ErrFollowLimitReached
	}
	r.following[pair{follower, followee}] = true
	r.stat(follower).FollowingCount++
	r.stat(followee).FollowerCount++
	return true, nil
}

func (r *fakeRepo) Unfollow(_ context.Context, follower, followee uuid.UUID) (bool, error) {
	r.unfollowCalls++
	if r.unfollowErr != nil {
		return false, r.unfollowErr
	}
	if !r.following[pair{follower, followee}] {
		return false, nil
	}
	delete(r.following, pair{follower, followee})
	r.stat(follower).FollowingCount--
	r.stat(followee).FollowerCount--
	return true, nil
}

func (r *fakeRepo) GetRelation(_ context.Context, viewer, target uuid.UUID) (domain.Relation, error) {
	r.relationCalls++
	if r.relationErr != nil {
		return domain.RelationNone, r.relationErr
	}
	return domain.RelationOf(r.following[pair{viewer, target}], r.following[pair{target, viewer}]), nil
}

func (r *fakeRepo) GetStat(_ context.Context, id uuid.UUID) (*domain.UserFollowStat, error) {
	if r.statErr != nil {
		return nil, r.statErr
	}
	return r.stat(id), nil
}

func (r *fakeRepo) RecountStat(_ context.Context, id uuid.UUID) (*domain.UserFollowStat, error) {
	return r.stat(id), nil
}

// fakeUsers missing 里的用户视为不存在 / 已禁用 / 已删除。
type fakeUsers struct {
	missing map[uuid.UUID]bool
	err     error
	calls   int
}

func (u *fakeUsers) Exists(_ context.Context, id uuid.UUID) (bool, error) {
	u.calls++
	if u.err != nil {
		return false, u.err
	}
	return !u.missing[id], nil
}

func newSvc(limit int) (FollowService, *fakeRepo, *fakeUsers) {
	repo := newFakeRepo()
	users := &fakeUsers{missing: map[uuid.UUID]bool{}}
	return NewFollowService(repo, repo, users, func() int { return limit }), repo, users
}

var errBoom = errors.New("boom")

func TestFollow_IdempotentAndRelation(t *testing.T) {
	svc, repo, _ := newSvc(2000)
	a, b := uuid.New(), uuid.New()
	ctx := context.Background()

	vo, err := svc.Follow(ctx, a, b)
	if err != nil || vo.Relation != domain.RelationFollowing {
		t.Fatalf("first follow: vo=%+v err=%v, want relation 1", vo, err)
	}
	if repo.gotLimit != 2000 {
		t.Errorf("limit passed to repo = %d, want 2000", repo.gotLimit)
	}
	// 重复关注：成功，计数不变
	if vo, err = svc.Follow(ctx, a, b); err != nil || vo.Relation != domain.RelationFollowing {
		t.Fatalf("repeat follow: vo=%+v err=%v", vo, err)
	}
	if repo.stat(a).FollowingCount != 1 || repo.stat(b).FollowerCount != 1 {
		t.Errorf("counts after repeat follow = %d/%d, want 1/1", repo.stat(a).FollowingCount, repo.stat(b).FollowerCount)
	}
	// 对方回关：互关
	if vo, err = svc.Follow(ctx, b, a); err != nil || vo.Relation != domain.RelationMutual {
		t.Fatalf("follow back: vo=%+v err=%v, want relation 3", vo, err)
	}
}

func TestFollow_Rejections(t *testing.T) {
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()

	t.Run("self", func(t *testing.T) {
		svc, repo, users := newSvc(10)
		if _, err := svc.Follow(ctx, a, a); !errors.Is(err, domain.ErrCannotFollowSelf) {
			t.Fatalf("err = %v, want ErrCannotFollowSelf", err)
		}
		if repo.followCalls != 0 || users.calls != 0 {
			t.Error("self follow must not reach user check / repo")
		}
	})
	t.Run("target missing or disabled", func(t *testing.T) {
		svc, repo, users := newSvc(10)
		users.missing[b] = true
		if _, err := svc.Follow(ctx, a, b); !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("err = %v, want ErrUserNotFound", err)
		}
		if repo.followCalls != 0 {
			t.Error("repo must not be called for a missing target")
		}
	})
	t.Run("user check error", func(t *testing.T) {
		svc, _, users := newSvc(10)
		users.err = errBoom
		if _, err := svc.Follow(ctx, a, b); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v, want boom", err)
		}
	})
	t.Run("repo error", func(t *testing.T) {
		svc, repo, _ := newSvc(10)
		repo.followErr = errBoom
		if _, err := svc.Follow(ctx, a, b); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v, want boom", err)
		}
	})
	t.Run("relation read error", func(t *testing.T) {
		svc, repo, _ := newSvc(10)
		repo.relationErr = errBoom
		if _, err := svc.Follow(ctx, a, b); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v, want boom", err)
		}
	})
}

func TestFollow_LimitThenUnfollowAllowsAgain(t *testing.T) {
	svc, _, _ := newSvc(2)
	ctx := context.Background()
	a, t1, t2, t3 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, target := range []uuid.UUID{t1, t2} {
		if _, err := svc.Follow(ctx, a, target); err != nil {
			t.Fatalf("follow within limit: %v", err)
		}
	}
	_, err := svc.Follow(ctx, a, t3)
	if !errors.Is(err, domain.ErrFollowLimitReached) || !strings.Contains(err.Error(), "at most 2 users") {
		t.Fatalf("err = %v, want ErrFollowLimitReached mentioning the limit", err)
	}
	// 已达上限时重复关注已关注的人仍然成功（幂等）
	if _, err = svc.Follow(ctx, a, t1); err != nil {
		t.Fatalf("repeat follow at limit: %v", err)
	}
	// 取关一个后可以再关注
	if _, err = svc.Unfollow(ctx, a, t1); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	if _, err = svc.Follow(ctx, a, t3); err != nil {
		t.Fatalf("follow after unfollow: %v", err)
	}
}

func TestUnfollow(t *testing.T) {
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()

	svc, repo, users := newSvc(10)
	if _, err := svc.Unfollow(ctx, a, a); !errors.Is(err, domain.ErrCannotFollowSelf) {
		t.Fatalf("self unfollow err = %v, want ErrCannotFollowSelf", err)
	}
	// b 回关 a，a 关注 b 后取关：关系回到「对方关注了我」
	mustFollow(t, svc, b, a)
	mustFollow(t, svc, a, b)
	users.missing[b] = true // 对方被禁用后仍可取关
	checksBefore := users.calls
	vo, err := svc.Unfollow(ctx, a, b)
	if err != nil || vo.Relation != domain.RelationFollowedBy {
		t.Fatalf("unfollow: vo=%+v err=%v, want relation 2", vo, err)
	}
	if users.calls != checksBefore {
		t.Error("unfollow must not check target user status")
	}
	// 重复取关：成功，计数不变
	if vo, err = svc.Unfollow(ctx, a, b); err != nil || vo.Relation != domain.RelationFollowedBy {
		t.Fatalf("repeat unfollow: vo=%+v err=%v", vo, err)
	}
	if repo.stat(a).FollowingCount != 0 || repo.stat(b).FollowerCount != 0 || repo.stat(a).FollowerCount != 1 {
		t.Errorf("counts after repeat unfollow: a=%+v b=%+v", repo.stat(a), repo.stat(b))
	}

	repo.unfollowErr = errBoom
	if _, err = svc.Unfollow(ctx, a, b); !errors.Is(err, errBoom) {
		t.Fatalf("repo error: err = %v, want boom", err)
	}
}

func TestGetStats(t *testing.T) {
	ctx := context.Background()
	svc, repo, users := newSvc(10)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	mustFollow(t, svc, a, b)
	mustFollow(t, svc, b, a)
	mustFollow(t, svc, c, b)

	for _, tc := range []struct {
		name          string
		viewer        uuid.UUID
		want          domain.Relation
		relationReads int
	}{
		{"guest", uuid.Nil, domain.RelationNone, 0},
		{"self", b, domain.RelationNone, 0},
		{"mutual", a, domain.RelationMutual, 1},
		{"following only", c, domain.RelationFollowing, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := repo.relationCalls
			vo, err := svc.GetStats(ctx, tc.viewer, b)
			if err != nil {
				t.Fatalf("GetStats: %v", err)
			}
			if vo.UserID != b.String() || vo.FollowingCount != 1 || vo.FollowerCount != 2 || vo.Relation != tc.want {
				t.Errorf("vo = %+v, want following=1 follower=2 relation=%d", vo, tc.want)
			}
			if got := repo.relationCalls - before; got != tc.relationReads {
				t.Errorf("relation reads = %d, want %d", got, tc.relationReads)
			}
		})
	}

	// 被关注但没回关：relation=2
	if vo, err := svc.GetStats(ctx, b, c); err != nil || vo.Relation != domain.RelationFollowedBy {
		t.Fatalf("followed-by: vo=%+v err=%v", vo, err)
	}

	users.missing[b] = true
	if _, err := svc.GetStats(ctx, a, b); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("missing user err = %v, want ErrUserNotFound", err)
	}
	delete(users.missing, b)

	repo.statErr = errBoom
	if _, err := svc.GetStats(ctx, a, b); !errors.Is(err, errBoom) {
		t.Fatalf("stat error: err = %v, want boom", err)
	}
	repo.statErr, repo.relationErr = nil, errBoom
	if _, err := svc.GetStats(ctx, a, b); !errors.Is(err, errBoom) {
		t.Fatalf("relation error: err = %v, want boom", err)
	}
}

func mustFollow(t *testing.T, svc FollowService, follower, followee uuid.UUID) {
	t.Helper()
	if _, err := svc.Follow(context.Background(), follower, followee); err != nil {
		t.Fatalf("follow %s -> %s: %v", follower, followee, err)
	}
}
