package application

import (
	"context"
	"errors"
	"os"
	"testing"

	"interestBar/pkg/domains/collect/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

// fakeCollectCache 模拟用户收藏 ZSET：inZset 之外的都算 miss。
type fakeCollectCache struct {
	inZset     map[uuid.UUID]bool
	setArgs    []bool
	backfilled []uuid.UUID
	setErr     error
}

func (c *fakeCollectCache) Set(_ context.Context, _, postID uuid.UUID, collected bool) (domain.ToggleResult, error) {
	c.setArgs = append(c.setArgs, collected)
	if c.setErr != nil {
		return 0, c.setErr
	}
	c.inZset[postID] = collected
	return domain.ToggleResultUnchanged, nil
}
func (c *fakeCollectCache) StatsExists(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (c *fakeCollectCache) BatchCheck(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, []uuid.UUID, error) {
	hits := map[uuid.UUID]bool{}
	var missed []uuid.UUID
	for _, id := range ids {
		hits[id] = c.inZset[id]
		if !c.inZset[id] {
			missed = append(missed, id)
		}
	}
	return hits, missed, nil
}
func (c *fakeCollectCache) Backfill(_ context.Context, _ uuid.UUID, ids []uuid.UUID) error {
	c.backfilled = append(c.backfilled, ids...)
	for _, id := range ids {
		c.inZset[id] = true
	}
	return nil
}

// fakeCollectRepo 模拟 post_collect 流水：SetCollected 只在状态迁移时返回 changed=true。
type fakeCollectRepo struct {
	rows  map[uuid.UUID]bool
	dbErr error
}

func (r *fakeCollectRepo) ListCollectedPostIDs(context.Context, uuid.UUID, string, int, string) ([]uuid.UUID, int64, string, error) {
	return nil, 0, "", nil
}
func (r *fakeCollectRepo) IsCollected(_ context.Context, _, postID uuid.UUID) (bool, error) {
	return r.rows[postID], r.dbErr
}
func (r *fakeCollectRepo) SetCollected(_ context.Context, _, postID uuid.UUID, active bool) (bool, error) {
	if r.dbErr != nil {
		return false, r.dbErr
	}
	changed := r.rows[postID] != active
	r.rows[postID] = active
	return changed, nil
}

type fakeCollectPublisher struct{ amounts []int64 }

func (p *fakeCollectPublisher) PublishPostCollect(_ context.Context, _, _ uuid.UUID, amount int64) error {
	p.amounts = append(p.amounts, amount)
	return nil
}

type fakePostTarget struct{}

func (fakePostTarget) Exists(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (fakePostTarget) RestoreStats(context.Context, uuid.UUID) error   { return nil }

func newCollectTestService(cache *fakeCollectCache, repo *fakeCollectRepo, pub *fakeCollectPublisher) CollectService {
	svc := NewCollectService(cache, repo, pub)
	svc.SetPostTarget(fakePostTarget{})
	return svc
}

func newFakes() (*fakeCollectCache, *fakeCollectRepo, *fakeCollectPublisher) {
	return &fakeCollectCache{inZset: map[uuid.UUID]bool{}}, &fakeCollectRepo{rows: map[uuid.UUID]bool{}}, &fakeCollectPublisher{}
}

// 回归 #46（收藏同病）：ZSET 过期但 DB 已收藏，点击应取消，而不是再收藏一次让 collect_count 漂移。
func TestCollectToggle_CacheMissButCollectedInDB_Uncollects(t *testing.T) {
	cache, repo, pub := newFakes()
	postID := uuid.New()
	repo.rows[postID] = true

	res, err := newCollectTestService(cache, repo, pub).Toggle(context.Background(), uuid.New(), ToggleInput{PostID: postID})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsCollected || repo.rows[postID] {
		t.Fatal("want uncollected")
	}
	if len(cache.backfilled) != 1 {
		t.Fatal("DB-confirmed state must be backfilled before setting")
	}
	if len(pub.amounts) != 1 || pub.amounts[0] != -1 {
		t.Fatalf("want one -1 event, got %v", pub.amounts)
	}
}

func TestCollectToggle_ExplicitActionIsIdempotent(t *testing.T) {
	cache, repo, pub := newFakes()
	svc := newCollectTestService(cache, repo, pub)
	postID, user := uuid.New(), uuid.New()

	for i := 0; i < 2; i++ {
		res, err := svc.Toggle(context.Background(), user, ToggleInput{PostID: postID, Action: "collect"})
		if err != nil || !res.IsCollected {
			t.Fatalf("attempt %d: %+v %v", i, res, err)
		}
	}
	if len(pub.amounts) != 1 || pub.amounts[0] != 1 {
		t.Fatalf("want exactly one +1 event, got %v", pub.amounts)
	}
}

func TestCollectToggle_DBFailureLeavesCacheUntouched(t *testing.T) {
	cache, repo, pub := newFakes()
	// 状态解析成功、流水写入失败
	svc := NewCollectService(cache, &failingSetRepo{fakeCollectRepo: repo}, pub)
	svc.SetPostTarget(fakePostTarget{})

	if _, err := svc.Toggle(context.Background(), uuid.New(), ToggleInput{PostID: uuid.New()}); err == nil {
		t.Fatal("want error")
	}
	if len(cache.setArgs) != 0 || len(pub.amounts) != 0 {
		t.Fatal("cache and events must not change when the DB write fails")
	}
}

type failingSetRepo struct{ *fakeCollectRepo }

func (r *failingSetRepo) SetCollected(context.Context, uuid.UUID, uuid.UUID, bool) (bool, error) {
	return false, errors.New("db down")
}

func TestCollectToggle_CacheFailureStillSucceeds(t *testing.T) {
	cache, repo, pub := newFakes()
	cache.setErr = errors.New("redis down")
	postID := uuid.New()

	res, err := newCollectTestService(cache, repo, pub).Toggle(context.Background(), uuid.New(), ToggleInput{PostID: postID})
	if err != nil || !res.IsCollected {
		t.Fatalf("DB is authoritative, want success: %+v %v", res, err)
	}
	if len(pub.amounts) != 1 {
		t.Fatal("event must follow the DB transition")
	}
}

func TestCollectToggle_InvalidAction(t *testing.T) {
	cache, repo, pub := newFakes()
	_, err := newCollectTestService(cache, repo, pub).Toggle(context.Background(), uuid.New(), ToggleInput{PostID: uuid.New(), Action: "like"})
	if !errors.Is(err, domain.ErrInvalidAction) {
		t.Fatalf("want ErrInvalidAction, got %v", err)
	}
}
