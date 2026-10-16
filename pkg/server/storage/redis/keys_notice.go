// keys_notice.go：notice 领域的 Redis 键名常量与 key 构造函数。

package redis

import (
	"time"

	"github.com/google/uuid"
)

// NoticeUnreadPrefix 用户未读通知计数器 key 前缀。
// 完整 key 格式: notice:unread:{user_id}
// String 计数器: NotificationEventConsumer upsert 通知后 INCRBY；
// /notice/read 按实际已读行数 DECRBY(floor 0)；/notice/read-all SET 0；
// 读 miss 时回源 DB COUNT 回填。TTL 24h 滚动——到期 recount 校正 INCRBY/DECRBY 漂移(软信号)。
const NoticeUnreadPrefix = "notice:unread:"

// NoticeUnreadTTL 未读计数器 TTL（INCRBY/SET 时滚动续期）。
const NoticeUnreadTTL = 24 * time.Hour

// GetNoticeUnreadKey 获取用户未读通知计数器的完整 key。
func GetNoticeUnreadKey(userID uuid.UUID) string {
	return NoticeUnreadPrefix + userID.String()
}
