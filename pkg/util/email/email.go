package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"go.uber.org/zap"
)

var (
	EmailClient *Client
	once        sync.Once
)

// mailtrapRequest 对应 Mailtrap 发送 API 的请求体
type mailtrapRequest struct {
	From     mailtrapAddress   `json:"from"`
	To       []mailtrapAddress `json:"to"`
	Subject  string            `json:"subject"`
	Text     string            `json:"text,omitempty"`
	HTML     string            `json:"html,omitempty"`
	Category string            `json:"category,omitempty"`
}

// mailtrapTemplateRequest 对应 Mailtrap 模板发送 API 的请求体
type mailtrapTemplateRequest struct {
	From              mailtrapAddress        `json:"from"`
	To                []mailtrapAddress      `json:"to"`
	TemplateUUID      string                 `json:"template_uuid"`
	TemplateVariables map[string]interface{} `json:"template_variables"`
}

type mailtrapAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// SendOptions 邮件发送的可选参数，零值字段使用配置默认值
type SendOptions struct {
	FromEmail string // 覆盖默认发件人邮箱
	FromName  string // 覆盖默认发件人名称
	Category  string // Mailtrap 分类标签
	TextBody  string // 纯文本正文
	HTMLBody  string // HTML 正文（TextBody 和 HTMLBody 至少填一个）
}

// Client Mailtrap 邮件客户端
type Client struct {
	apiToken               string
	apiURL                 string
	senderEmail            string
	senderName             string
	httpClient             *http.Client
	verificationTemplates  map[string]string // lang -> template_uuid（注册验证码）
	passwordResetTemplates map[string]string // lang -> template_uuid（找回密码验证码）
}

// InitEmail 初始化全局 Mailtrap 邮件客户端
func InitEmail() error {
	var initErr error
	once.Do(func() {
		cfg := conf.Config.Mailtrap

		if cfg.APIToken == "" {
			initErr = fmt.Errorf("mailtrap configuration is incomplete: api_token is required")
			return
		}

		apiURL := cfg.APIURL
		if apiURL == "" {
			apiURL = "https://send.api.mailtrap.io/api/send"
		}

		EmailClient = &Client{
			apiToken:    cfg.APIToken,
			apiURL:      apiURL,
			senderEmail: cfg.SenderEmail,
			senderName:  cfg.SenderName,
			httpClient: &http.Client{
				Timeout: 10 * time.Second,
			},
			verificationTemplates: map[string]string{
				"zh": cfg.Templates.VerificationCode.Zh,
				"en": cfg.Templates.VerificationCode.En,
			},
			passwordResetTemplates: map[string]string{
				"zh": cfg.Templates.PasswordReset.Zh,
				"en": cfg.Templates.PasswordReset.En,
			},
		}

		logger.Log.Info("Mailtrap email client initialized successfully",
			zap.String("sender_email", cfg.SenderEmail),
			zap.String("api_url", apiURL),
		)
	})
	return initErr
}

// GetClient 获取已初始化的邮件客户端
func GetClient() *Client {
	if EmailClient == nil {
		logger.Log.Error("Mailtrap email client is not initialized, call InitEmail first")
		return nil
	}
	return EmailClient
}

// Send 通过 Mailtrap API 发送邮件
func (c *Client) Send(ctx context.Context, toEmail, toName, subject string, opts SendOptions) error {
	if toEmail == "" {
		return fmt.Errorf("recipient email is required")
	}
	if opts.TextBody == "" && opts.HTMLBody == "" {
		return fmt.Errorf("at least one of text body or html body is required")
	}

	fromEmail := opts.FromEmail
	if fromEmail == "" {
		fromEmail = c.senderEmail
	}
	fromName := opts.FromName
	if fromName == "" {
		fromName = c.senderName
	}

	reqBody := mailtrapRequest{
		From:     mailtrapAddress{Email: fromEmail, Name: fromName},
		To:       []mailtrapAddress{{Email: toEmail, Name: toName}},
		Subject:  subject,
		Text:     opts.TextBody,
		HTML:     opts.HTMLBody,
		Category: opts.Category,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal email request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Mailtrap response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mailtrap API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	logger.Log.Info("email sent successfully",
		zap.String("to", toEmail),
		zap.String("subject", subject),
	)
	return nil
}

// SendVerificationCode 通过模板发送注册验证码邮件，lang 为 "zh" 或 "en"，无效值 fallback 到 "en"
func (c *Client) SendVerificationCode(ctx context.Context, toEmail, code, lang string) error {
	return c.SendWithTemplate(ctx, toEmail, lang, c.verificationTemplates, map[string]interface{}{
		"Email": toEmail,
		"Code":  code,
	})
}

// SendPasswordResetCode 通过模板发送找回密码验证码邮件，lang 为 "zh" 或 "en"，无效值 fallback 到 "en"
func (c *Client) SendPasswordResetCode(ctx context.Context, toEmail, code, lang string) error {
	return c.SendWithTemplate(ctx, toEmail, lang, c.passwordResetTemplates, map[string]interface{}{
		"Email": toEmail,
		"Code":  code,
	})
}

// SendWithTemplate 通过 Mailtrap 模板发送邮件。
// templates 参数按 lang 索引到 template_uuid，由调用方按场景传入对应的模板 map。
func (c *Client) SendWithTemplate(ctx context.Context, toEmail, lang string, templates map[string]string, variables map[string]interface{}) error {
	if toEmail == "" {
		return fmt.Errorf("recipient email is required")
	}

	// 解析语言，无效时 fallback 到英文
	if lang != "zh" && lang != "en" {
		lang = "en"
	}
	templateUUID := templates[lang]
	if templateUUID == "" {
		return fmt.Errorf("no template configured for lang: %s", lang)
	}

	reqBody := mailtrapTemplateRequest{
		From:              mailtrapAddress{Email: c.senderEmail, Name: c.senderName},
		To:                []mailtrapAddress{{Email: toEmail}},
		TemplateUUID:      templateUUID,
		TemplateVariables: variables,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal template email request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send template email request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Mailtrap response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mailtrap API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	logger.Log.Info("template email sent successfully",
		zap.String("to", toEmail),
		zap.String("lang", lang),
		zap.String("template_uuid", templateUUID),
	)
	return nil
}
