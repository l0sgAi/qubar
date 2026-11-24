//go:build integration

// share_integration_test.go 分享记录仓储（PG）与分享数缓存（Redis）对开发栈的集成测试（make test-integration，#232）。
package infrastructure_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"interestBar/pkg/domains/share/domain"
	"interestBar/pkg/domains/share/infrastructure"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func newShare(user, post uuid.UUID) *domain.PostShare {
	return &domain.PostShare{ID: sharedomain.NewID(), UserID: user, PostID: post}
}

// 一人一帖只插入一次：重复插入 created=false，记录数只按分享人计；Delete 只删指定行。
func TestShareRepo_CreateDedupAndCount(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostShareRepository(db)
	ctx := t.Context()
	postID := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post_share", "post_id", postID)
	a, b := sharedomain.NewID(), sharedomain.NewID()

	first := newShare(a, postID)
	for i, s := range []struct {
		share *domain.PostShare
		want  bool
	}{
		{first, true},
		{newShare(a, postID), false}, // 同一用户再分享：新主键也不插入
		{newShare(b, postID), true},
		{newShare(b, postID), false},
	} {
		created, err := repo.Create(ctx, s.share)
		if err != nil || created != s.want {
			t.Fatalf("step %d Create = %v, %v; want %v", i, created, err, s.want)
		}
	}
	if n, err := repo.CountByPost(ctx, postID); err != nil || n != 2 {
		t.Fatalf("CountByPost = %d, %v; want 2", n, err)
	}

	// 回滚只删刚插入的那行，另一个用户的记录保留
	if err := repo.Delete(ctx, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, err := repo.CountByPost(ctx, postID); err != nil || n != 1 {
		t.Fatalf("CountByPost after delete = %d, %v; want 1", n, err)
	}
	if created, err := repo.Create(ctx, newShare(a, postID)); err != nil || !created {
		t.Fatalf("re-share after rollback = %v, %v; want created", created, err)
	}
}

// 并发：同一用户同时插入只有一个成功（唯一索引兜底，不依赖应用层锁）。
func TestShareRepo_ConcurrentSameUser(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostShareRepository(db)
	ctx := t.Context()
	user, postID := sharedomain.NewID(), sharedomain.NewID()
	testkit.CleanupRows(t, "domains.post_share", "post_id", postID)

	var created atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			ok, err := repo.Create(ctx, newShare(user, postID))
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			if ok {
				created.Add(1)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("created = %d, want exactly 1", created.Load())
	}
	if n, err := repo.CountByPost(ctx, postID); err != nil || n != 1 {
		t.Fatalf("CountByPost = %d, %v; want 1", n, err)
	}
}

// 分享数缓存：冷 key 不自增；SET NX 回种不覆盖已有值；落库后校准只升不降。
func TestShareCountCache_SeedIncrReconcile(t *testing.T) {
	t.Parallel()
	testkit.Redis(t)
	cache := infrastructure.NewShareCountCache()
	ctx := t.Context()
	postID := sharedomain.NewID()
	key := redispkg.GetPostShareCountKey(postID)
	testkit.CleanupKeys(t, key)

	if _, ok, err := cache.IncrIfCached(ctx, postID); err != nil || ok {
		t.Fatalf("IncrIfCached on cold key = ok %v, %v; want not cached", ok, err)
	}
	if redispkg.Client.Exists(ctx, key).Val() != 0 {
		t.Fatal("IncrIfCached must not create a cold key")
	}
	if _, ok, err := cache.Get(ctx, postID); err != nil || ok {
		t.Fatalf("Get on cold key = ok %v, %v", ok, err)
	}

	if v, err := cache.Seed(ctx, postID, 3); err != nil || v != 3 {
		t.Fatalf("Seed = %d, %v; want 3", v, err)
	}
	if ttl := redispkg.Client.TTL(ctx, key).Val(); ttl <= 0 {
		t.Fatalf("seeded key TTL = %v, want > 0", ttl)
	}
	if v, err := cache.Seed(ctx, postID, 1); err != nil || v != 3 {
		t.Fatalf("second Seed = %d, %v; want existing 3 kept", v, err)
	}
	if v, ok, err := cache.IncrIfCached(ctx, postID); err != nil || !ok || v != 4 {
		t.Fatalf("IncrIfCached = %d, %v, %v; want 4", v, ok, err)
	}

	// 校准：低于 PG 抬高，高于 PG 不降；不存在的 key 跳过
	other := sharedomain.NewID()
	testkit.CleanupKeys(t, redispkg.GetPostShareCountKey(other))
	fixed, err := redispkg.ReconcilePostShareCounts(map[uuid.UUID]int64{postID: 6, other: 9})
	if err != nil || fixed != 1 {
		t.Fatalf("Reconcile up = %d, %v; want 1 fixed", fixed, err)
	}
	if v, _, _ := cache.Get(ctx, postID); v != 6 {
		t.Fatalf("after raise = %d, want 6", v)
	}
	if redispkg.Client.Exists(ctx, redispkg.GetPostShareCountKey(other)).Val() != 0 {
		t.Fatal("reconcile must not create missing keys")
	}
	if fixed, err := redispkg.ReconcilePostShareCounts(map[uuid.UUID]int64{postID: 2}); err != nil || fixed != 0 {
		t.Fatalf("Reconcile down = %d, %v; want no-op", fixed, err)
	}
	if v, _, _ := cache.Get(ctx, postID); v != 6 {
		t.Fatalf("after lower reconcile = %d, want 6 kept", v)
	}
}
