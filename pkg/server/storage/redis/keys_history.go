// keys_history.go：history 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

// UserPostViewListPrefix 用户浏览历史列表ZSET key前缀
// 完整key格式: user:view:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
// score 倒序即「最近浏览」顺序;cap 500(超限按最低 score 淘汰);TTL 复用 postStatsTTL。
const UserPostViewListPrefix = "user:view:posts:"

// GetUserPostViewListKey 获取用户浏览历史列表ZSET的完整key
func GetUserPostViewListKey(userID uuid.UUID) string {
	return UserPostViewListPrefix + userID.String()
}
