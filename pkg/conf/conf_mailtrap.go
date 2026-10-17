// conf_mailtrap.go：配置段 mailtrap，聚合进 AppConfig（conf.go）。

package conf

// Mailtrap 邮件发送配置
type Mailtrap struct {
	APIToken    string            `mapstructure:"api_token" json:"api_token" yaml:"api_token"`
	SenderEmail string            `mapstructure:"sender_email" json:"sender_email" yaml:"sender_email"`
	SenderName  string            `mapstructure:"sender_name" json:"sender_name" yaml:"sender_name"`
	APIURL      string            `mapstructure:"api_url" json:"api_url" yaml:"api_url"`
	Templates   MailtrapTemplates `mapstructure:"templates" json:"templates" yaml:"templates"`
}

// MailtrapTemplates Mailtrap 邮件模板配置
type MailtrapTemplates struct {
	VerificationCode MailtrapTemplate `mapstructure:"verification_code" json:"verification_code" yaml:"verification_code"`
	PasswordReset    MailtrapTemplate `mapstructure:"password_reset" json:"password_reset" yaml:"password_reset"`
}

// MailtrapTemplate 支持多语言的模板 UUID 映射
type MailtrapTemplate struct {
	Zh string `mapstructure:"zh" json:"zh" yaml:"zh"`
	En string `mapstructure:"en" json:"en" yaml:"en"`
}
