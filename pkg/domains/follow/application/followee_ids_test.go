package application

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// fakeFolloweeIDs 记录入参并返回预设的关注 ID。
type fakeFolloweeIDs struct {
	ids         []uuid.UUID
	err         error
	calls       int
	gotFollower uuid.UUID
	gotLimit    int
}

func (r *fakeFolloweeIDs) ListFolloweeIDs(_ context.Context, followerID uuid.UUID, limit int) ([]uuid.UUID, error) {
	r.calls++
	r.gotFollower, r.gotLimit = followerID, limit
	return r.ids, r.err
}

func newFolloweeSvc(repo *fakeFolloweeIDs, limit int) FollowService {
	return NewFollowService(nil, nil, nil, func() int { return limit }, repo, ListDeps{})
}

// 上限取当前生效的 follow.max_following，结果原样返回（顺序由仓储决定）。
func TestListFolloweeIDs_UsesMaxFollowingAsLimit(t *testing.T) {
	repo := &fakeFolloweeIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}
	user := uuid.New()

	got, err := newFolloweeSvc(repo, 2000).ListFolloweeIDs(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotFollower != user || repo.gotLimit != 2000 || !slices.Equal(got, repo.ids) {
		t.Fatalf("follower=%s limit=%d got=%v", repo.gotFollower, repo.gotLimit, got)
	}
}

// 访客（uuid.Nil）没有关注关系：直接返回空，不查库。
func TestListFolloweeIDs_NilUser(t *testing.T) {
	repo := &fakeFolloweeIDs{ids: []uuid.UUID{uuid.New()}}
	got, err := newFolloweeSvc(repo, 2000).ListFolloweeIDs(context.Background(), uuid.Nil)
	if err != nil || len(got) != 0 || repo.calls != 0 {
		t.Fatalf("err=%v got=%v calls=%d", err, got, repo.calls)
	}
}

// 仓储错误原样返回，由调用方（首页关注 tab）决定是否降级。
func TestListFolloweeIDs_RepoError(t *testing.T) {
	repo := &fakeFolloweeIDs{err: errBoom}
	if _, err := newFolloweeSvc(repo, 2000).ListFolloweeIDs(context.Background(), uuid.New()); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}
}
