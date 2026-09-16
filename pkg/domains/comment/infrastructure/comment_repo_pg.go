// Package infrastructure 提供 comment 领域基础设施层实现。
package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// GetByID 根据 ID 获取评论（未删除）。
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

// GetRootCommentsByCursor 游标分页获取帖子的顶层评论。
// sort: 0=按点赞倒序, 1=按时间倒序。
func (r *commentRepoPG) GetRootCommentsByCursor(ctx context.Context, postID uuid.UUID, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	query := r.db.WithContext(ctx).Model(&domain.Comment{}).Where("post_id = ? AND root_id IS NULL AND deleted = 0", postID)

	// 应用游标条件
	var err error
	query, err = applyCursorCondition(query, cursor, sort)
	if err != nil {
		return nil, "", false, err
	}

	// 排序
	query = applyOrderBy(query, sort)

	// 多查一条判断 has_more
	var comments []domain.Comment
	if err := query.Limit(size + 1).Find(&comments).Error; err != nil {
		return nil, "", false, err
	}

	hasMore := len(comments) > size
	if hasMore {
		comments = comments[:size]
	}

	// 构建下一页游标
	var nextCursor string
	if hasMore && len(comments) > 0 {
		nextCursor = buildNextCursor(&comments[len(comments)-1], sort)
	}

	return comments, nextCursor, hasMore, nil
}

// GetRepliesByCursor 游标分页获取某条评论的子回复。
// sort: 0=按点赞倒序, 1=按时间倒序（与顶层列表同一套排序键映射）。
func (r *commentRepoPG) GetRepliesByCursor(ctx context.Context, rootID uuid.UUID, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	query := r.db.WithContext(ctx).Model(&domain.Comment{}).Where("root_id = ? AND deleted = 0", rootID)

	// 应用游标条件
	var err error
	query, err = applyCursorCondition(query, cursor, sort)
	if err != nil {
		return nil, "", false, err
	}

	// 排序
	query = applyOrderBy(query, sort)

	// 多查一条判断 has_more
	var comments []domain.Comment
	if err := query.Limit(size + 1).Find(&comments).Error; err != nil {
		return nil, "", false, err
	}

	hasMore := len(comments) > size
	if hasMore {
		comments = comments[:size]
	}

	// 构建下一页游标
	var nextCursor string
	if hasMore && len(comments) > 0 {
		nextCursor = buildNextCursor(&comments[len(comments)-1], sort)
	}

	return comments, nextCursor, hasMore, nil
}

// LocateRootCursor 计算顶层列表的定位游标（设计见 docs/comment-locate-design.md）。
func (r *commentRepoPG) LocateRootCursor(ctx context.Context, postID uuid.UUID, sort int, target *domain.Comment, size int) (string, error) {
	scope := func(q *gorm.DB) *gorm.DB {
		return q.Where("post_id = ? AND root_id IS NULL AND deleted = 0", postID)
	}
	cursor, _, err := r.locateCursor(ctx, scope, sort, target, size)
	return cursor, err
}

// LocateReplyCursor 计算回复列表的定位游标与页码（设计见 docs/comment-locate-design.md）。
func (r *commentRepoPG) LocateReplyCursor(ctx context.Context, rootID uuid.UUID, sort int, target *domain.Comment, size int) (string, int, error) {
	scope := func(q *gorm.DB) *gorm.DB {
		return q.Where("root_id = ? AND deleted = 0", rootID)
	}
	return r.locateCursor(ctx, scope, sort, target, size)
}

// locateCursor 定位算法公共部分：rank 计数 → 页码 → 页起始游标（上一页末条 keyset）。
//
// 返回 (cursor, page, err)：page 从 1 开始；page=1 时 cursor 为 ""（首页无需游标）。
// 游标编码复用 buildNextCursor，与列表接口游标格式逐字节一致。
func (r *commentRepoPG) locateCursor(ctx context.Context, scope func(*gorm.DB) *gorm.DB, sort int, target *domain.Comment, size int) (string, int, error) {
	if size <= 0 {
		return "", 0, fmt.Errorf("invalid page size %d", size)
	}

	// 1. rank 查询：严格排在 target 之前的条数（索引前缀等值 + 范围扫描）
	var before int64
	if err := applyRankBeforeCondition(
		scope(r.db.WithContext(ctx).Model(&domain.Comment{})), sort, target,
	).Count(&before).Error; err != nil {
		return "", 0, err
	}

	page, k := locatePage(before, size)
	if page == 1 {
		return "", 1, nil
	}

	// 2. 取上一页末条（排序后第 k 条，1-based）的 keyset 作为页起始游标
	var items []domain.Comment
	if err := applyOrderBy(scope(r.db.WithContext(ctx).Model(&domain.Comment{})), sort).
		Limit(1).Offset(int(k - 1)).Find(&items).Error; err != nil {
		return "", 0, err
	}
	if len(items) == 0 {
		// 竞态：COUNT 与 OFFSET 之间目标页之前的评论被删除，位次失效。
		// 按定位失败处理（上层映射 404），前端 toast 降级。
		return "", 0, domain.ErrCommentNotFound
	}
	return buildNextCursor(&items[0], sort), int(page), nil
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
	case 0: // 点赞倒序：排在前面 = 赞更多，或同赞时 id 更大（更新）
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

// ===== 游标工具函数（与旧 model/comment.go 中的实现一致）=====

// encodeCursor 将 map 编码为 base64 游标字符串。
func encodeCursor(values map[string]interface{}) string {
	data, _ := json.Marshal(values)
	return base64.StdEncoding.EncodeToString(data)
}

// decodeCursor 将 base64 游标字符串解码为 map。
func decodeCursor(cursor string) (map[string]interface{}, error) {
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}
	var values map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// buildNextCursor 根据评论和排序方式构建下一页游标。
// 注意：id 使用 UUIDv7 字符串编码(字典序 == 时间序,与 ORDER BY id DESC 配合)。
func buildNextCursor(comment *domain.Comment, sort int) string {
	switch sort {
	case 0: // 按点赞
		return encodeCursor(map[string]interface{}{
			"like_count": float64(comment.LikeCount),
			"id":         comment.ID.String(),
		})
	case 1: // 按时间
		return encodeCursor(map[string]interface{}{
			"id": comment.ID.String(),
		})
	}
	return ""
}

// applyCursorCondition 根据游标和排序方式添加 WHERE 条件。
//
// 游标来自用户可控的 query 参数，必须防御性解析：所有类型断言用 comma-ok，
// 任何字段缺失/类型错误都返回包装了 ErrInvalidCursor 的错误（而非 panic）。
func applyCursorCondition(query *gorm.DB, cursor string, sort int) (*gorm.DB, error) {
	if cursor == "" {
		return query, nil
	}

	likeCount, id, err := parseCursorValues(cursor, sort)
	if err != nil {
		return nil, err
	}

	switch sort {
	case 0: // 按点赞倒序：keyset (like_count, id)
		query = query.Where(
			"(like_count < ?) OR (like_count = ? AND id < ?)",
			likeCount, likeCount, id,
		)
	case 1: // 按时间倒序：id DESC
		query = query.Where("id < ?", id)
	}

	return query, nil
}

// parseCursorValues 解码并校验游标，返回 (likeCount, id, err)。
//
// 抽成纯函数便于单测（无需 gorm）。sort==0 需要 like_count + id，
// sort==1 只需要 id。所有类型断言用 comma-ok，绝不 panic。
// 错误统一用 fmt.Errorf("%w: ...", domain.ErrInvalidCursor, ...) 包装。
func parseCursorValues(cursor string, sort int) (likeCount int64, id uuid.UUID, err error) {
	values, derr := decodeCursor(cursor)
	if derr != nil {
		// base64 / JSON 解析失败统一归为非法游标
		return 0, uuid.Nil, fmt.Errorf("%w: decode failed: %v", domain.ErrInvalidCursor, derr)
	}

	idStr, ok := values["id"].(string)
	if !ok {
		return 0, uuid.Nil, fmt.Errorf("%w: missing or invalid id", domain.ErrInvalidCursor)
	}
	id, perr := uuid.Parse(idStr)
	if perr != nil {
		return 0, uuid.Nil, fmt.Errorf("%w: invalid id: %v", domain.ErrInvalidCursor, perr)
	}

	if sort == 0 {
		likeCountRaw, ok := values["like_count"]
		if !ok {
			return 0, uuid.Nil, fmt.Errorf("%w: missing like_count", domain.ErrInvalidCursor)
		}
		// JSON unmarshal 数字到 map[string]interface{} 会得到 float64
		likeCountF, ok := likeCountRaw.(float64)
		if !ok {
			return 0, uuid.Nil, fmt.Errorf("%w: like_count has wrong type %T", domain.ErrInvalidCursor, likeCountRaw)
		}
		likeCount = int64(likeCountF)
	}

	return likeCount, id, nil
}

// applyOrderBy 根据排序方式添加 ORDER BY。
func applyOrderBy(query *gorm.DB, sort int) *gorm.DB {
	switch sort {
	case 0: // 按点赞倒序
		return query.Order("like_count DESC, id DESC")
	case 1: // 按时间倒序
		return query.Order("id DESC")
	}
	return query.Order("id DESC")
}
