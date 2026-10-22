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

// UserPostCollectLoadedPrefix 用户帖子收藏 ZSET 的"完整加载"标记 String key 前缀（#129）。
// 完整key格式: user:collect:posts:loaded:{user_id}
// 值: "1"=ZSET 已装入该用户全部有效收藏（成员缺失即未收藏，不回源）；"0"=收藏数超过全量装载上限（缺失仍逐批回源）。
// TTL 与 ZSET 相同，只在与 ZSET 同一原子操作里设置 / 续期（过期时刻永不晚于 ZSET），见 interaction_loaded.go。
const UserPostCollectLoadedPrefix = "user:collect:posts:loaded:"

// GetUserPostCollectLoadedKey 获取用户帖子收藏 ZSET 完整加载标记的完整key
func GetUserPostCollectLoadedKey(userID uuid.UUID) string {
	return UserPostCollectLoadedPrefix + userID.String()
}
