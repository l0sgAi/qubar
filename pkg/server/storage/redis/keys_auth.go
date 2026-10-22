// keys_auth.go：auth（注册 / 找回密码验证码） 领域的 Redis 键名常量与 key 构造函数。

package redis

const (
	// RegisterCodePrefix 注册验证码缓存key前缀
	// 完整key格式: register:code:{email}
	// 存储内容：6位数字验证码，TTL 5分钟
	RegisterCodePrefix = "register:code:"

	// RegisterVerifiedPrefix 注册邮箱已验证标记key前缀
	// 完整key格式: register:verified:{email}
	// 存储内容：标记邮箱已通过验证码校验，TTL 10分钟
	RegisterVerifiedPrefix = "register:verified:"

	// RegisterRatePrefix 注册验证码发送频率限制key前缀
	// 完整key格式: register:rate:{email}
	// 存储内容：频率限制标记，TTL 60秒
	RegisterRatePrefix = "register:rate:"
)

const (
	// PwdResetCodePrefix 找回密码验证码缓存key前缀
	// 完整key格式: pwd_reset:code:{email}
	// 存储内容：6位数字验证码，TTL 5分钟
	// 与 register:code: 隔离，避免同邮箱同时进行注册和找回密码时互相覆盖
	PwdResetCodePrefix = "pwd_reset:code:"

	// PwdResetVerifiedPrefix 找回密码邮箱已验证标记key前缀
	// 完整key格式: pwd_reset:verified:{email}
	// 存储内容：标记邮箱已通过验证码校验，TTL 10分钟
	PwdResetVerifiedPrefix = "pwd_reset:verified:"

	// PwdResetRatePrefix 找回密码验证码发送频率限制key前缀
	// 完整key格式: pwd_reset:rate:{email}
	// 存储内容：频率限制标记，TTL 60秒
	PwdResetRatePrefix = "pwd_reset:rate:"
)

// GetRegisterCodeKey 获取注册验证码缓存的完整key
func GetRegisterCodeKey(email string) string {
	return RegisterCodePrefix + email
}

// GetRegisterVerifiedKey 获取注册邮箱已验证标记的完整key
func GetRegisterVerifiedKey(email string) string {
	return RegisterVerifiedPrefix + email
}

// GetRegisterRateKey 获取注册验证码发送频率限制的完整key
func GetRegisterRateKey(email string) string {
	return RegisterRatePrefix + email
}

// GetPwdResetCodeKey 获取找回密码验证码缓存的完整key
func GetPwdResetCodeKey(email string) string {
	return PwdResetCodePrefix + email
}

// GetPwdResetVerifiedKey 获取找回密码邮箱已验证标记的完整key
func GetPwdResetVerifiedKey(email string) string {
	return PwdResetVerifiedPrefix + email
}

// GetPwdResetRateKey 获取找回密码验证码发送频率限制的完整key
func GetPwdResetRateKey(email string) string {
	return PwdResetRatePrefix + email
}

const (
	// AuthSessionsPrefix 用户在线终端索引key前缀
	// 完整key格式: auth:sessions:{user_id}
	// 存储内容：ZSET，member=sa-token token，score=登录时间（Unix 毫秒），按登录先后排序；
	// 用于终端上限（sa_token.max_login_count，超出时踢最早登录的）与注销全部终端。
	// TTL 与 sa_token.timeout 一致，每次登录写入时刷新；登录时顺手清理已失效的 member。
	AuthSessionsPrefix = "auth:sessions:"

	// AuthSessionMetaPrefix 用户在线终端元数据key前缀
	// 完整key格式: auth:session_meta:{user_id}
	// 存储内容：HASH，field=token，value=终端元数据 JSON（type、device_id、登录时间、IP、UA 摘要）；
	// 与 auth:sessions:{user_id} 成对写入 / 删除，TTL 相同。
	AuthSessionMetaPrefix = "auth:session_meta:"
)

// GetAuthSessionsKey 获取用户在线终端索引的完整key
func GetAuthSessionsKey(userID string) string {
	return AuthSessionsPrefix + userID
}

// GetAuthSessionMetaKey 获取用户在线终端元数据的完整key
func GetAuthSessionMetaKey(userID string) string {
	return AuthSessionMetaPrefix + userID
}
