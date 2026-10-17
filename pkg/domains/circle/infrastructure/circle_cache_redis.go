package infrastructure

import (
	"context"
	"errors"
	"time"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// circleBaseCacheTTL 圈子基础信息缓存有效期（与旧 controller 一致：24 小时）。
const circleBaseCacheTTL = 24 * time.Hour

// circleBaseCacheRedis 基于 Redis 的 CircleBaseCache 实现（压缩存储）。
type circleBaseCacheRedis struct{}

// NewCircleBaseCache 构造 CircleBaseCache。
func NewCircleBaseCache() domain.CircleBaseCache {
	return &circleBaseCacheRedis{}
}

func (c *circleBaseCacheRedis) GetBase(ctx context.Context, circleID uuid.UUID) (*domain.CircleBaseInfo, error) {
	key := redispkg.GetCircleInfoKey(circleID)
	var info domain.CircleBaseInfo
	if err := redispkg.GetJSONCompressed(key, &info); err != nil {
		// 缓存 miss 不告警；其他错误（连接失败/反序列化失败）记日志便于观测
		if !errors.Is(err, redis.Nil) {
			logger.Log.Warn("Redis cache error for circle base info: " + err.Error())
		}
		return nil, nil
	}
	return &info, nil
}

func (c *circleBaseCacheRedis) SetBase(ctx context.Context, circleID uuid.UUID, info *domain.CircleBaseInfo) error {
	key := redispkg.GetCircleInfoKey(circleID)
	return redispkg.SetJSONCompressed(key, info, circleBaseCacheTTL)
}

// DeleteBase 删除圈子基础信息缓存（编辑圈子资料后失效；TTL 24h 兜底残余）。
func (c *circleBaseCacheRedis) DeleteBase(ctx context.Context, circleID uuid.UUID) error {
	return redispkg.Client.Del(ctx, redispkg.GetCircleInfoKey(circleID)).Err()
}

// circleStatsCacheRedis 基于 Redis Hash 的 CircleStatsCache 实现。
//
// 复用 redispkg 的 CircleStatistics 类型与原子操作函数，保持与旧 controller
// incrementCircleMemberCount / decrementCircleMemberCount / getCircleStatistics 行为一致。
type circleStatsCacheRedis struct{}

// NewCircleStatsCache 构造 CircleStatsCache。
func NewCircleStatsCache() domain.CircleStatsCache {
	return &circleStatsCacheRedis{}
}

func (c *circleStatsCacheRedis) GetStats(ctx context.Context, circleID uuid.UUID) (*domain.CircleStatistics, error) {
	stats, err := redispkg.GetCircleStatistics(circleID)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		return nil, nil
	}
	return &domain.CircleStatistics{
		MemberCount: stats.MemberCount,
		PostCount:   stats.PostCount,
		Hot:         stats.Hot,
	}, nil
}

func (c *circleStatsCacheRedis) StatsExists(ctx context.Context, circleID uuid.UUID) (bool, error) {
	return redispkg.CircleStatisticsExists(circleID)
}

func (c *circleStatsCacheRedis) SetStats(ctx context.Context, circleID uuid.UUID, stats *domain.CircleStatistics) error {
	return redispkg.UpdateCircleStatistics(circleID, &redispkg.CircleStatistics{
		MemberCount: stats.MemberCount,
		PostCount:   stats.PostCount,
		Hot:         stats.Hot,
	})
}

func (c *circleStatsCacheRedis) IncrMemberCount(ctx context.Context, circleID uuid.UUID) error {
	return redispkg.IncrementCircleMemberCount(circleID)
}

func (c *circleStatsCacheRedis) DecrMemberCount(ctx context.Context, circleID uuid.UUID) error {
	return redispkg.DecrementCircleMemberCount(circleID)
}

func (c *circleStatsCacheRedis) IncrPostCount(ctx context.Context, circleID uuid.UUID) error {
	return redispkg.IncrementCirclePostCount(circleID)
}

// joinedCirclesCacheTTL 用户加入圈子 ZSET 缓存有效期（与旧 controller 一致：24 小时）。
const joinedCirclesCacheTTL = 24 * time.Hour

// joinedKey 返回用户已加入圈子 ZSET 的完整 key（前缀 circle:joined:）。
func joinedKey(userID uuid.UUID) string {
	return redispkg.GetUserJoinedCirclesKey(userID)
}

// joinedCirclesCacheRedis 基于 Redis ZSET 的 JoinedCirclesCache 实现。
//
// member=circle_id(uuid hex), score=加入时间 Unix 毫秒，倒序（最近加入在前）。
// Add/Rebuild 时刷新 TTL。
type joinedCirclesCacheRedis struct{}

// NewJoinedCirclesCache 构造 JoinedCirclesCache。
func NewJoinedCirclesCache() domain.JoinedCirclesCache {
	return &joinedCirclesCacheRedis{}
}

func (c *joinedCirclesCacheRedis) PageByRank(ctx context.Context, userID uuid.UUID, start, limit int64) ([]uuid.UUID, error) {
	if limit <= 0 {
		return nil, nil
	}
	members, err := redispkg.Client.ZRangeArgs(ctx, redis.ZRangeArgs{Key: joinedKey(userID), Start: start, Stop: start + limit - 1, Rev: true}).Result()
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		if id, parseErr := uuid.Parse(m); parseErr == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *joinedCirclesCacheRedis) Card(ctx context.Context, userID uuid.UUID) (int64, error) {
	return redispkg.Client.ZCard(ctx, joinedKey(userID)).Result()
}

func (c *joinedCirclesCacheRedis) Exists(ctx context.Context, userID uuid.UUID) (bool, error) {
	n, err := redispkg.Client.Exists(ctx, joinedKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (c *joinedCirclesCacheRedis) Add(ctx context.Context, userID, circleID uuid.UUID, scoreMs int64) error {
	key := joinedKey(userID)
	pipe := redispkg.Client.TxPipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(scoreMs), Member: circleID.String()})
	pipe.Expire(ctx, key, joinedCirclesCacheTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *joinedCirclesCacheRedis) Remove(ctx context.Context, userID, circleID uuid.UUID) error {
	return redispkg.Client.ZRem(ctx, joinedKey(userID), circleID.String()).Err()
}

// Rebuild 先 Del 清旧，再分批 ZADD + 续 TTL。
// 分批（每批 500）防超大成员数单次 pipeline 过大。
func (c *joinedCirclesCacheRedis) Rebuild(ctx context.Context, userID uuid.UUID, members []domain.JoinedMember) error {
	const chunk = 500
	key := joinedKey(userID)
	first := true
	for i := 0; i < len(members); i += chunk {
		end := i + chunk
		if end > len(members) {
			end = len(members)
		}
		pipe := redispkg.Client.TxPipeline()
		if first {
			pipe.Del(ctx, key)
			first = false
		}
		for _, m := range members[i:end] {
			pipe.ZAdd(ctx, key, redis.Z{Score: float64(m.ScoreMs), Member: m.CircleID.String()})
		}
		pipe.Expire(ctx, key, joinedCirclesCacheTTL)
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	// 空成员：Del 清旧 key（避免残留）。
	if first {
		return redispkg.Client.Del(ctx, key).Err()
	}
	return nil
}
