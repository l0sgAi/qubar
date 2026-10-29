// Package application 提供 history 领域的应用服务层。
//
// 职责:
//   - 记录浏览(RecordView):Redis ZSET 即时写(列表权威) + Redpanda 事件异步落库
//   - 「最近浏览」列表(ListHistoryPosts):Redis ZSET 即时读 + 冷启动 DB 回源 + 复用 post 领域 ES 组装
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/domains/history/domain"
	postapp "interestBar/pkg/domains/post/application"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// PostFetcher history 领域需要的帖子组装能力(ES 来源,按 ID 列表)。
type PostFetcher interface {
	// SearchByIDs 按 ID 列表从 ES 批量获取已组装帖子(仅未删除 + 已发布)。
	// 顺序不保证,调用方按浏览时间自行排序;失效帖静默过滤。
	// viewerID 不可见的私密圈帖子过滤（退出 / 被移出后浏览记录保留，列表不展示）。
	SearchByIDs(ctx context.Context, viewerID uuid.UUID, postIDs []uuid.UUID) ([]postapp.PostListItem, error)
	// SearchByIDsAndKeyword 在 ID 集合内按关键字搜索并组装帖子(title^3/summary),
	// 按 _score desc 排序,offset 分页;返回匹配总数。供「最近浏览」关键字搜索用。
	SearchByIDsAndKeyword(ctx context.Context, viewerID uuid.UUID, postIDs []uuid.UUID, keyword string, size, offset int) ([]postapp.PostListItem, int64, error)
}

// ===== DTO =====

// HistoryPostItem 「最近浏览」列表项。
//
// 嵌入 PostListItem(JSON 字段平铺到顶层,前端可直接复用帖子卡片组件),
// 附加 viewed_at(最近访问时间,取自 ZSET score / DB update_time)。
type HistoryPostItem struct {
	ViewedAt time.Time `json:"viewed_at"` // 最近访问时间(RFC3339Nano)
	postapp.PostListItem
}

// ListHistoryPostsResult 「最近浏览」列表结果。
type ListHistoryPostsResult struct {
	Posts      []HistoryPostItem `json:"posts"`
	Total      int64             `json:"total"`
	Size       int               `json:"size"`
	NextOffset *int              `json:"next_offset,omitempty"` // 下一页 offset;无更多页时省略
}

// ===== Service 接口 =====

// HistoryService 是 history 领域的应用服务接口。
type HistoryService interface {
	// RecordView 记录一次浏览(供 post 域详情页 async 回调)。
	// Redis ZSET 即时写 + MQ 异步落库;失败仅记日志,不影响详情接口。
	RecordView(ctx context.Context, userID, postID uuid.UUID) error
	// ListHistoryPosts 查看当前用户的「最近浏览」列表(按最近访问时间倒序,ZSET offset 分页)。
	// keyword 非空时:在最近浏览的帖子(≤500)内按关键字搜索(title^3/summary),
	// 结果按相关性(_score)排序,仍用 offset 分页。
	ListHistoryPosts(ctx context.Context, userID uuid.UUID, keyword string, size, offset int) (*ListHistoryPostsResult, error)

	// SetPostFetcher 注入 ES 帖子组装端口(「最近浏览」列表用)。
	SetPostFetcher(f PostFetcher)
}

type historyServiceImpl struct {
	cache     domain.PostHistoryCache
	repo      domain.PostHistoryRepository
	publisher domain.HistoryEventPublisher
	fetcher   PostFetcher
}

// NewHistoryService 构造 HistoryService。
//
// fetcher 是跨领域依赖,通过 setter 注入(composition 层负责把它们连起来)。
func NewHistoryService(
	cache domain.PostHistoryCache,
	repo domain.PostHistoryRepository,
	publisher domain.HistoryEventPublisher,
) HistoryService {
	return &historyServiceImpl{
		cache:     cache,
		repo:      repo,
		publisher: publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *historyServiceImpl) SetPostFetcher(f PostFetcher) { s.fetcher = f }

// RecordView 记录浏览(对称 collect.Toggle:Redis 即时写 + MQ 异步落库)。
//
//  1. Redis ZSET 即时写(ZADD + trim 500)——「最近浏览」列表权威源,即时可见;
//  2. 发布 Redpanda 浏览事件——consumer 批量 ON CONFLICT upsert post_view_history(DB 最终一致)。
//
// Redis 写失败返回 error(上游 post 域仅记日志,不影响详情);MQ 写失败仅记日志(下次浏览补偿)。
func (s *historyServiceImpl) RecordView(ctx context.Context, userID, postID uuid.UUID) error {
	// 1. Redis ZSET 即时写(列表权威源,需即时可见)
	if err := s.cache.RecordView(ctx, userID, postID); err != nil {
		return fmt.Errorf("failed to record view in redis: %w", err)
	}

	// 2. 发布 MQ 事件(异步落库 DB,失败仅日志)
	if err := s.publisher.PublishPostView(ctx, userID, postID); err != nil {
		logger.Log.Error("Failed to publish post view event: " + err.Error())
	}
	return nil
}

// ListHistoryPosts 查看当前用户的「最近浏览」列表。
//
// 数据源:Redis ZSET(即时读),按最近访问时间倒序 offset 分页,每条带 viewed_at(ZSET score 还原)。
// 冷启动(ZCARD==0)时从 DB top500 回源(update_time 作 score)+ Backfill,保证访问时间一致。
// 帖子数据走 ES(SearchByIDs),失效帖(被删/未发布)在 ES terms 查询时静默过滤。
//
// keyword 非空时改走关键字分支(searchHistoryPosts):取该用户全部浏览 entries(≤500)
// 作 ID 集合 → ES 在集合内 multi_match(title^3/summary) 过滤打分,按 _score 排序,offset 分页。
func (s *historyServiceImpl) ListHistoryPosts(ctx context.Context, userID uuid.UUID, keyword string, size, offset int) (*ListHistoryPostsResult, error) {
	if s.fetcher == nil {
		return nil, errors.New("post fetcher is not configured")
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	if offset < 0 {
		offset = 0
	}

	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		return s.searchHistoryPosts(ctx, userID, keyword, size, offset)
	}

	entries, total, err := s.cache.ListViews(ctx, userID, offset, size)
	if err != nil {
		return nil, err
	}

	// 冷启动:ZSET 空 → DB top500 回源(ViewedAt = update_time)+ Backfill,再读一次
	if total == 0 {
		if dbEntries, e := s.repo.ListTopByUserID(ctx, userID, 500); e == nil && len(dbEntries) > 0 {
			if be := s.cache.Backfill(ctx, userID, dbEntries); be == nil {
				entries, total, err = s.cache.ListViews(ctx, userID, offset, size)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	posts := make([]HistoryPostItem, 0, len(entries))
	if len(entries) > 0 {
		postIDs := make([]uuid.UUID, 0, len(entries))
		for _, e := range entries {
			postIDs = append(postIDs, e.PostID)
		}
		fetched, err := s.fetcher.SearchByIDs(ctx, userID, postIDs)
		if err != nil {
			return nil, err
		}
		// 按 entries 顺序(ZSET 倒序 = 最近访问倒序)组装,附 viewed_at;失效帖静默过滤。
		byID := make(map[uuid.UUID]postapp.PostListItem, len(fetched))
		for _, p := range fetched {
			byID[p.ID] = p
		}
		for _, e := range entries {
			if p, ok := byID[e.PostID]; ok {
				posts = append(posts, HistoryPostItem{ViewedAt: e.ViewedAt, PostListItem: p})
			}
		}
	}

	result := &ListHistoryPostsResult{
		Posts: posts,
		Total: total,
		Size:  len(posts),
	}
	// 计算下一页 offset:还有更多数据时返回 offset+size,否则省略
	if int64(offset+size) < total {
		next := offset + size
		result.NextOffset = &next
	}
	return result, nil
}

// searchHistoryPosts 在「最近浏览」范围内按关键字搜索。
//
//  1. loadAllViewEntries 取该用户全部浏览 entries(≤500,含冷启动 backfill)作 ID 集合 +
//     postID→ViewedAt 映射(ZSET 序 = 最近访问序,仅用于回填 viewed_at,不参与排序);
//  2. ES 在 ID 集合内 multi_match(title^3/summary) 过滤 + 失效帖过滤,按 _score desc 排序,
//     from/size offset 分页,返回匹配总数;
//  3. 按 ES 返回(相关性序)组装,附 viewed_at(查映射)。
//
// 与无关键字路径的差异:排序改为相关性(_score),非最近访问时间;分页语义(offset)不变。
func (s *historyServiceImpl) searchHistoryPosts(ctx context.Context, userID uuid.UUID, keyword string, size, offset int) (*ListHistoryPostsResult, error) {
	entries, _, err := s.cache.ListViews(ctx, userID, 0, 500)
	if err != nil {
		return nil, err
	}
	// 冷启动:ZSET 空 → DB top500 回源 + Backfill,再读一次(与无关键字路径一致)
	if len(entries) == 0 {
		if dbEntries, e := s.repo.ListTopByUserID(ctx, userID, 500); e == nil && len(dbEntries) > 0 {
			if be := s.cache.Backfill(ctx, userID, dbEntries); be == nil {
				if entries, _, err = s.cache.ListViews(ctx, userID, 0, 500); err != nil {
					return nil, err
				}
			}
		}
	}

	posts := make([]HistoryPostItem, 0, size)
	if len(entries) == 0 {
		return &ListHistoryPostsResult{Posts: posts, Total: 0, Size: 0}, nil
	}

	postIDs := make([]uuid.UUID, 0, len(entries))
	viewedAtByID := make(map[uuid.UUID]time.Time, len(entries))
	for _, e := range entries {
		postIDs = append(postIDs, e.PostID)
		viewedAtByID[e.PostID] = e.ViewedAt
	}

	matched, total, err := s.fetcher.SearchByIDsAndKeyword(ctx, userID, postIDs, keyword, size, offset)
	if err != nil {
		return nil, err
	}
	for _, p := range matched {
		if viewedAt, ok := viewedAtByID[p.ID]; ok {
			posts = append(posts, HistoryPostItem{ViewedAt: viewedAt, PostListItem: p})
		}
	}

	result := &ListHistoryPostsResult{
		Posts: posts,
		Total: total,
		Size:  len(posts),
	}
	if int64(offset+size) < total {
		next := offset + size
		result.NextOffset = &next
	}
	return result, nil
}
