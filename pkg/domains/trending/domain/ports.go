// Package domain 定义 trending 领域的端口与 DTO。
//
// trending 是跨域编排器（无聚合根，与 recommend 同范式）：聚合 post/circle/user 三类热点榜单。
// 所有跨域依赖经此包接口抽象，由 composition 注入桥接器，本包不 import post/circle/user 的
// application 包，避免环依赖。
//
// 唯一例外是复用 recommend.domain.FeedPostItem 作为帖子项展示 DTO（它是纯值对象，无 infra 依赖，
// 与本项目"消费域重新声明 Facade 接口"的惯例一致——DTO 复用避免重复定义同形结构）。
package domain

import (
	"context"

	"github.com/google/uuid"

	circledomain "interestBar/pkg/domains/circle/domain"
	recommenddomain "interestBar/pkg/domains/recommend/domain"
)

// 热点维度与时间窗口枚举（字符串字面量，用于构造 Redis key 与 ES 入参）。
const (
	DimensionPost   = "post"
	DimensionCircle = "circle"
	DimensionUser   = "user"

	Window24h = "24h"
	Window7d  = "7d"

	SectionAll     = "all"
	SectionPosts   = "posts"
	SectionCircles = "circles"
	SectionUsers   = "users"
)

// ScoredID ZSET 读出的「实体 ID + 热度分」，按 score 降序。
type ScoredID struct {
	ID    uuid.UUID
	Score float64
}

// TrendingPostItem 热门帖子项。
//
// 复用 recommend.domain.FeedPostItem（作者/圈子/图片/统计 + is_liked/is_collected），
// 由 PostHydrator + InteractionChecker 回填；HotScore 来自 ZSET score（窗口内 post.hot）。
type TrendingPostItem struct {
	recommenddomain.FeedPostItem
	HotScore float64 `json:"hot_score"` // 窗口内 hot（帖子榜=post.hot）
}

// TrendingCircleItem 热门圈子项（镜像 circle/application ActiveCircleDoc + 打分）。
type TrendingCircleItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	AvatarURL   string  `json:"avatar_url,omitempty"`
	Description string  `json:"description,omitempty"`
	CategoryID  string  `json:"category_id,omitempty"`
	MemberCount int     `json:"member_count"`
	PostCount   int     `json:"post_count"` // 累积（circle.post_count）
	Hot         int     `json:"hot"`        // 累积（circle.hot）
	JoinType    int16   `json:"join_type"`
	CreateTime  string  `json:"create_time"`
	HotScore    float64 `json:"hot_score"` // ★ 窗口内 Σhot（趋势信号）
}

// TrendingUserItem 热门用户项（镜像 user UserBrief + 打分）。
type TrendingUserItem struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	AvatarURL string  `json:"avatar_url,omitempty"`
	HotScore  float64 `json:"hot_score"` // ★ 窗口内 Σhot
}

// TrendingBoard 热点聚合看板（GET /trending 响应体）。
type TrendingBoard struct {
	Window      string               `json:"window"`              // "24h" | "7d"
	Posts       []TrendingPostItem   `json:"posts,omitempty"`     // section=all|posts 时填充
	Circles     []TrendingCircleItem `json:"circles,omitempty"`   // section=all|circles 时填充
	Users       []TrendingUserItem   `json:"users,omitempty"`     // section=all|users 时填充
	RefreshedAt int64                `json:"refreshed_at"`        // 榜单最近刷新 Unix 秒（0=从未刷新）
	Truncated   bool                 `json:"truncated,omitempty"` // 触达 top_n 上限
	Offset      int                  `json:"offset,omitempty"`    // 单板块翻页时回显
	Size        int                  `json:"size"`
}

// ===== 端口接口（跨域依赖抽象，composition 注入桥接器） =====

// BoardStore 热点榜单 ZSET 存储（Redis 实现）。
//
// 读路径用 Range/RefreshedAt（供 TrendingService）；
// 写路径用 Rewrite（供 TrendingRankSyncer，覆盖式重写：DEL+ZADD+SetMeta 原子）。
type BoardStore interface {
	// Range 按热度降序读取 [offset, offset+size) 的实体 + 分数。
	Range(ctx context.Context, dimension, window string, offset, size int64) ([]ScoredID, error)
	// RefreshedAt 返回榜单最近刷新 Unix 秒（从未刷新返回 0）。
	RefreshedAt(ctx context.Context, dimension, window string) (int64, error)
	// Rewrite 覆盖式重写榜单：DEL 旧 → ZADD 入本轮 Top-N → 记刷新时间。供 syncer 调用。
	Rewrite(ctx context.Context, dimension, window string, items []ScoredID) error
}

// PostHydrator 把 postID 列表 hydrate 成展示项（作者/圈子/图片/统计；不含交互态）。
//
// 直接复用 recommend.domain.FeedPostItem 作为返回类型（纯 DTO，无 infra 依赖）。
type PostHydrator interface {
	Hydrate(ctx context.Context, postIDs []uuid.UUID) ([]recommenddomain.FeedPostItem, error)
}

// InteractionChecker 批量回填 is_liked/is_collected（基于 user:like/collect ZSET）。
type InteractionChecker interface {
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (liked, collected map[uuid.UUID]bool, err error)
}

// CircleLookup 批量取圈子实体（桥接 circle.domain.CircleRepository.GetByIDs）。
// 返回完整 Circle 实体（含 member_count/post_count/hot），由 service 组装 TrendingCircleItem。
type CircleLookup interface {
	GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*circledomain.Circle, error)
}

// UserBrief 用户精简视图（跨域 Facade DTO，镜像 user.application.UserBrief 字段）。
//
// 在 domain 层独立定义（而非 import user.application），避免 domain 依赖兄弟域 application 包。
// 由 composition 桥接器做字段拷贝（与 circle/post 各自重声明 UserBrief 同款惯例）。
type UserBrief struct {
	ID        string
	Username  string
	AvatarURL string
}

// UserLookup 批量取用户简视图（桥接 user.application.UserFacade.GetBriefs）。
type UserLookup interface {
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
}
