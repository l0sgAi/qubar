// keys_comment.go：comment 领域的 Redis 键名常量与 key 构造函数。

package redis

import "github.com/google/uuid"

// CommentStatsPrefix 评论统计信息Hash key前缀
// 完整key格式: comment:stats:{comment_id}
// 包含字段：like_count
const CommentStatsPrefix = "comment:stats:"

// GetCommentStatsKey 获取评论统计信息Hash缓存的完整key
func GetCommentStatsKey(commentID uuid.UUID) string {
	return CommentStatsPrefix + commentID.String()
}
