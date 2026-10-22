// keys_like.go：like 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

// UserCommentLikeListPrefix 用户评论点赞列表ZSET key前缀
// 完整key格式: user:like:comments:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: commentId(UUID字符串)
const UserCommentLikeListPrefix = "user:like:comments:"

// UserPostLikeListPrefix 用户帖子点赞列表ZSET key前缀
// 完整key格式: user:like:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
const UserPostLikeListPrefix = "user:like:posts:"

// GetUserCommentLikeListKey 获取用户评论点赞列表ZSET的完整key
func GetUserCommentLikeListKey(userID uuid.UUID) string {
	return UserCommentLikeListPrefix + userID.String()
}

// GetUserPostLikeListKey 获取用户帖子点赞列表ZSET的完整key
func GetUserPostLikeListKey(userID uuid.UUID) string {
	return UserPostLikeListPrefix + userID.String()
}

// UserPostLikeLoadedPrefix 用户帖子点赞 ZSET 的"完整加载"标记 String key 前缀（#129）。
// 完整key格式: user:like:posts:loaded:{user_id}
// 值: "1"=ZSET 已装入该用户全部有效点赞（成员缺失即未赞，不回源）；"0"=点赞数超过全量装载上限（缺失仍逐批回源）。
// TTL 与 ZSET 相同，只在与 ZSET 同一原子操作里设置 / 续期（过期时刻永不晚于 ZSET），见 interaction_loaded.go。
const UserPostLikeLoadedPrefix = "user:like:posts:loaded:"

// UserCommentLikeLoadedPrefix 用户评论点赞 ZSET 的"完整加载"标记 String key 前缀（#129，语义同上）。
// 完整key格式: user:like:comments:loaded:{user_id}
const UserCommentLikeLoadedPrefix = "user:like:comments:loaded:"

// GetUserPostLikeLoadedKey 获取用户帖子点赞 ZSET 完整加载标记的完整key
func GetUserPostLikeLoadedKey(userID uuid.UUID) string {
	return UserPostLikeLoadedPrefix + userID.String()
}

// GetUserCommentLikeLoadedKey 获取用户评论点赞 ZSET 完整加载标记的完整key
func GetUserCommentLikeLoadedKey(userID uuid.UUID) string {
	return UserCommentLikeLoadedPrefix + userID.String()
}
