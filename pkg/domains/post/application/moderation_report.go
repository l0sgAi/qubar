package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// 帖子举报与圈管复核（#153，先发后审）。
//
//	用户举报已发布帖子 → post_report（每人每帖一次、24 小时上限）
//	  → 待处理举报数达到 report_notify_threshold：通知本圈圈主 + 管理员（本轮只通知一次）
//	  → 达到 report_hide_threshold（>0 才启用）：自动屏蔽（status 1 → 4），不进 #118 待审队列
//	圈主 / 管理员在被举报列表里复核：
//	  dismiss → 关闭本轮举报，帖子保留（已屏蔽的恢复为已发布）
//	  remove  → 关闭本轮举报，帖子下架为 status 3 + 通知作者
//	复核与举报写入在仓储内按帖子串行化，并发复核只有一方成功（其余 404），人工动作写 post_moderation_log（source=2）。

// reportRecentPerPost 被举报列表每帖附带的最近举报条数。
const reportRecentPerPost = 10

// reportLimitWindow 举报上限统计窗口（滚动 24 小时）。
const reportLimitWindow = 24 * time.Hour

// ReportPostInput 举报入参。
type ReportPostInput struct {
	PostID     uuid.UUID
	ReasonType int16  // 见 domain.ReportReason*
	Reason     string // 可选补充说明，≤200 字符
}

// ReportAction 举报复核动作。
type ReportAction string

const (
	ReportDismiss ReportAction = "dismiss" // 举报不成立：保留帖子，关闭举报
	ReportRemove  ReportAction = "remove"  // 举报成立：下架帖子（status 3），通知作者
)

// ReportBriefVO 一条举报（不含举报人，举报人不对外暴露）。
type ReportBriefVO struct {
	ReasonType int16     `json:"reason_type"`
	Reason     string    `json:"reason"`
	CreateTime time.Time `json:"create_time"`
}

// ReportedPostItem 被举报帖子列表项：帖子 + AI 结论（同待审队列）+ 举报信息。
type ReportedPostItem struct {
	ReviewQueueItem
	ReportCount     int             `json:"report_count"`      // 本轮待处理举报数
	FirstReportTime time.Time       `json:"first_report_time"` // 本轮首次举报时间（列表排序键）
	Reports         []ReportBriefVO `json:"reports"`           // 最近 10 条待处理举报（新→旧）
}

// ReportedPostsResult 被举报帖子列表（keyset 游标分页，首次举报时间倒序）。
type ReportedPostsResult struct {
	Posts  []ReportedPostItem `json:"posts"`
	Size   int                `json:"size"`
	Cursor string             `json:"cursor"` // 下一页游标，无更多为 ""
}

// ResolveReportInput 举报复核入参。
type ResolveReportInput struct {
	CircleID uuid.UUID
	PostID   uuid.UUID
	Action   ReportAction
	Reason   string // 可选备注，只进审核日志
}

// ResolveReportResult 举报复核结果。
type ResolveReportResult struct {
	PostID        uuid.UUID `json:"post_id"`
	Status        int16     `json:"status"`         // 1 已发布（保留）/ 3 已下架
	ClosedReports int       `json:"closed_reports"` // 本次关闭的举报数
}

func (s *postServiceImpl) SetReportRepository(r domain.PostReportRepository) { s.reportRepo = r }

// ReportPost 举报帖子（实现 PostService）。
//
// 顺序：校验原因 → 读帖子（不存在 / 未发布 / 私密圈不可见 404）→ 不能举报自己 → 24 小时上限 → 写入（重复 409）→ 达到阈值通知圈管。
func (s *postServiceImpl) ReportPost(ctx context.Context, reporterID uuid.UUID, in ReportPostInput) error {
	if s.reportRepo == nil {
		return errors.New("post report repository is not configured")
	}
	if !domain.ValidReportReason(in.ReasonType) {
		return errInvalidReportReason
	}
	post, err := s.repo.GetByID(ctx, in.PostID)
	if errors.Is(err, domain.ErrPostNotFound) {
		return errReportPostNotFound
	}
	if err != nil {
		return err
	}
	if post.Status != domain.PostStatusPublished {
		return errReportPostNotFound // 审核中 / 驳回 / 屏蔽 / 草稿都不能举报，也不泄露其存在
	}
	visible, err := s.canViewCircle(ctx, reporterID, post.CircleID)
	if err != nil {
		return err
	}
	if !visible {
		return errReportPostNotFound // 私密圈非成员同样按不存在处理
	}
	if post.UserID == reporterID {
		return errCannotReportOwnPost
	}

	cfg := s.moderationCfg.WithDefaults()
	n, err := s.reportRepo.CountByReporterSince(ctx, reporterID, time.Now().Add(-reportLimitWindow))
	if err != nil {
		return err
	}
	if n >= int64(cfg.ReportDailyLimit) {
		return errReportLimitExceeded
	}

	report := &domain.PostReport{
		PostID:     post.ID,
		CircleID:   post.CircleID,
		ReporterID: reporterID,
		ReasonType: in.ReasonType,
		Reason:     normalizeReviewReason(in.Reason),
	}
	out, err := s.reportRepo.Create(ctx, report, domain.ReportThresholds{
		Notify: cfg.ReportNotifyThreshold, Hide: cfg.ReportHideThreshold,
	})
	if errors.Is(err, domain.ErrPostReportDuplicate) {
		return errAlreadyReported
	}
	if err != nil {
		return err
	}

	// 举报已落库：通知不跟随请求取消
	ctx = context.WithoutCancel(ctx)
	if out.Hidden {
		logger.Log.Info(fmt.Sprintf("Post %s auto-hidden after %d pending reports", post.ID, out.Pending))
	}
	if out.Notify && s.publisher != nil {
		if err := s.publisher.PublishModeratorNotice(ctx, domain.ModeratorNoticeReported, uuid.Nil, post.ID); err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to publish report notice for post %s: %s", post.ID, err.Error()))
		}
	}
	return nil
}

// ListReportedPosts 圈内被举报帖子列表（实现 PostService）。先鉴权后查询。
func (s *postServiceImpl) ListReportedPosts(ctx context.Context, operatorID, circleID uuid.UUID, size int, cursor string) (*ReportedPostsResult, error) {
	if circleID == uuid.Nil {
		return nil, errCircleIDRequired
	}
	if err := s.requireCircleModerator(ctx, circleID, operatorID); err != nil {
		return nil, err
	}
	if s.reportRepo == nil {
		return nil, errors.New("post report repository is not configured")
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	after, err := decodeReviewCursor(cursor)
	if err != nil {
		return nil, err
	}

	rows, err := s.reportRepo.ListReportedByCircle(ctx, circleID, after, size+1)
	if err != nil {
		return nil, err
	}
	next := ""
	if len(rows) > size {
		rows = rows[:size]
		last := rows[size-1]
		next = encodeReviewCursor(last.FirstReportTime, last.ID)
	}

	posts := make([]domain.Post, 0, len(rows))
	postIDs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		posts = append(posts, r.Post)
		postIDs = append(postIDs, r.ID)
	}
	recent := map[uuid.UUID][]domain.PostReport{}
	if len(postIDs) > 0 {
		if recent, err = s.reportRepo.ListPendingByPostIDs(ctx, postIDs, reportRecentPerPost); err != nil {
			return nil, err
		}
	}

	base := s.buildReviewItems(ctx, posts)
	items := make([]ReportedPostItem, 0, len(rows))
	for i, r := range rows {
		reports := make([]ReportBriefVO, 0, len(recent[r.ID]))
		for _, rp := range recent[r.ID] {
			reports = append(reports, ReportBriefVO{ReasonType: rp.ReasonType, Reason: rp.Reason, CreateTime: rp.CreateTime})
		}
		items = append(items, ReportedPostItem{
			ReviewQueueItem: base[i],
			ReportCount:     r.ReportCount,
			FirstReportTime: r.FirstReportTime,
			Reports:         reports,
		})
	}
	return &ReportedPostsResult{Posts: items, Size: size, Cursor: next}, nil
}

// ResolveReport 举报复核（实现 PostService）。
//
// 顺序：校验动作 → 鉴权（403）→ 读帖子，不存在 / 不在该圈 404 → 仓储事务（关闭举报 + 改状态 + 审计行；
// 无待处理举报 / 并发输掉 / 帖子已不可处理 404）→ 下架时通知作者。
func (s *postServiceImpl) ResolveReport(ctx context.Context, operatorID uuid.UUID, in ResolveReportInput) (*ResolveReportResult, error) {
	var remove bool
	switch in.Action {
	case ReportDismiss:
	case ReportRemove:
		remove = true
	default:
		return nil, errInvalidReportAction
	}
	if in.CircleID == uuid.Nil {
		return nil, errCircleIDRequired
	}
	if err := s.requireCircleModerator(ctx, in.CircleID, operatorID); err != nil {
		return nil, err
	}
	if s.reportRepo == nil {
		return nil, errors.New("post report repository is not configured")
	}

	post, err := s.repo.GetByID(ctx, in.PostID)
	if errors.Is(err, domain.ErrPostNotFound) {
		return nil, errReportedPostNotFound
	}
	if err != nil {
		return nil, err
	}
	if post.CircleID != in.CircleID {
		return nil, errReportedPostNotFound
	}

	decision, status := domain.ModerationReportDismiss, int16(domain.PostStatusPublished)
	if remove {
		decision, status = domain.ModerationReportRemove, domain.PostStatusRejected
	}
	operator := operatorID
	entry := &domain.PostModerationLog{
		PostID:     post.ID,
		Decision:   decision,
		Hazards:    domain.HazardScores{},
		Source:     domain.ModerationSourceManual,
		OperatorID: &operator,
	}
	if reason := normalizeReviewReason(in.Reason); reason != "" {
		entry.Reason = &reason
	}

	closed, ok, err := s.reportRepo.Resolve(ctx, post.ID, in.CircleID, remove, entry)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errReportedPostNotFound
	}

	if remove && s.publisher != nil {
		if err := s.publisher.PublishPostRemovedNotice(context.WithoutCancel(ctx), operatorID, post.ID); err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to publish post removed notice for post %s: %s", post.ID, err.Error()))
		}
	}
	return &ResolveReportResult{PostID: post.ID, Status: status, ClosedReports: closed}, nil
}

// ModerationSummary 圈内待处理数量（审核页角标，#156）。
type ModerationSummary struct {
	ReviewPending int64 `json:"review_pending"` // 待审帖子数（同 GET /circle/moderation/posts）
	ReportPending int64 `json:"report_pending"` // 被举报帖子数（同 GET /circle/moderation/reports）
}

// GetModerationSummary 圈内待处理数量（实现 PostService）。先鉴权后查询，鉴权同待审 / 被举报列表。
func (s *postServiceImpl) GetModerationSummary(ctx context.Context, operatorID, circleID uuid.UUID) (*ModerationSummary, error) {
	if circleID == uuid.Nil {
		return nil, errCircleIDRequired
	}
	if err := s.requireCircleModerator(ctx, circleID, operatorID); err != nil {
		return nil, err
	}
	if s.reportRepo == nil {
		return nil, errors.New("post report repository is not configured")
	}
	review, err := s.repo.CountReviewingByCircle(ctx, circleID)
	if err != nil {
		return nil, err
	}
	report, err := s.reportRepo.CountReportedByCircle(ctx, circleID)
	if err != nil {
		return nil, err
	}
	return &ModerationSummary{ReviewPending: review, ReportPending: report}, nil
}
