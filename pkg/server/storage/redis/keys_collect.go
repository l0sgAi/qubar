// keys_collect.go：collect 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

// UserPostCollectListPrefix 用户帖子收藏列表ZSET key前缀
// 完整key格式: user:collect:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
const UserPostCollectListPrefix = "user:collect:posts:"

// GetUserPostCollectListKey 获取用户帖子收藏列表ZSET的完整key
func GetUserPostCollectListKey(userID uuid.UUID) string {
	return UserPostCollectListPrefix + userID.String()
}
