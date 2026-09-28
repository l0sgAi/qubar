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

-- Renew TTL on both keys
redis.call('EXPIRE', statsKey, ttl)
redis.call('EXPIRE', zsetKey, ttl)
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
	return executeLikeSet(ctx, GetCommentStatsKey(commentID), GetUserCommentLikeListKey(userID), commentID, liked)
}

// SetPostLike 原子设置帖子点赞状态（liked=true 赞 / false 取消）。
func SetPostLike(ctx context.Context, userID, postID uuid.UUID, liked bool) (LikeSetResult, error) {
	return executeLikeSet(ctx, GetPostStatsKey(postID), GetUserPostLikeListKey(userID), postID, liked)
}

func executeLikeSet(ctx context.Context, statsKey, zsetKey string, targetID uuid.UUID, liked bool) (LikeSetResult, error) {
	want := 0
	if liked {
		want = 1
	}
	keys := []string{statsKey, zsetKey}
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

// BatchCheckCommentLiked 批量检查用户是否点赞了多条评论
// ZMScore 对不存在的成员返回 float64(0)，由于我们的 score 是时间戳(>0)，score==0 等价于不存在
// ctx 由调用方传入，使超时/取消得以传播（覆盖包级 background ctx）。
func BatchCheckCommentLiked(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(commentIDs) == 0 {
		return make(map[uuid.UUID]bool), nil
	}

	zsetKey := GetUserCommentLikeListKey(userID)
	members := make([]string, len(commentIDs))
	for i, id := range commentIDs {
		members[i] = id.String()
	}

	scores, err := Client.ZMScore(ctx, zsetKey, members...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to batch check comment liked: %w", err)
	}

	result := make(map[uuid.UUID]bool, len(commentIDs))
	for i, score := range scores {
		result[commentIDs[i]] = score > 0
	}

	// Renew TTL on the ZSET since we accessed it
	Client.Expire(ctx, zsetKey, postStatsTTL)

	return result, nil
}

// GetCommentLikedMissIDs 从 BatchCheckCommentLiked 结果中提取缓存未命中的ID列表
func GetCommentLikedMissIDs(commentIDs []uuid.UUID, likedMap map[uuid.UUID]bool) []uuid.UUID {
	var missIDs []uuid.UUID
	for _, id := range commentIDs {
		if !likedMap[id] {
			missIDs = append(missIDs, id)
		}
	}
	return missIDs
}

// BatchCheckPostLiked 批量检查用户是否点赞了多个帖子
func BatchCheckPostLiked(userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, []uuid.UUID, error) {
	if len(postIDs) == 0 {
		return make(map[uuid.UUID]bool), nil, nil
	}

	zsetKey := GetUserPostLikeListKey(userID)
	members := make([]string, len(postIDs))
	for i, id := range postIDs {
		members[i] = id.String()
	}

	scores, err := Client.ZMScore(ctx, zsetKey, members...).Result()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to batch check post liked: %w", err)
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
