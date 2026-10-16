// conf_s3.go：配置段 s3，聚合进 AppConfig（conf.go）。

package conf

// S3 AWS S3 对象存储配置
type S3 struct {
	AccessKeyID      string `mapstructure:"access_key_id" json:"access_key_id" yaml:"access_key_id"`
	SecretAccessKey  string `mapstructure:"secret_access_key" json:"secret_access_key" yaml:"secret_access_key"`
	Region           string `mapstructure:"region" json:"region" yaml:"region"`
	Bucket           string `mapstructure:"bucket" json:"bucket" yaml:"bucket"`
	Endpoint         string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`
	PresignURLExpire int    `mapstructure:"presign_url_expire" json:"presign_url_expire" yaml:"presign_url_expire"`
	CloudfrontDomain string `mapstructure:"cloudfront_domain" json:"cloudfront_domain" yaml:"cloudfront_domain"`
}
