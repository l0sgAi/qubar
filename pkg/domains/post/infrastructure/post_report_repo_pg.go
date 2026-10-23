package infrastructure

import (
	"context"
	"errors"
	"time"

	"interestBar/pkg/domains/post/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// postReportLockClass 举报 advisory lock 的命名空间（两参数形式 pg_advisory_xact_lock(class, key)，避免与其他锁撞号）。
const postReportLockClass = 153

// postReportRepoPG 基于 GORM 的 PostReportRepository 实现（#153）。
type postReportRepoPG struct {
	db *gorm.DB
}

// NewPostReportRepository 构造 PostReportRepository。
func NewPostReportRepository(db *gorm.DB) domain.PostReportRepository {
	return &postReportRepoPG{db: db}
}

// lockPost 事务级 advisory lock：同一帖子的举报写入与复核串行化，事务结束自动释放。
// 不锁 post 行，避免和点赞 / 评论等计数更新互相阻塞。
func lockPost(tx *gorm.DB, postID uuid.UUID) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(?, hashtext(?))", postReportLockClass, postID.String()).Error
}

// Create 事务内：加锁 → 插入（唯一冲突即重复举报）→ 统计待处理数 → 本轮未通知且达到阈值则把本条标为已通知
// → 达到自动屏蔽阈值则条件更新帖子 1 → 4。
//
// 加锁后计数，同一帖子并发举报不会都看不到对方而漏通知，也不会重复通知；
// 复核关闭待处理举报后 notified 随之失效，下一轮举报重新计数、重新通知。
func (r *postReportRepoPG) Create(ctx context.Context, report *domain.PostReport, t domain.ReportThresholds) (domain.ReportOutcome, error) {
	if report.ID == uuid.Nil {
		report.ID = sharedomain.NewID()
	}
	report.Status, report.Notified = domain.PostReportPending, 0

	var out domain.ReportOutcome
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPost(tx, report.PostID); err != nil {
			return err
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(report)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrPostReportDuplicate
		}

		var agg struct {
			Pending  int
			Notified bool
		}
		if err := tx.Raw(`SELECT count(*) AS pending, COALESCE(bool_or(notified = 1), false) AS notified
			FROM domains.post_report WHERE post_id = ? AND status = ?`, report.PostID, domain.PostReportPending).
			Scan(&agg).Error; err != nil {
			return err
		}
		out.Pending = agg.Pending

		if !agg.Notified && agg.Pending >= t.Notify {
			if err := tx.Model(&domain.PostReport{}).Where("id = ?", report.ID).
				UpdateColumn("notified", 1).Error; err != nil {
				return err
			}
			report.Notified = 1
			out.Notify = true
		}
		if t.Hide > 0 && agg.Pending >= t.Hide {
			res := tx.Model(&domain.Post{}).
				Where("id = ? AND status = ? AND deleted = ?", report.PostID, domain.PostStatusPublished, 0).
				UpdateColumns(map[string]any{"status": domain.PostStatusBlocked, "update_time": gorm.Expr("CURRENT_TIMESTAMP")})
			if res.Error != nil {
				return res.Error
			}
			out.Hidden = res.RowsAffected == 1
		}
		return nil
	})
	if err != nil {
		return domain.ReportOutcome{}, err
	}
	return out, nil
}

// CountByReporterSince 走 idx_post_report_reporter (reporter_id, create_time DESC)。
func (r *postReportRepoPG) CountByReporterSince(ctx context.Context, reporterID uuid.UUID, since time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.PostReport{}).
		Where("reporter_id = ? AND create_time >= ?", reporterID, since).
		Count(&n).Error
	return n, err
}

// ListReportedByCircle 先在 idx_post_report_circle_pending 上按帖子聚合待处理举报，再关联帖子过滤已删除 / 非可见状态。
// 排序键用首次举报时间（同一轮内不变），翻页稳定。
func (r *postReportRepoPG) ListReportedByCircle(ctx context.Context, circleID uuid.UUID, after *domain.PostKeyset, limit int) ([]domain.ReportedPost, error) {
	sql := `SELECT p.*, r.report_count, r.first_report_time
		FROM (SELECT post_id, count(*) AS report_count, min(create_time) AS first_report_time
			FROM domains.post_report WHERE circle_id = ? AND status = ? GROUP BY post_id) r
		JOIN domains.post p ON p.id = r.post_id AND p.deleted = 0 AND p.status IN (?, ?)`
	args := []any{circleID, domain.PostReportPending, domain.PostStatusPublished, domain.PostStatusBlocked}
	if after != nil {
		sql += ` WHERE (r.first_report_time, r.post_id) < (?, ?)`
		args = append(args, after.CreateTime, after.ID)
	}
	sql += ` ORDER BY r.first_report_time DESC, r.post_id DESC LIMIT ?`
	args = append(args, limit)

	var rows []domain.ReportedPost
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}

// ListPendingByPostIDs 每帖最近 perPost 条待处理举报，走 uk_post_report_post_reporter 的 post_id 前缀。
func (r *postReportRepoPG) ListPendingByPostIDs(ctx context.Context, postIDs []uuid.UUID, perPost int) (map[uuid.UUID][]domain.PostReport, error) {
	out := make(map[uuid.UUID][]domain.PostReport, len(postIDs))
	if len(postIDs) == 0 {
		return out, nil
	}
	var rows []domain.PostReport
	err := r.db.WithContext(ctx).Raw(`SELECT id, post_id, circle_id, reporter_id, reason_type, reason, status, notified,
			handler_id, handle_time, create_time, update_time
		FROM (SELECT *, row_number() OVER (PARTITION BY post_id ORDER BY create_time DESC, id DESC) AS rn
			FROM domains.post_report WHERE post_id IN ? AND status = ?) t
		WHERE rn <= ? ORDER BY post_id, create_time DESC, id DESC`, postIDs, domain.PostReportPending, perPost).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.PostID] = append(out[row.PostID], row)
	}
	return out, nil
}

// Resolve 事务内：加锁（与举报写入串行）→ 关闭待处理举报（0 行说明已被处理 / 无举报）→ 条件更新帖子状态
// （remove：1/4 → 3；dismiss：4 → 1，要求帖子仍是已发布 / 已屏蔽）→ 写人工审核日志。
// 任一条件不满足整体回滚并返回 ok=false。
func (r *postReportRepoPG) Resolve(ctx context.Context, postID, circleID uuid.UUID, remove bool, log *domain.PostModerationLog) (int, bool, error) {
	closed, ok := 0, false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPost(tx, postID); err != nil {
			return err
		}
		reportStatus := domain.PostReportDismissed
		if remove {
			reportStatus = domain.PostReportRemoved
		}
		res := tx.Model(&domain.PostReport{}).
			Where("post_id = ? AND circle_id = ? AND status = ?", postID, circleID, domain.PostReportPending).
			UpdateColumns(map[string]any{
				"status": reportStatus, "handler_id": log.OperatorID,
				"handle_time": gorm.Expr("CURRENT_TIMESTAMP"), "update_time": gorm.Expr("CURRENT_TIMESTAMP"),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		closed = int(res.RowsAffected)

		// remove：已发布 / 已屏蔽 → 驳回；dismiss：已屏蔽恢复为已发布（已发布保持不变，仍需命中一行）
		target := int16(domain.PostStatusPublished)
		if remove {
			target = domain.PostStatusRejected
		}
		postRes := tx.Model(&domain.Post{}).
			Where("id = ? AND circle_id = ? AND deleted = ? AND status IN ?", postID, circleID, 0,
				[]int16{domain.PostStatusPublished, domain.PostStatusBlocked}).
			UpdateColumns(map[string]any{"status": target, "update_time": gorm.Expr("CURRENT_TIMESTAMP")})
		if postRes.Error != nil {
			return postRes.Error
		}
		if postRes.RowsAffected != 1 {
			return errResolveConflict // 帖子已被删除 / 驳回 / 转入审核：回滚，举报保持待处理
		}

		if log.ID == uuid.Nil {
			log.ID = sharedomain.NewID()
		}
		if err := tx.Create(log).Error; err != nil {
			return err
		}
		ok = true
		return nil
	})
	if errors.Is(err, errResolveConflict) {
		return 0, false, nil
	}
	if err != nil || !ok {
		return 0, false, err
	}
	return closed, true, nil
}

// errResolveConflict 事务内哨兵：触发回滚但不当作错误返回。
var errResolveConflict = errors.New("post report resolve conflict")
