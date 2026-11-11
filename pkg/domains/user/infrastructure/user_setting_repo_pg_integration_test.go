//go:build integration

// user_setting_repo_pg_integration_test.go 用户设置仓储对开发栈 PostgreSQL 的集成测试（#218）：
// 没有设置行时取默认值、首次修改建行、重复修改只有一行、值不变时不产生写入。
package infrastructure_test

import (
	"testing"
	"time"

	"interestBar/pkg/domains/user/domain"
	"interestBar/pkg/domains/user/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func TestSettingRepo_DefaultUpsertAndToggle(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewSettingRepository(db)
	ctx := t.Context()
	userID, other := sharedomain.NewID(), sharedomain.NewID()
	testkit.CleanupRows(t, "domains.user_setting", "user_id", userID, other)

	rowCount := func() (n int64) {
		t.Helper()
		if err := db.WithContext(ctx).Model(&domain.UserSetting{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
			t.Fatalf("count rows: %v", err)
		}
		return n
	}

	// 没有行：默认公开，读取不建行
	got, err := repo.Get(ctx, userID)
	if err != nil || got == nil || got.UserID != userID || got.FollowListVisibility != domain.FollowListPublic {
		t.Fatalf("default: %+v err=%v, want public", got, err)
	}
	if rowCount() != 0 {
		t.Fatal("Get must not create a row")
	}

	// 首次修改建行
	if err = repo.SetFollowListVisibility(ctx, userID, domain.FollowListPrivate); err != nil {
		t.Fatalf("set private: %v", err)
	}
	got, err = repo.Get(ctx, userID)
	if err != nil || got.FollowListVisibility != domain.FollowListPrivate || got.CreateTime.IsZero() || rowCount() != 1 {
		t.Fatalf("after set private: %+v err=%v rows=%d", got, err, rowCount())
	}
	firstUpdate := got.UpdateTime

	// 值不变：不产生写入（update_time 不动）
	time.Sleep(5 * time.Millisecond)
	if err = repo.SetFollowListVisibility(ctx, userID, domain.FollowListPrivate); err != nil {
		t.Fatalf("repeat set: %v", err)
	}
	if got, _ = repo.Get(ctx, userID); !got.UpdateTime.Equal(firstUpdate) || rowCount() != 1 {
		t.Errorf("repeat set changed the row: update_time %v -> %v, rows=%d", firstUpdate, got.UpdateTime, rowCount())
	}

	// 改回公开：还是同一行，update_time 前进
	if err = repo.SetFollowListVisibility(ctx, userID, domain.FollowListPublic); err != nil {
		t.Fatalf("set public: %v", err)
	}
	got, err = repo.Get(ctx, userID)
	if err != nil || got.FollowListVisibility != domain.FollowListPublic || !got.UpdateTime.After(firstUpdate) || rowCount() != 1 {
		t.Errorf("after set public: %+v err=%v rows=%d", got, err, rowCount())
	}

	// 别的用户不受影响
	if got, err = repo.Get(ctx, other); err != nil || got.FollowListVisibility != domain.FollowListPublic {
		t.Errorf("other user: %+v err=%v, want public", got, err)
	}
}
