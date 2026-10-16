// conf_security.go：配置段 security，聚合进 AppConfig（conf.go）。

package conf

// Security 安全相关配置（密码哈希、加密参数等）。
type Security struct {
	// PasswordHash 密码哈希配置。
	PasswordHash PasswordHash `mapstructure:"password_hash" json:"password_hash" yaml:"password_hash"`

	// DataKey 敏感字段应用层加密密钥（AES-256-GCM，SHA-256 派生）。
	// 用于 ai_agent.api_key 等入库前加密。为空时相关加密接口返回 ErrEmptyKey。
	DataKey string `mapstructure:"data_key" json:"data_key" yaml:"data_key"`
}

// PasswordHash 密码哈希算法参数（Argon2id）。
//
// OWASP 2024 推荐参数：time=2, memory=19MB, threads=1（最低）。
// 本项目默认 time=3, memory=64MB, threads=4，单次约 150ms，比推荐更强。
// 所有字段为 0 时使用默认值，便于配置文件不写时正常工作。
type PasswordHash struct {
	// Time Argon2id 迭代次数（time cost），建议 >=2，默认 3。
	Time uint32 `mapstructure:"time" json:"time" yaml:"time"`
	// Memory Argon2id 内存消耗（KiB），建议 >=19456 (19MB)，默认 65536 (64MB)。
	Memory uint32 `mapstructure:"memory" json:"memory" yaml:"memory"`
	// Threads Argon2id 并行度，建议 1-4，默认 4。
	Threads uint8 `mapstructure:"threads" json:"threads" yaml:"threads"`
	// KeyLen 输出哈希长度（字节），默认 32。
	KeyLen uint32 `mapstructure:"key_len" json:"key_len" yaml:"key_len"`
	// SaltLen 随机 salt 长度（字节），默认 16。
	SaltLen uint32 `mapstructure:"salt_len" json:"salt_len" yaml:"salt_len"`
}
