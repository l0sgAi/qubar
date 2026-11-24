package application

import (
	"context"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// batchLikedStatus 批量获取评论点赞状态（先查 Redis ZSET，miss 时回源 DB）。
//
// 与旧 controller.getCommentLikedStatus 行为一致。
func (s *commentServiceImpl) batchLikedStatus(ctx context.Context, userID uuid.UUID, comments []domain.Comment) map[uuid.UUID]bool {
	// 匿名访问（userID == uuid.Nil）直接返回空 map，避免对幽灵 key
	// "user:like:comments:00000000-..." 发起无意义的 Redis ZMScore + DB 回源。
	// 与 checkLiked 的 uuid.Nil 短路行为保持一致。
	if userID == uuid.Nil {
		return make(map[uuid.UUID]bool, len(comments))
	}

	commentIDs := make([]uuid.UUID, len(comments))
	for i, cm := range comments {
		commentIDs[i] = cm.ID
	}

	if len(commentIDs) == 0 {
		return make(map[uuid.UUID]bool)
	}

	// 1. Batch check from Redis ZSET（已完整加载时 Missed 为空）
	likedMap := make(map[uuid.UUID]bool, len(commentIDs))
	missIDs := commentIDs
	chk, err := s.likeCache.BatchCheck(ctx, userID, commentIDs)
	if err != nil {
		logger.Log.Error("Failed to batch check comment liked from Redis: " + err.Error())
	} else {
		for id, v := range chk.Hits {
			likedMap[id] = v
		}
		missIDs = chk.Missed
		// 2. 标记缺失：首次回源全量装载，此后 miss 即未赞不再查 PG（#129）
		if len(missIDs) > 0 && chk.Loadable {
			if all, ok := s.loadAllLikes(ctx, userID); ok {
				for _, id := range missIDs {
					likedMap[id] = all[id]
				}
				return likedMap
			}
		}
	}

	// 3. Fallback to DB for cache misses
	if len(missIDs) > 0 {
		dbLiked, err := s.repo.BatchCheckLiked(ctx, userID, missIDs)
		if err != nil {
			logger.Log.Error("Failed to batch check comment liked from DB: " + err.Error())
		} else {
			// Merge DB results into likedMap
			for id, liked := range dbLiked {
				likedMap[id] = liked
			}
			// Backfill ZSET for DB-confirmed likes
			var backfillIDs []uuid.UUID
			for _, id := range missIDs {
				if dbLiked[id] {
					backfillIDs = append(backfillIDs, id)
				}
			}
			if len(backfillIDs) > 0 {
				if err := s.likeCache.Backfill(ctx, userID, backfillIDs); err != nil {
					logger.Log.Error("Failed to backfill comment likes to Redis: " + err.Error())
				}
			}
		}
	}

	return likedMap
}

// likeFullLoadLimit 评论点赞首次回源全量装载的上限（#129）：有效评论点赞数 ≤ 该值时一次装入 ZSET
// 并写完整加载标记；超过则写超限标记，退回逐批 IN 回源。须小于 ZSET 容量（2000），留出淘汰余量。
const likeFullLoadLimit = 1000

// loadAllLikes 查用户全部有效评论点赞并装入缓存。返回 (全量集合, true)；超过装载上限或 DB 出错返回 false。
func (s *commentServiceImpl) loadAllLikes(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]bool, bool) {
	likes, err := s.repo.ListActiveLikes(ctx, userID, likeFullLoadLimit+1)
	if err != nil {
		logger.Log.Error("Failed to list comment likes for full load: " + err.Error())
		return nil, false
	}
	complete := len(likes) <= likeFullLoadLimit
	if !complete {
		likes = nil
	}
	if err := s.likeCache.LoadAll(ctx, userID, likes, complete); err != nil {
		logger.Log.Error("Failed to load comment like cache: " + err.Error())
	}
	if !complete {
		return nil, false
	}
	all := make(map[uuid.UUID]bool, len(likes))
	for _, l := range likes {
		all[l.CommentID] = true
	}
	return all, true
}

// checkLiked 检查单条评论的点赞状态（详情回显用，best-effort：出错视为未赞）。
func (s *commentServiceImpl) checkLiked(ctx context.Context, userID, commentID uuid.UUID) bool {
	if userID == uuid.Nil {
		return false
	}
	liked, err := s.IsLikedByUser(ctx, userID, commentID)
	if err != nil {
		logger.Log.Error("Failed to check comment liked: " + err.Error())
		return false
	}
	return liked
}

// IsLikedByUser 用户是否已赞该评论（缓存优先，miss 回源 DB，DB 已赞则回填缓存）。
//
// 用户评论点赞 ZSET 有 TTL 与容量上限，miss 不等于未赞，必须回源 DB 区分。
// 缓存故障直接回源 DB；DB 错误原样返回。
func (s *commentServiceImpl) IsLikedByUser(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	chk, err := s.likeCache.BatchCheck(ctx, userID, []uuid.UUID{commentID})
	if err == nil && (chk.Hits[commentID] || len(chk.Missed) == 0) {
		return chk.Hits[commentID], nil // 命中，或 ZSET 已完整加载（缺失即未赞）
	}
	if err != nil {
		logger.Log.Error("Failed to check comment liked from cache, falling back to DB: " + err.Error())
	}

	liked, err := s.repo.IsLiked(ctx, userID, commentID)
	if err != nil {
		return false, err
	}
	if liked {
		if bfErr := s.likeCache.Backfill(ctx, userID, []uuid.UUID{commentID}); bfErr != nil {
			logger.Log.Error("Failed to backfill comment like cache: " + bfErr.Error())
		}
	}
	return liked, nil
}
