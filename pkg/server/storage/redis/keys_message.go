// keys_message.go：message（私信）领域的 Redis 键名常量与 key 构造函数。

package redis

import "time"

const (
	// MessageRateMinutePrefix 私信发送频率：每分钟计数（#242）
	// 完整key格式: message:rate:min:{sender_id}
	// 存储内容：String 计数，发送前原子 +1（超限不加），发送失败退还；
	// TTL 60s，自窗口内第一次计数起算（固定窗口，到期整体清零）。
	MessageRateMinutePrefix = "message:rate:min:"

	// MessageRateDayPrefix 私信发送频率：每天计数（#242）
	// 完整key格式: message:rate:day:{sender_id}
	// 存储内容：String 计数，写法同 message:rate:min；TTL 24h，自窗口内第一次计数起算。
	MessageRateDayPrefix = "message:rate:day:"

	// MessageRateNewConvPrefix 私信发送频率：每天主动开启的新会话数（#242）
	// 完整key格式: message:rate:newconv:{sender_id}
	// 存储内容：String 计数。每次发送先预占 1 个名额（未满时），发送成功但没有新建会话、或发送失败时退还；
	// TTL 24h，自窗口内第一次预占起算。
	MessageRateNewConvPrefix = "message:rate:newconv:"
)

const (
	// MessageRateMinuteWindow / MessageRateDayWindow 私信发送频率的两个窗口长度（新会话数与每天计数同窗口长度）。
	MessageRateMinuteWindow = time.Minute
	MessageRateDayWindow    = 24 * time.Hour
)

// GetMessageRateMinuteKey 获取私信每分钟发送计数的完整key
func GetMessageRateMinuteKey(senderID string) string {
	return MessageRateMinutePrefix + senderID
}

// GetMessageRateDayKey 获取私信每天发送计数的完整key
func GetMessageRateDayKey(senderID string) string {
	return MessageRateDayPrefix + senderID
}

// GetMessageRateNewConvKey 获取私信每天新会话计数的完整key
func GetMessageRateNewConvKey(senderID string) string {
	return MessageRateNewConvPrefix + senderID
}
