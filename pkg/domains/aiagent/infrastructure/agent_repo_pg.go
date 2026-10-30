// Package infrastructure 提供 aiagent 领域基础设施层实现。
package infrastructure

import (
	"context"
	"encoding/json"
	"errors"

	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// agentRepoPG 基于 GORM 的 AgentRepository 实现。
type agentRepoPG struct {
	db *gorm.DB
}

// NewAgentRepository 构造 AgentRepository。
func NewAgentRepository(db *gorm.DB) domain.AgentRepository {
	return &agentRepoPG{db: db}
}

func (r *agentRepoPG) Create(ctx context.Context, agent *domain.AiAgent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

func (r *agentRepoPG) GetByID(ctx context.Context, agentID uuid.UUID) (*domain.AiAgent, error) {
	var a domain.AiAgent
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", agentID, 0).First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAgentNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *agentRepoPG) ExistsByName(ctx context.Context, name string, excludeID uuid.UUID) (bool, error) {
	var a domain.AiAgent
	query := r.db.WithContext(ctx).Where("name = ? AND deleted = ?", name, 0)
	if excludeID != uuid.Nil {
		query = query.Where("id <> ?", excludeID)
	}
	err := query.First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ExistsByNameInScope 检查 (作用域, name) 是否被占用。circleID=uuid.Nil 查全局桶
// （circle_id IS NULL），否则查该圈桶，与唯一索引 idx_ai_agent_name 的分桶口径一致。
func (r *agentRepoPG) ExistsByNameInScope(ctx context.Context, circleID uuid.UUID, name string, excludeID uuid.UUID) (bool, error) {
	var a domain.AiAgent
	query := r.db.WithContext(ctx).Where("name = ? AND deleted = ?", name, 0)
	if circleID == uuid.Nil {
		query = query.Where("circle_id IS NULL")
	} else {
		query = query.Where("circle_id = ?", circleID)
	}
	if excludeID != uuid.Nil {
		query = query.Where("id <> ?", excludeID)
	}
	err := query.First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CountByCircleIDs 批量统计各圈未删除机器人数（走 idx_ai_agent_circle 部分索引）。
// 无机器人的圈不在返回 map 中（调用方按 0 处理）。
func (r *agentRepoPG) CountByCircleIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	if len(circleIDs) == 0 {
		return map[uuid.UUID]int{}, nil
	}
	var rows []struct {
		CircleID uuid.UUID `gorm:"column:circle_id"`
		Cnt      int       `gorm:"column:cnt"`
	}
	err := r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Select("circle_id, COUNT(*) AS cnt").
		Where("circle_id IN ? AND deleted = ?", circleIDs, 0).
		Group("circle_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		result[row.CircleID] = row.Cnt
	}
	return result, nil
}

// ListByOffset keyword 非空时按 name 模糊过滤（ILIKE %kw%，大小写不敏感）。
// 仅返回全局机器人（circle_id IS NULL）：平台超管控制台不展示圈内机器人。
func (r *agentRepoPG) ListByOffset(ctx context.Context, keyword string, offset, limit int) ([]domain.AiAgent, int64, error) {
	var (
		agents []domain.AiAgent
		total  int64
	)
	query := r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Where("circle_id IS NULL AND deleted = ?", 0)
	if keyword != "" {
		query = query.Where("name ILIKE ?", "%"+keyword+"%")
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.
		Order("create_time DESC").
		Offset(offset).Limit(limit).
		Find(&agents).Error
	if err != nil {
		return nil, 0, err
	}
	return agents, total, nil
}

// ListByCircle 圈内机器人 offset 分页。keyword 非空时按 name 模糊过滤（ILIKE %kw%）。
func (r *agentRepoPG) ListByCircle(ctx context.Context, circleID uuid.UUID, keyword string, offset, limit int) ([]domain.AiAgent, int64, error) {
	var (
		agents []domain.AiAgent
		total  int64
	)
	query := r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Where("circle_id = ? AND deleted = ?", circleID, 0)
	if keyword != "" {
		query = query.Where("name ILIKE ?", "%"+keyword+"%")
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.
		Order("create_time DESC").
		Offset(offset).Limit(limit).
		Find(&agents).Error
	if err != nil {
		return nil, 0, err
	}
	return agents, total, nil
}

// CreateInCircle 单事务创建圈内机器人：先锁圈子行（SELECT ... FOR UPDATE）把同圈
// 并发创建串行化，再计数校验每圈上限（行锁前任何预检都防不了并发双过），最后插入。
// 圈行缺失（并发圈子被物理清除等极端场景）返回 ErrAgentNotFound，不落孤儿数据。
func (r *agentRepoPG) CreateInCircle(ctx context.Context, agent *domain.AiAgent, maxPerCircle int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 锁圈子行：同圈创建在此排队。圈行软删不物理删，锁目标稳定。
		var circleID string
		if err := tx.Raw("SELECT id FROM domains.circle WHERE id = ? FOR UPDATE", agent.CircleID).
			Scan(&circleID).Error; err != nil {
			return err
		}
		if circleID == "" {
			return domain.ErrAgentNotFound
		}
		var count int64
		if err := tx.Model(&domain.AiAgent{}).
			Where("circle_id = ? AND deleted = ?", agent.CircleID, 0).
			Count(&count).Error; err != nil {
			return err
		}
		if int(count) >= maxPerCircle {
			return domain.ErrCircleAgentLimit
		}
		return tx.Create(agent).Error
	})
}

// UpdateFields 部分更新。auto_config 补丁（domain.AutoConfigPatch）翻译为 jsonb 顶层合并
// `auto_config || ?::jsonb`：单条 UPDATE 原子执行，只覆盖补丁里的 key（不同子对象并发写互不覆盖）。
func (r *agentRepoPG) UpdateFields(ctx context.Context, agentID uuid.UUID, fields map[string]interface{}) error {
	if patch, ok := fields["auto_config"].(domain.AutoConfigPatch); ok {
		raw, err := json.Marshal(patch)
		if err != nil {
			return err
		}
		cp := make(map[string]interface{}, len(fields))
		for k, v := range fields {
			cp[k] = v
		}
		cp["auto_config"] = gorm.Expr("auto_config || ?::jsonb", string(raw))
		fields = cp
	}
	return r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Where("id = ? AND deleted = ?", agentID, 0).
		Updates(fields).Error
}

func (r *agentRepoPG) SoftDelete(ctx context.Context, agentID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Where("id = ? AND deleted = ?", agentID, 0).
		Updates(map[string]interface{}{
			"deleted": 1,
			"status":  domain.AgentStatusDisabled,
		}).Error
}

// ListEnabledForCircle 圈子作用域触发候选集：全局机器人 + 该圈启用中的机器人。
// circleID=uuid.Nil 时仅返回全局机器人（全站场景，OR 分支退化为 IS NULL 单独成条，
// 避免与全零 UUID 桶语义混淆）。原 ListEnabled 的"circle_id IS NULL 防泄漏护栏"
// 由本方法的 OR 语义接棒：他圈机器人不进任何帖子的触发链。
func (r *agentRepoPG) ListEnabledForCircle(ctx context.Context, circleID uuid.UUID) ([]domain.AiAgent, error) {
	var agents []domain.AiAgent
	query := r.db.WithContext(ctx).
		Where("deleted = ? AND status = ?", 0, domain.AgentStatusEnabled)
	if circleID == uuid.Nil {
		query = query.Where("circle_id IS NULL")
	} else {
		query = query.Where("circle_id IS NULL OR circle_id = ?", circleID)
	}
	err := query.Order("create_time ASC").Find(&agents).Error
	return agents, err
}

// ListAutoReplyEnabled 自动回复扫描候选机器人。表小（全局 + 每圈 ≤5），
// idx_ai_agent_active 部分索引过滤启用行后在 jsonb 上判 enabled 即可。
func (r *agentRepoPG) ListAutoReplyEnabled(ctx context.Context) ([]domain.AiAgent, error) {
	var agents []domain.AiAgent
	err := r.db.WithContext(ctx).
		Where("deleted = ? AND status = ?", 0, domain.AgentStatusEnabled).
		Where("auto_config -> ? -> 'enabled' = 'true'::jsonb", domain.AutoConfigKeyAutoReply).
		Order("create_time ASC").
		Find(&agents).Error
	return agents, err
}

func (r *agentRepoPG) ExistsByLinkedUserID(ctx context.Context, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Where("linked_user_id = ? AND deleted = ?", userID, 0).
		Count(&count).Error
	return count > 0, err
}
