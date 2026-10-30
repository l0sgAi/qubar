// keys_aiagent.go：aiagent 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

// AiAgentSearchQuotaPrefix 机器人每日网络搜索配额计数 key 前缀（#100）
// 完整key格式: aiagent:search:quota:{agent_id}:{yyyymmdd}
// string(int)：当日已占用次数（INCR，调用服务商前计数，失败也算）；
// EXPIREAT 次日零点（Asia/Shanghai）+ 1h 缓冲，按自然日过期。
const AiAgentSearchQuotaPrefix = "aiagent:search:quota:"

// GetAiAgentSearchQuotaKey 获取机器人某日（yyyymmdd）搜索配额计数的完整 key。
func GetAiAgentSearchQuotaKey(agentID uuid.UUID, day string) string {
	return AiAgentSearchQuotaPrefix + agentID.String() + ":" + day
}
