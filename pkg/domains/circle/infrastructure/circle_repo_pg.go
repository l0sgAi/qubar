// Package infrastructure 提供 circle 领域基础设施层实现。
//
// 包括：
//   - circleRepoPG / memberRepoPG：基于 GORM 的 Repository 实现
//   - circleBaseCacheRedis / circleStatsCacheRedis / joinedCirclesCacheRedis：Redis 实现
package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/domains/circle/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// circleRepoPG 基于 GORM 的 CircleRepository 实现。
type circleRepoPG struct {
	db *gorm.DB
}

// NewCircleRepository 构造 CircleRepository。
func NewCircleRepository(db *gorm.DB) domain.CircleRepository {
	return &circleRepoPG{db: db}
}

func (r *circleRepoPG) GetByID(ctx context.Context, circleID uuid.UUID) (*domain.Circle, error) {
	var c domain.Circle
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", circleID, 0).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCircleNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *circleRepoPG) GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*domain.Circle, error) {
	if len(circleIDs) == 0 {
		return make(map[uuid.UUID]*domain.Circle), nil
	}
	var circles []domain.Circle
	err := r.db.WithContext(ctx).Where("id IN ? AND deleted = ?", circleIDs, 0).Find(&circles).Error
	if err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]*domain.Circle, len(circles))
	for i := range circles {
		m[circles[i].ID] = &circles[i]
	}
	return m, nil
}

func (r *circleRepoPG) ExistsByName(ctx context.Context, name string) (bool, error) {
	var c domain.Circle
	err := r.db.WithContext(ctx).Where("name = ? AND deleted = ?", name, 0).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *circleRepoPG) ExistsBySlug(ctx context.Context, slug string) (bool, error) {
	var c domain.Circle
	err := r.db.WithContext(ctx).Where("slug = ? AND deleted = ?", slug, 0).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Create 创建圈子并自动将创建者设为圈主（事务），与旧 model.CreateCircle 一致。
func (r *circleRepoPG) Create(ctx context.Context, circle *domain.Circle) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if circle.ID == uuid.Nil {
			circle.ID = sharedomain.NewID()
		}
		if err := tx.Create(circle).Error; err != nil {
			return err
		}
		member := domain.CircleMember{
			CircleID:  circle.ID,
			UserID:    circle.CreatorID,
			Role:      domain.MemberRoleOwner,
			Status:    domain.MemberStatusNormal,
			IsTop:     0,
			IsDisturb: 0,
		}
		if member.ID == uuid.Nil {
			member.ID = sharedomain.NewID()
		}
		if err := tx.Create(&member).Error; err != nil {
			return err
		}
		return nil
	})
}

// Update 更新圈子资料的可编辑字段（nil 字段跳过）。
//
// name/slug 冲突由唯一索引兜底：service 层 ExistsByName/ExistsBySlug 预检与
// 并发写入之间存在窗口，DB 约束是最终防线，命中时转换为领域哨兵错误。
func (r *circleRepoPG) Update(ctx context.Context, circleID uuid.UUID, f domain.CircleUpdateFields) error {
	updates := map[string]interface{}{}
	if f.Name != nil {
		updates["name"] = *f.Name
	}
	if f.Slug != nil {
		// 空串表示清除 slug：落 NULL（PG 唯一索引视 NULL 为互不相等，空串会互相碰撞）。
		if *f.Slug == "" {
			updates["slug"] = nil
		} else {
			updates["slug"] = *f.Slug
		}
	}
	if f.AvatarURL != nil {
		updates["avatar_url"] = *f.AvatarURL
	}
	if f.CoverURL != nil {
		updates["cover_url"] = *f.CoverURL
	}
	if f.Description != nil {
		updates["description"] = *f.Description
	}
	if f.Rule != nil {
		updates["rule"] = *f.Rule
	}
	if f.CategoryID != nil {
		updates["category_id"] = *f.CategoryID
	}
	if f.JoinType != nil {
		updates["join_type"] = *f.JoinType
	}
	if len(updates) == 0 {
		return nil
	}

	res := r.db.WithContext(ctx).Model(&domain.Circle{}).
		Where("id = ? AND deleted = ?", circleID, 0).
		Updates(updates)
	if res.Error != nil {
		msg := res.Error.Error()
		if strings.Contains(msg, "idx_circle_name") {
			return domain.ErrCircleNameExists
		}
		if strings.Contains(msg, "idx_circle_slug") {
			return domain.ErrCircleSlugExists
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrCircleNotFound
	}
	return nil
}

// memberRepoPG 基于 GORM 的 MemberRepository 实现。
type memberRepoPG struct {
	db *gorm.DB
}

// NewMemberRepository 构造 MemberRepository。
func NewMemberRepository(db *gorm.DB) domain.MemberRepository {
	return &memberRepoPG{db: db}
}

func (r *memberRepoPG) GetMember(ctx context.Context, circleID, userID uuid.UUID) (*domain.CircleMember, error) {
	var m domain.CircleMember
	err := r.db.WithContext(ctx).
		Where("circle_id = ? AND user_id = ?", circleID, userID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMemberNotFound
		}
		return nil, err
	}

	// 惰性解禁：status=禁言且 mute_end_time 已过 → 自愈回 normal（不建定时 job）。
	// 发帖校验按 mute_end_time 判定本就放行过期禁言，这里保证管理端/详情展示一致。
	// 自愈一次后 status=1，后续读不再触发写。CAS 失败（并发已变更）不影响正确性。
	if m.Status == domain.MemberStatusMuted && m.MuteEndTime != nil && m.MuteEndTime.Before(time.Now()) {
		res := r.db.WithContext(ctx).Model(&domain.CircleMember{}).
			Where("id = ? AND status = ?", m.ID, domain.MemberStatusMuted).
			Updates(map[string]interface{}{
				"status":        domain.MemberStatusNormal,
				"mute_end_time": nil,
			})
		if res.Error == nil && res.RowsAffected > 0 {
			m.Status = domain.MemberStatusNormal
			m.MuteEndTime = nil
		}
	}
	return &m, nil
}

// ListJoinedWithScore 列出用户 normal 成员的 (circleID, 加入时间ms)，按加入时间倒序。
// limit=0 表示不限制。用于 JoinedCirclesCache 重建。
func (r *memberRepoPG) ListJoinedWithScore(ctx context.Context, userID uuid.UUID, limit int) ([]domain.JoinedMember, error) {
	var rows []struct {
		CircleID   uuid.UUID `gorm:"column:circle_id"`
		CreateTime time.Time `gorm:"column:create_time"`
	}
	q := r.db.WithContext(ctx).Model(&domain.CircleMember{}).
		Where("user_id = ? AND status = ?", userID, domain.MemberStatusNormal).
		Order("create_time DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Select("circle_id, create_time").Find(&rows).Error; err != nil {
		return nil, err
	}
	members := make([]domain.JoinedMember, len(rows))
	for i, row := range rows {
		members[i] = domain.JoinedMember{
			CircleID: row.CircleID,
			ScoreMs:  row.CreateTime.UnixMilli(),
		}
	}
	return members, nil
}

// JoinCircle 用户加入圈子，与旧 model.JoinCircle 状态机完全一致。
func (r *memberRepoPG) JoinCircle(ctx context.Context, circleID, userID uuid.UUID, joinType int16) (*domain.CircleMember, error) {
	var existing domain.CircleMember
	err := r.db.WithContext(ctx).
		Where("circle_id = ? AND user_id = ?", circleID, userID).
		First(&existing).Error
	if err == nil {
		// 已存在成员记录
		if existing.Status == domain.MemberStatusBanned {
			return nil, fmt.Errorf("user is banned from this circle")
		}
		if existing.Status == domain.MemberStatusNormal {
			return nil, fmt.Errorf("user is already a member of this circle")
		}
		if existing.Status == domain.MemberStatusPending || existing.Status == domain.MemberStatusLeft {
			existing.Status = domain.MemberStatusNormal
			if e := r.db.WithContext(ctx).Save(&existing).Error; e != nil {
				return nil, e
			}
			return &existing, nil
		}
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var status int16 = domain.MemberStatusNormal
	switch joinType {
	case domain.CircleJoinTypeApproval:
		status = domain.MemberStatusPending
	case domain.CircleJoinTypePrivate:
		return nil, fmt.Errorf("this circle is private and requires invitation")
	}

	member := domain.CircleMember{
		CircleID:  circleID,
		UserID:    userID,
		Role:      domain.MemberRoleMember,
		Status:    status,
		IsTop:     0,
		IsDisturb: 0,
	}
	if member.ID == uuid.Nil {
		member.ID = sharedomain.NewID()
	}
	if err := r.db.WithContext(ctx).Create(&member).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

// LeaveCircle 与旧 model.LeaveCircle 一致。
func (r *memberRepoPG) LeaveCircle(ctx context.Context, circleID, userID uuid.UUID) error {
	var member domain.CircleMember
	err := r.db.WithContext(ctx).
		Where("circle_id = ? AND user_id = ?", circleID, userID).
		First(&member).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("user is not a member of this circle")
		}
		return err
	}
	if member.Role == domain.MemberRoleOwner {
		return fmt.Errorf("circle owner cannot leave the circle")
	}
	if member.Status != domain.MemberStatusNormal {
		return fmt.Errorf("member status is not normal, cannot leave")
	}
	return r.db.WithContext(ctx).Model(&member).Update("status", domain.MemberStatusLeft).Error
}

// ===== 成员管理（circle management）=====

// ListMembers 管理端成员列表，keyset 分页对齐 idx_member_circle_role。
//
// 排序 role DESC, create_time DESC, id DESC（id 为 UUIDv7，字典序 == 时间序，
// 作同毫秒写入的确定性 tiebreaker）。多取 1 行（size+1）判断 hasMore。
func (r *memberRepoPG) ListMembers(ctx context.Context, circleID uuid.UUID, role, status int16, userIDs []uuid.UUID, cursor string, size int) ([]domain.CircleMember, string, error) {
	// 惰性解禁：批量自愈已过期的禁言，保证管理列表状态准确。best-effort，失败不阻断查询。
	_ = r.healExpiredMutes(ctx, circleID)

	q := r.db.WithContext(ctx).Model(&domain.CircleMember{}).
		Where("circle_id = ?", circleID)
	if role >= 0 {
		q = q.Where("role = ?", role)
	}
	if status >= 0 {
		q = q.Where("status = ?", status)
	}
	if len(userIDs) > 0 {
		// 成员搜索过滤：user_id IN 走 idx_member_unique(circle_id, user_id) 前缀。
		q = q.Where("user_id IN ?", userIDs)
	}
	if cursor != "" {
		c, err := decodeMemberCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		q = q.Where(
			"(role < ? OR (role = ? AND (create_time < ? OR (create_time = ? AND id < ?))))",
			c.Role, c.Role, c.Time, c.Time, c.ID,
		)
	}

	var members []domain.CircleMember
	if err := q.Order("role DESC, create_time DESC, id DESC").Limit(size + 1).Find(&members).Error; err != nil {
		return nil, "", err
	}
	if len(members) > size {
		last := members[size-1]
		return members[:size], encodeMemberCursor(&last), nil
	}
	return members, "", nil
}

// healExpiredMutes 批量解除圈内已过期的禁言（status=2 且 mute_end_time 已过 → normal）。
func (r *memberRepoPG) healExpiredMutes(ctx context.Context, circleID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.CircleMember{}).
		Where("circle_id = ? AND status = ? AND mute_end_time IS NOT NULL AND mute_end_time < ?",
			circleID, domain.MemberStatusMuted, time.Now()).
		Updates(map[string]interface{}{
			"status":        domain.MemberStatusNormal,
			"mute_end_time": nil,
		}).Error
}

// UpdateMemberRole 角色变更（CAS）。要求目标当前为正常状态成员。
func (r *memberRepoPG) UpdateMemberRole(ctx context.Context, circleID, userID uuid.UUID, fromRole, toRole int16) error {
	res := r.db.WithContext(ctx).Model(&domain.CircleMember{}).
		Where("circle_id = ? AND user_id = ? AND role = ? AND status = ?",
			circleID, userID, fromRole, domain.MemberStatusNormal).
		Updates(map[string]interface{}{"role": toRole})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrMemberStateConflict
	}
	return nil
}

// UpdateMemberStatus 状态迁移（CAS）。toStatus=禁言时写 muteEndTime，其余迁移清空 mute_end_time。
func (r *memberRepoPG) UpdateMemberStatus(ctx context.Context, circleID, userID uuid.UUID, fromStatus, toStatus int16, muteEndTime time.Time) error {
	updates := map[string]interface{}{"status": toStatus}
	if toStatus == domain.MemberStatusMuted {
		updates["mute_end_time"] = muteEndTime
	} else {
		updates["mute_end_time"] = nil
	}
	res := r.db.WithContext(ctx).Model(&domain.CircleMember{}).
		Where("circle_id = ? AND user_id = ? AND status = ?", circleID, userID, fromStatus).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrMemberStateConflict
	}
	return nil
}

// TransferOwner 转让圈主（单事务）：旧圈主降为普通成员，目标升为圈主。
// 第二条 UPDATE 要求目标非圈主且状态正常；任一条 0 行受影响则回滚。
func (r *memberRepoPG) TransferOwner(ctx context.Context, circleID, fromUser, toUser uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		demote := tx.Model(&domain.CircleMember{}).
			Where("circle_id = ? AND user_id = ? AND role = ?", circleID, fromUser, domain.MemberRoleOwner).
			Update("role", domain.MemberRoleMember)
		if demote.Error != nil {
			return demote.Error
		}
		if demote.RowsAffected == 0 {
			return domain.ErrMemberStateConflict
		}

		promote := tx.Model(&domain.CircleMember{}).
			Where("circle_id = ? AND user_id = ? AND role <> ? AND status = ?",
				circleID, toUser, domain.MemberRoleOwner, domain.MemberStatusNormal).
			Update("role", domain.MemberRoleOwner)
		if promote.Error != nil {
			return promote.Error
		}
		if promote.RowsAffected == 0 {
			return domain.ErrMemberStateConflict
		}
		return nil
	})
}

// ===== 可管理圈子列表（AI 代理管理控制台）=====

// ListManagedCircles 列出用户作为圈主/管理员（role IN (20,30), status=normal）
// 的圈子，JOIN 未删除圈子；keyword 非空时按 name/description 子串过滤（ILIKE，
// 通配符已转义）。排序：圈主在前（role DESC），同角色按建圈时间新→旧，
// id（UUIDv7，字典序==时间序）作同毫秒确定性 tiebreaker。
// 直查 PG 不走 ES/缓存：管理控制台要求 SetMemberRole/TransferOwner 后立即可见。
// 含非正常状态圈子（banned/pending，其代理仍需管理/停用），由上层用 status 置灰。
func (r *memberRepoPG) ListManagedCircles(ctx context.Context, userID uuid.UUID, keyword string, offset, size int) ([]domain.ManagedCircle, int64, error) {
	filtered := func() *gorm.DB {
		q := r.db.WithContext(ctx).
			Table("domains.circle_member AS m").
			Joins("JOIN domains.circle AS c ON c.id = m.circle_id AND c.deleted = 0").
			Where("m.user_id = ? AND m.role IN ? AND m.status = ?", userID,
				[]int16{domain.MemberRoleAdmin, domain.MemberRoleOwner}, domain.MemberStatusNormal)
		if keyword != "" {
			like := "%" + escapeLike(keyword) + "%"
			q = q.Where("(c.name ILIKE ? ESCAPE '\\' OR c.description ILIKE ? ESCAPE '\\')", like, like)
		}
		return q
	}

	var total int64
	if err := filtered().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var circles []domain.ManagedCircle
	if err := filtered().
		Select("c.*, m.role AS my_role").
		Order("m.role DESC, c.create_time DESC, c.id DESC").
		Limit(size).Offset(offset).
		Find(&circles).Error; err != nil {
		return nil, 0, err
	}
	return circles, total, nil
}

// escapeLike 转义 LIKE/ILIKE 通配符（\ % _），配合 ESCAPE '\' 使用：
// 用户关键词中的通配符不得扩大匹配范围（如 "%%" 全匹配）或破坏子串语义。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ===== 成员列表游标工具 =====

// memberCursor 成员列表 keyset 游标（对齐排序键 role DESC, create_time DESC, id DESC）。
type memberCursor struct {
	Role int16
	Time time.Time
	ID   uuid.UUID
}

// encodeMemberCursor 编码为不透明 base64 JSON 游标。
// 时间用 UnixMicro：timestamptz 微秒精度，保证续页比较精确。
func encodeMemberCursor(m *domain.CircleMember) string {
	b, _ := json.Marshal(struct {
		R int16  `json:"r"`
		T int64  `json:"t"`
		I string `json:"i"`
	}{R: m.Role, T: m.CreateTime.UnixMicro(), I: m.ID.String()})
	return base64.StdEncoding.EncodeToString(b)
}

// decodeMemberCursor 防御性解析游标（用户可控参数）：字段缺失/类型错/坏 UUID
// 均返回包装 domain.ErrInvalidCursor 的错误，绝不 panic。
func decodeMemberCursor(s string) (*memberCursor, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64", domain.ErrInvalidCursor)
	}
	var raw struct {
		R *float64 `json:"r"`
		T *float64 `json:"t"`
		I string   `json:"i"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalidCursor, err.Error())
	}
	if raw.R == nil || raw.T == nil || raw.I == "" {
		return nil, fmt.Errorf("%w: missing cursor fields", domain.ErrInvalidCursor)
	}
	id, err := uuid.Parse(raw.I)
	if err != nil {
		return nil, fmt.Errorf("%w: bad member id", domain.ErrInvalidCursor)
	}
	return &memberCursor{Role: int16(*raw.R), Time: time.UnixMicro(int64(*raw.T)), ID: id}, nil
}
