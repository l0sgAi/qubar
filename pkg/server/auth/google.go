package auth

import (
	"context"
	"encoding/json"
	"fmt" // Assuming config access path, may need adjustment
	"net/http"

	"interestBar/pkg/conf"
	"interestBar/pkg/server/model"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleUser represents the structure of user data returned by Google
type GoogleUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
	Locale        string `json:"locale"`
}

// GetGoogleOAuthConfig creates the oauth2.Config for Google
func GetGoogleOAuthConfig() *oauth2.Config {
	// 检查点：确保 Config 已经被初始化了
	if conf.Config == nil {
		panic("配置尚未初始化，请先调用 InitConfig")
	}

	return &oauth2.Config{
		// 直接从结构体中取值，既安全又有代码提示
		RedirectURL:  conf.Config.Oauth.Google.RedirectURL,
		ClientID:     conf.Config.Oauth.Google.ClientID,
		ClientSecret: conf.Config.Oauth.Google.ClientSecret,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}
}

// GoogleProvider implements Provider for Google OAuth2.
type GoogleProvider struct{}

func (p *GoogleProvider) Name() string { return "google" }

func (p *GoogleProvider) OAuthConfig() *oauth2.Config {
	return GetGoogleOAuthConfig()
}

func (p *GoogleProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*OAuthUserInfo, error) {
	// ctx 已携带代理感知 client（由适配器注入）与超时；用它构造带 token 的 client。
	client := p.OAuthConfig().Client(ctx, token)
	gu, err := GetGoogleUser(client)
	if err != nil {
		return nil, err
	}
	return &OAuthUserInfo{
		ProviderID: gu.ID,
		Email:      gu.Email,
		Name:       gu.Name,
		AvatarURL:  gu.Picture,
	}, nil
}

func (p *GoogleProvider) UserLookupField() string { return "google_id" }

func (p *GoogleProvider) ApplyProviderID(user *model.SysUser, providerID string) {
	user.GoogleID = providerID
}

func (p *GoogleProvider) GetProviderID(user *model.SysUser) string {
	return user.GoogleID
}

func (p *GoogleProvider) FrontendRedirectURL() string {
	return conf.Config.Oauth.Google.FrontendRedirectURL
}

// GetGoogleUser fetches user info from Google using the access token.
// client 由调用方传入（携带 OAuth 代理与超时配置）。
func GetGoogleUser(client *http.Client) (*GoogleUser, error) {
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: status code %d", resp.StatusCode)
	}

	var user GoogleUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %v", err)
	}

	return &user, nil
}
