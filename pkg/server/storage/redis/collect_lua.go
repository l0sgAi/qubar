package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// collectSetScript 帖子收藏"设值"原子脚本（与 likeSetScript 同构，独立脚本不动点赞热代码）。
//
// 调用方传入期望状态 want（1=收藏 / 0=取消）；仅在状态真正变化时修改 ZSET 与
// post:stats:{post_id} 的 collect_count，已处于期望态则 no-op 返回 0。
//
// 返回：1=新收藏（+1）/ -1=取消（-1）/ 0=未变化。
const collectSetScript = `
local statsKey = KEYS[1]
local zsetKey = KEYS[2]
local targetId = ARGV[1]
local now = tonumber(ARGV[2])
local maxSize = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local want = tonumber(ARGV[5])

local score = redis.call('ZSCORE', zsetKey, targetId)
local result = 0

if want == 1 then
    if not score then
        redis.call('ZADD', zsetKey, now, targetId)
        redis.call('HINCRBY', statsKey, 'collect_count', 1)
        -- Evict oldest entries if ZSET exceeds max size
        local zsetSize = tonumber(redis.call('ZCARD', zsetKey))
        if zsetSize > maxSize then
            redis.call('ZREMRANGEBYRANK', zsetKey, 0, zsetSize - maxSize - 1)
        end
        result = 1
    end
else
    if score then
        redis.call('ZREM', zsetKey, targetId)
        local newCount = redis.call('HINCRBY', statsKey, 'collect_count', -1)
        if tonumber(newCount) < 0 then
            redis.call('HSET', statsKey, 'collect_count', 0)
        end
        result = -1
    end
end

-- Renew TTL on both keys
redis.call('EXPIRE', statsKey, ttl)
redis.call('EXPIRE', zsetKey, ttl)
return result
`

// collectZsetMaxSize 用户收藏 ZSET 上限（超出按时间淘汰最旧成员）。
const collectZsetMaxSize = 2000

var collectSetSHA string

// CollectSetResult 收藏设值结果。
type CollectSetResult int

const (
	// CollectSetCollected 新收藏（+1）。
	CollectSetCollected CollectSetResult = 1
	// CollectSetUncollected 取消收藏（-1）。
	CollectSetUncollected CollectSetResult = -1
	// CollectSetUnchanged 已处于期望状态，未变化。
	CollectSetUnchanged CollectSetResult = 0
)

// InitCollectLuaScripts 预加载收藏 Lua 脚本到 Redis（启动时调用）。
func InitCollectLuaScripts() error {
	var err error
	collectSetSHA, err = Client.ScriptLoad(ctx, collectSetScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load collect set script: %w", err)
	}
	return nil
}

// SetPostCollect 原子设置帖子收藏状态（collected=true 收藏 / false 取消）。
func SetPostCollect(ctx context.Context, userID, postID uuid.UUID, collected bool) (CollectSetResult, error) {
	want := 0
	if collected {
		want = 1
	}
	keys := []string{GetPostStatsKey(postID), GetUserPostCollectListKey(userID)}
	// targetId 以 UUID 字符串形式作为 ARGV 传入(Lua 中 ZADD/ZSCORE 的 member)
	args := []interface{}{postID.String(), time.Now().UnixMilli(), collectZsetMaxSize, int64(postStatsTTL.Seconds()), want}

	result, err := Client.EvalSha(ctx, collectSetSHA, keys, args...).Int64()
	if err != nil && redis.HasErrorPrefix(err, "NOSCRIPT") {
		// Redis 重启/脚本缓存被清：重新加载后重试一次
		if collectSetSHA, err = Client.ScriptLoad(ctx, collectSetScript).Result(); err != nil {
			return 0, fmt.Errorf("failed to reload collect set script: %w", err)
		}
		result, err = Client.EvalSha(ctx, collectSetSHA, keys, args...).Int64()
	}
	if err != nil {
		return 0, fmt.Errorf("failed to execute collect set: %w", err)
	}
	return CollectSetResult(result), nil
}

// BatchCheckPostCollected 批量检查用户是否收藏了多个帖子
// ZMScore 对不存在的成员返回 float64(0)，由于 score 是时间戳(>0)，score==0 等价于不存在
func BatchCheckPostCollected(userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, []uuid.UUID, error) {
	if len(postIDs) == 0 {
		return make(map[uuid.UUID]bool), nil, nil
	}

	zsetKey := GetUserPostCollectListKey(userID)
	members := make([]string, len(postIDs))
	for i, id := range postIDs {
		members[i] = id.String()
	}

	scores, err := Client.ZMScore(ctx, zsetKey, members...).Result()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to batch check post collected: %w", err)
	}

	result := make(map[uuid.UUID]bool, len(postIDs))
	var missIDs []uuid.UUID
	for i, score := range scores {
		if score > 0 {
			result[postIDs[i]] = true
		} else {
			result[postIDs[i]] = false
			missIDs = append(missIDs, postIDs[i])
		}
	}

	Client.Expire(ctx, zsetKey, postStatsTTL)
	return result, missIDs, nil
}

// BackfillPostCollects 将DB查询确认的帖子收藏状态回填到ZSET
func BackfillPostCollects(userID uuid.UUID, collectedPostIDs []uuid.UUID) error {
	if len(collectedPostIDs) == 0 {
		return nil
	}
	zsetKey := GetUserPostCollectListKey(userID)
	now := float64(time.Now().UnixMilli())
	members := make([]redis.Z, len(collectedPostIDs))
	for i, id := range collectedPostIDs {
		members[i] = redis.Z{Score: now, Member: id.String()}
	}
	pipe := Client.Pipeline()
	pipe.ZAdd(ctx, zsetKey, members...)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}
