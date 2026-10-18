// conf_oauth.go：配置段 oauth，聚合进 AppConfig（conf.go）。

package conf

// 新增：对应 yaml 中的 oauth 层级
type Oauth struct {
	// ProxyURL OAuth 出站（换 token / 拉用户信息）使用的 HTTP 代理。
	// 为空则直连。本地开发在网络受限时可配置（如 http://127.0.0.1:6268）。
	ProxyURL string `mapstructure:"proxy_url" json:"proxy_url" yaml:"proxy_url"`

	Google    Google    `mapstructure:"google" json:"google" yaml:"google"`
	Github    Github    `mapstructure:"github" json:"github" yaml:"github"`
	Microsoft Microsoft `mapstructure:"microsoft" json:"microsoft" yaml:"microsoft"`
}

// 新增:对应 yaml 中的 google 层级
type Google struct {
	ClientID            string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret        string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL         string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url" json:"frontend_redirect_url" yaml:"frontend_redirect_url"`
}

// 新增:对应 yaml 中的 github 层级
type Github struct {
	ClientID            string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret        string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL         string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url" json:"frontend_redirect_url" yaml:"frontend_redirect_url"`
}

// Microsoft 对应 yaml 中的 microsoft 层级
type Microsoft struct {
	ClientID            string `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret        string `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret"`
	RedirectURL         string `mapstructure:"redirect_url" json:"redirect_url" yaml:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url" json:"frontend_redirect_url" yaml:"frontend_redirect_url"`
}
