// Package infrastructure 提供 post 领域基础设施层实现。
package infrastructure

import (
	"context"
	"errors"
	"time"

	"interestBar/pkg/domains/post/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// postRepoPG 基于 GORM 的 PostRepository 实现。
type postRepoPG struct {
	db *gorm.DB
}

// NewPostRepository 构造 PostRepository。
func NewPostRepository(db *gorm.DB) domain.PostRepository {
	return &postRepoPG{db: db}
}

func (r *postRepoPG) GetByID(ctx context.Context, postID uuid.UUID) (*domain.Post, error) {
	var p domain.Post
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", postID, 0).First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrPostNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *postRepoPG) Create(ctx context.Context, post *domain.Post) error {
	if post.ID == uuid.Nil {
		post.ID = sharedomain.NewID()
	}
	return r.db.WithContext(ctx).Create(post).Error
}

func (r *postRepoPG) GetMediaByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]domain.MediaExtraJSON, error) {
	if len(postIDs) == 0 {
		return make(map[uuid.UUID]domain.MediaExtraJSON), nil
	}
	var posts []domain.Post
	err := r.db.WithContext(ctx).Select("id, media_extra").Where("id IN ?", postIDs).Find(&posts).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]domain.MediaExtraJSON, len(posts))
	for _, p := range posts {
		result[p.ID] = p.MediaExtra
	}
	return result, nil
}

// ListByIDs 批量获取帖子（仅未删除 + 已发布 status=1）。
// 供 collect「我的收藏」组装用：失效帖（已删/未发布）在此静默过滤。
func (r *postRepoPG) ListByIDs(ctx context.Context, postIDs []uuid.UUID) ([]domain.Post, error) {
	if len(postIDs) == 0 {
		return nil, nil
	}
	var posts []domain.Post
	err := r.db.WithContext(ctx).
		Where("id IN ? AND deleted = ? AND status = ?", postIDs, 0, domain.PostStatusPublished).
		Find(&posts).Error
	if err != nil {
		return nil, err
	}
	return posts, nil
}

func (r *postRepoPG) IsLiked(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.PostLike{}).
		Where("user_id = ? AND post_id = ? AND deleted = ?", userID, postID, domain.PostLikeActive).
		Count(&count).Error
	return count > 0, err
}

// IsCollected 检查用户是否收藏了帖子（DB 回源用）。
// post_collect 表属 collect 领域，这里用 Table() 按表名查询，避免跨领域 import 实体。
// deleted=0 等价 collect.domain.PostCollectActive。
func (r *postRepoPG) IsCollected(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("domains.post_collect").
		Where("user_id = ? AND post_id = ? AND deleted = ?", userID, postID, 0).
		Count(&count).Error
	return count > 0, err
}

// BatchIsLiked 批量检查用户点赞了哪些帖子（走 uk_post_like_user_post 唯一索引）。
func (r *postRepoPG) BatchIsLiked(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	return r.batchActivePostIDs(ctx, "domains.post_like", userID, postIDs)
}

// BatchIsCollected 批量检查用户收藏了哪些帖子（走 uk_post_collect_user_post 唯一索引）。
// post_collect 表属 collect 领域，按表名查询，避免跨领域 import 实体。
func (r *postRepoPG) BatchIsCollected(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	return r.batchActivePostIDs(ctx, "domains.post_collect", userID, postIDs)
}

// batchActivePostIDs 在 (user_id, post_id, deleted) 结构的交互流水表中批量查有效行（deleted=0）。
func (r *postRepoPG) batchActivePostIDs(ctx context.Context, table string, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	result := make(map[uuid.UUID]bool)
	if len(postIDs) == 0 {
		return result, nil
	}
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Table(table).
		Where("user_id = ? AND post_id IN ? AND deleted = ?", userID, postIDs, 0).
		Pluck("post_id", &ids).Error
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		result[id] = true
	}
	return result, nil
}

// ListActiveLikes 列出用户的有效点赞（走 idx_user_post_active 部分索引，新→旧，最多 limit 条）。
func (r *postRepoPG) ListActiveLikes(ctx context.Context, userID uuid.UUID, limit int) ([]domain.InteractionEntry, error) {
	return r.listActiveInteractions(ctx, "domains.post_like", userID, limit)
}

// ListActiveCollects 列出用户的有效收藏（走 idx_pcollect_user_active 部分索引，新→旧，最多 limit 条）。
func (r *postRepoPG) ListActiveCollects(ctx context.Context, userID uuid.UUID, limit int) ([]domain.InteractionEntry, error) {
	return r.listActiveInteractions(ctx, "domains.post_collect", userID, limit)
}

// listActiveInteractions 列出交互流水表中用户的有效行；At 取 update_time（最近一次进入有效态的时间）。
func (r *postRepoPG) listActiveInteractions(ctx context.Context, table string, userID uuid.UUID, limit int) ([]domain.InteractionEntry, error) {
	var rows []struct {
		PostID     uuid.UUID
		UpdateTime time.Time
	}
	err := r.db.WithContext(ctx).Table(table).
		Select("post_id, update_time").
		Where("user_id = ? AND deleted = ?", userID, 0).
		Order("create_time DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	entries := make([]domain.InteractionEntry, len(rows))
	for i, row := range rows {
		entries[i] = domain.InteractionEntry{ID: row.PostID, At: row.UpdateTime}
	}
	return entries, nil
}

// IncrCommentCount 同步递增帖子评论计数（comment_count + 1）。
// 实时持久化到 DB，替代旧的 Redpanda 异步聚合；GREATEST 防御性兜底，与批量聚合语义一致。
func (r *postRepoPG) IncrCommentCount(ctx context.Context, postID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.Post{}).
		Where("id = ? AND deleted = ?", postID, 0).
		UpdateColumn("comment_count", gorm.Expr("GREATEST(comment_count + 1, 0)")).Error
}

// CreateMentions 批量写入帖子提及名单（append-only，幂等：重复行忽略）。
//
// PostMention 未内嵌 BaseModel、无 BeforeCreate 钩子，须在此显式预生成 UUIDv7，
// 否则 GORM 会把 uuid.Nil 当有效值发送覆盖 DB 默认值。
func (r *postRepoPG) CreateMentions(ctx context.Context, postID uuid.UUID, userIDs []uuid.UUID) error {
	if len(userIDs) == 0 {
		return nil
	}
	mentions := make([]domain.PostMention, 0, len(userIDs))
	for _, uid := range userIDs {
		mentions = append(mentions, domain.PostMention{
			ID:     sharedomain.NewID(),
			PostID: postID,
			UserID: uid,
		})
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&mentions).Error
}

// GetMentionUserIDsByPostIDs 批量获取帖子提及用户ID。
//
// 按 id ASC 排序：UUIDv7 字典序 == 时间序，即提及写入顺序（≈正文出现顺序）。
func (r *postRepoPG) GetMentionUserIDsByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	result := make(map[uuid.UUID][]uuid.UUID, len(postIDs))
	if len(postIDs) == 0 {
		return result, nil
	}
	var mentions []domain.PostMention
	err := r.db.WithContext(ctx).
		Where("post_id IN ?", postIDs).
		Order("id ASC").
		Find(&mentions).Error
	if err != nil {
		return nil, err
	}
	for _, m := range mentions {
		result[m.PostID] = append(result[m.PostID], m.UserID)
	}
	return result, nil
}

// UpdateStatusFromReviewing 条件更新：UPDATE ... SET status = ? WHERE id = ? AND status = 2 AND deleted = 0。
//
// 帖子表第一条状态变更路径（#117）：靠 WHERE status = 2 保证同一帖子只会被转出审核中一次，
// 重复消费 / 并发 worker / 人工审核抢先时更新 0 行，调用方据此跳过发布副作用。
func (r *postRepoPG) UpdateStatusFromReviewing(ctx context.Context, postID uuid.UUID, status int16) (bool, error) {
	res := r.db.WithContext(ctx).Model(&domain.Post{}).
		Where("id = ? AND status = ? AND deleted = ?", postID, domain.PostStatusReviewing, 0).
		UpdateColumns(map[string]any{"status": status, "update_time": gorm.Expr("CURRENT_TIMESTAMP")})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ResolveReview 人工审核（#118）：事务内 UPDATE ... WHERE status = 2 AND deleted = 0，更新 1 行才写审核日志。
//
// 与 worker 共用同一条件更新：人工 / worker / 多个管理员并发处理同一帖子时只有一方更新成功；
// 状态与审计行同生同灭，不会出现「状态已改但没有人工审核记录」。
func (r *postRepoPG) ResolveReview(ctx context.Context, postID uuid.UUID, status int16, log *domain.PostModerationLog) (bool, error) {
	updated := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&domain.Post{}).
			Where("id = ? AND status = ? AND deleted = ?", postID, domain.PostStatusReviewing, 0).
			UpdateColumns(map[string]any{"status": status, "update_time": gorm.Expr("CURRENT_TIMESTAMP")})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		if log.ID == uuid.Nil {
			log.ID = sharedomain.NewID()
		}
		if err := tx.Create(log).Error; err != nil {
			return err
		}
		updated = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return updated, nil
}

// ListReviewingByCircle 圈内审核中帖子，(create_time, id) 倒序 keyset。
// 审核中的帖子全站很少，走 idx_post_status (status, create_time DESC) 再按 circle_id 过滤即可。
func (r *postRepoPG) ListReviewingByCircle(ctx context.Context, circleID uuid.UUID, after *domain.PostKeyset, limit int) ([]domain.Post, error) {
	q := r.db.WithContext(ctx).
		Where("circle_id = ? AND status = ? AND deleted = ?", circleID, domain.PostStatusReviewing, 0)
	if after != nil {
		q = q.Where("(create_time, id) < (?, ?)", after.CreateTime, after.ID)
	}
	var posts []domain.Post
	err := q.Order("create_time DESC, id DESC").Limit(limit).Find(&posts).Error
	return posts, err
}
