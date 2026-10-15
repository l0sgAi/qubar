// Package password 提供密码哈希与校验工具。
//
// 算法：Argon2id（RFC 9106 标准、OWASP 2024 首选）。
// 兼容：自动识别旧 SHA256（64 字符十六进制）格式以支持透明升级。
//
// 哈希格式（PHC 字符串）：
//
//	$argon2id$v=19$m=65536,t=3,p=4$<base64-salt>$<base64-hash>
//
// 该格式自描述（参数 + salt 与哈希同行存储），支持未来调参时无缝兼容旧数据。
package password

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"golang.org/x/crypto/argon2"
)

// 默认参数（OWASP 2024 推荐 + 偏强方向，单次约 150ms）。
const (
	defaultTime    uint32 = 3
	defaultMemory  uint32 = 64 * 1024 // 64MB
	defaultThreads uint8  = 4
	defaultKeyLen  uint32 = 32
	defaultSaltLen uint32 = 16
)

// MinLength 密码最小长度。
//
// 统一注册与重置密码的最小长度校验，避免各处硬编码 6 导致规则漂移。
const MinLength = 6

// Params 哈希算法参数。
type Params struct {
	Time    uint32 // Argon2id 迭代次数（time cost）
	Memory  uint32 // 内存消耗（KiB）
	Threads uint8  // 并行度
	KeyLen  uint32 // 输出哈希长度（字节）
	SaltLen uint32 // 随机 salt 长度（字节）
}

// activeParams 是当前用于生成新哈希的参数。
// 通过 atomic.Value 支持运行时热更新（配合 conf 的 fsnotify 热更新）。
var activeParams atomic.Value // stores Params

func init() {
	activeParams.Store(Params{
		Time:    defaultTime,
		Memory:  defaultMemory,
		Threads: defaultThreads,
		KeyLen:  defaultKeyLen,
		SaltLen: defaultSaltLen,
	})
}

// SetParams 设置当前用于生成新哈希的参数。
// 字段为 0 时使用默认值。线程安全，可在配置热更新时调用。
//
// 注意：仅影响后续 Hash() 调用；旧哈希仍可被 Verify() 正确校验
// （PHC 串自带参数，与全局参数无关）。
func SetParams(p Params) {
	if p.Time == 0 {
		p.Time = defaultTime
	}
	if p.Memory == 0 {
		p.Memory = defaultMemory
	}
	if p.Threads == 0 {
		p.Threads = defaultThreads
	}
	if p.KeyLen == 0 {
		p.KeyLen = defaultKeyLen
	}
	if p.SaltLen == 0 {
		p.SaltLen = defaultSaltLen
	}
	activeParams.Store(p)
}

// GetParams 返回当前生效参数（含默认值兜底）。
func GetParams() Params {
	return activeParams.Load().(Params)
}

// 错误。
var (
	// ErrInvalidHash 存储哈希格式无效（既不是 Argon2id PHC 串，也不是旧 SHA256）。
	ErrInvalidHash = errors.New("password: invalid hash format")
	// ErrIncompatibleVersion Argon2 版本不匹配（理论不该发生）。
	ErrIncompatibleVersion = errors.New("password: incompatible argon2 version")
)

// Hash 用当前参数生成 Argon2id 哈希。
//
// 返回 PHC 格式字符串，可直接存入数据库。
func Hash(plain string) (string, error) {
	p := GetParams()
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: failed to read salt: %w", err)
	}

	hash := argon2.IDKey([]byte(plain), salt, p.Time, p.Memory, p.Threads, p.KeyLen)

	// PHC 格式：$argon2id$v=19$m=<mem>,t=<time>,p=<threads>$<b64-salt>$<b64-hash>
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
	return encoded, nil
}

// Verify 校验明文密码与存储哈希是否匹配。
//
// 自动识别格式：
//   - Argon2id PHC 串（以 "$argon2id$" 开头）→ argon2 比对
//   - 64 字符十六进制 → 旧 SHA256 比对，且返回 needsUpgrade=true
//
// 返回值：
//   - ok=true 表示密码正确；
//   - needsUpgrade=true 表示当前哈希是旧算法，建议在登录成功后用 Hash() 重新生成并 UPDATE；
//   - err 仅在哈希格式损坏时非 nil；密码错误不报 err，由 ok=false 表达。
func Verify(plain, stored string) (ok bool, needsUpgrade bool, err error) {
	if stored == "" {
		return false, false, ErrInvalidHash
	}

	// Argon2id PHC 格式
	if strings.HasPrefix(stored, "$argon2id$") {
		matched, verr := verifyArgon2id(plain, stored)
		if verr != nil {
			return false, false, verr
		}
		return matched, false, nil
	}

	// 旧 SHA256 格式：64 字符十六进制
	if isLegacySHA256(stored) {
		// %x 输出小写；isLegacySHA256 同时接受大小写，故将存储值归一为小写后再比对，
		// 避免外部迁移来的大写哈希无法验证。
		expected := fmt.Sprintf("%x", sha256.Sum256([]byte(plain)))
		matched := subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(stored))) == 1
		return matched, true, nil
	}

	return false, false, ErrInvalidHash
}

// IsLegacyHash 判断存储哈希是否是旧 SHA256 格式（用于决策是否需要升级）。
//
// Verify() 内部已使用该判断；外部调用方一般不需要直接用此函数，
// 但暴露出来便于测试和未来其他场景（如批量重置脚本）。
func IsLegacyHash(stored string) bool {
	return isLegacySHA256(stored)
}

// ---- 内部实现 ----

// isLegacySHA256 判断是否是 64 字符十六进制串（旧 SHA256 输出）。
func isLegacySHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// verifyArgon2id 解析 PHC 串并比对。
func verifyArgon2id(plain, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// 期望 6 段：["", "argon2id", "v=19", "m=...,t=...,p=...", "<salt>", "<hash>"]
	if len(parts) != 6 {
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, ErrIncompatibleVersion
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}

	// 调用 argon2.IDKey 前的守卫：time=0 或 threads=0 会让 argon2 直接 panic
	// （"number of rounds too small" / "parallelism degree too low"），
	// 损坏或被篡改的存储哈希不应因此崩溃登录流程。
	if time == 0 || memory == 0 || threads == 0 {
		return false, ErrInvalidHash
	}

	computed := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(hash)))
	return subtle.ConstantTimeCompare(hash, computed) == 1, nil
}
