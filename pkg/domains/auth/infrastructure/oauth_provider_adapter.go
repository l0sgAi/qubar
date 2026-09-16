package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/auth/domain"
	"interestBar/pkg/server/auth"

	"golang.org/x/oauth2"
)

// oauthProviderAdapter 把现有的 pkg/server/auth.Provider 包装成
// domain.OAuthProvider 接口。
//
// 为什么用适配器而非重写：
//   - 现有 Provider 实现含完整的 OAuth 拉取逻辑（含 Azure 头像上传到 S3），
//     重写风险高、收益低；
//   - 适配器保持行为零变化，把"绑定 model.SysUser"这部分留在旧包里，
//     本适配器只暴露 domain.OAuthProvider 这一层抽象。
//
// 注意：auth.Provider.Exchange/FetchUser 接收的 ctx 是 context.Context，
// 适配器这里统一接收标准库 context.Context，满足 oauth2.Config.Exchange 的要求，
// 不依赖任何 Web 框架（旧版曾透传 *gin.Context，但 oauth2 只把它当 context.Context 用）。
type oauthProviderAdapter struct {
	legacy auth.Provider
}

// NewOAuthProviderRegistry 构造一个基于现有 pkg/server/auth 全局 provider 表的注册表。
func NewOAuthProviderRegistry() domain.OAuthProviderRegistry {
	return &oauthRegistryImpl{}
}

// oauthRegistryImpl 是 provider 注册表实现。
type oauthRegistryImpl struct{}

// Get 按 name 返回 provider。
func (r *oauthRegistryImpl) Get(name string) domain.OAuthProvider {
	p := auth.GetProvider(name)
	if p == nil {
		return nil
	}
	return &oauthProviderAdapter{legacy: p}
}

// Name 返回 provider 名。
func (a *oauthProviderAdapter) Name() string {
	return a.legacy.Name()
}

// AuthCodeURL 生成跳转到 OAuth 同意页的 URL。
func (a *oauthProviderAdapter) AuthCodeURL(state string) string {
	return a.legacy.OAuthConfig().AuthCodeURL(state)
}

// Exchange 用 code 换 token。
//
// 旧 Provider 接口的 Exchange 签名是 Exchange(c *gin.Context, code string)，
// 但它实际只把 c 当 context.Context 用（传给 oauth2.Config.Exchange）。
// 这里传入标准库 context.Context，类型满足 oauth2.Config.Exchange 的要求。
//
// 错误包装：provider 返回 invalid_grant（code 无效/过期/已使用/redirect_uri 不匹配）
// 时包装为 domain.ErrOAuthInvalidGrant，供 application 层映射为 HTTP 400，
// 避免落入笼统的 500。
func (a *oauthProviderAdapter) Exchange(ctx context.Context, code string) (interface{}, error) {
	// 注入代理感知的 OAuth HTTP 客户端（conf.Oauth.ProxyURL 为空时等同直连），
	// 使换 token 的出站请求走配置的代理。oauth2.Config.Exchange 接收 context.Context。
	token, err := a.legacy.OAuthConfig().Exchange(auth.WithHTTPClient(ctx), code)
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
			return nil, fmt.Errorf("%w: %s", domain.ErrOAuthInvalidGrant, retrieveErr.ErrorDescription)
		}
		return nil, err
	}
	return token, nil
}

// FetchUser 用 token 拉取用户信息。
func (a *oauthProviderAdapter) FetchUser(ctx context.Context, token interface{}) (*domain.OAuthUserInfo, error) {
	t, ok := token.(*oauth2.Token)
	if !ok {
		return nil, errInvalidTokenType
	}
	// 注入代理感知 client，使拉取用户信息的出站请求同样走配置的代理。
	info, err := a.legacy.FetchUser(auth.WithHTTPClient(ctx), t)
	if err != nil {
		return nil, err
	}
	return &domain.OAuthUserInfo{
		ProviderID: info.ProviderID,
		Email:      info.Email,
		Name:       info.Name,
		AvatarURL:  info.AvatarURL,
	}, nil
}

// UserLookupField 返回 DB 中存储 provider ID 的列名。
func (a *oauthProviderAdapter) UserLookupField() string {
	return a.legacy.UserLookupField()
}

// FrontendRedirectURL 返回登录成功后跳转的前端 URL。
func (a *oauthProviderAdapter) FrontendRedirectURL() string {
	return a.legacy.FrontendRedirectURL()
}
