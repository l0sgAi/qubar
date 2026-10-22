package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"interestBar/pkg/domains/auth/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/redis/go-redis/v9"
)

// sessionIndexRedis 基于 Redis 的 SessionIndex 实现。
//
// 两个 key 成对维护（见 keys_auth.go）：
//   - auth:sessions:<uid>     ZSET，member=token，score=登录时间（Unix 毫秒）
//   - auth:session_meta:<uid> HASH，field=token，value=TerminalMeta JSON
//
// 写入用 MULTI 保证两个 key 同时更新、同时刷新 TTL。
type sessionIndexRedis struct{}

// NewSessionIndex 构造一个基于 Redis 的 SessionIndex。
func NewSessionIndex() domain.SessionIndex {
	return &sessionIndexRedis{}
}

// Add 登记 token 并刷新索引 TTL。score 取写入时的 Unix 毫秒（登录成功后立即写入，即登录时间）。
func (s *sessionIndexRedis) Add(ctx context.Context, loginID, token string, meta domain.TerminalMeta, ttl time.Duration) error {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal terminal meta: %w", err)
	}
	score := time.Now().UnixMilli()
	zkey, hkey := redispkg.GetAuthSessionsKey(loginID), redispkg.GetAuthSessionMetaKey(loginID)
	_, err = redispkg.Client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.ZAdd(ctx, zkey, redis.Z{Score: float64(score), Member: token})
		p.HSet(ctx, hkey, token, metaJSON)
		p.Expire(ctx, zkey, ttl)
		p.Expire(ctx, hkey, ttl)
		return nil
	})
	return err
}

// List 按登录时间升序返回全部 token（同一毫秒按 token 字典序）。
func (s *sessionIndexRedis) List(ctx context.Context, loginID string) ([]string, error) {
	return redispkg.Client.ZRange(ctx, redispkg.GetAuthSessionsKey(loginID), 0, -1).Result()
}

// Remove 删除给定 token 及其元数据。
func (s *sessionIndexRedis) Remove(ctx context.Context, loginID string, tokens ...string) error {
	if len(tokens) == 0 {
		return nil
	}
	members := make([]any, len(tokens))
	for i, t := range tokens {
		members[i] = t
	}
	_, err := redispkg.Client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.ZRem(ctx, redispkg.GetAuthSessionsKey(loginID), members...)
		p.HDel(ctx, redispkg.GetAuthSessionMetaKey(loginID), tokens...)
		return nil
	})
	return err
}

// Clear 删除整个索引。
func (s *sessionIndexRedis) Clear(ctx context.Context, loginID string) error {
	return redispkg.Client.Del(ctx, redispkg.GetAuthSessionsKey(loginID), redispkg.GetAuthSessionMetaKey(loginID)).Err()
}
