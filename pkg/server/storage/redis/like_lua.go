package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// likeSetScript 点赞"设值"原子脚本（设计见 docs/design/like-pipeline-fix-design.md §三 F1.1）。
//
// 与旧 toggle 脚本的区别：由调用方传入期望状态 want（1=赞 / 0=取消），脚本只在状态真正变化时
// 修改 ZSET 与 like_count；已处于期望态则 no-op 返回 0。方向不再由有损的 ZSET 推断，
// 调用方需先解析真实状态（ZSET miss 时回源 DB 并回填）。
//
// 返回：1=新赞（+1）/ -1=取消（-1）/ 0=未变化。
const likeSetScript = `
local statsKey = KEYS[1]
local zsetKey = KEYS[2]
local loadedKey = KEYS[3]
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
        redis.call('HINCRBY', statsKey, 'like_count', 1)
        -- Evict oldest entries if ZSET exceeds max size
        local zsetSize = tonumber(redis.call('ZCARD', zsetKey))
        if zsetSize > maxSize then
            redis.call('ZREMRANGEBYRANK', zsetKey, 0, zsetSize - maxSize - 1)
            -- 淘汰后 ZSET 不再是全量：完整加载标记降为"超限"，此后 miss 回源 DB（#129）
            if redis.call('EXISTS', loadedKey) == 1 then
                redis.call('SET', loadedKey, '0', 'KEEPTTL')
            end
        end
        result = 1
    end
else
    if score then
        redis.call('ZREM', zsetKey, targetId)
        local newCount = redis.call('HINCRBY', statsKey, 'like_count', -1)
        if tonumber(newCount) < 0 then
            redis.call('HSET', statsKey, 'like_count', 0)
        end
        result = -1
    end
end

-- Renew TTL; 完整加载标记与 ZSET 同一脚本内同 TTL 续期（标记不存在时 no-op）
redis.call('EXPIRE', statsKey, ttl)
redis.call('EXPIRE', zsetKey, ttl)
redis.call('EXPIRE', loadedKey, ttl)
return result
`

// likeZsetMaxSize 用户点赞 ZSET 上限（超出按时间淘汰最旧成员）。
const likeZsetMaxSize = 2000

var likeSetSHA string

// LikeSetResult 点赞设值结果。
type LikeSetResult int

const (
	// LikeSetLiked 新赞（+1）。
	LikeSetLiked LikeSetResult = 1
	// LikeSetUnliked 取消赞（-1）。
	LikeSetUnliked LikeSetResult = -1
	// LikeSetUnchanged 已处于期望状态，未变化。
	LikeSetUnchanged LikeSetResult = 0
)

// InitLikeLuaScripts 预加载 Lua 脚本到 Redis（启动时调用）
func InitLikeLuaScripts() error {
	var err error
	likeSetSHA, err = Client.ScriptLoad(ctx, likeSetScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load like set script: %w", err)
	}
	return nil
}

// SetCommentLike 原子设置评论点赞状态（liked=true 赞 / false 取消）。
func SetCommentLike(ctx context.Context, userID, commentID uuid.UUID, liked bool) (LikeSetResult, error) {
	return executeLikeSet(ctx, GetCommentStatsKey(commentID), GetUserCommentLikeListKey(userID), GetUserCommentLikeLoadedKey(userID), commentID, liked)
}

// SetPostLike 原子设置帖子点赞状态（liked=true 赞 / false 取消）。
func SetPostLike(ctx context.Context, userID, postID uuid.UUID, liked bool) (LikeSetResult, error) {
	return executeLikeSet(ctx, GetPostStatsKey(postID), GetUserPostLikeListKey(userID), GetUserPostLikeLoadedKey(userID), postID, liked)
}

func executeLikeSet(ctx context.Context, statsKey, zsetKey, loadedKey string, targetID uuid.UUID, liked bool) (LikeSetResult, error) {
	want := 0
	if liked {
		want = 1
	}
	keys := []string{statsKey, zsetKey, loadedKey}
	// targetId 以 UUID 字符串形式作为 ARGV 传入(Lua 中 ZADD/ZSCORE 的 member)
	args := []interface{}{targetID.String(), time.Now().UnixMilli(), likeZsetMaxSize, int64(postStatsTTL.Seconds()), want}

	result, err := Client.EvalSha(ctx, likeSetSHA, keys, args...).Int64()
	if err != nil && redis.HasErrorPrefix(err, "NOSCRIPT") {
		// Redis 重启/脚本缓存被清：重新加载后重试一次
		if likeSetSHA, err = Client.ScriptLoad(ctx, likeSetScript).Result(); err != nil {
			return 0, fmt.Errorf("failed to reload like set script: %w", err)
		}
		result, err = Client.EvalSha(ctx, likeSetSHA, keys, args...).Int64()
	}
	if err != nil {
		return 0, fmt.Errorf("failed to execute like set: %w", err)
	}
	return LikeSetResult(result), nil
}

// BackfillCommentLikes 将DB查询确认的点赞状态回填到ZSET
// ctx 由调用方传入，使超时/取消得以传播（覆盖包级 background ctx）。
func BackfillCommentLikes(ctx context.Context, userID uuid.UUID, likedCommentIDs []uuid.UUID) error {
	if len(likedCommentIDs) == 0 {
		return nil
	}
	zsetKey := GetUserCommentLikeListKey(userID)
	now := float64(time.Now().UnixMilli())
	members := make([]redis.Z, len(likedCommentIDs))
	for i, id := range likedCommentIDs {
		members[i] = redis.Z{Score: now, Member: id.String()}
	}
	pipe := Client.Pipeline()
	pipe.ZAdd(ctx, zsetKey, members...)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// raiseStatsLikeCountScript 仅当 post:stats 缓存存在且 like_count 低于落库后的 PG 值时抬高到 PG 值。
// 只升不降：缓存里可能还有未落库的点赞增量（缓存值 ≥ PG 值），降下来会让刚点的赞短暂消失。
const raiseStatsLikeCountScript = `
local cur = redis.call('HGET', KEYS[1], 'like_count')
if not cur then
    return 0
end
local want = tonumber(ARGV[1])
if tonumber(cur) < want then
    redis.call('HSET', KEYS[1], 'like_count', want)
    return 1
end
return 0
`

var raiseStatsLikeCount = redis.NewScript(raiseStatsLikeCountScript)

// ReconcilePostLikeCounts 点赞落库后校正缓存：缓存若在落库前由 PG 回填，会漏掉当时未落库的点赞而永久少计，
// 这里在每次落库后把低于 PG 的 like_count 抬回 PG 值（缓存不存在则跳过，下次回填自然读到新值）。
// 返回被校正的帖子数。
func ReconcilePostLikeCounts(counts map[uuid.UUID]int64) (int, error) {
	fixed := 0
	var firstErr error
	for postID, n := range counts {
		r, err := raiseStatsLikeCount.Run(ctx, Client, []string{GetPostStatsKey(postID)}, n).Int64()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to reconcile like_count of post %s: %w", postID, err)
			}
			continue
		}
		fixed += int(r)
	}
	return fixed, firstErr
}

// BackfillPostLikes 将DB查询确认的帖子点赞状态回填到ZSET
func BackfillPostLikes(userID uuid.UUID, likedPostIDs []uuid.UUID) error {
	if len(likedPostIDs) == 0 {
		return nil
	}
	zsetKey := GetUserPostLikeListKey(userID)
	now := float64(time.Now().UnixMilli())
	members := make([]redis.Z, len(likedPostIDs))
	for i, id := range likedPostIDs {
		members[i] = redis.Z{Score: now, Member: id.String()}
	}
	pipe := Client.Pipeline()
	pipe.ZAdd(ctx, zsetKey, members...)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}
