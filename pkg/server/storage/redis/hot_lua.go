package redis

import (
	"fmt"

	"interestBar/pkg/conf"

	"github.com/google/uuid"
)

// 热度维度常量（作为 post:hotcap:{postID} Hash 的字段名，仅 capped 维度写入）。
const (
	HotDimPostLike    = "post_like"    // 帖子点赞（无上限）
	HotDimPostCollect = "post_collect" // 帖子收藏（无上限）
	HotDimPostShare   = "post_share"   // 帖子分享（无上限；一人一帖只记一次、不可撤销，#232）
	HotDimComment     = "comment"      // 评论（上限 cap.comment）
	HotDimCommentLike = "comment_like" // 评论点赞（上限 cap.comment_like）
)

// applyHotDeltaScript 原子计算帖子热度增量（含权重 × 方向 × clamp）。
//
// KEYS[1] = post:hotcap:{postID}        （capped 维度的累计贡献 Hash）
// ARGV[1] = dim                         维度名（Hash 字段）
// ARGV[2] = dir                         +1 增加 / -1 撤销
// ARGV[3] = weight                      单次权重
// ARGV[4] = cap                         上限（<=0 表示无上限）
// ARGV[5] = ttl                         Hash TTL（秒）
//
// 返回最终签名 Δ（已 clamp）。无上限维度直接返回 weight*dir。
// 不变式：cap 必须为 weight 的整数倍，否则 undo 会与 clamp 边界产生微小漂移。
//   - comment:      cap=5000, weight=5  → 1000 条整数倍 ✓
//   - comment_like: cap=25000, weight=1 → 25000 整数倍 ✓
const applyHotDeltaScript = `
local dim    = ARGV[1]
local dir    = tonumber(ARGV[2])
local weight = tonumber(ARGV[3])
local cap    = tonumber(ARGV[4])
local ttl    = tonumber(ARGV[5])

if cap == nil or cap <= 0 then
	return weight * dir
end

local cur = tonumber(redis.call('HGET', KEYS[1], dim) or '0')
local delta
if dir > 0 then
	local remaining = cap - cur
	if remaining <= 0 then
		return 0
	end
	delta = math.min(weight, remaining)
	redis.call('HINCRBY', KEYS[1], dim, delta)
else
	-- 撤销：按已贡献值扣减，floor 0
	delta = math.min(weight, cur)
	redis.call('HINCRBY', KEYS[1], dim, -delta)
	delta = -delta
end
if ttl > 0 then
	redis.call('EXPIRE', KEYS[1], ttl)
end
return delta
`

var applyHotDeltaSHA string

// InitHotLuaScripts 预加载热度 Lua 脚本（启动时调用）。
func InitHotLuaScripts() error {
	var err error
	applyHotDeltaSHA, err = Client.ScriptLoad(ctx, applyHotDeltaScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load hot delta script: %w", err)
	}
	return nil
}

// hotWeightCap 返回维度的权重与上限（cap=0 表示无上限）。来自 conf.Hot。
func hotWeightCap(dim string) (weight, cap int64) {
	h := conf.Config.Hot
	switch dim {
	case HotDimPostLike:
		return int64(h.Weight.PostLike), 0
	case HotDimPostCollect:
		return int64(h.Weight.PostCollect), 0
	case HotDimPostShare:
		return int64(h.Weight.PostShare), 0
	case HotDimComment:
		return int64(h.Weight.Comment), int64(h.Cap.Comment)
	case HotDimCommentLike:
		return int64(h.Weight.CommentLike), int64(h.Cap.CommentLike)
	}
	return 0, 0
}

// ApplyHotDelta 原子计算并累积帖子热度增量（权重 × 方向 × clamp）。
//
// dir: +1 增加, -1 撤销（toggle/删除时反向）。返回最终签名 Δ（已 clamp），供 MQ 发布。
// 调用方拿到 Δ 后 PublishPostHot(postID, Δ)；Δ=0（被 cap 截断或未知维度）则不必发布。
func ApplyHotDelta(postID uuid.UUID, dim string, dir int) (int64, error) {
	weight, cap := hotWeightCap(dim)
	if weight == 0 {
		return 0, nil // 未知维度或权重 0，不发 hot
	}

	hotcapKey := GetPostHotCapKey(postID)
	ttlSeconds := int64(postStatsTTL.Seconds())

	result, err := Client.EvalSha(ctx, applyHotDeltaSHA,
		[]string{hotcapKey},
		dim, dir, weight, cap, ttlSeconds,
	).Int64()
	if err != nil {
		// SHA 丢失（Redis 重启）→ 重新加载并重试
		applyHotDeltaSHA, err = Client.ScriptLoad(ctx, applyHotDeltaScript).Result()
		if err != nil {
			return 0, fmt.Errorf("failed to reload hot delta script: %w", err)
		}
		result, err = Client.EvalSha(ctx, applyHotDeltaSHA,
			[]string{hotcapKey},
			dim, dir, weight, cap, ttlSeconds,
		).Int64()
		if err != nil {
			return 0, fmt.Errorf("failed to execute hot delta: %w", err)
		}
	}
	return result, nil
}
