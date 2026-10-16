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
