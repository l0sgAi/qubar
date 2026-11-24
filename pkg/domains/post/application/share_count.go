package application

import (
	"context"
	"fmt"

	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// 分享数 / 分享状态回填（#233）。
//
// 口径：详情与所有帖子卡片列表都走 resolveShareCounts，读同一个 Redis post:sharecnt:{post_id}
// （share 领域分享时自增，读路径真值），miss 回源 PG post.share_count 并 SET NX 回种。
// share_count 不进 ES（es_sinks 不含该列），所以列表不能用 ES 文档里的计数。
//
// 回种用的 post.share_count 由帖子统计消费者批量落库，可能落后于 Redis；SET NX 不覆盖已有 key，
// 偏旧的回种值由落库后的「只升不降」校准补回（redis.ReconcilePostShareCounts）。

// SetShareCountCache 注入分享数缓存（未注入时分享数直接读 PG post.share_count）。
func (s *postServiceImpl) SetShareCountCache(c domain.PostShareCountCache) { s.shareCache = c }

// resolveShareCounts 批量解析分享数：缓存 MGET → miss 先用 known（调用方已从 PG 读到的 share_count），
// 其余一次 PG 主键查询 → 回种缓存。best-effort：缓存故障时直接用 PG 值且不回种；PG 出错的帖子不在结果里（按 0 展示）。
func (s *postServiceImpl) resolveShareCounts(ctx context.Context, postIDs []uuid.UUID, known map[uuid.UUID]int64) map[uuid.UUID]int64 {
	result := make(map[uuid.UUID]int64, len(postIDs))
	if len(postIDs) == 0 {
		return result
	}

	missed := postIDs
	cacheOK := false
	if s.shareCache != nil {
		hits, err := s.shareCache.BatchGet(ctx, postIDs)
		if err != nil {
			logger.Log.Error("Failed to batch get post share counts from cache, falling back to DB: " + err.Error())
		} else {
			cacheOK = true
			missed = make([]uuid.UUID, 0, len(postIDs))
			for _, id := range postIDs {
				if n, ok := hits[id]; ok {
					result[id] = n
				} else {
					missed = append(missed, id)
				}
			}
		}
	}
	if len(missed) == 0 {
		return result
	}

	fromDB := make(map[uuid.UUID]int64, len(missed))
	query := make([]uuid.UUID, 0, len(missed))
	for _, id := range missed {
		if n, ok := known[id]; ok {
			fromDB[id] = n
		} else {
			query = append(query, id)
		}
	}
	if len(query) > 0 {
		rows, err := s.repo.GetShareCounts(ctx, query)
		if err != nil {
			logger.Log.Error("Failed to get post share counts from DB: " + err.Error())
		}
		for id, n := range rows {
			fromDB[id] = n
		}
	}
	for id, n := range fromDB {
		result[id] = n
	}

	if cacheOK && len(fromDB) > 0 {
		if err := s.shareCache.SeedIfAbsent(ctx, fromDB); err != nil {
			logger.Log.Error("Failed to seed post share count cache: " + err.Error())
		}
	}
	return result
}

// applyShareCounts 回填列表项 share_count。known 为调用方已从 PG 读到的 share_count（ES 来源的列表传 nil）。
func (s *postServiceImpl) applyShareCounts(ctx context.Context, posts []PostListItem, known map[uuid.UUID]int64) {
	if len(posts) == 0 {
		return
	}
	postIDs := make([]uuid.UUID, 0, len(posts))
	for _, p := range posts {
		postIDs = append(postIDs, p.ID)
	}
	counts := s.resolveShareCounts(ctx, postIDs, known)
	for i := range posts {
		posts[i].ShareCount = int(counts[posts[i].ID])
	}
}

// BatchGetShareCounts 批量取帖子分享数（口径同详情 / 列表，供 circle 圈内帖子列表经桥接回填）。
// best-effort：取不到的帖子不在结果里，调用方按 0 展示。
func (s *postServiceImpl) BatchGetShareCounts(ctx context.Context, postIDs []uuid.UUID) map[uuid.UUID]int {
	counts := s.resolveShareCounts(ctx, postIDs, nil)
	out := make(map[uuid.UUID]int, len(counts))
	for id, n := range counts {
		out[id] = int(n)
	}
	return out
}

// checkShared 详情 is_shared（best-effort：出错视为未分享）。分享不可撤销、没有用户维度缓存，直接查 post_share 唯一索引。
func (s *postServiceImpl) checkShared(ctx context.Context, userID, postID uuid.UUID) bool {
	shared, err := s.repo.IsShared(ctx, userID, postID)
	if err != nil {
		logger.Log.Error("Failed to check post shared: " + err.Error())
		return false
	}
	return shared
}

// BatchCheckShared 批量查当前用户分享过哪些帖子（信息流 is_shared 回显，口径同详情）。
// 访客（userID==uuid.Nil）返回空 map；best-effort：DB 出错只记日志，返回空 map。
func (s *postServiceImpl) BatchCheckShared(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) map[uuid.UUID]bool {
	if userID == uuid.Nil || len(postIDs) == 0 {
		return make(map[uuid.UUID]bool)
	}
	shared, err := s.repo.BatchIsShared(ctx, userID, postIDs)
	if err != nil {
		logger.Log.Error(fmt.Sprintf("Failed to batch check post shared from DB: %s", err.Error()))
		return make(map[uuid.UUID]bool)
	}
	return shared
}
