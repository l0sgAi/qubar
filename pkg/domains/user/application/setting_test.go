package application

import (
	"context"
	"errors"
	"testing"

	"interestBar/pkg/domains/user/domain"

	"github.com/google/uuid"
)

// fakeSettingRepo 内存版设置仓储：没有行返回默认值，语义与 PG 实现一致。
type fakeSettingRepo struct {
	rows           map[uuid.UUID]int16
	dm             map[uuid.UUID]int16 // 私信权限（#240），nil 时按需建
	getErr, setErr error
	sets           int
}

func (r *fakeSettingRepo) Get(_ context.Context, userID uuid.UUID) (*domain.UserSetting, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return &domain.UserSetting{UserID: userID, FollowListVisibility: r.rows[userID], DMPermission: r.dm[userID]}, nil
}

func (r *fakeSettingRepo) SetDMPermission(_ context.Context, userID uuid.UUID, v int16) error {
	r.sets++
	if r.setErr != nil {
		return r.setErr
	}
	if r.dm == nil {
		r.dm = map[uuid.UUID]int16{}
	}
	r.dm[userID] = v
	return nil
}

func (r *fakeSettingRepo) SetFollowListVisibility(_ context.Context, userID uuid.UUID, v int16) error {
	r.sets++
	if r.setErr != nil {
		return r.setErr
	}
	r.rows[userID] = v
	return nil
}

func intPtr(v int) *int { return &v }

func TestSettings_DefaultUpdateAndValidation(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSettingRepo{rows: map[uuid.UUID]int16{}}
	svc := NewSettingService(repo)
	a, b := uuid.New(), uuid.New()

	// 没有设置行：默认公开
	if vo, err := svc.GetSettings(ctx, a); err != nil || vo.FollowListVisibility != domain.FollowListPublic {
		t.Fatalf("default: vo=%+v err=%v, want public", vo, err)
	}

	vo, err := svc.UpdateSettings(ctx, a, UpdateSettingsInput{FollowListVisibility: intPtr(1)})
	if err != nil || vo.FollowListVisibility != domain.FollowListPrivate {
		t.Fatalf("set private: vo=%+v err=%v", vo, err)
	}
	if got, _ := svc.GetSettings(ctx, b); got.FollowListVisibility != domain.FollowListPublic {
		t.Errorf("another user's setting changed: %+v", got)
	}
	if vo, err = svc.UpdateSettings(ctx, a, UpdateSettingsInput{FollowListVisibility: intPtr(0)}); err != nil ||
		vo.FollowListVisibility != domain.FollowListPublic {
		t.Fatalf("set public again: vo=%+v err=%v", vo, err)
	}

	// 非法值（含会溢出成 0 / 1 的值）与空请求：不写库
	sets := repo.sets
	for _, bad := range []int{-1, 2, 65536, 65537} {
		if _, err = svc.UpdateSettings(ctx, a, UpdateSettingsInput{FollowListVisibility: intPtr(bad)}); !IsFollowListVisibilityErr(err) {
			t.Errorf("value %d: err = %v, want follow_list_visibility error", bad, err)
		}
	}
	if _, err = svc.UpdateSettings(ctx, a, UpdateSettingsInput{}); !IsNoSettingFieldErr(err) {
		t.Errorf("empty input: err = %v, want no-field error", err)
	}
	if repo.sets != sets {
		t.Errorf("invalid input reached the repo (%d writes)", repo.sets-sets)
	}
}

func TestSettings_RepoErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	repo := &fakeSettingRepo{rows: map[uuid.UUID]int16{}, setErr: boom}
	svc := NewSettingService(repo)
	if _, err := svc.UpdateSettings(ctx, uuid.New(), UpdateSettingsInput{FollowListVisibility: intPtr(1)}); !errors.Is(err, boom) {
		t.Errorf("set error: err = %v, want boom", err)
	}
	repo.setErr, repo.getErr = nil, boom
	if _, err := svc.GetSettings(ctx, uuid.New()); !errors.Is(err, boom) {
		t.Errorf("get error: err = %v, want boom", err)
	}
	if _, err := svc.UpdateSettings(ctx, uuid.New(), UpdateSettingsInput{FollowListVisibility: intPtr(1)}); !errors.Is(err, boom) {
		t.Errorf("read-back error: err = %v, want boom", err)
	}
}

func TestSettings_DMPermission(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSettingRepo{rows: map[uuid.UUID]int16{}}
	svc := NewSettingService(repo)
	a, b := uuid.New(), uuid.New()

	// 没有设置行：默认所有人
	if vo, err := svc.GetSettings(ctx, a); err != nil || vo.DMPermission != domain.DMPermissionEveryone {
		t.Fatalf("default: vo=%+v err=%v, want everyone", vo, err)
	}
	for _, v := range []int16{
		domain.DMPermissionFollowing, domain.DMPermissionMutual, domain.DMPermissionClosed, domain.DMPermissionEveryone,
	} {
		vo, err := svc.UpdateSettings(ctx, a, UpdateSettingsInput{DMPermission: intPtr(int(v))})
		if err != nil || vo.DMPermission != v || vo.FollowListVisibility != domain.FollowListPublic {
			t.Fatalf("set %d: vo=%+v err=%v", v, vo, err)
		}
	}
	if got, _ := svc.GetSettings(ctx, b); got.DMPermission != domain.DMPermissionEveryone {
		t.Errorf("another user's setting changed: %+v", got)
	}

	// 两项一起改
	vo, err := svc.UpdateSettings(ctx, a, UpdateSettingsInput{FollowListVisibility: intPtr(1), DMPermission: intPtr(3)})
	if err != nil || vo.FollowListVisibility != domain.FollowListPrivate || vo.DMPermission != domain.DMPermissionClosed {
		t.Fatalf("set both: vo=%+v err=%v", vo, err)
	}

	// 非法值（含会溢出成合法值的值）：不写库；另一项合法也不写
	sets := repo.sets
	for _, bad := range []int{-1, 4, 65536, 65539} {
		if _, err = svc.UpdateSettings(ctx, a, UpdateSettingsInput{DMPermission: intPtr(bad)}); !IsDMPermissionErr(err) {
			t.Errorf("value %d: err = %v, want dm_permission error", bad, err)
		}
	}
	if _, err = svc.UpdateSettings(ctx, a, UpdateSettingsInput{FollowListVisibility: intPtr(0), DMPermission: intPtr(9)}); !IsDMPermissionErr(err) {
		t.Errorf("mixed: err = %v, want dm_permission error", err)
	}
	if repo.sets != sets {
		t.Errorf("invalid input reached the repo (%d writes)", repo.sets-sets)
	}
	if got, _ := svc.GetSettings(ctx, a); got.FollowListVisibility != domain.FollowListPrivate || got.DMPermission != domain.DMPermissionClosed {
		t.Errorf("settings changed by invalid input: %+v", got)
	}
}
