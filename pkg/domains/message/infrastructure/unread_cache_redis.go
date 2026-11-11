package infrastructure

import (
	"context"
	"errors"
	"strconv"

	"interestBar/pkg/domains/message/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// unreadCacheRedis 基于 Redis String 的私信未读总数缓存（key 见 redispkg.GetMessageUnreadKey）。
type unreadCacheRedis struct{}

// NewUnreadCache 构造私信未读总数缓存。
func NewUnreadCache() domain.UnreadCache {
	return unreadCacheRedis{}
}

// Get 见 domain.UnreadCache。值不是整数时视同 miss（回源校正）。
func (unreadCacheRedis) Get(ctx context.Context, userID uuid.UUID) (int64, bool, error) {
	val, err := redispkg.Client.Get(ctx, redispkg.GetMessageUnreadKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	count, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, false, nil
	}
	return count, true, nil
}

// Set 见 domain.UnreadCache。
func (unreadCacheRedis) Set(ctx context.Context, userID uuid.UUID, count int64) error {
	return redispkg.Client.Set(ctx, redispkg.GetMessageUnreadKey(userID), count, redispkg.MessageUnreadTTL).Err()
}

// incrIfExistsScript key 存在才 INCRBY（INCRBY 不改 TTL）；不存在返回 -1 不动作。
// 不能用普通 INCRBY：key 不存在时会从 0 起算并建出无 TTL 的 key，少计已有未读。
var incrIfExistsScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
    return -1
end
return redis.call('INCRBY', KEYS[1], ARGV[1])
`)

// IncrIfExists 见 domain.UnreadCache。
func (unreadCacheRedis) IncrIfExists(ctx context.Context, userID uuid.UUID, delta int64) error {
	err := incrIfExistsScript.Run(ctx, redispkg.Client, []string{redispkg.GetMessageUnreadKey(userID)}, delta).Err()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	return err
}

// Invalidate 见 domain.UnreadCache。
func (unreadCacheRedis) Invalidate(ctx context.Context, userID uuid.UUID) error {
	return redispkg.Client.Del(ctx, redispkg.GetMessageUnreadKey(userID)).Err()
}
