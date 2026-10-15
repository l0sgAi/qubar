package redis

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// historyRecordScript 浏览历史记录原子脚本。
//
// 与 collectToggleScript 同构,但 history 是单向记录(无 toggle/ZREM/stats Hash 交互):
//   - ZADD upsert:已存在则更新 score(移到「最近浏览」顶部),不存在则插入;
//   - 超限 trim:仅保留最近 maxSize 条(按 score 降序),淘汰最低分(最久未访);
//   - 续期 TTL(复用 postStatsTTL,与 collect/like ZSET 一致)。
const historyRecordScript = `
local zsetKey = KEYS[1]
local postId  = ARGV[1]
local now     = tonumber(ARGV[2])
local maxSize = tonumber(ARGV[3])
local ttl     = tonumber(ARGV[4])

-- upsert:更新 score(移到顶部) 或插入
redis.call('ZADD', zsetKey, now, postId)

-- trim:仅保留 score 最高的 maxSize 条,淘汰最低分
local size = redis.call('ZCARD', zsetKey)
if tonumber(size) > maxSize then
    local removeCount = tonumber(size) - maxSize
    redis.call('ZREMRANGEBYRANK', zsetKey, 0, removeCount - 1)
end

redis.call('EXPIRE', zsetKey, ttl)
return 1
`

var historyRecordSHA string

// historyMaxZsetSize 浏览历史 ZSET 容量上限(用户要求 500 条)。
const historyMaxZsetSize int64 = 500

// InitHistoryLuaScripts 预加载浏览历史 Lua 脚本到 Redis(启动时调用)。
func InitHistoryLuaScripts() error {
	var err error
	historyRecordSHA, err = Client.ScriptLoad(ctx, historyRecordScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load history record script: %w", err)
	}
	return nil
}

// RecordPostView 原子记录一次浏览(ZADD upsert + trim 500 + EXPIRE)。
// score=当前时间戳(Unix 毫秒),即「最近访问时间」;重复浏览同帖则 bump score 移到顶部。
func RecordPostView(userID, postID uuid.UUID) error {
	zsetKey := GetUserPostViewListKey(userID)
	now := time.Now().UnixMilli()
	ttlSeconds := int64(postStatsTTL.Seconds())

	_, err := Client.EvalSha(ctx, historyRecordSHA,
		[]string{zsetKey},
		postID.String(), now, historyMaxZsetSize, ttlSeconds,
	).Int64()

	if err != nil {
		// Redis 重启后 SHA 失效,重新加载并重试(与 collect/like 一致)
		historyRecordSHA, err = Client.ScriptLoad(ctx, historyRecordScript).Result()
		if err != nil {
			return fmt.Errorf("failed to reload history record script: %w", err)
		}
		_, err = Client.EvalSha(ctx, historyRecordSHA,
			[]string{zsetKey},
			postID.String(), now, historyMaxZsetSize, ttlSeconds,
		).Int64()
		if err != nil {
			return fmt.Errorf("failed to execute history record: %w", err)
		}
	}
	return nil
}

// PostViewEntry 浏览历史条目(postID + 最近访问时间)。
// ViewedAt 由 ZSET score(Unix 毫秒)还原;冷启动回源时由 DB update_time 还原。
type PostViewEntry struct {
	ID       string
	ViewedAt time.Time
}

// ListPostViews 倒序取浏览历史 ZSET 的 [offset, offset+size-1] 区间条目(含访问时间)。
// 返回 PostViewEntry 列表(按最近访问时间倒序)、总数(ZCARD)、error。
// 用于「最近浏览」列表 offset 分页 + 展示「N 分钟前看过」。
func ListPostViews(userID uuid.UUID, offset, size int64) ([]PostViewEntry, int64, error) {
	zsetKey := GetUserPostViewListKey(userID)

	pipe := Client.Pipeline()
	zrevRange := pipe.ZRevRangeWithScores(ctx, zsetKey, offset, offset+size-1)
	zcard := pipe.ZCard(ctx, zsetKey)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, 0, fmt.Errorf("failed to list post views: %w", err)
	}

	members, err := zrevRange.Result()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get post view members: %w", err)
	}
	total, err := zcard.Result()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get post view total: %w", err)
	}

	entries := make([]PostViewEntry, 0, len(members))
	for _, z := range members {
		id, ok := z.Member.(string)
		if !ok {
			continue
		}
		entries = append(entries, PostViewEntry{
			ID:       id,
			ViewedAt: time.UnixMilli(int64(z.Score)),
		})
	}
	return entries, total, nil
}

// BackfillPostViews 将 DB 回源确认的浏览历史批量回填到 ZSET。
// 用于 ZSET 冷启动(ZCARD==0)时从 DB top500 恢复。
//
// 与旧版差异:用每条的 ViewedAt(= DB update_time)作 score,而非统一时间,
// 保证冷热路径 viewed_at 一致;ZSET score 降序自然等于 update_time 倒序(DB 入参已排序)。
func BackfillPostViews(userID uuid.UUID, entries []PostViewEntry) error {
	if len(entries) == 0 {
		return nil
	}
	zsetKey := GetUserPostViewListKey(userID)
	members := make([]redis.Z, 0, len(entries))
	for _, e := range entries {
		members = append(members, redis.Z{
			Score:  float64(e.ViewedAt.UnixMilli()),
			Member: e.ID,
		})
	}
	pipe := Client.Pipeline()
	pipe.ZAdd(ctx, zsetKey, members...)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// ListPostLikedIDs 倒序取用户「点赞过的帖子」ZSET 的前 limit 个 postID（供推荐流 CF seed / 行为圈子）。
//
// ZSET user:like:posts:{user_id}，score=最近访问时间ms；ZREVRANGE 取最近点赞。
// 仅返回 postID 字符串（推荐召回只关心 ID，不需要时间）。
func ListPostLikedIDs(userID uuid.UUID, limit int64) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	members, err := Client.ZRangeArgs(ctx, redis.ZRangeArgs{Key: GetUserPostLikeListKey(userID), Start: 0, Stop: limit - 1, Rev: true}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list liked post ids: %w", err)
	}
	return members, nil
}

// ListPostCollectedIDs 倒序取用户「收藏过的帖子」ZSET 的前 limit 个 postID（供推荐流 CF seed）。
//
// ZSET user:collect:posts:{user_id}，score=最近访问时间ms；ZREVRANGE 取最近收藏。
func ListPostCollectedIDs(userID uuid.UUID, limit int64) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	members, err := Client.ZRangeArgs(ctx, redis.ZRangeArgs{Key: GetUserPostCollectListKey(userID), Start: 0, Stop: limit - 1, Rev: true}).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list collected post ids: %w", err)
	}
	return members, nil
}
