package infrastructure

import (
	"context"
	"errors"
	"time"

	"interestBar/pkg/domains/aiagent/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// replyLogRepoPG 基于 GORM 的 ReplyLogRepository 实现。
type replyLogRepoPG struct {
	db *gorm.DB
}

// NewReplyLogRepository 构造 ReplyLogRepository。
func NewReplyLogRepository(db *gorm.DB) domain.ReplyLogRepository {
	return &replyLogRepoPG{db: db}
}

func (r *replyLogRepoPG) Create(ctx context.Context, log *domain.ReplyLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// CountSinceByAgent 限频口径排除 status=2/3（分类器未产出回复的判定/运维事件，不占配额）。
func (r *replyLogRepoPG) CountSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.ReplyLog{}).
		Where("agent_id = ? AND create_time >= ? AND status NOT IN ?", agentID, since, domain.RateLimitExcludedStatuses).
		Count(&count).Error
	return count, err
}

// GetLastByAgent 限频口径排除 status=2/3（同上，最小间隔只相对真实回复/失败尝试计算）。
func (r *replyLogRepoPG) GetLastByAgent(ctx context.Context, agentID uuid.UUID) (*domain.ReplyLog, error) {
	var log domain.ReplyLog
	err := r.db.WithContext(ctx).
		Where("agent_id = ? AND status NOT IN ?", agentID, domain.RateLimitExcludedStatuses).
		Order("create_time DESC").
		First(&log).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &log, nil
}

// zeroUUID 与部分唯一索引 idx_ai_reply_auto_target 的 COALESCE 归桶值一致（回复帖子本身）。
const zeroUUID = "00000000-0000-0000-0000-000000000000"

// CreateAutoPlaceholder 插入自动回复占位行；ON CONFLICT DO NOTHING 吸收撞
// idx_ai_reply_auto_target（该机器人已对该目标触发过）的情况，RowsAffected=0 即已存在。
func (r *replyLogRepoPG) CreateAutoPlaceholder(ctx context.Context, log *domain.ReplyLog) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(log)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// FinishAutoPlaceholder 占位行原地更新为终态（只更新 trigger_source=4 的行，防误改被动触发日志）。
func (r *replyLogRepoPG) FinishAutoPlaceholder(ctx context.Context, log *domain.ReplyLog) error {
	return r.db.WithContext(ctx).Model(&domain.ReplyLog{}).
		Where("id = ? AND trigger_source = ?", log.ID, domain.TriggerSourceAuto).
		Updates(map[string]interface{}{
			"status":            log.Status,
			"comment_id":        log.CommentID,
			"error_msg":         log.ErrorMsg,
			"latency_ms":        log.LatencyMs,
			"prompt_tokens":     log.PromptTokens,
			"completion_tokens": log.CompletionTokens,
		}).Error
}

// CountAutoSinceByAgent 自动回复每小时上限口径：trigger_source=4 且排除 status=2/3（同限频）。
// 走 idx_ai_reply_agent_time。
func (r *replyLogRepoPG) CountAutoSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.ReplyLog{}).
		Where("agent_id = ? AND trigger_source = ? AND create_time >= ? AND status NOT IN ?",
			agentID, domain.TriggerSourceAuto, since, domain.RateLimitExcludedStatuses).
		Count(&count).Error
	return count, err
}

// CountAutoByTarget 每目标自动回复机器人数（排除分类器判定不回复的 status=2）。
// 条件与 idx_ai_reply_auto_target 的 (post_id, COALESCE(target_comment_id, 全零)) 前缀一致，走该索引。
func (r *replyLogRepoPG) CountAutoByTarget(ctx context.Context, postID uuid.UUID, targetCommentID *uuid.UUID) (int64, error) {
	target := uuid.MustParse(zeroUUID)
	if targetCommentID != nil {
		target = *targetCommentID
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.ReplyLog{}).
		Where("trigger_source = ? AND post_id = ? AND COALESCE(target_comment_id, '"+zeroUUID+"'::uuid) = ? AND status <> ?",
			domain.TriggerSourceAuto, postID, target, domain.ReplyStatusSkipped).
		Count(&count).Error
	return count, err
}

// ListAutoTargetsSince 时间窗口内全部自动回复目标。日志主键是 UUIDv7（时间序），
// 用 id 下界走主键范围扫描，避免按 create_time 全表扫；create_time 条件保证语义精确。
func (r *replyLogRepoPG) ListAutoTargetsSince(ctx context.Context, since time.Time) ([]domain.AutoTargetRecord, error) {
	var rows []domain.ReplyLog
	err := r.db.WithContext(ctx).
		Select("agent_id", "post_id", "target_comment_id", "status").
		Where("id >= ? AND create_time >= ? AND trigger_source = ?", sharedomain.UUIDv7LowerBound(since), since, domain.TriggerSourceAuto).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.AutoTargetRecord, 0, len(rows))
	for i := range rows {
		out = append(out, domain.AutoTargetRecord{
			AgentID:         rows[i].AgentID,
			PostID:          rows[i].PostID,
			TargetCommentID: rows[i].TargetCommentID,
			Status:          rows[i].Status,
		})
	}
	return out, nil
}
