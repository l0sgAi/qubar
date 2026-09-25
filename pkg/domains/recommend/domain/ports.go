// Package domain 定义 recommend 领域的端口与 DTO。
//
// recommend 是跨域编排器（无聚合根）：调 post(hydrate/circle_id)、circle(joined IDs)、
// redis(seed/cf:item/feed 池)。所有跨域依赖经此包接口抽象，由 composition 注入桥接器，
// 本包不 import post/circle，避免环依赖。
package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// FeedPostItem 推荐流帖子项（镜像 post.PostListItem 字段 + 当前用户交互态）。
//
// post 域的 PostListItem 无 is_liked/is_collected 字段且不改其签名；
// recommend 域自定义本类型，由 InteractionChecker 批量回填两个 flag。
type FeedPostItem struct {
	ID           uuid.UUID `json:"id"`
	CircleID     uuid.UUID `json:"circle_id"`
	UserID       uuid.UUID `json:"user_id"`
	Type         int16     `json:"type"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	Content      string    `json:"content"`
	ViewCount    int       `json:"view_count"`
	CommentCount int       `json:"comment_count"`
	LikeCount    int       `json:"like_count"`
	CollectCount int       `json:"collect_count"`
	IsPinned     int16     `json:"is_pinned"`
	IsEssence    int16     `json:"is_essence"`
	IsLock       int16     `json:"is_lock"`
	Status       int16     `json:"status"`
	CreateTime   time.Time `json:"create_time"`
	AuthorName   string    `json:"author_name"`
	AuthorAvatar string    `json:"author_avatar"`
	CircleName   string    `json:"circle_name"`
	CircleAvatar string    `json:"circle_avatar"`
	Images       []string  `json:"images"`
	IsLiked      bool      `json:"is_liked"`
	IsCollected  bool      `json:"is_collected"`
}

// FeedPage 推荐流分页结果。
//
// recommend tab 用 PoolToken + offset 翻页（候选池）；
// hot/latest/following tab 用 SearchAfter 翻页（ES search_after，稳定序）。
type FeedPage struct {
	Posts         []FeedPostItem `json:"posts"`
	PoolToken     string         `json:"pool_token,omitempty"`     // recommend：候选池版本 token
	SearchAfter   string         `json:"search_after,omitempty"`   // hot/latest/following：ES search_after 游标
	HasMore       bool           `json:"has_more"`                 // 是否还有更多
	PoolRefreshed bool           `json:"pool_refreshed,omitempty"` // recommend：池已重建，本次回 offset=0
}

// HomeFeedSearcher 推荐流 ES 检索（返回有序 postID，searcher 边界提取 PostDoc.ID 做纯 ID 合并）。
type HomeFeedSearcher interface {
	// Search sort: "hot"=rank_score 时间衰减 | "latest"=create_time desc。
	// circleIDs nil/空=全局；非空=terms 过滤。next 为下一页 searchAfter（recall 内部翻页用）。
	Search(ctx context.Context, sort string, circleIDs []uuid.UUID, size int, searchAfter []interface{}) (ids []uuid.UUID, next []interface{}, err error)
}

// PostHydrator 把 postID 列表 hydrate 成 FeedPostItem（作者/圈子/图片/统计；不含交互态）。
type PostHydrator interface {
	Hydrate(ctx context.Context, postIDs []uuid.UUID) ([]FeedPostItem, error)
}

// PostMetaReader 取帖子所属 circle_id（C3 行为圈子路用）。
type PostMetaReader interface {
	ListCircleIDsByPostIDs(ctx context.Context, postIDs []uuid.UUID) ([]uuid.UUID, error)
}

// CircleLookup 用户已加入的圈子 ID 列表（C1 兴趣圈子路用）。
type CircleLookup interface {
	ListJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
}

// SeedReader 用户互动种子（C5 CF seed + exclude 已交互 + CF 相似召回）。
type SeedReader interface {
	LikedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
	CollectedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
	ViewedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
	// CFSimilar 对每个 seed 帖读 cf:item:{seed} ZSET top-N 相似帖，聚合 candidate→Σ相似度。
	CFSimilar(ctx context.Context, seedPostIDs []uuid.UUID, topNPerSeed int) (map[uuid.UUID]float64, error)
}

// InteractionChecker 批量回填 is_liked/is_collected（ZSET 缓存优先，miss 回源 DB；由 composition 桥接 post）。
type InteractionChecker interface {
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (liked, collected map[uuid.UUID]bool, err error)
}

// FeedCache 推荐流候选池（Redis LIST feed:recommend:{uid} + 版本 token）。
type FeedCache interface {
	Exists(ctx context.Context, userID uuid.UUID) (bool, error)
	Len(ctx context.Context, userID uuid.UUID) (int64, error)
	Range(ctx context.Context, userID uuid.UUID, offset, size int64) ([]uuid.UUID, error)
	Token(ctx context.Context, userID uuid.UUID) (string, error)
	Build(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, ttl time.Duration) (string, error)
}

// InterestCircleCache 用户行为兴趣圈子缓存（C3 用）。
//
// 缓存「seed 帖子 → circle_id」反查结果，避免每轮 recall 都查 DB。
// 读路径在内存中减去 joined circles 得 C3 候选圈。兴趣随点赞/收藏漂移，TTL 可配。
type InterestCircleCache interface {
	Exists(ctx context.Context, userID uuid.UUID) (bool, error)
	Get(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	Set(ctx context.Context, userID uuid.UUID, circleIDs []uuid.UUID, ttl time.Duration) error
}
