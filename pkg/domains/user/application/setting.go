package application

import (
	"context"

	"interestBar/pkg/domains/user/domain"

	"github.com/google/uuid"
)

// SettingService 用户设置（个人中心的开关类设置）的读写。只操作 userID 自己的设置，
// 调用方（handler）保证 userID 是当前登录用户。
//
// 跨域读取（如 follow 域判断列表是否私密）也走本接口，由 composition 桥接，不直接查表。
type SettingService interface {
	// GetSettings 返回用户当前的全部设置；从未修改过的设置项为默认值。
	GetSettings(ctx context.Context, userID uuid.UUID) (*SettingsVO, error)
	// UpdateSettings 部分更新：只改 input 里非 nil 的设置项，返回更新后的全部设置。
	// 一个设置项都没传、或取值非法时返回可识别错误（见 errors.go）。
	UpdateSettings(ctx context.Context, userID uuid.UUID, input UpdateSettingsInput) (*SettingsVO, error)
}

// SettingsVO 用户设置视图。
type SettingsVO struct {
	// FollowListVisibility 关注列表 / 粉丝列表可见性：0=公开（默认），1=私密（仅本人可看名单）。
	FollowListVisibility int16 `json:"follow_list_visibility"`
	// DMPermission 谁可以给我发私信（#240）：0=所有人（默认），1=仅我关注的人，2=仅互相关注，3=关闭。
	DMPermission int16 `json:"dm_permission"`
}

// UpdateSettingsInput 修改设置的入参（指针字段允许部分更新）。
type UpdateSettingsInput struct {
	FollowListVisibility *int
	DMPermission         *int
}

type settingServiceImpl struct {
	repo domain.SettingRepository
}

// NewSettingService 构造 SettingService。
func NewSettingService(repo domain.SettingRepository) SettingService {
	return &settingServiceImpl{repo: repo}
}

func (s *settingServiceImpl) GetSettings(ctx context.Context, userID uuid.UUID) (*SettingsVO, error) {
	setting, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &SettingsVO{FollowListVisibility: setting.FollowListVisibility, DMPermission: setting.DMPermission}, nil
}

func (s *settingServiceImpl) UpdateSettings(ctx context.Context, userID uuid.UUID, input UpdateSettingsInput) (*SettingsVO, error) {
	if input.FollowListVisibility == nil && input.DMPermission == nil {
		return nil, errNoSettingField
	}
	// 先校验全部传入项再写入：任一项非法时什么都不改。
	// 先按 int 判断范围再转 int16，避免 65536 这类值溢出成合法值。
	if v := input.FollowListVisibility; v != nil && *v != int(domain.FollowListPublic) && *v != int(domain.FollowListPrivate) {
		return nil, errFollowListVisibility
	}
	if v := input.DMPermission; v != nil && (*v < int(domain.DMPermissionEveryone) || *v > int(domain.DMPermissionClosed)) {
		return nil, errDMPermission
	}
	// 一列一条 upsert（各自幂等）：同时传两项时分两条语句写，第二条失败时第一条已生效，客户端重试即可收敛。
	if v := input.FollowListVisibility; v != nil {
		if err := s.repo.SetFollowListVisibility(ctx, userID, int16(*v)); err != nil {
			return nil, err
		}
	}
	if v := input.DMPermission; v != nil {
		if err := s.repo.SetDMPermission(ctx, userID, int16(*v)); err != nil {
			return nil, err
		}
	}
	return s.GetSettings(ctx, userID)
}
