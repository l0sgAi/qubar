package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	s3storage "interestBar/pkg/server/storage/s3"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"
)

// AzureUser represents the structure of user data returned by Microsoft Graph API
type AzureUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

// GetAzureOAuthConfig creates the oauth2.Config for Microsoft Azure AD
func GetAzureOAuthConfig() *oauth2.Config {
	if conf.Config == nil {
		panic("配置尚未初始化，请先调用 InitConfig")
	}

	return &oauth2.Config{
		RedirectURL:  conf.Config.Oauth.Microsoft.RedirectURL,
		ClientID:     conf.Config.Oauth.Microsoft.ClientID,
		ClientSecret: conf.Config.Oauth.Microsoft.ClientSecret,
		Scopes: []string{
			"User.Read",
		},
		Endpoint: microsoft.AzureADEndpoint("consumers"),
	}
}

// GetAzureUser fetches user info from Microsoft Graph API using the access token.
// client 由调用方传入（携带 OAuth 代理与超时配置）。
func GetAzureUser(client *http.Client) (*AzureUser, error) {
	resp, err := client.Get("https://graph.microsoft.com/v1.0/me")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: status code %d", resp.StatusCode)
	}

	var user AzureUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %v", err)
	}

	return &user, nil
}

// GetAzureProfilePhoto fetches the user's profile photo binary data from Microsoft Graph API.
// Returns (nil, "", nil) if the user has no photo set (404).
func GetAzureProfilePhoto(client *http.Client) ([]byte, string, error) {
	resp, err := client.Get("https://graph.microsoft.com/v1.0/me/photo/$value")
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch profile photo: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort：关闭响应体失败不影响结果

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("failed to fetch profile photo: status code %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read profile photo data: %v", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	return data, contentType, nil
}

// AzureProvider implements Provider for Microsoft Azure AD OAuth2.
type AzureProvider struct{}

func (p *AzureProvider) Name() string { return "azure" }

func (p *AzureProvider) OAuthConfig() *oauth2.Config {
	return GetAzureOAuthConfig()
}

func (p *AzureProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*OAuthUserInfo, error) {
	// ctx 已携带代理感知 client（由适配器注入）与超时；用它构造带 token 的 client。
	client := p.OAuthConfig().Client(ctx, token)
	au, err := GetAzureUser(client)
	if err != nil {
		return nil, err
	}
	// Microsoft Graph: Mail may be empty, fall back to UserPrincipalName
	email := au.Mail
	if email == "" {
		email = au.UserPrincipalName
	}

	var avatarURL string
	data, contentType, err := GetAzureProfilePhoto(client)
	if err != nil {
		logger.Log.Warn("failed to fetch Azure profile photo: " + err.Error())
	} else if data != nil {
		avatarURL = uploadAzureAvatar(ctx, data, contentType, au.ID)
	}

	return &OAuthUserInfo{
		ProviderID: au.ID,
		Email:      email,
		Name:       au.DisplayName,
		AvatarURL:  avatarURL,
	}, nil
}

// uploadAzureAvatar uploads the profile photo to S3 and returns the URL.
// Returns empty string on any failure (never blocks login).
func uploadAzureAvatar(ctx context.Context, imageData []byte, contentType string, azureUserID string) string {
	s3Client := s3storage.GetS3Client()
	if s3Client == nil {
		logger.Log.Warn("S3 client not initialized, skipping avatar upload")
		return ""
	}

	ext := ".jpg"
	switch contentType {
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	}

	key := s3storage.GenerateKeyWithUUID("avatars/azure", azureUserID+ext)
	url, err := s3Client.UploadFileFromBytes(ctx, key, imageData, contentType, "")
	if err != nil {
		logger.Log.Warn("failed to upload Azure avatar to S3: " + err.Error())
		return ""
	}
	return url
}

func (p *AzureProvider) UserLookupField() string { return "microsoft_id" }

func (p *AzureProvider) ApplyProviderID(user *model.SysUser, providerID string) {
	user.MicrosoftID = providerID
}

func (p *AzureProvider) GetProviderID(user *model.SysUser) string {
	return user.MicrosoftID
}

func (p *AzureProvider) FrontendRedirectURL() string {
	return conf.Config.Oauth.Microsoft.FrontendRedirectURL
}
