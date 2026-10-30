package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
)

// CircleInviteLink 私密圈邀请链接（#169）。库内只存 token 的 SHA-256，明文只在创建响应中出现一次。
type CircleInviteLink struct {
	ID         uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CircleID   uuid.UUID  `json:"circle_id" gorm:"column:circle_id;type:uuid;not null"`
	CreatorID  uuid.UUID  `json:"creator_id" gorm:"column:creator_id;type:uuid;not null"`
	TokenHash  string     `json:"-" gorm:"column:token_hash;type:varchar(64);not null"`
	ExpireTime time.Time  `json:"expire_time" gorm:"column:expire_time;not null"`
	MaxUses    *int       `json:"max_uses" gorm:"column:max_uses"`
	UsedCount  int        `json:"used_count" gorm:"column:used_count;default:0"`
	Revoked    int16      `json:"revoked" gorm:"column:revoked;type:smallint;default:0"`
	CreateTime time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	UpdateBy   *uuid.UUID `json:"update_by,omitempty" gorm:"column:update_by;type:uuid"`
}

// TableName 指定表名。
func (CircleInviteLink) TableName() string {
	return "domains.circle_invite_link"
}

// Usable 链接在 now 时刻是否可用：未撤销、未过期、未用尽。
// 只是读路径的预检；接受时以 InviteLinkRepository.ConsumeUse 的条件 UPDATE 为准。
func (l *CircleInviteLink) Usable(now time.Time) bool {
	if l.Revoked != 0 || !l.ExpireTime.After(now) {
		return false
	}
	return l.MaxUses == nil || l.UsedCount < *l.MaxUses
}

// HashInviteToken 计算邀请 token 的存储形式（SHA-256 小写 hex）。校验一律按 hash 等值查询，不比较明文。
func HashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ErrInviteLinkNotFound 邀请链接不存在（按 ID 或 token_hash 查询未命中）。
var ErrInviteLinkNotFound = errors.New("invite link not found")

// ErrInviteLinkUnusable 条件 UPDATE 未命中：链接已撤销、已过期或次数用尽（含并发抢占最后一次）。
var ErrInviteLinkUnusable = errors.New("invite link is revoked, expired or used up")

// InviteLinkRepository 邀请链接持久化接口。
type InviteLinkRepository interface {
	// Create 写入链接（调用方显式设置 ID）。
	Create(ctx context.Context, link *CircleInviteLink) error
	// GetByID 按 ID 查询。未找到返回 ErrInviteLinkNotFound。
	GetByID(ctx context.Context, id uuid.UUID) (*CircleInviteLink, error)
	// GetByTokenHash 按 token_hash 等值查询（不过滤撤销 / 过期，由调用方判断）。未找到返回 ErrInviteLinkNotFound。
	GetByTokenHash(ctx context.Context, tokenHash string) (*CircleInviteLink, error)
	// ListByCircle 圈内全部链接（含已撤销 / 已过期），按创建时间倒序 offset 分页，返回总数。
	ListByCircle(ctx context.Context, circleID uuid.UUID, offset, limit int) ([]CircleInviteLink, int64, error)
	// Revoke 撤销单个链接（已撤销视为成功，幂等）。
	Revoke(ctx context.Context, id, operatorID uuid.UUID) error
	// RevokeByCircle 批量撤销圈内全部未撤销链接（私密圈改为公开时调用），返回撤销条数。
	RevokeByCircle(ctx context.Context, circleID, operatorID uuid.UUID) (int64, error)
	// ConsumeUse 原子消耗一次使用：UPDATE ... SET used_count = used_count + 1
	// WHERE 未撤销 AND 未过期 AND (max_uses IS NULL OR used_count < max_uses)。
	// 0 行受影响返回 ErrInviteLinkUnusable，并发下不会超用。
	ConsumeUse(ctx context.Context, id uuid.UUID) error
	// ReleaseUse 归还一次使用（消耗后入圈失败的补偿；used_count 不减到 0 以下）。
	ReleaseUse(ctx context.Context, id uuid.UUID) error
}

// CircleInviteEmail 私密圈指定 email 邀请（#169）。Email 存储前 trim + 小写，与 users.email 等值匹配。
type CircleInviteEmail struct {
	ID         uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CircleID   uuid.UUID  `json:"circle_id" gorm:"column:circle_id;type:uuid;not null"`
	InviterID  uuid.UUID  `json:"inviter_id" gorm:"column:inviter_id;type:uuid;not null"`
	Email      string     `json:"email" gorm:"column:email;type:varchar(100);not null"`
	Status     int16      `json:"status" gorm:"column:status;type:smallint;default:0"`
	CreateTime time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	UpdateBy   *uuid.UUID `json:"update_by,omitempty" gorm:"column:update_by;type:uuid"`
}

// TableName 指定表名。
func (CircleInviteEmail) TableName() string {
	return "domains.circle_invite_email"
}

// InviteEmailStatus email 邀请状态。
const (
	InviteEmailPending  = 0 // 待接受
	InviteEmailAccepted = 1 // 已接受
	InviteEmailRevoked  = 2 // 已撤销
	InviteEmailRejected = 3 // 已拒绝
)

// ErrInviteEmailNotFound email 邀请不存在。
var ErrInviteEmailNotFound = errors.New("email invite not found")

// ErrInviteEmailStateConflict email 邀请状态 CAS 未命中（并发变更或状态不符）。
var ErrInviteEmailStateConflict = errors.New("email invite state conflict")

// InviteEmailRepository email 邀请持久化接口。
type InviteEmailRepository interface {
	// AddPending 批量添加邀请（emails 已规整去重）：不存在则新建待接受；已撤销 / 已拒绝重置为待接受
	// （inviter_id 改为本次操作者）；待接受 / 已接受保持不变。返回本次新建或重置的 email。
	AddPending(ctx context.Context, circleID, inviterID uuid.UUID, emails []string) ([]string, error)
	// GetByID 按 ID 查询。未找到返回 ErrInviteEmailNotFound。
	GetByID(ctx context.Context, id uuid.UUID) (*CircleInviteEmail, error)
	// ListByCircle 圈内邀请列表，status=-1 不过滤；按创建时间倒序 offset 分页，返回总数。
	ListByCircle(ctx context.Context, circleID uuid.UUID, status int16, offset, limit int) ([]CircleInviteEmail, int64, error)
	// ListPendingByEmail 某邮箱的待接受邀请，按创建时间倒序 offset 分页，返回总数。
	ListPendingByEmail(ctx context.Context, email string, offset, limit int) ([]CircleInviteEmail, int64, error)
	// UpdateStatus 状态 CAS（WHERE status=from），写 update_by。0 行受影响返回 ErrInviteEmailStateConflict。
	UpdateStatus(ctx context.Context, id uuid.UUID, from, to int16, operatorID uuid.UUID) error
}
