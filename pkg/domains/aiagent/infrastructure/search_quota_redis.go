package infrastructure

import (
	"context"
	"fmt"
	"time"

	"interestBar/pkg/domains/aiagent/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// quotaZone 配额自然日的时区（平台面向国内用户；固定偏移，不依赖容器内 tzdata）。
var quotaZone = time.FixedZone("CST", 8*3600)

// quotaExpireBuffer 计数 key 在次日零点后再保留的时长（跨零点的在途请求仍落在旧 key 上，留缓冲便于排查）。
const quotaExpireBuffer = time.Hour

// searchQuotaRedis SearchQuota 的 Redis 实现：aiagent:search:quota:{agent_id}:{yyyymmdd} 自增计数。
type searchQuotaRedis struct {
	now func() time.Time
}

// NewSearchQuota 构造机器人每日搜索配额计数器（使用全局 redis.Client）。
func NewSearchQuota() domain.SearchQuota {
	return &searchQuotaRedis{now: time.Now}
}

// Consume 原子 INCR + EXPIREAT（MULTI 事务）。EXPIREAT 取当日固定的次日零点，
// 每次重复设置也是同一时刻，无需 Lua 判断首次写入。
func (q *searchQuotaRedis) Consume(ctx context.Context, agentID uuid.UUID, limit int) error {
	day, expireAt := quotaDay(q.now())
	key := redispkg.GetAiAgentSearchQuotaKey(agentID, day)
	var incr *redis.IntCmd
	_, err := redispkg.Client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		incr = p.Incr(ctx, key)
		p.ExpireAt(ctx, key, expireAt)
		return nil
	})
	if err != nil {
		return fmt.Errorf("search quota incr: %w", err)
	}
	if incr.Val() > int64(limit) {
		return domain.ErrSearchQuotaExhausted
	}
	return nil
}

// quotaDay 返回 t 所在自然日（Asia/Shanghai）的 yyyymmdd 与计数 key 的过期时刻。
func quotaDay(t time.Time) (string, time.Time) {
	t = t.In(quotaZone)
	y, m, d := t.Date()
	nextMidnight := time.Date(y, m, d+1, 0, 0, 0, 0, quotaZone)
	return t.Format("20060102"), nextMidnight.Add(quotaExpireBuffer)
}
