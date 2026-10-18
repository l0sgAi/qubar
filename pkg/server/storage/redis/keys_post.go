// keys_post.go：post 领域的 Redis 键名常量与 key 构造函数。

package redis

import (
	"fmt"

	"github.com/google/uuid"
)

// PostStatsPrefix 帖子统计信息Hash key前缀
// 完整key格式: post:stats:{post_id}
// 包含字段：view_count, comment_count, like_count, collect_count
const PostStatsPrefix = "post:stats:"

// PostStatistics 帖子统计信息
type PostStatistics struct {
	ViewCount    int `json:"view_count"`    // 浏览量
	CommentCount int `json:"comment_count"` // 评论数
	LikeCount    int `json:"like_count"`    // 点赞数
	CollectCount int `json:"collect_count"` // 收藏数
}

// GetPostStatsKey 获取帖子统计信息Hash缓存的完整key
func GetPostStatsKey(postID uuid.UUID) string {
	return PostStatsPrefix + postID.String()
}

// GetPostViewDedupeKey 获取帖子浏览去重 key
func GetPostViewDedupeKey(postID, userID uuid.UUID) string {
	return fmt.Sprintf("%s%s:%s", PostViewDedupePrefix, postID.String(), userID.String())
}

// PostViewDedupePrefix 帖子浏览去重 key 前缀
// 完整 key: post:viewdedup:{postID}:{userID}
// TTL: 5 分钟，同一用户对同一帖子在此窗口内只计一次浏览
const PostViewDedupePrefix = "post:viewdedup:"
