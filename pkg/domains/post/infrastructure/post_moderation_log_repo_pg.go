package infrastructure

import (
	"context"

	"interestBar/pkg/domains/post/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// postModerationLogRepoPG 基于 GORM 的 PostModerationLogRepository 实现。
type postModerationLogRepoPG struct {
	db *gorm.DB
}

// NewPostModerationLogRepository 构造 PostModerationLogRepository。
func NewPostModerationLogRepository(db *gorm.DB) domain.PostModerationLogRepository {
	return &postModerationLogRepoPG{db: db}
}

// Create 写入一行审核日志。实体无 BeforeCreate 钩子，须显式生成 UUIDv7；未指定来源按 AI 审核记录。
func (r *postModerationLogRepoPG) Create(ctx context.Context, log *domain.PostModerationLog) error {
	if log.ID == uuid.Nil {
		log.ID = sharedomain.NewID()
	}
	if log.Source == 0 {
		log.Source = domain.ModerationSourceAI
	}
	return r.db.WithContext(ctx).Create(log).Error
}

// ExistsByPostID 走 idx_post_moderation_log_post。
func (r *postModerationLogRepoPG) ExistsByPostID(ctx context.Context, postID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).
		Raw("SELECT EXISTS (SELECT 1 FROM domains.post_moderation_log WHERE post_id = ?)", postID).
		Scan(&exists).Error
	return exists, err
}

// LatestAIByPostIDs DISTINCT ON 取每篇帖子最近一条 AI 审核日志，走 idx_post_moderation_log_post (post_id, create_time DESC)。
func (r *postModerationLogRepoPG) LatestAIByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]*domain.PostModerationLog, error) {
	result := make(map[uuid.UUID]*domain.PostModerationLog, len(postIDs))
	if len(postIDs) == 0 {
		return result, nil
	}
	var rows []domain.PostModerationLog
	err := r.db.WithContext(ctx).
		Raw(`SELECT DISTINCT ON (post_id) * FROM domains.post_moderation_log
			WHERE post_id IN ? AND source = ?
			ORDER BY post_id, create_time DESC, id DESC`, postIDs, domain.ModerationSourceAI).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		result[rows[i].PostID] = &rows[i]
	}
	return result, nil
}
