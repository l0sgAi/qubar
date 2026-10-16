// keys_hot.go：热度（post hotcap / circle hot 累加器） 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

// PostHotCapPrefix 帖子热度上限计数器 Hash key 前缀
// 完整key格式: post:hotcap:{post_id}
// 包含字段: comment, comment_like（已累积的 hot 贡献值，用于 clamp per-post 上限）
// 仅评论 / 评论点赞两个维度有上限；点赞 / 收藏 / 分享无上限不写此 Hash。
const PostHotCapPrefix = "post:hotcap:"

// CircleHotPrefix 圈子热度增量累加器 key 前缀
// 完整key格式: circle:hot:{circle_id}
// string(int)：累加帖子 hot 的 fan-out Δ；CircleHotSyncer 定时 GETDEL 读后清零并落库。TTL 50h。
// 与 circle:stats:{circle_id} 的 hot 字段区别：本 key 是 Δ 累加器（待落库），stats hash 是读路径热值。
const CircleHotPrefix = "circle:hot:"

// GetPostHotCapKey 获取帖子热度上限计数器 Hash 的完整 key。
func GetPostHotCapKey(postID uuid.UUID) string {
	return PostHotCapPrefix + postID.String()
}

// GetCircleHotKey 获取圈子热度增量累加器的完整 key。
func GetCircleHotKey(circleID uuid.UUID) string {
	return CircleHotPrefix + circleID.String()
}
