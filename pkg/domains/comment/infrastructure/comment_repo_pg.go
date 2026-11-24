// Package infrastructure 提供 comment 领域基础设施层实现。
package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/comment/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// commentRepoPG 基于 GORM 的 CommentRepository 实现。
type commentRepoPG struct {
	db *gorm.DB
}

// NewCommentRepository 构造 CommentRepository。
func NewCommentRepository(db *gorm.DB) domain.CommentRepository {
	return &commentRepoPG{db: db}
}

// Create 创建评论（事务内：插入评论 + 如为回复则递增根评论 reply_count）。
//
// 与旧 model.CreateComment 行为一致：
//   - 帖子评论计数由 Redis + Redpanda 异步处理，不在事务内更新数据库。
func (r *commentRepoPG) Create(ctx context.Context, comment *domain.Comment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 插入评论
		if comment.ID == uuid.Nil {
			comment.ID = sharedomain.NewID()
		}
		if err := tx.Create(comment).Error; err != nil {
			return err
		}

		// 2. 如果是回复（root_id 非空），增加根评论的回复计数
		if comment.RootID != nil {
			if err := tx.Model(&domain.Comment{}).Where("id = ?", *comment.RootID).
				UpdateColumn("reply_count", gorm.Expr("reply_count + ?", 1)).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// GetByID 根据 ID 获取评论（未删除，不看 status：写路径用；读路径在 application 层再判 Visible）。
func (r *commentRepoPG) GetByID(ctx context.Context, commentID uuid.UUID) (*domain.Comment, error) {
	var comment domain.Comment
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", commentID, 0).First(&comment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCommentNotFound
		}
		return nil, err
	}
	return &comment, nil
}

// visibleScope 评论可见性谓词（domain.VisibleCondition：未删除且状态正常，#208）。
func visibleScope(q *gorm.DB) *gorm.DB {
	return q.Where(domain.VisibleCondition)
}

// listScope 一个评论列表的范围：过滤条件 + 游标作用域 ID（顶层列表 = 帖子 ID，楼内回复 = 根评论 ID）。
// 两者绑在一起，列表与定位（rank 计数 / 取页起点 / 游标编码）共用同一个值，口径逐字节一致。
type listScope struct {
	id    uuid.UUID
	apply func(*gorm.DB) *gorm.DB
}

// rootListScope 帖子顶层评论列表的范围。
func rootListScope(postID uuid.UUID) listScope {
	return listScope{id: postID, apply: func(q *gorm.DB) *gorm.DB {
		return visibleScope(q.Where("post_id = ? AND root_id IS NULL", postID))
	}}
}

// replyListScope 楼内回复列表的范围。
func replyListScope(rootID uuid.UUID) listScope {
	return listScope{id: rootID, apply: func(q *gorm.DB) *gorm.DB {
		return visibleScope(q.Where("root_id = ?", rootID))
	}}
}

// GetRootCommentsByCursor 游标分页获取帖子的顶层评论（仅可见评论）。
// sort: 0=按点赞倒序, 1=按时间倒序。
func (r *commentRepoPG) GetRootCommentsByCursor(ctx context.Context, postID uuid.UUID, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	return r.listByCursor(ctx, rootListScope(postID), size, sort, cursor)
}

// GetRepliesByCursor 游标分页获取某条评论的子回复（仅可见评论）。
// sort: 0=按点赞倒序, 1=按时间倒序（与顶层列表同一套排序键映射）。
func (r *commentRepoPG) GetRepliesByCursor(ctx context.Context, rootID uuid.UUID, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	return r.listByCursor(ctx, replyListScope(rootID), size, sort, cursor)
}

// listByCursor 顶层列表与楼内回复共用的 keyset 分页：游标条件 → 排序 → 多查一条判断 has_more。
// 游标须属于 scope 且与 sort 一致（见 parseListCursor），否则返回 ErrInvalidCursor 包装错误。
func (r *commentRepoPG) listByCursor(ctx context.Context, scope listScope, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	query, err := applyCursorCondition(scope.apply(r.db.WithContext(ctx).Model(&domain.Comment{})), cursor, scope.id, sort)
	if err != nil {
		return nil, "", false, err
	}

	// 多查一条判断 has_more
	var comments []domain.Comment
	if err := applyOrderBy(query, sort).Limit(size + 1).Find(&comments).Error; err != nil {
		return nil, "", false, err
	}
	hasMore := len(comments) > size
	if hasMore {
		comments = comments[:size]
	}

	// 构建下一页游标
	var nextCursor string
	if hasMore && len(comments) > 0 {
		nextCursor = encodeListCursor(scope.id, sort, &comments[len(comments)-1])
	}
	return comments, nextCursor, hasMore, nil
}

// LocateRootCursor 计算顶层列表的定位游标（设计见 docs/comment-locate-design.md）。
func (r *commentRepoPG) LocateRootCursor(ctx context.Context, postID uuid.UUID, sort int, target *domain.Comment, size int) (string, error) {
	cursor, _, err := r.locateCursor(ctx, rootListScope(postID), sort, target, size)
	return cursor, err
}

// LocateReplyCursor 计算回复列表的定位游标与页码（设计见 docs/comment-locate-design.md）。
func (r *commentRepoPG) LocateReplyCursor(ctx context.Context, rootID uuid.UUID, sort int, target *domain.Comment, size int) (string, int, error) {
	return r.locateCursor(ctx, replyListScope(rootID), sort, target, size)
}

// locateCursor 定位算法公共部分：rank 计数 → 页码 → 页起始游标（上一页末条 keyset）。
//
// 返回 (cursor, page, err)：page 从 1 开始；page=1 时 cursor 为 ""（首页无需游标）。
// 游标编码复用 encodeListCursor（同一 scope.id / sort），与列表接口的 next_cursor 逐字节一致。
func (r *commentRepoPG) locateCursor(ctx context.Context, scope listScope, sort int, target *domain.Comment, size int) (string, int, error) {
	if size <= 0 {
		return "", 0, fmt.Errorf("invalid page size %d", size)
	}

	// 1. rank 查询：严格排在 target 之前的条数（索引前缀等值 + 范围扫描）
	var before int64
	if err := applyRankBeforeCondition(
		scope.apply(r.db.WithContext(ctx).Model(&domain.Comment{})), sort, target,
	).Count(&before).Error; err != nil {
		return "", 0, err
	}

	page, k := locatePage(before, size)
	if page == 1 {
		return "", 1, nil
	}

	// 2. 取上一页末条（排序后第 k 条，1-based）的 keyset 作为页起始游标
	var items []domain.Comment
	if err := applyOrderBy(scope.apply(r.db.WithContext(ctx).Model(&domain.Comment{})), sort).
		Limit(1).Offset(int(k - 1)).Find(&items).Error; err != nil {
		return "", 0, err
	}
	if len(items) == 0 {
		// 竞态：COUNT 与 OFFSET 之间目标页之前的评论被删除，位次失效。
		// 按定位失败处理（上层映射 404），前端 toast 降级。
		return "", 0, domain.ErrCommentNotFound
	}
	return encodeListCursor(scope.id, sort, &items[0]), int(page), nil
}

// locatePage 由「严格排在目标之前的条数」before 与页大小 size 计算：
//
//	page: 目标所在页码（从 1）
//	k:    页起始游标对应条目的位次（= 上一页末条，1-based）；page=1 时 k=0 表示无需游标
//
// off-by-one 边界（size=20）：before=0..19 → page=1,k=0；before=20 → page=2,k=20；
// before=39 → page=2,k=20；before=40 → page=3,k=40。
func locatePage(before int64, size int) (page int64, k int64) {
	page = before/int64(size) + 1
	if page > 1 {
		k = (page - 1) * int64(size)
	}
	return page, k
}

// applyRankBeforeCondition 添加「严格排在 target 之前」的 WHERE 条件，
// 排序键与 applyCursorCondition / applyOrderBy 严格对应：
// sort=0 → (like_count, id) 双键；sort=1 → id 单键（UUIDv7 字典序=时间序）。
func applyRankBeforeCondition(query *gorm.DB, sort int, target *domain.Comment) *gorm.DB {
	switch sort {
	case sortByLike: // 点赞倒序：排在前面 = 赞更多，或同赞时 id 更大（更新）
		return query.Where(
			"(like_count > ?) OR (like_count = ? AND id > ?)",
			target.LikeCount, target.LikeCount, target.ID,
		)
	default: // 时间倒序：排在前面 = id 更大（更新）
		return query.Where("id > ?", target.ID)
	}
}

// IsLiked 检查用户是否点赞了评论（DB 回源用）。
func (r *commentRepoPG) IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.CommentLike{}).
		Where("user_id = ? AND comment_id = ? AND deleted = ?", userID, commentID, domain.CommentLikeActive).
		Count(&count).Error
	return count > 0, err
}

// BatchCheckLiked 批量检查用户是否点赞了多条评论（DB 回源用）。
func (r *commentRepoPG) BatchCheckLiked(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(commentIDs) == 0 {
		return make(map[uuid.UUID]bool), nil
	}
	var likes []domain.CommentLike
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND comment_id IN ? AND deleted = ?", userID, commentIDs, domain.CommentLikeActive).
		Find(&likes).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]bool, len(likes))
	for _, like := range likes {
		result[like.CommentID] = true
	}
	return result, nil
}

// ListActiveLikes 列出用户的有效评论点赞（走 idx_clike_user_active 部分索引，新→旧，最多 limit 条）。
func (r *commentRepoPG) ListActiveLikes(ctx context.Context, userID uuid.UUID, limit int) ([]domain.CommentLike, error) {
	var likes []domain.CommentLike
	err := r.db.WithContext(ctx).
		Select("comment_id, update_time").
		Where("user_id = ? AND deleted = ?", userID, domain.CommentLikeActive).
		Order("create_time DESC").
		Limit(limit).
		Find(&likes).Error
	return likes, err
}

// CreateMentions 批量写入评论提及名单（append-only，幂等：重复行忽略）。
//
// CommentMention 未内嵌 BaseModel、无 BeforeCreate 钩子，须在此显式预生成 UUIDv7，
// 否则 GORM 会把 uuid.Nil 当有效值发送覆盖 DB 默认值。
func (r *commentRepoPG) CreateMentions(ctx context.Context, commentID uuid.UUID, userIDs []uuid.UUID) error {
	if len(userIDs) == 0 {
		return nil
	}
	mentions := make([]domain.CommentMention, 0, len(userIDs))
	for _, uid := range userIDs {
		mentions = append(mentions, domain.CommentMention{
			ID:        sharedomain.NewID(),
			CommentID: commentID,
			UserID:    uid,
		})
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&mentions).Error
}

// GetMentionUserIDsByCommentIDs 批量获取评论提及用户ID。
//
// 按 id ASC 排序：UUIDv7 字典序 == 时间序，即提及写入顺序（≈正文出现顺序）。
func (r *commentRepoPG) GetMentionUserIDsByCommentIDs(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	result := make(map[uuid.UUID][]uuid.UUID, len(commentIDs))
	if len(commentIDs) == 0 {
		return result, nil
	}
	var mentions []domain.CommentMention
	err := r.db.WithContext(ctx).
		Where("comment_id IN ?", commentIDs).
		Order("id ASC").
		Find(&mentions).Error
	if err != nil {
		return nil, err
	}
	for _, m := range mentions {
		result[m.CommentID] = append(result[m.CommentID], m.UserID)
	}
	return result, nil
}

// applyCursorCondition 根据游标和排序方式添加 keyset WHERE 条件（空游标 = 首页，不加条件）。
// 游标的解码与校验（v1 / v2、sort 与 scopeID 一致性）见 parseListCursor。
func applyCursorCondition(query *gorm.DB, cursor string, scopeID uuid.UUID, sort int) (*gorm.DB, error) {
	if cursor == "" {
		return query, nil
	}

	likeCount, id, err := parseListCursor(cursor, scopeID, sort)
	if err != nil {
		return nil, err
	}

	switch sort {
	case sortByLike: // 按点赞倒序：keyset (like_count, id)
		query = query.Where(
			"(like_count < ?) OR (like_count = ? AND id < ?)",
			likeCount, likeCount, id,
		)
	case sortByTime: // 按时间倒序：id DESC
		query = query.Where("id < ?", id)
	}

	return query, nil
}

// applyOrderBy 根据排序方式添加 ORDER BY。
func applyOrderBy(query *gorm.DB, sort int) *gorm.DB {
	switch sort {
	case sortByLike: // 按点赞倒序
		return query.Order("like_count DESC, id DESC")
	case sortByTime: // 按时间倒序
		return query.Order("id DESC")
	}
	return query.Order("id DESC")
}

// 自动回复候选查询的跨表裸值（不 import 兄弟域包）：users.role=2 机器人系统账号
// （aiagent 创建，防自动回复回环）、post.status=1 已发布（post.PostStatusPublished）。
const (
	botUserRole         = 2
	postStatusPublished = 1
)

// ListHotForAutoReply 自动回复评论候选。
//
// 评论表没有 create_time 索引：主键是 UUIDv7（时间序），用 id 下界把时间窗口转成主键范围扫描，
// create_time 条件保证语义精确。JOIN domains.post 取圈子并校验帖子可回复（同 collect 仓储先例）。
func (r *commentRepoPG) ListHotForAutoReply(ctx context.Context, f domain.AutoReplyCommentFilter) ([]domain.AutoReplyCommentRow, error) {
	if f.Limit <= 0 || (f.LikeMin <= 0 && f.ReplyMin <= 0) {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Table("domains.comment AS c").
		Select("c.id, c.post_id, c.user_id, c.root_id, c.content, c.like_count, c.reply_count, p.circle_id AS post_circle_id").
		Joins("JOIN domains.post AS p ON p.id = c.post_id").
		Where("c.id >= ? AND c.create_time >= ?", sharedomain.UUIDv7LowerBound(f.Since), f.Since).
		Where("c.deleted = ? AND c.status = ?", 0, domain.CommentStatusNormal).
		Where("p.deleted = ? AND p.status = ? AND p.is_lock = ?", 0, postStatusPublished, 0).
		Where("NOT EXISTS (SELECT 1 FROM domains.users u WHERE u.id = c.user_id AND u.role = ?)", botUserRole)
	switch {
	case f.LikeMin > 0 && f.ReplyMin > 0:
		q = q.Where("(c.like_count >= ? OR c.reply_count >= ?)", f.LikeMin, f.ReplyMin)
	case f.LikeMin > 0:
		q = q.Where("c.like_count >= ?", f.LikeMin)
	default:
		q = q.Where("c.reply_count >= ?", f.ReplyMin)
	}
	if f.CircleID != uuid.Nil {
		q = q.Where("p.circle_id = ?", f.CircleID)
	}
	if len(f.ExcludeIDs) > 0 {
		q = q.Where("c.id NOT IN ?", f.ExcludeIDs)
	}
	var rows []domain.AutoReplyCommentRow
	err := q.Order("c.like_count + c.reply_count DESC, c.id DESC").Limit(f.Limit).Scan(&rows).Error
	return rows, err
}
