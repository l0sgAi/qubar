//go:build integration

// user_setting_dm_permission_integration_test.go 私信权限设置列（#240）的集成测试：
// 没有行时默认所有人、首次修改建行、与关注列表可见性按列独立 upsert 互不覆盖、值不变时不产生写入。
package infrastructure_test

import (
	"testing"
	"time"

	"interestBar/pkg/domains/user/domain"
	"interestBar/pkg/domains/user/infrastructure"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/shared/testkit"
)

func TestSettingRepo_DMPermission(t *testing.T) {
	t.Parallel()
	db := testkit.PG(t)
	repo := infrastructure.NewSettingRepository(db)
	ctx := t.Context()
	userID := sharedomain.NewID()
	testkit.CleanupRows(t, "domains.user_setting", "user_id", userID)

	get := func() *domain.UserSetting {
		t.Helper()
		got, err := repo.Get(ctx, userID)
		if err != nil || got == nil {
			t.Fatalf("Get: %+v err=%v", got, err)
		}
		return got
	}

	// 没有行：默认所有人
	if got := get(); got.DMPermission != domain.DMPermissionEveryone {
		t.Fatalf("default dm_permission = %d", got.DMPermission)
	}
	// 首次修改建行；另一列保持默认
	if err := repo.SetDMPermission(ctx, userID, domain.DMPermissionClosed); err != nil {
		t.Fatal(err)
	}
	if got := get(); got.DMPermission != domain.DMPermissionClosed || got.FollowListVisibility != domain.FollowListPublic {
		t.Fatalf("after set closed: %+v", got)
	}
	// 改另一列不覆盖本列，改本列不覆盖另一列
	if err := repo.SetFollowListVisibility(ctx, userID, domain.FollowListPrivate); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetDMPermission(ctx, userID, domain.DMPermissionMutual); err != nil {
		t.Fatal(err)
	}
	got := get()
	if got.DMPermission != domain.DMPermissionMutual || got.FollowListVisibility != domain.FollowListPrivate {
		t.Fatalf("after both columns: %+v", got)
	}
	// 值不变：不产生写入（update_time 不变）
	time.Sleep(10 * time.Millisecond)
	if err := repo.SetDMPermission(ctx, userID, domain.DMPermissionMutual); err != nil {
		t.Fatal(err)
	}
	if again := get(); !again.UpdateTime.Equal(got.UpdateTime) {
		t.Errorf("same value rewrote the row: update_time %v -> %v", got.UpdateTime, again.UpdateTime)
	}
}
