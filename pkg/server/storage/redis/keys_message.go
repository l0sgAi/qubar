// keys_message.go：message（私信）领域的 Redis 键名常量与 key 构造函数。

package redis

import (
	"time"

	"github.com/google/uuid"
)

// MessageUnreadPrefix 用户私信未读总数 String key 前缀（#241）。
// 完整 key 格式: message:unread:{user_id}
// 值: 该用户各会话未读数之和（DB 真值 SUM(conversation_member.unread_count) WHERE user_id = ? AND deleted = 0）。
// 写: 发送提交后对接收方 INCRBY（仅 key 存在时，Lua，不改 TTL）；标记已读 / 删除会话后 DEL；
// 读 miss 时回源 DB SUM 回填（SET + TTL）。TTL 到期即按 DB 重算，兜底校正竞态造成的漂移。
const MessageUnreadPrefix = "message:unread:"

// MessageUnreadTTL 私信未读总数缓存 TTL（回填时设置，INCRBY 不续期）。
const MessageUnreadTTL = 24 * time.Hour

// GetMessageUnreadKey 获取用户私信未读总数的完整 key。
func GetMessageUnreadKey(userID uuid.UUID) string {
	return MessageUnreadPrefix + userID.String()
}
