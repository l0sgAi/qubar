// keys_user.go：user 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

const (
	// UserInfoPrefix 用户基础信息缓存key前缀
	// 完整key格式: user:info:{user_id}
	// 包含字段：id, username, email, phone, google_id, github_id, avatar_url, gender, birthdate, status, role, deleted, create_time, update_time
	UserInfoPrefix = "user:info:"
)

// GetUserInfoKey 获取用户基础信息缓存的完整key
func GetUserInfoKey(userID uuid.UUID) string {
	return UserInfoPrefix + userID.String()
}
