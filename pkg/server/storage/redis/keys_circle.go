// keys_circle.go：circle 领域的 Redis 键名常量与 key 构造函数。

package redis

import (
	"time"

	"github.com/google/uuid"
)

const (
	// CircleInfoPrefix 圈子基础信息缓存key前缀
	// 完整key格式: circle:info:{circle_id}
	// 包含字段：id, name, slug, avatar_url, cover_url, description, rule, creator_id, category_id, join_type, status, deleted, create_time, update_time
	CircleInfoPrefix = "circle:info:"

	// CircleStatsPrefix 圈子统计信息Hash key前缀
	// 完整key格式: circle:stats:{circle_id}
	// 包含字段：member_count, post_count, hot
	CircleStatsPrefix = "circle:stats:"

	// UserJoinedCirclesPrefix 用户已加入圈子 ZSET 缓存key前缀
	// 完整key格式: circle:joined:{user_id}
	// ZSET: member=circle_id, score=加入时间 Unix 毫秒，倒序（最近加入在前）
	// 注：前缀从 user_joined_circles: 改为 circle:joined:，避免旧 string 类型 key
	// 残留导致 ZSET 操作 WRONGTYPE（旧 key 任其 24h TTL 过期）。
	UserJoinedCirclesPrefix = "circle:joined:"

	// CirclePrivateIDsKey 私密圈（join_type=2）ID 集合缓存key（全局唯一）
	// string: JSON 数组（circle_id 字符串），空集合存 "[]"；TTL CirclePrivateIDsTTL。
	// 建圈、修改 join_type 时 DEL 失效；帖子 / 评论可见性过滤读取（私密圈很少，集合很小）。
	CirclePrivateIDsKey = "circle:private:ids"

	// CirclePrivateIDsTTL 私密圈 ID 集合缓存有效期（兜底失效与回填并发留下的旧值）。
	CirclePrivateIDsTTL = 5 * time.Minute
)

// CircleBaseInfo 圈子基础信息（不含统计信息）
type CircleBaseInfo struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	CoverURL    string     `json:"cover_url,omitempty"`
	Description string     `json:"description"`
	Rule        string     `json:"rule,omitempty"`
	CreatorID   uuid.UUID  `json:"creator_id"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	JoinType    int16      `json:"join_type"`
	Status      int16      `json:"status"`
	Deleted     int16      `json:"deleted"`
	CreateTime  time.Time  `json:"create_time"`
	UpdateTime  time.Time  `json:"update_time"`
}

// CircleStatistics 圈子统计信息（用于批量更新和MQ消费等场景）
// 注意：实际读取时直接从3个独立计数器读取，不使用此结构体
type CircleStatistics struct {
	MemberCount int `json:"member_count"` // 成员数量
	PostCount   int `json:"post_count"`   // 帖子数量
	Hot         int `json:"hot"`          // 热度
}

// GetUserJoinedCirclesKey 获取用户已加入圈子ID列表缓存的完整key
func GetUserJoinedCirclesKey(userID uuid.UUID) string {
	return UserJoinedCirclesPrefix + userID.String()
}

// GetCircleInfoKey 获取圈子基础信息缓存的完整key
func GetCircleInfoKey(circleID uuid.UUID) string {
	return CircleInfoPrefix + circleID.String()
}

// GetCircleStatsKey 获取圈子统计信息Hash缓存的完整key
func GetCircleStatsKey(circleID uuid.UUID) string {
	return CircleStatsPrefix + circleID.String()
}
