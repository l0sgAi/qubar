// Package infrastructure 提供 share 领域基础设施层实现。
package infrastructure

import (
	"context"
	"fmt"

	"interestBar/pkg/domains/share/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// postShareRepoPG 基于 GORM / PostgreSQL 的 PostShareRepository 实现。
type postShareRepoPG struct {
	db *gorm.DB
}

// NewPostShareRepository 构造 PostShareRepository。
func NewPostShareRepository(db *gorm.DB) domain.PostShareRepository {
	return &postShareRepoPG{db: db}
}

// createShareSQL 首次分享插入；已分享过则 0 行受影响（依赖 uk_post_share_user_post）。
const createShareSQL = `
INSERT INTO domains.post_share (id, user_id, post_id)
VALUES (?, ?, ?)
ON CONFLICT (user_id, post_id) DO NOTHING`

// Create 单语句去重插入，并发安全：受影响行数即「是否首次」。share.ID 由调用方 sharedomain.NewID() 生成。
func (r *postShareRepoPG) Create(ctx context.Context, share *domain.PostShare) (bool, error) {
	res := r.db.WithContext(ctx).Exec(createShareSQL, share.ID, share.UserID, share.PostID)
	if res.Error != nil {
		return false, fmt.Errorf("failed to insert post share: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

func (r *postShareRepoPG) Delete(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.PostShare{}).Error; err != nil {
		return fmt.Errorf("failed to delete post share: %w", err)
	}
	return nil
}

func (r *postShareRepoPG) CountByPost(ctx context.Context, postID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.PostShare{}).Where("post_id = ?", postID).Count(&n).Error
	return n, err
}
