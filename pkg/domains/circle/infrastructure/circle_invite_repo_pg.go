package infrastructure

import (
	"context"
	"errors"
	"time"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// inviteLinkRepoPG 基于 GORM 的 InviteLinkRepository 实现（#169）。
type inviteLinkRepoPG struct {
	db *gorm.DB
}

// NewInviteLinkRepository 构造 InviteLinkRepository。
func NewInviteLinkRepository(db *gorm.DB) domain.InviteLinkRepository {
	return &inviteLinkRepoPG{db: db}
}

func (r *inviteLinkRepoPG) Create(ctx context.Context, link *domain.CircleInviteLink) error {
	return r.db.WithContext(ctx).Create(link).Error
}

func (r *inviteLinkRepoPG) GetByID(ctx context.Context, id uuid.UUID) (*domain.CircleInviteLink, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *inviteLinkRepoPG) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.CircleInviteLink, error) {
	return r.first(ctx, "token_hash = ?", tokenHash)
}

func (r *inviteLinkRepoPG) first(ctx context.Context, query string, arg any) (*domain.CircleInviteLink, error) {
	var l domain.CircleInviteLink
	if err := r.db.WithContext(ctx).Where(query, arg).First(&l).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrInviteLinkNotFound
		}
		return nil, err
	}
	return &l, nil
}

// ListByCircle 走 idx_circle_invite_link_circle (circle_id, create_time DESC)；id 作同毫秒 tiebreaker。
func (r *inviteLinkRepoPG) ListByCircle(ctx context.Context, circleID uuid.UUID, offset, limit int) ([]domain.CircleInviteLink, int64, error) {
	q := r.db.WithContext(ctx).Model(&domain.CircleInviteLink{}).Where("circle_id = ?", circleID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var links []domain.CircleInviteLink
	if err := q.Order("create_time DESC, id DESC").Offset(offset).Limit(limit).Find(&links).Error; err != nil {
		return nil, 0, err
	}
	return links, total, nil
}

func (r *inviteLinkRepoPG) Revoke(ctx context.Context, id, operatorID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.CircleInviteLink{}).
		Where("id = ? AND revoked = 0", id).
		Updates(map[string]any{"revoked": 1, "update_by": operatorID, "update_time": time.Now()}).Error
}

func (r *inviteLinkRepoPG) RevokeByCircle(ctx context.Context, circleID, operatorID uuid.UUID) (int64, error) {
	res := r.db.WithContext(ctx).Model(&domain.CircleInviteLink{}).
		Where("circle_id = ? AND revoked = 0", circleID).
		Updates(map[string]any{"revoked": 1, "update_by": operatorID, "update_time": time.Now()})
	return res.RowsAffected, res.Error
}

// ConsumeUse 条件 UPDATE：行锁串行化并发请求，后到者重新求值 WHERE，used_count 不会超过 max_uses。
// 过期判断用数据库时钟 now()，与并发请求共用同一时间基准。
func (r *inviteLinkRepoPG) ConsumeUse(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Model(&domain.CircleInviteLink{}).
		Where("id = ? AND revoked = 0 AND expire_time > now() AND (max_uses IS NULL OR used_count < max_uses)", id).
		Updates(map[string]any{"used_count": gorm.Expr("used_count + 1"), "update_time": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrInviteLinkUnusable
	}
	return nil
}

func (r *inviteLinkRepoPG) ReleaseUse(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.CircleInviteLink{}).
		Where("id = ? AND used_count > 0", id).
		Updates(map[string]any{"used_count": gorm.Expr("used_count - 1"), "update_time": time.Now()}).Error
}
