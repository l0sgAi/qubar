// Package domain 定义 discover 领域的端口与 DTO。
//
// discover 是跨域编排器（无聚合根，与 recommend/trending 同范式）：
// random_score 随机采样圈子+帖子，登录态做反气泡排除（已加圈子+已交互帖），匿名退化为纯随机。
// 所有跨域依赖经此包接口抽象，由 composition 注入桥接器，本包不 import post/circle/recommend 的
// application 包，避免环依赖。
//
// 唯一例外是复用 recommend.domain.FeedPostItem 作为帖子项展示 DTO（它是纯值对象，无 infra 依赖，
// 与 trending/domain/ports.go 的 DTO 复用惯例一致——避免重复定义同形结构）。
package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	circledomain "interestBar/pkg/domains/circle/domain"
	recommenddomain "interestBar/pkg/domains/recommend/domain"
)

// 板块枚举（用于 GetDiscover section 参数）。
const (
	SectionAll     = "all"
	SectionPosts   = "posts"
	SectionCircles = "circles"
)

// AnonUserKey 匿名用户的 userKey 字面量（登录用 user_id 字符串区分）。
const AnonUserKey = "anon"

// ErrInvalidSection 请求的 section 不支持。
var ErrInvalidSection = errors.New("discover section not supported")

// DiscoverPostItem 发现页帖子项（复用 recommend.FeedPostItem，纯 DTO 嵌入）。
type DiscoverPostItem struct {
	recommenddomain.FeedPostItem
}

// DiscoverCircleItem 发现页圈子项（镜像 trending.TrendingCircleItem 去掉 HotScore）。
type DiscoverCircleItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Description string `json:"description,omitempty"`
	CategoryID  string `json:"category_id,omitempty"`
	MemberCount int    `json:"member_count"`
	PostCount   int    `json:"post_count"`
	Hot         int    `json:"hot"`
	JoinType    int16  `json:"join_type"`
	CreateTime  string `json:"create_time"`
}

// DiscoverBoard 发现页聚合看板（GET /discover 响应体）。
//
// 圈子+帖子两分区独立分页；section=all 时两分区各返 size（首屏聚合，忽略 offset）。
type DiscoverBoard struct {
	Circles       []DiscoverCircleItem `json:"circles,omitempty"`
	Posts         []DiscoverPostItem   `json:"posts,omitempty"`
	PoolToken     string               `json:"pool_token,omitempty"`     // 候选池版本 token（客户端回传比对）
	HasMore       bool                 `json:"has_more"`                 // 是否还有更多
	PoolRefreshed bool                 `json:"pool_refreshed,omitempty"` // 池已重建，本次回 offset=0
	Offset        int                  `json:"offset,omitempty"`         // 单分区翻页时回显
	Size          int                  `json:"size"`
}

// DiscoverPoolStore 发现页候选池存储（Redis LIST + 版本 token）。
//
// 读路径用 Range/Token/Exists/Len；写路径用 Rebuild（供 syncer 与读路径 miss 共用，原子 DEL+RPUSH+Set token）。
type DiscoverPoolStore interface {
	// Range LRANGE [offset, offset+size) 取实体 ID（按 LIST 写入序，即随机序）。
	Range(ctx context.Context, section, userKey string, offset, size int64) ([]uuid.UUID, error)
	// Token 当前池版本 token（池不存在返回 ""）。
	Token(ctx context.Context, userKey string) (string, error)
	// Exists 池是否存在（未过 TTL）。
	Exists(ctx context.Context, section, userKey string) (bool, error)
	// Len 池当前长度（LLEN）。
	Len(ctx context.Context, section, userKey string) (int64, error)
	// Rebuild 原子重建：DEL 旧池 → RPUSH 有序 IDs → 写版本 token（池与 token 同 TTL）。返回新 token。
	Rebuild(ctx context.Context, section, userKey string, ids []uuid.UUID, ttl time.Duration) (string, error)
}

// PostHydrator 把 postID 列表 hydrate 成展示项（作者/圈子/图片/统计；不含交互态）。
//
// 直接复用 recommend.domain.FeedPostItem 作为返回类型（纯 DTO，无 infra 依赖）。
//
// viewerID 不可见的私密圈帖子不返回（榜单 / 候选池是全局或缓存的，回填时按查看者过滤）。
type PostHydrator interface {
	Hydrate(ctx context.Context, viewerID uuid.UUID, postIDs []uuid.UUID) ([]recommenddomain.FeedPostItem, error)
}

// InteractionChecker 批量回填 is_liked/is_collected（基于 user:like/collect ZSET）。
// 仅登录态调用；匿名跳过（交互态视为 false）。
type InteractionChecker interface {
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (liked, collected map[uuid.UUID]bool, err error)
}

// CircleLookup 批量取圈子实体（桥接 circle.domain.CircleRepository.GetByIDs）。
// 返回完整 Circle 实体（含 member_count/post_count/hot），由 service 组装 DiscoverCircleItem。
type CircleLookup interface {
	GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*circledomain.Circle, error)
}

// SeedReader 用户互动种子（反气泡排除用：liked/collected/viewed 帖）。
// 复用 recommend 同名端口签名（桥接 redispkg user:like/collect/view ZSET）。
type SeedReader interface {
	LikedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
	CollectedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
	ViewedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
}

// JoinedCircleLookup 用户已加入的圈子 ID 列表（反气泡排除已加圈子用）。
// 复用 recommend.CircleLookup 签名（桥接 circle.application.CircleService.ListJoinedCircleIDs）。
type JoinedCircleLookup interface {
	ListJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
}
