package domain

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// 审核来源（post_moderation_log.source）。
const (
	ModerationSourceAI     int16 = 1 // 审核 worker（#117）
	ModerationSourceManual int16 = 2 // 圈主 / 管理员人工审核（#118）
)

// PostModerationLog 帖子审核日志（domains.post_moderation_log，append-only，#117 / #118）。
//
// AI 审核每次写一行；审核调用失败时 Hazards 为空、Severity / SeverityConfidence 为 nil、ErrorMsg 非空。
// 人工审核（Source=ModerationSourceManual）写一行 Decision=publish / reject，带处理人 OperatorID 与可选备注 Reason，
// 不含模型打分。
type PostModerationLog struct {
	ID                 uuid.UUID          `gorm:"type:uuid;primaryKey;column:id"`
	PostID             uuid.UUID          `gorm:"column:post_id;type:uuid;not null"`
	Decision           ModerationDecision `gorm:"column:decision;type:varchar(16);not null"`
	Hazards            HazardScores       `gorm:"column:hazards;type:jsonb"`
	Severity           *float64           `gorm:"column:severity"`
	SeverityConfidence *float64           `gorm:"column:severity_confidence"`
	Model              string             `gorm:"column:model;type:varchar(64)"`
	InputTokens        int                `gorm:"column:input_tokens"`
	LatencyMs          int                `gorm:"column:latency_ms"`
	ErrorMsg           *string            `gorm:"column:error_msg;type:varchar(2048)"`
	Source             int16              `gorm:"column:source;type:smallint;not null"`
	OperatorID         *uuid.UUID         `gorm:"column:operator_id;type:uuid"`
	Reason             *string            `gorm:"column:reason;type:varchar(200)"`
	CreateTime         time.Time          `gorm:"column:create_time;autoCreateTime"`
}

// TableName 指定表名。
func (PostModerationLog) TableName() string { return "domains.post_moderation_log" }

// HazardScores 各风险概率（jsonb），key 为 Hazard 名。
type HazardScores map[Hazard]float64

// Scan 实现 sql.Scanner 接口。
func (h *HazardScores) Scan(value interface{}) error {
	if value == nil {
		*h = HazardScores{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("HazardScores: cannot scan non-[]byte value")
	}
	return json.Unmarshal(bytes, h)
}

// Value 实现 driver.Valuer 接口。
func (h HazardScores) Value() (driver.Value, error) {
	if len(h) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(map[Hazard]float64(h))
}

// PostModerationLogRepository 审核日志持久化端口。
type PostModerationLogRepository interface {
	// Create 写入一行审核日志（ID 为空时生成 UUIDv7）。
	Create(ctx context.Context, log *PostModerationLog) error
	// ExistsByPostID 该帖子是否已有审核日志（worker 幂等：人工待审不改状态，靠它识别重复投递）。
	ExistsByPostID(ctx context.Context, postID uuid.UUID) (bool, error)
	// LatestAIByPostIDs 批量取每篇帖子最近一条 AI 审核日志（source=1），没有的帖子不在 map 中（人工待审列表用，#118）。
	LatestAIByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]*PostModerationLog, error)
}
