//go:build integration

// interaction_loaded_integration_test.go 交互 ZSET 完整加载标记（#129）对开发栈 Redis 的集成测试（make test-integration）：
// 批查语义、标记与 ZSET 同刻过期、设值脚本的续期与容量淘汰降级。
package redis_test

import (
	"strconv"
	"testing"
	"time"

	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func newUser(t *testing.T) uuid.UUID {
	t.Helper()
	u := sharedomain.NewID()
	testkit.CleanupKeys(t,
		redispkg.GetUserPostLikeListKey(u), redispkg.GetUserPostLikeLoadedKey(u),
		redispkg.GetUserPostCollectListKey(u), redispkg.GetUserPostCollectLoadedKey(u),
		redispkg.GetUserCommentLikeListKey(u), redispkg.GetUserCommentLikeLoadedKey(u),
	)
	return u
}

func TestBatchCheck_UnloadedCompleteOverflow(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	user := newUser(t)
	liked, notLiked := sharedomain.NewID(), sharedomain.NewID()
	ids := []uuid.UUID{liked, notLiked}

	chk, err := redispkg.BatchCheckPostLiked(ctx, user, ids)
	if err != nil || chk.State != redispkg.InteractionUnloaded || len(chk.Missed) != 2 {
		t.Fatalf("cold check = %+v, %v; want unloaded with 2 misses", chk, err)
	}

	at := time.Now().Add(-time.Hour)
	if err := redispkg.LoadPostLikes(ctx, user, []redispkg.InteractionEntry{{ID: liked, At: at}}, true); err != nil {
		t.Fatalf("LoadPostLikes: %v", err)
	}
	chk, err = redispkg.BatchCheckPostLiked(ctx, user, ids)
	if err != nil || chk.State != redispkg.InteractionComplete || len(chk.Missed) != 0 || !chk.Hits[liked] || chk.Hits[notLiked] {
		t.Fatalf("loaded check = %+v, %v", chk, err)
	}
	// score 取交互时间（推荐种子按 score 取最近点赞）
	zsetKey, loadedKey := redispkg.GetUserPostLikeListKey(user), redispkg.GetUserPostLikeLoadedKey(user)
	if s := redispkg.Client.ZScore(ctx, zsetKey, liked.String()).Val(); int64(s) != at.UnixMilli() {
		t.Fatalf("score = %v, want %d", s, at.UnixMilli())
	}
	// 批查续期后两者过期时刻一致
	if zt, lt := redispkg.Client.PExpireTime(ctx, zsetKey).Val(), redispkg.Client.PExpireTime(ctx, loadedKey).Val(); zt <= 0 || zt != lt {
		t.Fatalf("PEXPIRETIME zset=%v marker=%v, want equal", zt, lt)
	}

	// 0 条记录也是完整加载：仅标记存在，缺失即 false
	empty := newUser(t)
	if err := redispkg.LoadPostCollects(ctx, empty, nil, true); err != nil {
		t.Fatalf("LoadPostCollects(empty): %v", err)
	}
	if chk, err = redispkg.BatchCheckPostCollected(ctx, empty, ids); err != nil || chk.State != redispkg.InteractionComplete || len(chk.Missed) != 0 {
		t.Fatalf("empty complete check = %+v, %v", chk, err)
	}

	// 超限：只写 "0"，不动 ZSET，miss 仍需回源且不再全量装载
	big := newUser(t)
	if err := redispkg.LoadCommentLikes(ctx, big, []redispkg.InteractionEntry{{ID: liked, At: at}}, false); err != nil {
		t.Fatalf("LoadCommentLikes(overflow): %v", err)
	}
	if n := redispkg.Client.Exists(ctx, redispkg.GetUserCommentLikeListKey(big)).Val(); n != 0 {
		t.Fatal("overflow load must not touch the ZSET")
	}
	if chk, err = redispkg.BatchCheckCommentLiked(ctx, big, ids); err != nil || chk.State != redispkg.InteractionOverflow || len(chk.Missed) != 2 {
		t.Fatalf("overflow check = %+v, %v", chk, err)
	}
}

// 装载用 ZADD NX / SET NX：已在 ZSET 的成员（尚未落库的新赞）保留原 score，已有标记不被覆盖。
func TestLoad_KeepsExistingMembersAndMarker(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	user := newUser(t)
	postID := sharedomain.NewID()
	zsetKey, loadedKey := redispkg.GetUserPostLikeListKey(user), redispkg.GetUserPostLikeLoadedKey(user)

	if _, err := redispkg.SetPostLike(ctx, user, postID, true); err != nil {
		t.Fatalf("SetPostLike: %v", err)
	}
	testkit.CleanupKeys(t, redispkg.GetPostStatsKey(postID))
	before := redispkg.Client.ZScore(ctx, zsetKey, postID.String()).Val()
	if err := redispkg.LoadPostLikes(ctx, user, []redispkg.InteractionEntry{{ID: postID, At: time.Unix(1, 0)}}, true); err != nil {
		t.Fatalf("LoadPostLikes: %v", err)
	}
	if after := redispkg.Client.ZScore(ctx, zsetKey, postID.String()).Val(); after != before {
		t.Fatalf("score changed %v -> %v, want kept", before, after)
	}

	redispkg.Client.Set(ctx, loadedKey, "0", time.Minute)
	if err := redispkg.LoadPostLikes(ctx, user, nil, true); err != nil {
		t.Fatalf("LoadPostLikes: %v", err)
	}
	if v := redispkg.Client.Get(ctx, loadedKey).Val(); v != "0" {
		t.Fatalf("marker = %q, want existing overflow marker kept", v)
	}
}

// 设值脚本：取消到 ZSET 为空时标记仍完整（缺失即未赞）并与 ZSET 同步续期。
func TestSetScripts_RenewMarkerWithZset(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	user := newUser(t)
	postID := sharedomain.NewID()
	testkit.CleanupKeys(t, redispkg.GetPostStatsKey(postID))
	zsetKey, loadedKey := redispkg.GetUserPostCollectListKey(user), redispkg.GetUserPostCollectLoadedKey(user)

	if err := redispkg.LoadPostCollects(ctx, user, nil, true); err != nil {
		t.Fatalf("LoadPostCollects: %v", err)
	}
	redispkg.Client.Expire(ctx, loadedKey, time.Minute) // 模拟标记即将过期
	if _, err := redispkg.SetPostCollect(ctx, user, postID, true); err != nil {
		t.Fatalf("SetPostCollect(true): %v", err)
	}
	if zt, lt := redispkg.Client.TTL(ctx, zsetKey).Val(), redispkg.Client.TTL(ctx, loadedKey).Val(); lt <= time.Minute || lt > zt {
		t.Fatalf("TTL zset=%v marker=%v, want marker renewed and <= zset", zt, lt)
	}
	if _, err := redispkg.SetPostCollect(ctx, user, postID, false); err != nil {
		t.Fatalf("SetPostCollect(false): %v", err)
	}
	chk, err := redispkg.BatchCheckPostCollected(ctx, user, []uuid.UUID{postID})
	if err != nil || chk.State != redispkg.InteractionComplete || chk.Hits[postID] || len(chk.Missed) != 0 {
		t.Fatalf("after uncollect = %+v, %v", chk, err)
	}
}

// 容量淘汰后 ZSET 不再是全量：设值脚本必须把完整标记降为超限，避免"标记在、ZSET 缺成员"。
func TestSetScripts_EvictionDowngradesMarker(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	ctx := t.Context()
	user := newUser(t)
	zsetKey, loadedKey := redispkg.GetUserPostLikeListKey(user), redispkg.GetUserPostLikeLoadedKey(user)

	const zsetCap = 2000 // likeZsetMaxSize
	oldest := sharedomain.NewID()
	members := []redis.Z{{Score: 1, Member: oldest.String()}}
	for i := 1; i < zsetCap; i++ {
		members = append(members, redis.Z{Score: float64(1000 + i), Member: "seed-" + strconv.Itoa(i)})
	}
	redispkg.Client.ZAdd(ctx, zsetKey, members...)
	if err := redispkg.LoadPostLikes(ctx, user, nil, true); err != nil {
		t.Fatalf("LoadPostLikes: %v", err)
	}

	postID := sharedomain.NewID()
	testkit.CleanupKeys(t, redispkg.GetPostStatsKey(postID))
	if _, err := redispkg.SetPostLike(ctx, user, postID, true); err != nil {
		t.Fatalf("SetPostLike: %v", err)
	}
	if v := redispkg.Client.Get(ctx, loadedKey).Val(); v != "0" {
		t.Fatalf("marker = %q after eviction, want overflow", v)
	}
	if ttl := redispkg.Client.TTL(ctx, loadedKey).Val(); ttl <= 0 {
		t.Fatalf("marker TTL = %v, want kept", ttl)
	}
	chk, err := redispkg.BatchCheckPostLiked(ctx, user, []uuid.UUID{oldest, postID})
	if err != nil || chk.State != redispkg.InteractionOverflow || chk.Hits[oldest] || len(chk.Missed) != 1 || chk.Missed[0] != oldest {
		t.Fatalf("after eviction = %+v, %v; evicted member must be a miss", chk, err)
	}
}
