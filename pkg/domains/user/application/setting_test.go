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
	getErr, setErr error
	sets           int
}

func (r *fakeSettingRepo) Get(_ context.Context, userID uuid.UUID) (*domain.UserSetting, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return &domain.UserSetting{UserID: userID, FollowListVisibility: r.rows[userID]}, nil
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
