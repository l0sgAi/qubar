// keys_discover.go：discover 领域的 Redis 键名常量与 key 构造函数。

package redis

// DiscoverPoolPrefix 发现页候选池 LIST key 前缀（登录用户独立池）。
// 完整 key 格式: discover:{posts|circles}:{user_id}
// LIST: 由 DiscoverPoolSyncer / 读路径 miss 随机采样后 RPUSH 写入的实体 ID(uuid 字符串)；
// LRANGE offset 翻页；TTL ttl_minutes(默认 30)。登录态为反气泡排除后的随机集。
const DiscoverPoolPrefix = "discover:"

// DiscoverAnonPrefix 发现页匿名共享池前缀。
// 完整 key: discover:anon:{posts|circles}
// 纯随机无排除，所有匿名用户共享；TTL 同登录池。匿名用户无排除集，全局随机采样。
const DiscoverAnonPrefix = "discover:anon:"

// DiscoverTokenPrefix 发现页候选池版本 token key 前缀。
// 完整 key: discover:token:{user_id|anon}（string），与池同 TTL。
// 客户端翻页回传 pool_token，服务端比对：不一致 → 池已重建 → 回 offset=0（防翻页错位）。
const DiscoverTokenPrefix = "discover:token:"

// GetDiscoverPoolKey 获取登录用户发现页候选池 LIST 的完整 key。
// section = "posts" | "circles"；userKey = user_id 字符串。
func GetDiscoverPoolKey(section, userKey string) string {
	return DiscoverPoolPrefix + section + ":" + userKey
}

// GetDiscoverAnonKey 获取匿名共享发现页候选池 LIST 的完整 key。section = "posts" | "circles"。
func GetDiscoverAnonKey(section string) string {
	return DiscoverAnonPrefix + section
}

// GetDiscoverTokenKey 获取发现页候选池版本 token 的完整 key。userKey = user_id 字符串或 "anon"。
func GetDiscoverTokenKey(userKey string) string {
	return DiscoverTokenPrefix + userKey
}
