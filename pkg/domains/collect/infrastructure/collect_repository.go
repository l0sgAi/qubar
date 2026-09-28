package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/domains/collect/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// postCollectRepoGORM 基于 GORM 的 PostCollectRepository 实现。
type postCollectRepoGORM struct {
	db *gorm.DB
}

// NewPostCollectRepository 构造 PostCollectRepository。
func NewPostCollectRepository(db *gorm.DB) domain.PostCollectRepository {
	return &postCollectRepoGORM{db: db}
}

func (r *postCollectRepoGORM) IsCollected(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.PostCollect{}).
		Where("user_id = ? AND post_id = ? AND deleted = ?", userID, postID, domain.PostCollectActive).
		Count(&count).Error
	return count > 0, err
}

// setCollectedSQL 收藏：新建 或 从 deleted=1 复活（已是有效收藏则 0 行受影响）。
const setCollectedSQL = `
INSERT INTO domains.post_collect AS pc (id, user_id, post_id, deleted)
VALUES (?, ?, ?, 0)
ON CONFLICT (user_id, post_id) DO UPDATE
	SET deleted = 0, update_time = CURRENT_TIMESTAMP
	WHERE pc.deleted = 1`

// SetCollected 同步 upsert 收藏流水行（供 Toggle 即时入库）。
//
// 单条语句完成、依赖 uk_post_collect_user_post，并发安全：
//   - active=true：新建（PK 调 sharedomain.NewID()）或复活已取消行；已有效则 no-op；
//   - active=false：仅当前为有效收藏时标记取消；无行或已取消则 no-op。
//
// changed 以受影响行数判定，是计数（collect_count）与热度事件是否发布的唯一依据。
func (r *postCollectRepoGORM) SetCollected(ctx context.Context, userID, postID uuid.UUID, active bool) (bool, error) {
	db := r.db.WithContext(ctx)
	if active {
		res := db.Exec(setCollectedSQL, sharedomain.NewID(), userID, postID)
		if res.Error != nil {
			return false, fmt.Errorf("failed to set post collected: %w", res.Error)
		}
		return res.RowsAffected > 0, nil
	}
	res := db.Model(&domain.PostCollect{}).
		Where("user_id = ? AND post_id = ? AND deleted = ?", userID, postID, domain.PostCollectActive).
		Update("deleted", domain.PostCollectCanceled)
	if res.Error != nil {
		return false, fmt.Errorf("failed to cancel post collect: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// collectCursor keyset 游标（base64 编码的 JSON， opaque to client）。
type collectCursor struct {
	CreateTime time.Time `json:"t"`
	ID         uuid.UUID `json:"i"`
}

// ListCollectedPostIDs 按收藏时间倒序 keyset 分页。
//
// 游标语义：(create_time, id) 复合比较，配合索引 idx_pcollect_user_active
// (user_id, create_time DESC, id DESC) WHERE deleted=0 实现无 OFFSET 深翻页。
// 取 size+1 条判断是否还有下一页；nextCursor 编码本页最后一条。
//
// keyword 非空时 JOIN domains.post 过滤 title/summary（ILIKE %kw%）+ 仅已发布未删帖，
// 游标与排序仍基于 post_collect 列（JOIN 后列名歧义，故全限定表名）。
func (r *postCollectRepoGORM) ListCollectedPostIDs(ctx context.Context, userID uuid.UUID, keyword string, size int, cursor string) ([]uuid.UUID, int64, string, error) {
	keyword = strings.TrimSpace(keyword)

	q := r.db.WithContext(ctx).Model(&domain.PostCollect{}).
		Where("domains.post_collect.user_id = ? AND domains.post_collect.deleted = ?", userID, domain.PostCollectActive)

	// 关键字过滤：JOIN domains.post + title/summary ILIKE + 仅已发布未删帖
	if keyword != "" {
		q = q.Joins("JOIN domains.post ON domains.post.id = domains.post_collect.post_id").
			Where("domains.post.deleted = ? AND domains.post.status = ?", 0, 1).
			Where("(domains.post.title ILIKE ? OR domains.post.summary ILIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}

	if cursor != "" {
		c, err := decodeCollectCursor(cursor)
		if err != nil {
			return nil, 0, "", domain.ErrInvalidCursor
		}
		q = q.Where("(domains.post_collect.create_time, domains.post_collect.id) < (?, ?)", c.CreateTime, c.ID)
	}

	// JOIN 后 SELECT * 会带入 domains.post 同名列(id/create_time 等)，显式取 post_collect 列避免歧义
	var rows []domain.PostCollect
	if err := q.Select("domains.post_collect.id, domains.post_collect.user_id, domains.post_collect.post_id, domains.post_collect.deleted, domains.post_collect.create_time, domains.post_collect.update_time").
		Order("domains.post_collect.create_time DESC, domains.post_collect.id DESC").
		Limit(size + 1).
		Find(&rows).Error; err != nil {
		return nil, 0, "", err
	}

	var total int64
	countQ := r.db.WithContext(ctx).Model(&domain.PostCollect{}).
		Where("domains.post_collect.user_id = ? AND domains.post_collect.deleted = ?", userID, domain.PostCollectActive)
	if keyword != "" {
		countQ = countQ.Joins("JOIN domains.post ON domains.post.id = domains.post_collect.post_id").
			Where("domains.post.deleted = ? AND domains.post.status = ?", 0, 1).
			Where("(domains.post.title ILIKE ? OR domains.post.summary ILIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}
	if err := countQ.Count(&total).Error; err != nil {
		return nil, 0, "", err
	}

	nextCursor := ""
	if len(rows) > size {
		rows = rows[:size]
		last := rows[len(rows)-1]
		nextCursor = encodeCollectCursor(last.CreateTime, last.ID)
	}

	postIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		postIDs = append(postIDs, row.PostID)
	}
	return postIDs, total, nextCursor, nil
}

func encodeCollectCursor(t time.Time, id uuid.UUID) string {
	b, _ := json.Marshal(collectCursor{CreateTime: t, ID: id})
	return base64.URLEncoding.EncodeToString(b)
}

func decodeCollectCursor(s string) (*collectCursor, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var c collectCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.ID == uuid.Nil {
		return nil, errInvalidCursor
	}
	return &c, nil
}

var errInvalidCursor = &invalidCursorError{}

type invalidCursorError struct{}

func (e *invalidCursorError) Error() string { return "invalid collect cursor" }
