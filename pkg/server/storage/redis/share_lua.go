package redis

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// incrShareCountIfCachedScript 分享数 key 存在时 +1 并续期；不存在返回 -1、不写
// （冷 key 直接 INCR 会从 0 起算，漏掉 DB 里已有的分享）。
//
// KEYS[1] = post:sharecnt:{postID}
// ARGV[1] = TTL（秒）
const incrShareCountIfCachedScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
    return -1
end
local v = redis.call('INCR', KEYS[1])
redis.call('EXPIRE', KEYS[1], ARGV[1])
return v
`

// seedShareCountScript 冷 key 回种：SET NX（先到的回种生效，后到的不覆盖并发 INCR 过的值），返回当前值。
//
// KEYS[1] = post:sharecnt:{postID}
// ARGV[1] = DB 行数
// ARGV[2] = TTL（秒）
const seedShareCountScript = `
redis.call('SET', KEYS[1], ARGV[1], 'NX', 'EX', ARGV[2])
return tonumber(redis.call('GET', KEYS[1]))
`

// raiseShareCountScript 落库后校准：key 存在且低于 PG 值时抬高到 PG 值（只升不降，保留 TTL）。
// 回种与并发首次分享交错时，SET NX 可能让较小的行数先生效而少记，这里在每次落库后补回。
//
// KEYS[1] = post:sharecnt:{postID}
// ARGV[1] = PG share_count
const raiseShareCountScript = `
local cur = redis.call('GET', KEYS[1])
if not cur then
    return 0
end
local want = tonumber(ARGV[1])
if tonumber(cur) < want then
    redis.call('SET', KEYS[1], want, 'KEEPTTL')
    return 1
end
return 0
`

var (
	incrShareCountIfCached = redis.NewScript(incrShareCountIfCachedScript)
	seedShareCount         = redis.NewScript(seedShareCountScript)
	raiseShareCount        = redis.NewScript(raiseShareCountScript)
)

// IncrPostShareCountIfCached 分享数 key 已存在时原子 +1，返回新值与 true；key 不存在返回 (0, false)，调用方回种。
func IncrPostShareCountIfCached(ctx context.Context, postID uuid.UUID) (int64, bool, error) {
	ttl := int64(postStatsTTL.Seconds())
	v, err := incrShareCountIfCached.Run(ctx, Client, []string{GetPostShareCountKey(postID)}, ttl).Int64()
	if err != nil {
		return 0, false, fmt.Errorf("failed to incr post share count: %w", err)
	}
	if v < 0 {
		return 0, false, nil
	}
	return v, true, nil
}

// GetPostShareCount 读分享数；key 不存在返回 (0, false)。
func GetPostShareCount(ctx context.Context, postID uuid.UUID) (int64, bool, error) {
	v, err := Client.Get(ctx, GetPostShareCountKey(postID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to get post share count: %w", err)
	}
	return v, true, nil
}

// SeedPostShareCount 冷 key 用 DB 行数回种（SET NX），返回回种后的当前值（已有值时不覆盖，返回已有值）。
func SeedPostShareCount(ctx context.Context, postID uuid.UUID, count int64) (int64, error) {
	ttl := int64(postStatsTTL.Seconds())
	v, err := seedShareCount.Run(ctx, Client, []string{GetPostShareCountKey(postID)}, count, ttl).Int64()
	if err != nil {
		return 0, fmt.Errorf("failed to seed post share count: %w", err)
	}
	return v, nil
}

// ReconcilePostShareCounts 帖子统计落库后校准分享数缓存（口径同 ReconcilePostCommentCounts）：
// 把低于 PG 的值抬回 PG 值，key 不存在则跳过。返回被校正的帖子数。
func ReconcilePostShareCounts(counts map[uuid.UUID]int64) (int, error) {
	fixed := 0
	var firstErr error
	for postID, n := range counts {
		r, err := raiseShareCount.Run(ctx, Client, []string{GetPostShareCountKey(postID)}, n).Int64()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to reconcile share_count of post %s: %w", postID, err)
			}
			continue
		}
		fixed += int(r)
	}
	return fixed, firstErr
}
