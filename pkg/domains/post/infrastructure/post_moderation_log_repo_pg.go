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

// Create 写入一行审核日志。实体无 BeforeCreate 钩子，须显式生成 UUIDv7。
func (r *postModerationLogRepoPG) Create(ctx context.Context, log *domain.PostModerationLog) error {
	if log.ID == uuid.Nil {
		log.ID = sharedomain.NewID()
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
