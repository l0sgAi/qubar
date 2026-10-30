package infrastructure

import (
	"context"
	"errors"
	"strings"
	"time"

	"interestBar/pkg/domains/circle/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// inviteEmailRepoPG 基于 GORM 的 InviteEmailRepository 实现（#169）。
type inviteEmailRepoPG struct {
	db *gorm.DB
}

// NewInviteEmailRepository 构造 InviteEmailRepository。
func NewInviteEmailRepository(db *gorm.DB) domain.InviteEmailRepository {
	return &inviteEmailRepoPG{db: db}
}

// AddPending 单条 INSERT ... ON CONFLICT (circle_id, email) DO UPDATE ... WHERE status IN (撤销, 拒绝)：
// RETURNING 只返回新建行与被重置的行，待接受 / 已接受的冲突行不更新也不返回。
func (r *inviteEmailRepoPG) AddPending(ctx context.Context, circleID, inviterID uuid.UUID, emails []string) ([]string, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	values := make([]string, 0, len(emails))
	args := make([]any, 0, len(emails)*4)
	for _, e := range emails {
		values = append(values, "(?, ?, ?, ?)")
		args = append(args, sharedomain.NewID(), circleID, inviterID, e)
	}
	sql := `INSERT INTO domains.circle_invite_email (id, circle_id, inviter_id, email) VALUES ` +
		strings.Join(values, ", ") + `
ON CONFLICT (circle_id, email) DO UPDATE
   SET status = 0, inviter_id = EXCLUDED.inviter_id, update_by = EXCLUDED.inviter_id, update_time = now()
 WHERE domains.circle_invite_email.status IN (?, ?)
RETURNING email`
	args = append(args, domain.InviteEmailRevoked, domain.InviteEmailRejected)

	var added []string
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&added).Error; err != nil {
		return nil, err
	}
	return added, nil
}

func (r *inviteEmailRepoPG) GetByID(ctx context.Context, id uuid.UUID) (*domain.CircleInviteEmail, error) {
	var inv domain.CircleInviteEmail
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrInviteEmailNotFound
		}
		return nil, err
	}
	return &inv, nil
}

// ListByCircle 走 uk_circle_invite_email_circle_email 的 circle_id 前缀；圈内邀请量有限，排序代价可控。
func (r *inviteEmailRepoPG) ListByCircle(ctx context.Context, circleID uuid.UUID, status int16, offset, limit int) ([]domain.CircleInviteEmail, int64, error) {
	q := r.db.WithContext(ctx).Model(&domain.CircleInviteEmail{}).Where("circle_id = ?", circleID)
	if status >= 0 {
		q = q.Where("status = ?", status)
	}
	return r.page(q, offset, limit)
}

// ListPendingByEmail 走部分索引 idx_circle_invite_email_pending (email, create_time DESC) WHERE status = 0。
func (r *inviteEmailRepoPG) ListPendingByEmail(ctx context.Context, email string, offset, limit int) ([]domain.CircleInviteEmail, int64, error) {
	q := r.db.WithContext(ctx).Model(&domain.CircleInviteEmail{}).
		Where("email = ? AND status = ?", email, domain.InviteEmailPending)
	return r.page(q, offset, limit)
}

func (r *inviteEmailRepoPG) page(q *gorm.DB, offset, limit int) ([]domain.CircleInviteEmail, int64, error) {
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.CircleInviteEmail
	if err := q.Order("create_time DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *inviteEmailRepoPG) UpdateStatus(ctx context.Context, id uuid.UUID, from, to int16, operatorID uuid.UUID) error {
	res := r.db.WithContext(ctx).Model(&domain.CircleInviteEmail{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]any{"status": to, "update_by": operatorID, "update_time": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrInviteEmailStateConflict
	}
	return nil
}
