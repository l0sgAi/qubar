package domain

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// PostLog 机器人自动发帖日志（表 domains.ai_agent_post_log，#101）。
//
// 每次发帖尝试先插 status=2 占位行（撞唯一约束 (agent_id, topic_key) 即跳过，保证同一机器人
// 同一话题只发一次），终态时原地更新这一行。interval_min / daily_max 限频统计这张表（含失败）。
type PostLog struct {
	ID               uuid.UUID       `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	AgentID          uuid.UUID       `json:"agent_id" gorm:"column:agent_id;type:uuid;not null"`
	CircleID         uuid.UUID       `json:"circle_id" gorm:"column:circle_id;type:uuid;not null"`
	TopicKey         string          `json:"topic_key" gorm:"column:topic_key;type:varchar(64);not null"`
	PostID           *uuid.UUID      `json:"post_id,omitempty" gorm:"column:post_id;type:uuid"` // 占位 / 失败时为 NULL
	Status           int16           `json:"status" gorm:"column:status;type:smallint;not null"`
	SearchQuery      string          `json:"search_query" gorm:"column:search_query;type:varchar(128);not null"`
	Sources          PostSourcesJSON `json:"sources" gorm:"column:sources;type:jsonb;not null"`
	ErrorMsg         string          `json:"error_msg,omitempty" gorm:"column:error_msg;type:varchar(2048)"`
	LatencyMs        int             `json:"latency_ms" gorm:"column:latency_ms;not null;default:0"`
	PromptTokens     int             `json:"prompt_tokens" gorm:"column:prompt_tokens;not null;default:0"`
	CompletionTokens int             `json:"completion_tokens" gorm:"column:completion_tokens;not null;default:0"`
	CreateTime       time.Time       `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime       time.Time       `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (PostLog) TableName() string { return "domains.ai_agent_post_log" }

// 自动发帖结果状态常量。
const (
	PostLogStatusFailed  = 0 // 失败（LLM 报错 / 输出非法 / 引用了不存在的来源 / 发帖落库失败）
	PostLogStatusOK      = 1 // 发帖成功
	PostLogStatusPending = 2 // 占位（执行中；进程中断则保持 2，视为已触发不重试）
)

// PostSource 发帖日志里记录的一条搜索来源。
type PostSource struct {
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Source      string     `json:"source,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// PostSourcesJSON 来源列表（jsonb 数组）。
type PostSourcesJSON []PostSource

// Scan 实现 sql.Scanner 接口。
func (p *PostSourcesJSON) Scan(value interface{}) error {
	if value == nil {
		*p = PostSourcesJSON{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("PostSourcesJSON: cannot scan non-[]byte value")
	}
	return json.Unmarshal(bytes, p)
}

// Value 实现 driver.Valuer 接口。
func (p PostSourcesJSON) Value() (driver.Value, error) {
	if len(p) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(p)
}

// PostLogRepository 是自动发帖日志的持久化接口（由 infrastructure 实现）。
type PostLogRepository interface {
	// CreatePlaceholder 插入占位行（调用方已填 status=2）。撞唯一约束 (agent_id, topic_key)
	// （该机器人已发过该话题）返回 false, nil。
	CreatePlaceholder(ctx context.Context, log *PostLog) (bool, error)
	// Finish 把占位行原地更新为终态（status/post_id/sources/error_msg/latency/tokens）。
	Finish(ctx context.Context, log *PostLog) error
	// CountSinceByAgent 统计机器人自 since 以来的发帖尝试数（含失败 / 占位，daily_max 口径）。
	CountSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error)
	// GetLastTimeByAgent 机器人最近一次发帖尝试的时间（interval_min 口径）。无记录返回 nil, nil。
	GetLastTimeByAgent(ctx context.Context, agentID uuid.UUID) (*time.Time, error)
	// ListSourceURLsSince 该机器人自 since 以来日志里出现过的全部来源 URL（发帖前去重：
	// 失败 / 占位行是提供给 LLM 的全部材料，成功行是帖子实际引用的来源）。
	ListSourceURLsSince(ctx context.Context, agentID uuid.UUID, since time.Time) ([]string, error)
}
