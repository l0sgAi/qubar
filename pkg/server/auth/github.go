package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"interestBar/pkg/conf"
	"interestBar/pkg/server/model"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// GithubUser represents the structure of user data returned by GitHub
type GithubUser struct {
	Login      string `json:"login"`
	ID         int64  `json:"id"`
	NodeID     string `json:"node_id"`
	AvatarURL  string `json:"avatar_url"`
	GravatarID string `json:"gravatar_id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Bio        string `json:"bio"`
}

// GithubProvider implements Provider for GitHub OAuth2.
type GithubProvider struct{}

func (p *GithubProvider) Name() string { return "github" }

func (p *GithubProvider) OAuthConfig() *oauth2.Config {
	return GetGithubOAuthConfig()
}

func (p *GithubProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*OAuthUserInfo, error) {
	// ctx 已携带代理感知 client（由适配器注入）与超时；用它构造带 token 的 client。
	client := p.OAuthConfig().Client(ctx, token)
	gu, err := GetGithubUser(client)
	if err != nil {
		return nil, err
	}
	name := gu.Name
	if name == "" {
		name = gu.Login
	}
	return &OAuthUserInfo{
		ProviderID: strconv.FormatInt(gu.ID, 10),
		Email:      gu.Email,
		Name:       name,
		AvatarURL:  gu.AvatarURL,
	}, nil
}

func (p *GithubProvider) UserLookupField() string { return "github_id" }

func (p *GithubProvider) ApplyProviderID(user *model.SysUser, providerID string) {
	user.GithubID = providerID
}

func (p *GithubProvider) GetProviderID(user *model.SysUser) string {
	return user.GithubID
}

func (p *GithubProvider) FrontendRedirectURL() string {
	return conf.Config.Oauth.Github.FrontendRedirectURL
}

// GithubEmail represents the structure of email data returned by GitHub
type GithubEmail struct {
	Email      string `json:"email"`
	Primary    bool   `json:"primary"`
	Verified   bool   `json:"verified"`
	Visibility string `json:"visibility"`
}

// GetGithubOAuthConfig creates the oauth2.Config for GitHub
func GetGithubOAuthConfig() *oauth2.Config {
	// 检查点：确保 Config 已经被初始化了
	if conf.Config == nil {
		panic("配置尚未初始化，请先调用 InitConfig")
	}

	return &oauth2.Config{
		// 直接从结构体中取值，既安全又有代码提示
		RedirectURL:  conf.Config.Oauth.Github.RedirectURL,
		ClientID:     conf.Config.Oauth.Github.ClientID,
		ClientSecret: conf.Config.Oauth.Github.ClientSecret,
		Scopes: []string{
			"user:email",
			"read:user",
		},
		Endpoint: github.Endpoint,
	}
}

// GetGithubUser fetches user info from GitHub using the access token.
// client 由调用方传入（携带 OAuth 代理与超时配置）。
func GetGithubUser(client *http.Client) (*GithubUser, error) {
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: status code %d", resp.StatusCode)
	}

	var user GithubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %v", err)
	}

	// 如果 email 为空，尝试从 emails API 获取
	if user.Email == "" {
		emails, err := GetGithubEmails(client)
		if err == nil && len(emails) > 0 {
			// 查找主邮箱
			for _, email := range emails {
				if email.Primary {
					user.Email = email.Email
					break
				}
			}
			// 如果没有主邮箱，使用第一个已验证的邮箱
			if user.Email == "" {
				for _, email := range emails {
					if email.Verified {
						user.Email = email.Email
						break
					}
				}
			}
			// 如果还是没有，使用第一个邮箱
			if user.Email == "" && len(emails) > 0 {
				user.Email = emails[0].Email
			}
		}
	}

	return &user, nil
}

// GetGithubEmails fetches email info from GitHub using the access token
func GetGithubEmails(client *http.Client) ([]GithubEmail, error) {
	resp, err := client.Get("https://api.github.com/user/emails")
	if err != nil {
		return nil, fmt.Errorf("failed to get emails: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get emails: status code %d", resp.StatusCode)
	}

	var emails []GithubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return nil, fmt.Errorf("failed to decode emails: %v", err)
	}

	return emails, nil
}
