package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// 举报复核的人工审核日志 decision（post_moderation_log，source=2，#153）。
const (
	ModerationReportDismiss ModerationDecision = "report_dismiss" // 圈管复核：举报不成立，保留帖子并关闭举报
	ModerationReportRemove  ModerationDecision = "report_remove"  // 圈管复核：举报成立，下架帖子（status 3）
)

// 举报处理状态（post_report.status）。
const (
	PostReportPending   int16 = 0 // 待处理
	PostReportDismissed int16 = 1 // 已处理：举报不成立（帖子保留）
	PostReportRemoved   int16 = 2 // 已处理：帖子已下架
)

// 举报原因（post_report.reason_type）。
const (
	ReportReasonSpamAd     int16 = 1 // 垃圾广告 / 站外引流
	ReportReasonSexual     int16 = 2 // 色情低俗
	ReportReasonHarassment int16 = 3 // 辱骂骚扰 / 人身攻击
	ReportReasonIllegal    int16 = 4 // 违法违规（赌博、毒品、诈骗等）
	ReportReasonViolence   int16 = 5 // 暴力恐怖
	ReportReasonPrivacy    int16 = 6 // 泄露隐私
	ReportReasonSelfHarm   int16 = 7 // 自伤 / 轻生倾向
	ReportReasonOther      int16 = 8 // 其他
)

// ValidReportReason 举报原因是否合法。
func ValidReportReason(t int16) bool {
	return t >= ReportReasonSpamAd && t <= ReportReasonOther
}

// ErrPostReportDuplicate 同一用户重复举报同一帖子（uk (post_id, reporter_id)）。
var ErrPostReportDuplicate = errors.New("post already reported by this user")

// PostReport 帖子举报（domains.post_report，#153）。
//
// 每个用户对同一帖子只能举报一次（处理后也不能再举报）。circle_id 冗余自帖子，供圈内复核列表按圈查询。
// Notified 标记本轮待处理举报是否已通知过圈管：同一帖子在被处理前只通知一次。
type PostReport struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey;column:id"`
	PostID     uuid.UUID  `gorm:"column:post_id;type:uuid;not null"`
	CircleID   uuid.UUID  `gorm:"column:circle_id;type:uuid;not null"`
	ReporterID uuid.UUID  `gorm:"column:reporter_id;type:uuid;not null"`
	ReasonType int16      `gorm:"column:reason_type;type:smallint;not null"`
	Reason     string     `gorm:"column:reason;type:varchar(200);not null"`
	Status     int16      `gorm:"column:status;type:smallint;not null"`
	Notified   int16      `gorm:"column:notified;type:smallint;not null"`
	HandlerID  *uuid.UUID `gorm:"column:handler_id;type:uuid"`
	HandleTime *time.Time `gorm:"column:handle_time"`
	CreateTime time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time  `gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (PostReport) TableName() string { return "domains.post_report" }

// ReportThresholds 举报计数阈值（来自 moderation 配置，调用方先 WithDefaults）。
type ReportThresholds struct {
	Notify int // 待处理举报数 >= Notify 且本轮未通知过 → 通知圈管
	Hide   int // 待处理举报数 >= Hide 时把已发布帖子自动屏蔽（status 4）；<=0 关闭
}

// ReportOutcome 一次举报写入后的结果。
type ReportOutcome struct {
	Pending int  // 本帖当前待处理举报数（含本次）
	Notify  bool // 本次举报让帖子达到通知阈值（每轮只有一次为 true）
	Hidden  bool // 本次举报让帖子被自动屏蔽
}

// ReportedPost 圈内有待处理举报的帖子（复核列表行）。
type ReportedPost struct {
	Post
	ReportCount     int       `gorm:"column:report_count"`
	FirstReportTime time.Time `gorm:"column:first_report_time"`
}

// PostReportRepository 举报持久化端口（#153）。
type PostReportRepository interface {
	// Create 写入一条举报并按阈值决定是否通知 / 自动屏蔽。同一帖子的举报与复核串行化（事务级 advisory lock），
	// 保证「处理前只通知一次」与计数准确。重复举报返回 ErrPostReportDuplicate。
	Create(ctx context.Context, report *PostReport, t ReportThresholds) (ReportOutcome, error)
	// CountByReporterSince 用户在 since 之后提交的举报数（每日上限）。
	CountByReporterSince(ctx context.Context, reporterID uuid.UUID, since time.Time) (int64, error)
	// ListReportedByCircle 圈内有待处理举报、且已发布 / 已屏蔽的帖子，按 (首次举报时间, post_id) 倒序 keyset 翻页；
	// after 为 nil 表示第一页（PostKeyset.CreateTime 为首次举报时间）。
	ListReportedByCircle(ctx context.Context, circleID uuid.UUID, after *PostKeyset, limit int) ([]ReportedPost, error)
	// CountReportedByCircle 圈内有待处理举报、且已发布 / 已屏蔽的帖子数，口径与 ListReportedByCircle 一致（#156）。
	CountReportedByCircle(ctx context.Context, circleID uuid.UUID) (int64, error)
	// ListPendingByPostIDs 每篇帖子最近 perPost 条待处理举报（新→旧）。
	ListPendingByPostIDs(ctx context.Context, postIDs []uuid.UUID, perPost int) (map[uuid.UUID][]PostReport, error)
	// Resolve 圈管复核（同一事务）：关闭该帖全部待处理举报 → remove 时帖子转为驳回（status 3），
	// dismiss 时把自动屏蔽的帖子恢复为已发布 → 写人工审核日志。
	// 帖子没有待处理举报 / 不在该圈 / 已不是已发布或已屏蔽时返回 false（并发复核只有一方成功）。
	Resolve(ctx context.Context, postID, circleID uuid.UUID, remove bool, log *PostModerationLog) (closed int, ok bool, err error)
}
