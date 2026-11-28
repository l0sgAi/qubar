package redpanda

import (
	"encoding/json"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// lockPostStats 按 post_id 升序创建或锁住有效帖的计数行。ON CONFLICT 的 WHERE false
// 只锁住已存在的行；零值新帖在第一次消费事件时建行。等待结束后再开新语句读取明细快照。
func lockPostStats(tx *gorm.DB, ids []uuid.UUID) error {
	payload, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO domains.post_stats (post_id)
		SELECT id FROM domains.post WHERE deleted = 0 AND id IN
			(SELECT jsonb_array_elements_text(?::jsonb)::uuid)
		ORDER BY id
		ON CONFLICT (post_id) DO UPDATE SET view_count = EXCLUDED.view_count WHERE false`, string(payload)).Error
}
