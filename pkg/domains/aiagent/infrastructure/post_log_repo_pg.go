package infrastructure

import (
	"context"
	"errors"
	"time"

	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// postLogRepoPG 基于 GORM 的 PostLogRepository 实现。
type postLogRepoPG struct {
	db *gorm.DB
}

// NewPostLogRepository 构造 PostLogRepository。
func NewPostLogRepository(db *gorm.DB) domain.PostLogRepository {
	return &postLogRepoPG{db: db}
}

// CreatePlaceholder ON CONFLICT DO NOTHING 吸收撞 uk_ai_agent_post_log_topic 的情况，RowsAffected=0 即已存在。
func (r *postLogRepoPG) CreatePlaceholder(ctx context.Context, log *domain.PostLog) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(log)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// Finish 占位行原地更新为终态（只改 status=2 的行，终态不可再改）。
func (r *postLogRepoPG) Finish(ctx context.Context, log *domain.PostLog) error {
	return r.db.WithContext(ctx).Model(&domain.PostLog{}).
		Where("id = ? AND status = ?", log.ID, domain.PostLogStatusPending).
		Updates(map[string]interface{}{
			"status":            log.Status,
			"post_id":           log.PostID,
			"sources":           log.Sources,
			"error_msg":         log.ErrorMsg,
			"latency_ms":        log.LatencyMs,
			"prompt_tokens":     log.PromptTokens,
			"completion_tokens": log.CompletionTokens,
			"update_time":       time.Now(),
		}).Error
}

// CountSinceByAgent 走 idx_ai_agent_post_log_agent_time。
func (r *postLogRepoPG) CountSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.PostLog{}).
		Where("agent_id = ? AND create_time >= ?", agentID, since).
		Count(&count).Error
	return count, err
}

// GetLastTimeByAgent 走 idx_ai_agent_post_log_agent_time。
func (r *postLogRepoPG) GetLastTimeByAgent(ctx context.Context, agentID uuid.UUID) (*time.Time, error) {
	var log domain.PostLog
	err := r.db.WithContext(ctx).Select("create_time").
		Where("agent_id = ?", agentID).
		Order("create_time DESC").
		Take(&log).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &log.CreateTime, nil
}

// ListSourceURLsSince 走 idx_ai_agent_post_log_agent_time 后展开 sources（单机器人窗口内行数有限）。
func (r *postLogRepoPG) ListSourceURLsSince(ctx context.Context, agentID uuid.UUID, since time.Time) ([]string, error) {
	var urls []string
	err := r.db.WithContext(ctx).Raw(`SELECT DISTINCT s->>'url' FROM domains.ai_agent_post_log l,
		jsonb_array_elements(l.sources) s WHERE l.agent_id = ? AND l.create_time >= ?`, agentID, since).
		Scan(&urls).Error
	return urls, err
}
