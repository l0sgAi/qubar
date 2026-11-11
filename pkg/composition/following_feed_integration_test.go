//go:build integration

// following_feed_integration_test.go 首页关注 tab（#220）的 recommend ← follow 桥接集成测试：真实 PG + Redis，
// 经 composition 装配的 FollowService 关注 / 取关后，recommendFolloweeLookup 立即读到新的关注 ID
// （普通用户与 AI 机器人账号都算；别人的关注、关注我的人不算）。复用 follow_integration_test.go 的 followEnv。
// ES 检索本身见 pkg/server/storage/elasticsearch/following_feed_integration_test.go。
package composition

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestFollowingFeed_FolloweeLookupReflectsFollowAndUnfollow(t *testing.T) {
	t.Parallel()
	e := setupFollow(t)
	ctx := t.Context()
	me, user, bot, fan := e.insertUser(t, 0, 1, 0), e.insertUser(t, 0, 1, 0), e.insertUser(t, 2, 1, 0), e.insertUser(t, 0, 1, 0)
	lookup := &recommendFolloweeLookup{delegate: e.svc}

	followees := func() []uuid.UUID {
		t.Helper()
		ids, err := lookup.ListFolloweeIDs(ctx, me)
		if err != nil {
			t.Fatalf("ListFolloweeIDs: %v", err)
		}
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
		return ids
	}
	sorted := func(ids ...uuid.UUID) []uuid.UUID {
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
		return ids
	}

	if got := followees(); len(got) != 0 {
		t.Fatalf("before following anyone: got %v, want empty", got)
	}

	for _, target := range []uuid.UUID{user, bot} {
		if _, err := e.svc.Follow(ctx, me, target); err != nil {
			t.Fatalf("follow %s: %v", target, err)
		}
	}
	// 干扰关系：别人关注我、别人关注 user，都不属于「我关注的人」
	if _, err := e.svc.Follow(ctx, fan, me); err != nil {
		t.Fatalf("fan follows me: %v", err)
	}
	if _, err := e.svc.Follow(ctx, fan, user); err != nil {
		t.Fatalf("fan follows user: %v", err)
	}
	if got, want := followees(), sorted(user, bot); !slices.Equal(got, want) {
		t.Fatalf("after follow: got %v, want %v (user + bot)", got, want)
	}

	// 取关后下一次读取即不再包含
	if _, err := e.svc.Unfollow(ctx, me, user); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	if got, want := followees(), []uuid.UUID{bot}; !slices.Equal(got, want) {
		t.Fatalf("after unfollow: got %v, want %v", got, want)
	}
}
