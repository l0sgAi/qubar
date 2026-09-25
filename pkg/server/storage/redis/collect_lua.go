package redis

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// collectToggleScript 帖子收藏原子切换脚本。
//
// 与 likeToggleScript 逻辑完全一致（方案 B：独立脚本，不动点赞热代码），
// 仅 HINCRBY 的 Hash 字段从 'like_count' 改为 'collect_count'。
// 复用同一份 stats Hash（post:stats:{post_id}），收藏数与点赞数同存于该 Hash。
const collectToggleScript = `
local statsKey = KEYS[1]
local zsetKey = KEYS[2]
local targetId = ARGV[1]
local now = tonumber(ARGV[2])
local maxSize = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])

-- Check if the target is already collected (exists in ZSET)
local score = redis.call('ZSCORE', zsetKey, targetId)

if score then
    -- Uncollect: remove from ZSET, decrement count
    redis.call('ZREM', zsetKey, targetId)
    local newCount = redis.call('HINCRBY', statsKey, 'collect_count', -1)
    if tonumber(newCount) < 0 then
        redis.call('HSET', statsKey, 'collect_count', 0)
    end
    -- Renew TTL on both keys
    redis.call('EXPIRE', statsKey, ttl)
    redis.call('EXPIRE', zsetKey, ttl)
    return -1
else
    -- Collect: add to ZSET with current timestamp, increment count
    redis.call('ZADD', zsetKey, now, targetId)
    redis.call('HINCRBY', statsKey, 'collect_count', 1)
    -- Evict oldest entries if ZSET exceeds max size
    local zsetSize = redis.call('ZCARD', zsetKey)
    if tonumber(zsetSize) > maxSize then
        local removeCount = tonumber(zsetSize) - maxSize
        redis.call('ZREMRANGEBYRANK', zsetKey, 0, removeCount - 1)
    end
    -- Renew TTL on both keys
    redis.call('EXPIRE', statsKey, ttl)
    redis.call('EXPIRE', zsetKey, ttl)
    return 1
end
`

var collectToggleSHA string

// ToggleCollectResult 收藏切换操作结果（1=收藏 / -1=取消收藏）。
type ToggleCollectResult int

const (
	// ToggleCollectCollected 收藏成功（+1）。
	ToggleCollectCollected ToggleCollectResult = 1
	// ToggleCollectUncollected 取消收藏（-1）。
	ToggleCollectUncollected ToggleCollectResult = -1
)

// InitCollectLuaScripts 预加载收藏 Lua 脚本到 Redis（启动时调用）。
func InitCollectLuaScripts() error {
	var err error
	collectToggleSHA, err = Client.ScriptLoad(ctx, collectToggleScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load collect toggle script: %w", err)
	}
	return nil
}

// TogglePostCollect 原子切换帖子收藏状态。
func TogglePostCollect(userID, postID uuid.UUID) (ToggleCollectResult, error) {
	statsKey := GetPostStatsKey(postID)
	zsetKey := GetUserPostCollectListKey(userID)
	return executeCollectToggle(statsKey, zsetKey, postID)
}

func executeCollectToggle(statsKey, zsetKey string, targetID uuid.UUID) (ToggleCollectResult, error) {
	now := time.Now().UnixMilli()
	maxZsetSize := int64(2000)
	ttlSeconds := int64(postStatsTTL.Seconds())

	// targetId 以 UUID 字符串形式作为 ARGV 传入(Lua 中 ZADD/ZSCORE 的 member)
	result, err := Client.EvalSha(ctx, collectToggleSHA,
		[]string{statsKey, zsetKey},
		targetID.String(), now, maxZsetSize, ttlSeconds,
	).Int64()

	if err != nil {
		// If script SHA is missing (Redis restarted), reload and retry
		collectToggleSHA, err = Client.ScriptLoad(ctx, collectToggleScript).Result()
		if err != nil {
			return 0, fmt.Errorf("failed to reload collect toggle script: %w", err)
		}
		result, err = Client.EvalSha(ctx, collectToggleSHA,
			[]string{statsKey, zsetKey},
			targetID.String(), now, maxZsetSize, ttlSeconds,
		).Int64()
		if err != nil {
			return 0, fmt.Errorf("failed to execute collect toggle: %w", err)
		}
	}

	return ToggleCollectResult(result), nil
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
