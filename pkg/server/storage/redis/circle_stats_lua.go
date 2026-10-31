package redis

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// circleStatsTTL 圈子统计 Hash（circle:stats:{circleID}）有效期，回种 / 增减时刷新。
const circleStatsTTL = 24 * time.Hour

// circleStatsSeededField 统计 Hash「已回种」的标志字段：回种在 MULTI 里一次写齐三个字段，
// 增减只在它存在时生效，所以 Hash 要么不存在、要么三个字段齐全（热度同步同样以它判断，见 circle_hot_syncer.go）。
const circleStatsSeededField = "member_count"

// SeedCircleStatisticsIfAbsent 冷 Hash 从 DB 回种：逐字段 HSETNX，只写尚不存在的字段（#180）。
// 并发请求可能已经回种并 HINCRBY 过，覆盖写（UpdateCircleStatistics）会把这些增量冲掉；
// HSETNX 让先到的回种生效、后到的回种成为空操作。MULTI 保证其他客户端看不到只种了一半的 Hash。
func SeedCircleStatisticsIfAbsent(circleID uuid.UUID, statistics *CircleStatistics) error {
	key := GetCircleStatsKey(circleID)
	_, err := Client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSetNX(ctx, key, "member_count", statistics.MemberCount)
		pipe.HSetNX(ctx, key, "post_count", statistics.PostCount)
		pipe.HSetNX(ctx, key, "hot", statistics.Hot)
		pipe.Expire(ctx, key, circleStatsTTL)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to seed circle statistics: %w", err)
	}
	return nil
}

// incrCircleStatIfSeededScript 仅当统计 Hash 已回种时增减一个计数字段，结果小于 0 时钳到 0。
// 冷 Hash 不写：直接 HINCRBY 会建出只有一个字段的半截 Hash，之后的 HSETNX 回种补不正它。
//
// KEYS[1] = circle:stats:{circleID}
// ARGV[1] = 已回种标志字段（circleStatsSeededField）
// ARGV[2] = 计数字段
// ARGV[3] = 增量
// ARGV[4] = TTL（秒）
//
// 返回新值；-1 表示 Hash 未回种、未写入。
const incrCircleStatIfSeededScript = `
if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 0 then
    return -1
end
local v = redis.call('HINCRBY', KEYS[1], ARGV[2], ARGV[3])
if v < 0 then
    redis.call('HSET', KEYS[1], ARGV[2], 0)
    v = 0
end
redis.call('EXPIRE', KEYS[1], ARGV[4])
return v
`

var incrCircleStatIfSeeded = redis.NewScript(incrCircleStatIfSeededScript)

// incrCircleStat 已回种时原子增减圈子计数字段；未回种时不写（由读路径 / 下次写路径从 DB 回种）。
func incrCircleStat(circleID uuid.UUID, field string, delta int64) error {
	key := GetCircleStatsKey(circleID)
	ttl := int64(circleStatsTTL / time.Second)
	if err := incrCircleStatIfSeeded.Run(ctx, Client, []string{key}, circleStatsSeededField, field, delta, ttl).Err(); err != nil {
		return fmt.Errorf("failed to incr circle %s by %d: %w", field, delta, err)
	}
	return nil
}
