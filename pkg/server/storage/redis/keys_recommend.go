// keys_recommend.go：recommend（CF / 候选池 / 兴趣圈子） 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

// CFItemPrefix item-based 协同过滤「相似帖」ZSET key 前缀。
// 完整 key 格式: cf:item:{post_id}
// ZSET: member=相似 post_id(uuid 字符串), score=相似度(0..1]
// 由 ItemCFSyncer 夜级全量计算写入；TTL zset_ttl_hours(默认 48h)。
// 召回时：用户 seed 帖(点赞/收藏) → ZREVRANGE cf:item:{seed} 取 top 相似帖。
const CFItemPrefix = "cf:item:"

// GetCFItemKey 获取帖子协同过滤相似帖 ZSET 的完整 key。
func GetCFItemKey(postID uuid.UUID) string {
	return CFItemPrefix + postID.String()
}

// RecommendFeedPrefix 推荐流候选池 LIST key 前缀。
// 完整 key 格式: feed:recommend:{user_id}
// LIST: 按推荐序 RPUSH 的 post_id(uuid 字符串)；LRANGE offset 分页；TTL ttl_minutes(默认 30)。
// 池 miss/过期时由 RecommendService 触发重建（5 路召回 + 交错合并）。
const RecommendFeedPrefix = "feed:recommend:"

// RecommendFeedTokenPrefix 推荐流候选池版本 token key 前缀。
// 完整 key: feed:recommend:token:{user_id}（string），与池同 TTL。
// 客户端翻页回传 token，服务端比对：不一致 → 池已重建 → 回 offset=0（防翻页错位）。
const RecommendFeedTokenPrefix = "feed:recommend:token:"

// GetRecommendFeedKey 获取推荐流候选池 LIST 的完整 key。
func GetRecommendFeedKey(userID uuid.UUID) string {
	return RecommendFeedPrefix + userID.String()
}

// GetRecommendFeedTokenKey 获取推荐流候选池版本 token 的完整 key。
func GetRecommendFeedTokenKey(userID uuid.UUID) string {
	return RecommendFeedTokenPrefix + userID.String()
}

// UserInterestCirclesPrefix 用户「行为兴趣圈子」SET key 前缀。
// 完整 key 格式: user:interest_circles:{user_id}
// SET: 由用户点赞/收藏的 seed 帖子反查得到的 circle_id 集合（C3 行为圈子召回用）。
// miss 时从 DB 反查并落缓存；TTL interest_circles_ttl_minutes(默认 120)。读路径减去 joined circles。
const UserInterestCirclesPrefix = "user:interest_circles:"

// GetUserInterestCirclesKey 获取用户行为兴趣圈子 SET 的完整 key。
func GetUserInterestCirclesKey(userID uuid.UUID) string {
	return UserInterestCirclesPrefix + userID.String()
}
