// Package composition 的 user_session_store_bridge.go：
// 实现 auth 领域的 UserSessionStore 接口，内部桥接到 user 领域。
//
// 这是"领域间通过 Facade 通信"模式的关键示例：
//   - auth.domain 定义 UserSessionStore 接口（只含 auth 关心的方法）；
//   - user.domain.SysUser 是完整用户实体（属于 user 领域）；
//   - composition 层写一个桥接器，把两者连起来。
//
// 这样 auth 包不 import user 包，user 包也不 import auth 包，
// 它们只通过 composition 层耦合。未来拆服务时，把桥接器换成 RPC 即可。
package composition

import (
	"strings"

	authdomain "interestBar/pkg/domains/auth/domain"
	userdomain "interestBar/pkg/domains/user/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// userSessionStoreBridge 把 user 领域的 SysUser 桥接到 auth 领域的 LoginUser。
//
// 它直接持有 *gorm.DB（过渡期），实现 auth.domain.UserSessionStore 接口。
// 未来可以把 DB 访问收口到 user 领域的 UserService，桥接器只做类型转换。
type userSessionStoreBridge struct {
	db *gorm.DB
}

// NewUserSessionStore 构造一个桥接到 user 领域的 UserSessionStore。
func NewUserSessionStore(db *gorm.DB) authdomain.UserSessionStore {
	return &userSessionStoreBridge{db: db}
}

// GetByEmail 按邮箱查询用户，转换为 LoginUser。
func (b *userSessionStoreBridge) GetByEmail(email string) (*authdomain.LoginUser, error) {
	var u userdomain.SysUser
	err := b.db.Where("email = ? AND deleted = ?", email, 0).First(&u).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return b.toLoginUser(&u), nil
}

// Create 创建用户（邮箱注册流程）。
func (b *userSessionStoreBridge) Create(input authdomain.CreateUserInput) (*authdomain.LoginUser, error) {
	u := userdomain.SysUser{
		ID:        sharedomain.NewID(),
		Username:  input.Username,
		Email:     input.Email,
		Pwd:       input.Pwd,
		AvatarURL: input.AvatarURL,
		Gender:    input.Gender,
		Role:      0,
		Status:    authdomain.UserStatusActive,
		Deleted:   0,
	}
	// OAuth 注册时设置 provider ID
	if input.Provider != "" && input.ProviderID != "" {
		applyProviderID(&u, input.Provider, input.ProviderID)
	}
	if err := b.db.Create(&u).Error; err != nil {
		return nil, err
	}
	return b.toLoginUser(&u), nil
}

// FindOrCreateForOAuth 按 provider ID 或 email 查找用户；不存在则创建；
// 若按 email 匹配但缺 provider ID 则补写。
//
// 与旧 controller/oauth.go 的 Callback 逻辑完全一致。
func (b *userSessionStoreBridge) FindOrCreateForOAuth(lookup authdomain.OAuthUserLookup) (*authdomain.LoginUser, error) {
	var u userdomain.SysUser
	result := b.db.Where(
		"("+lookup.LookupField+" = ? OR email = ?) AND deleted = ?",
		lookup.ProviderID, lookup.Email, 0,
	).First(&u)

	if result.Error != nil {
		if result.Error != gorm.ErrRecordNotFound {
			return nil, result.Error
		}
		// 不存在 → 创建
		username := lookup.Name
		if username == "" {
			username = strings.Split(lookup.Email, "@")[0]
		}
		newUser := userdomain.SysUser{
			ID:        sharedomain.NewID(),
			Username:  username,
			Email:     lookup.Email,
			AvatarURL: lookup.AvatarURL,
			Role:      0,
			Status:    authdomain.UserStatusActive,
			Deleted:   0,
		}
		applyProviderID(&newUser, lookup.Provider, lookup.ProviderID)

		if err := b.db.Create(&newUser).Error; err != nil {
			return nil, err
		}
		return b.toLoginUser(&newUser), nil
	}

	// 已存在 → 若按 email 匹配但 provider ID 缺失，则补写
	if getProviderID(&u, lookup.Provider) == "" {
		applyProviderID(&u, lookup.Provider, lookup.ProviderID)
		if err := b.db.Save(&u).Error; err != nil {
			// 补写失败：返回错误中止本次登录。
			// 宁可本次登录失败，也不能让 provider ID 缺失导致下次按 email 再次命中并重复创建用户。
			return nil, err
		}
	}
	return b.toLoginUser(&u), nil
}

// UpdatePassword 更新指定用户的密码哈希（仅更新 pwd 字段）。
//
// 用于密码哈希算法透明升级：登录时若发现 pwd 是旧 SHA256 格式，
// 校验成功后调用此方法把哈希替换为新的 Argon2id PHC 串。
func (b *userSessionStoreBridge) UpdatePassword(userID, newHash string) error {
	id, err := uuid.Parse(userID)
	if err != nil {
		return err
	}
	return b.db.Model(&userdomain.SysUser{}).
		Where("id = ? AND deleted = ?", id, 0).
		Update("pwd", newHash).Error
}

// toLoginUser 把 user 领域的 SysUser 转成 auth 领域的 LoginUser。
func (b *userSessionStoreBridge) toLoginUser(u *userdomain.SysUser) *authdomain.LoginUser {
	return &authdomain.LoginUser{
		ID:          u.ID.String(),
		Username:    u.Username,
		Email:       u.Email,
		Phone:       u.Phone,
		Pwd:         u.Pwd,
		GoogleID:    u.GoogleID,
		XID:         u.XID,
		GithubID:    u.GithubID,
		MicrosoftID: u.MicrosoftID,
		AvatarURL:   u.AvatarURL,
		Gender:      u.Gender,
		Status:      u.Status,
	}
}

// applyProviderID 按 provider name 设置 SysUser 对应字段。
func applyProviderID(u *userdomain.SysUser, provider, id string) {
	switch provider {
	case "google":
		u.GoogleID = id
	case "github":
		u.GithubID = id
	case "azure", "microsoft":
		u.MicrosoftID = id
	}
}

// getProviderID 按 provider name 读取 SysUser 对应字段。
func getProviderID(u *userdomain.SysUser, provider string) string {
	switch provider {
	case "google":
		return u.GoogleID
	case "github":
		return u.GithubID
	case "azure", "microsoft":
		return u.MicrosoftID
	}
	return ""
}

// 编译期保证：userSessionStoreBridge 实现了 authdomain.UserSessionStore。
var _ authdomain.UserSessionStore = (*userSessionStoreBridge)(nil)
