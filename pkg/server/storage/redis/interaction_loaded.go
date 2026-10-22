// interaction_loaded.go：用户点赞 / 收藏 ZSET 的批查与"完整加载"标记（#129）。
//
// ZSET（user:like:posts / user:collect:posts / user:like:comments）只存"已赞 / 已藏"，
// 有 TTL 与容量上限，单看 ZSET 时 miss ≠ 未赞，必须回源 DB。首次回源时把用户全部记录装入 ZSET
// 并写完整加载标记后，miss 即 false，信息流 / 评论列表渲染不再查 PG。
//
// 一致性约束：标记与 ZSET 的过期时刻相同，且只在与 ZSET 同一原子操作里设置 / 续期
// （本文件的 MULTI、like_lua.go / collect_lua.go 的设值脚本），因此不会出现"标记在、ZSET 已过期"；
// 设值脚本按容量淘汰成员时把标记降为 "0"，不会出现"标记为完整、ZSET 缺成员"。
// 生产 Redis 是 noeviction（deploy/compose/middleware/redis.conf），不会被内存淘汰拆散。

package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	// interactionLoadedComplete 完整加载标记值：ZSET 即该用户全部有效记录。
	interactionLoadedComplete = "1"
	// interactionLoadedOverflow 超限标记值：记录数超过全量装载上限（或 ZSET 发生过容量淘汰），miss 仍需回源。
	interactionLoadedOverflow = "0"
)

// InteractionLoadState 用户交互 ZSET 的完整加载状态。
type InteractionLoadState int8

const (
	// InteractionUnloaded 标记不存在：ZSET 只是部分缓存，miss 需回源；调用方可尝试全量装载。
	InteractionUnloaded InteractionLoadState = iota
	// InteractionComplete 已完整加载：miss 即 false。
	InteractionComplete
	// InteractionOverflow 超限：miss 逐批回源，不再尝试全量装载。
	InteractionOverflow
)

// InteractionCheck 交互 ZSET 批查结果。
type InteractionCheck struct {
	// Hits 每个查询 ID → 是否命中 ZSET（已赞 / 已藏）；未命中为 false。
	Hits map[uuid.UUID]bool
	// Missed 缓存无法判定、需回源 DB 的 ID；已完整加载时为空。
	Missed []uuid.UUID
	// State 完整加载标记状态。
	State InteractionLoadState
}

// InteractionEntry 全量装载的一条有效记录：目标 ID + 交互时间（作为 ZSET score）。
type InteractionEntry struct {
	ID uuid.UUID
	At time.Time
}

// BatchCheckPostLiked 批量检查用户是否点赞了多个帖子（ZSET + 完整加载标记）。
func BatchCheckPostLiked(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (InteractionCheck, error) {
	return batchCheckInteraction(ctx, GetUserPostLikeListKey(userID), GetUserPostLikeLoadedKey(userID), postIDs)
}

// BatchCheckPostCollected 批量检查用户是否收藏了多个帖子（ZSET + 完整加载标记）。
func BatchCheckPostCollected(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (InteractionCheck, error) {
	return batchCheckInteraction(ctx, GetUserPostCollectListKey(userID), GetUserPostCollectLoadedKey(userID), postIDs)
}

// BatchCheckCommentLiked 批量检查用户是否点赞了多条评论（ZSET + 完整加载标记）。
func BatchCheckCommentLiked(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (InteractionCheck, error) {
	return batchCheckInteraction(ctx, GetUserCommentLikeListKey(userID), GetUserCommentLikeLoadedKey(userID), commentIDs)
}

// LoadPostLikes 全量装载用户的帖子点赞并写完整加载标记；complete=false 表示超过装载上限，只写超限标记。
func LoadPostLikes(ctx context.Context, userID uuid.UUID, entries []InteractionEntry, complete bool) error {
	return loadInteractions(ctx, GetUserPostLikeListKey(userID), GetUserPostLikeLoadedKey(userID), entries, complete)
}

// LoadPostCollects 全量装载用户的帖子收藏并写完整加载标记；complete=false 只写超限标记。
func LoadPostCollects(ctx context.Context, userID uuid.UUID, entries []InteractionEntry, complete bool) error {
	return loadInteractions(ctx, GetUserPostCollectListKey(userID), GetUserPostCollectLoadedKey(userID), entries, complete)
}

// LoadCommentLikes 全量装载用户的评论点赞并写完整加载标记；complete=false 只写超限标记。
func LoadCommentLikes(ctx context.Context, userID uuid.UUID, entries []InteractionEntry, complete bool) error {
	return loadInteractions(ctx, GetUserCommentLikeListKey(userID), GetUserCommentLikeLoadedKey(userID), entries, complete)
}

// batchCheckInteraction 一次 MULTI：ZMSCORE + 读标记 + 以同一绝对过期时刻续期 ZSET 与标记。
// ZMSCORE 对不存在的成员返回 0，score 是毫秒时间戳（>0），score==0 等价于不存在。
func batchCheckInteraction(ctx context.Context, zsetKey, loadedKey string, ids []uuid.UUID) (InteractionCheck, error) {
	res := InteractionCheck{Hits: make(map[uuid.UUID]bool, len(ids))}
	if len(ids) == 0 {
		return res, nil
	}
	members := make([]string, len(ids))
	for i, id := range ids {
		members[i] = id.String()
	}

	deadline := time.Now().Add(postStatsTTL)
	pipe := Client.TxPipeline()
	scoresCmd := pipe.ZMScore(ctx, zsetKey, members...)
	loadedCmd := pipe.Get(ctx, loadedKey)
	pipe.PExpireAt(ctx, zsetKey, deadline)
	pipe.PExpireAt(ctx, loadedKey, deadline) // 标记不存在时是 no-op
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return res, fmt.Errorf("failed to batch check %s: %w", zsetKey, err)
	}
	scores, err := scoresCmd.Result()
	if err != nil {
		return res, fmt.Errorf("failed to batch check %s: %w", zsetKey, err)
	}
	switch loaded, _ := loadedCmd.Result(); loaded {
	case interactionLoadedComplete:
		res.State = InteractionComplete
	case interactionLoadedOverflow:
		res.State = InteractionOverflow
	}

	for i, score := range scores {
		hit := score > 0
		res.Hits[ids[i]] = hit
		if !hit && res.State != InteractionComplete {
			res.Missed = append(res.Missed, ids[i])
		}
	}
	return res, nil
}

// loadInteractions 一次 MULTI 装入全量记录并写标记（ZSET 与标记共用同一绝对过期时刻）。
//
// ZADD NX：已在 ZSET 的成员（含 DB 尚未落库的新赞）保留原 score；标记 SET NX：并发装载 / 设值脚本
// 已写入的标记（如容量淘汰降级的 "0"）不被覆盖。超限时不动 ZSET，只记 "0" 避免每次请求重复全量查询。
func loadInteractions(ctx context.Context, zsetKey, loadedKey string, entries []InteractionEntry, complete bool) error {
	deadline := time.Now().Add(postStatsTTL)
	pipe := Client.TxPipeline()
	value := interactionLoadedOverflow
	if complete {
		value = interactionLoadedComplete
		if len(entries) > 0 {
			zs := make([]redis.Z, len(entries))
			now := time.Now()
			for i, e := range entries {
				at := e.At
				if at.IsZero() {
					at = now // score 必须 >0，否则批查会当作不存在
				}
				zs[i] = redis.Z{Score: float64(at.UnixMilli()), Member: e.ID.String()}
			}
			pipe.ZAddNX(ctx, zsetKey, zs...)
		}
		pipe.PExpireAt(ctx, zsetKey, deadline)
	}
	pipe.SetArgs(ctx, loadedKey, value, redis.SetArgs{Mode: "NX", ExpireAt: deadline})
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to load %s: %w", zsetKey, err)
	}
	return nil
}
