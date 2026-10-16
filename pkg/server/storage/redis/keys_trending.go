// keys_trending.go：trending 领域的 Redis 键名常量与 key 构造函数。

package redis

// TrendingPrefix 热点榜单 ZSET key 前缀。
// 完整 key 格式: trending:{dimension}:{window}
//
//	dimension = post | circle | user
//	window    = 24h | 7d
//
// ZSET: member=实体 ID(uuid 字符串), score=热度（post 自身 hot / 圈子&用户为窗口内 Σhot）。
// 由 TrendingRankSyncer 周期性 ZADD 覆盖重写；不设 TTL（job 覆盖刷新），
// 不走 GETDEL（与 circle:hot Δ 累加器语义不同——那是增量累加待落库，本榜是全量重算快照）。
const TrendingPrefix = "trending:"

// TrendingMetaPrefix 热点榜单刷新时间戳 key 前缀。
// 完整 key: trending:meta:{dimension}:{window}（string, Unix 秒）。
// 读路径返回 refreshed_at，供前端显示「X 分钟前更新」与降级时标注时效。
const TrendingMetaPrefix = "trending:meta:"

// GetTrendingKey 获取热点榜单 ZSET 的完整 key。
func GetTrendingKey(dimension, window string) string {
	return TrendingPrefix + dimension + ":" + window
}

// GetTrendingMetaKey 获取热点榜单刷新时间戳的完整 key。
func GetTrendingMetaKey(dimension, window string) string {
	return TrendingMetaPrefix + dimension + ":" + window
}
