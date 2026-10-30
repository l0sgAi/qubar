package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ReplyLog AI 回复日志（表 domains.ai_agent_reply_log）。
//
// 每次真实发起回复调用链路（LLM 调用/评论落库）写一行终态记录，失败不重试。
// 关键词/@提及/手动触发 append-only；自动回复（trigger_source=4）先插 status=4 占位行
// （撞部分唯一索引即跳过，保证每机器人每目标最多一次），终态时原地更新这一行。
// 频率限制统计排除 status=2/3（分类器未产出回复的判定/运维事件，不占配额）。
type ReplyLog struct {
	ID               uuid.UUID     `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	AgentID          uuid.UUID     `json:"agent_id" gorm:"column:agent_id;type:uuid;not null"`
	PostID           uuid.UUID     `json:"post_id" gorm:"column:post_id;type:uuid;not null"`
	CommentID        *uuid.UUID    `json:"comment_id,omitempty" gorm:"column:comment_id;type:uuid"` // 失败时为 NULL
	UserID           uuid.UUID     `json:"user_id" gorm:"column:user_id;type:uuid;not null"`        // 帖子作者ID（冗余，风控分析用）
	TriggerSource    TriggerSource `json:"trigger_source" gorm:"column:trigger_source;type:smallint;not null;default:0"`
	TargetCommentID  *uuid.UUID    `json:"target_comment_id,omitempty" gorm:"column:target_comment_id;type:uuid"` // 回复挂在该评论楼层内时非 nil
	Status           int16         `json:"status" gorm:"column:status;type:smallint;not null"`                    // 0=失败, 1=成功, 2=分类器跳过, 3=分类器超时降级, 4=自动回复占位
	ErrorMsg         string        `json:"error_msg,omitempty" gorm:"column:error_msg;type:varchar(2048)"`
	LatencyMs        int           `json:"latency_ms" gorm:"column:latency_ms;not null;default:0"`
	PromptTokens     int           `json:"prompt_tokens" gorm:"column:prompt_tokens;not null;default:0"`
	CompletionTokens int           `json:"completion_tokens" gorm:"column:completion_tokens;not null;default:0"`
	CreateTime       time.Time     `json:"create_time" gorm:"column:create_time;autoCreateTime"`
}

// TableName 指定表名。
func (ReplyLog) TableName() string { return "domains.ai_agent_reply_log" }

// 回复结果状态常量。
const (
	ReplyStatusFailed  = 0 // 调用链路失败（LLM 报错/空回复/评论落库失败）
	ReplyStatusOK      = 1 // 回复成功
	ReplyStatusSkipped = 2 // 分类器判定不回复（reason 入 error_msg；不参与限频统计）
	// ReplyStatusClassifierTimeout 分类器超时降级直回（运维事件，error_msg 前缀
	// classifier_timeout_fallback）。与 2 区分：2 是分类器明确判定不回复（内容拒绝），
	// 3 是判定未完成、流程降级继续生成（后续通常另有 0/1 终态行）。不参与限频统计。
	ReplyStatusClassifierTimeout = 3
	// ReplyStatusPending 自动回复占位行（先占位后调 LLM，终态时原地更新为 0/1/2）。
	// 进程中断时保持 4，视为已触发不重试（决策 C）；计入限频统计（是真实回复尝试）。
	ReplyStatusPending = 4
)

// TriggerSource 回复触发来源（trigger_source 列，smallint）。
//
// 与 TriggerMode（机器人配置的被动触发模式）是两套编码：TriggerSource 记录的是
// 某一行日志实际由哪个入口触发。0 为迁移窗口期旧版本写入的未知来源。
type TriggerSource int16

// 触发来源枚举值。
const (
	TriggerSourceKeyword TriggerSource = 1 // 评论关键词
	TriggerSourceMention TriggerSource = 2 // 发帖 @提及
	TriggerSourceManual  TriggerSource = 3 // 管理端手动
	TriggerSourceAuto    TriggerSource = 4 // 自动回复（定时扫描高热度帖子 / 评论）
)

// AutoTargetRecord 一条自动回复记录的目标（供扫描时排除已触发 / 已饱和的目标）。
type AutoTargetRecord struct {
	AgentID         uuid.UUID
	PostID          uuid.UUID
	TargetCommentID *uuid.UUID // nil = 回复帖子本身
	Status          int16
}

// RateLimitExcludedStatuses 不参与限频统计的状态（未产出真实回复尝试的运营/判定事件）。
var RateLimitExcludedStatuses = []int16{ReplyStatusSkipped, ReplyStatusClassifierTimeout}

// ReplyLogRepository 是回复日志的持久化接口（由 infrastructure 实现）。
type ReplyLogRepository interface {
	// Create 插入一条终态日志（append-only，不设防重）。
	Create(ctx context.Context, log *ReplyLog) error
	// CountSinceByAgent 统计机器人自 since 以来的日志行数（含失败、排除 status=2/3，限频口径）。
	CountSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error)
	// GetLastByAgent 取机器人最新一条日志（排除 status=2/3，算最小回复间隔）。无日志返回 nil, nil。
	GetLastByAgent(ctx context.Context, agentID uuid.UUID) (*ReplyLog, error)

	// ---- 自动回复（trigger_source=4）----

	// CreateAutoPlaceholder 插入自动回复占位行（调用方已填 trigger_source=4 / status=4）。
	// 撞部分唯一索引 idx_ai_reply_auto_target（该机器人已对该目标触发过）返回 false, nil。
	CreateAutoPlaceholder(ctx context.Context, log *ReplyLog) (bool, error)
	// FinishAutoPlaceholder 把占位行原地更新为终态（status/comment_id/error_msg/latency/tokens）。
	FinishAutoPlaceholder(ctx context.Context, log *ReplyLog) error
	// CountAutoSinceByAgent 统计机器人自 since 以来的自动回复行数（排除 status=2/3，口径同限频）。
	CountAutoSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error)
	// CountAutoByTarget 统计某目标已有的自动回复机器人数（排除 status=2：分类器判定不回复的不占名额）。
	// targetCommentID=nil 表示回复帖子本身。
	CountAutoByTarget(ctx context.Context, postID uuid.UUID, targetCommentID *uuid.UUID) (int64, error)
	// ListAutoTargetsSince 列出自 since 以来全部机器人的自动回复目标（扫描时排除本机器人已触发
	// 与已达 max_agents_per_target 的目标；since 取时间窗口起点，窗口内内容的回复行必然在其后）。
	ListAutoTargetsSince(ctx context.Context, since time.Time) ([]AutoTargetRecord, error)
}
