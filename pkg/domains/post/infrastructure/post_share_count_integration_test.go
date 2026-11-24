//go:build integration

// post_share_count_integration_test.go 帖子分享数 / 分享状态查询（#233）对开发栈 PostgreSQL 的集成测试（make test-integration）。
package infrastructure_test

import (
	"maps"
	"testing"

	"interestBar/pkg/domains/post/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestPostRepo_ShareCountsAndShared(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewPostRepository(db)
	ctx := t.Context()

	shared := newPost(t, repo, nil)
	plain := newPost(t, repo, nil)
	testkit.CleanupRows(t, "domains.post_share", "post_id", shared.ID, plain.ID)
	if err := db.Exec("UPDATE domains.post SET share_count = 4 WHERE id = ?", shared.ID).Error; err != nil {
		t.Fatalf("set share_count: %v", err)
	}
	user, other := sharedomain.NewID(), sharedomain.NewID()
	if err := db.Exec("INSERT INTO domains.post_share (id, user_id, post_id) VALUES (?, ?, ?)",
		sharedomain.NewID(), user, shared.ID).Error; err != nil {
		t.Fatalf("insert post_share: %v", err)
	}

	// 实体读到 share_count 列
	if p, err := repo.GetByID(ctx, shared.ID); err != nil || p.ShareCount != 4 {
		t.Fatalf("GetByID share_count = %+v, %v; want 4", p, err)
	}

	missing := sharedomain.NewID()
	counts, err := repo.GetShareCounts(ctx, []uuid.UUID{shared.ID, plain.ID, missing})
	if err != nil || !maps.Equal(counts, map[uuid.UUID]int64{shared.ID: 4, plain.ID: 0}) {
		t.Fatalf("GetShareCounts = %v, %v; want shared=4 plain=0, missing absent", counts, err)
	}
	if got, err := repo.GetShareCounts(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("GetShareCounts(nil) = %v, %v", got, err)
	}

	for _, c := range []struct {
		user, post uuid.UUID
		want       bool
	}{
		{user, shared.ID, true},
		{user, plain.ID, false},
		{other, shared.ID, false},
	} {
		if got, err := repo.IsShared(ctx, c.user, c.post); err != nil || got != c.want {
			t.Fatalf("IsShared(%v, %v) = %v, %v; want %v", c.user, c.post, got, err, c.want)
		}
	}
	got, err := repo.BatchIsShared(ctx, user, []uuid.UUID{shared.ID, plain.ID})
	if err != nil || !maps.Equal(got, map[uuid.UUID]bool{shared.ID: true}) {
		t.Fatalf("BatchIsShared = %v, %v", got, err)
	}
	if got, err := repo.BatchIsShared(ctx, other, []uuid.UUID{shared.ID}); err != nil || len(got) != 0 {
		t.Fatalf("BatchIsShared(other) = %v, %v", got, err)
	}
}
