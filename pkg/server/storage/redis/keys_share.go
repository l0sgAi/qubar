// keys_share.go：share 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

// PostShareCountPrefix 帖子分享数 String key 前缀（#232）。
// 完整key格式: post:sharecnt:{post_id}
// 值: 去重后的分享人数（读路径真值）；TTL 与 post:stats 相同（postStatsTTL），写入 / 自增时续期。
// 不放进 post:stats Hash：只写一个字段会建出半截 Hash，RestoreStats 以 EXISTS 判断已回种，会跳过回种。
// miss 时由 share 域按 domains.post_share 行数回种（SET NX），帖子统计落库后只升不降校准（见 share_lua.go）。
const PostShareCountPrefix = "post:sharecnt:"

// GetPostShareCountKey 获取帖子分享数 String 的完整key
func GetPostShareCountKey(postID uuid.UUID) string {
	return PostShareCountPrefix + postID.String()
}
