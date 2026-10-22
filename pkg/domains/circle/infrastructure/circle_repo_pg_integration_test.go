//go:build integration

// circle_repo_pg_integration_test.go 圈子仓储对开发栈 PostgreSQL 的集成测试（make test-integration）：
// 不填 slug 连续建圈不碰撞、name/slug 唯一索引冲突映射为领域哨兵错误。
package infrastructure_test

import (
	"errors"
	"testing"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/domains/circle/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"

	"github.com/google/uuid"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// newCircle 构造一个随机 name 的圈子（slug 由调用方指定），并注册圈子与成员行清理。
func newCircle(t *testing.T, slug string) *domain.Circle {
	t.Helper()
	id := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.circle_member", "circle_id", id)
	testkit.CleanupRows(t, "domains.circle", "id", id)
	return &domain.Circle{
		ID:          id,
		Name:        "it-" + id.String(),
		Slug:        slug,
		Description: "集成测试圈子",
		CreatorID:   sharedomain.NewID(),
	}
}

func TestCircleRepo_Create_EmptySlugStoresNull(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewCircleRepository(db)

	a, b := newCircle(t, ""), newCircle(t, "")
	for _, c := range []*domain.Circle{a, b} {
		if err := repo.Create(t.Context(), c); err != nil {
			t.Fatalf("create circle without slug: %v", err)
		}
	}

	for _, c := range []*domain.Circle{a, b} {
		var isNull bool
		if err := db.Raw("SELECT slug IS NULL FROM domains.circle WHERE id = ?", c.ID).Scan(&isNull).Error; err != nil {
			t.Fatalf("query slug: %v", err)
		}
		if !isNull {
			t.Errorf("circle %s: slug should be NULL", c.ID)
		}
		got, err := repo.GetByID(t.Context(), c.ID)
		if err != nil {
			t.Fatalf("get circle: %v", err)
		}
		if got.Slug != "" {
			t.Errorf("circle %s: slug = %q, want empty", c.ID, got.Slug)
		}
	}
}

func TestCircleRepo_Create_UniqueConflicts(t *testing.T) {
	t.Parallel()
	repo := infrastructure.NewCircleRepository(testkit.PG(t))

	slug := "s-" + uuid.NewString()[:30]
	first := newCircle(t, slug)
	if err := repo.Create(t.Context(), first); err != nil {
		t.Fatalf("create first: %v", err)
	}

	dupSlug := newCircle(t, slug)
	if err := repo.Create(t.Context(), dupSlug); !errors.Is(err, domain.ErrCircleSlugExists) {
		t.Errorf("duplicate slug: err = %v, want ErrCircleSlugExists", err)
	}

	dupName := newCircle(t, "")
	dupName.Name = first.Name
	if err := repo.Create(t.Context(), dupName); !errors.Is(err, domain.ErrCircleNameExists) {
		t.Errorf("duplicate name: err = %v, want ErrCircleNameExists", err)
	}
}
