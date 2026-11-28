package redpanda

import (
	"encoding/json"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// mirrorPostStats 在调用方已经按 ID 锁住并更新 post 后，按相同顺序复制四个计数。
// 复制绝对值可处理 M1 之后的新帖和旧版本写入的增量，重放同一状态不会产生新行版本。
func mirrorPostStats(tx *gorm.DB, ids []uuid.UUID) error {
	payload, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO domains.post_stats AS s
		(post_id, view_count, comment_count, like_count, collect_count, update_time)
		SELECT id, view_count, comment_count, like_count, collect_count, update_time
		FROM domains.post WHERE id IN (SELECT jsonb_array_elements_text(?::jsonb)::uuid) AND deleted = 0 ORDER BY id
		ON CONFLICT (post_id) DO UPDATE SET
			(view_count, comment_count, like_count, collect_count, update_time) =
			(EXCLUDED.view_count, EXCLUDED.comment_count, EXCLUDED.like_count, EXCLUDED.collect_count, EXCLUDED.update_time)
		WHERE (s.view_count, s.comment_count, s.like_count, s.collect_count) IS DISTINCT FROM
			(EXCLUDED.view_count, EXCLUDED.comment_count, EXCLUDED.like_count, EXCLUDED.collect_count)`, string(payload)).Error
}
