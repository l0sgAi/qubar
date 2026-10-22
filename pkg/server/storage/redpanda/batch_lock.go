package redpanda

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 批量更新的统一取锁顺序（l0sgAi/qubar#124）。
//
// 帖子统计 / 热度 / 收藏 / 点赞消费者都用 jsonb_to_recordset 批量 UPDATE domains.post，
// 圈子统计消费者与圈子热度同步器批量 UPDATE domains.circle。UPDATE ... FROM 的取锁顺序由执行计划
// （嵌套循环按 JSON 行序、哈希连接按扫描序）决定，两个事务以不同顺序锁同一批行就会死锁（40P01），
// 生产压测中帖子统计与热度 flush 同时触发时 4 次失败 3 次。
//
// 统一做法：同一事务内先 lockRowsByID（SELECT ... ORDER BY id FOR NO KEY UPDATE，LockRows 在排序之上，
// 按主键升序逐行加锁），再执行 UPDATE（锁已持有，不再等待）。所有批量写入方按同一全序取锁，彼此只会排队、不会成环。
// 单行写入（如评论创建同步 IncrCommentCount）只锁一行，与之也不会成环。
// Go 侧行切片同样按 id 排序（sortByID），JSON 与日志确定、便于排查。

// compareID 与 PostgreSQL uuid 排序一致（按 16 字节逐字节比较）。
func compareID(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }

// sortByID 按 id 升序原地排序行切片。
func sortByID[T any](rows []T, id func(T) uuid.UUID) {
	slices.SortFunc(rows, func(a, b T) int { return compareID(id(a), id(b)) })
}

// sortedIDs 返回 ids 去重并升序排列后的切片。
func sortedIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, compareID)
	return slices.Compact(out)
}

// lockRowsByID 在 tx 内按 id 升序锁定 table 中的 ids 行。table 仅限内部常量。
//
// 用 FOR NO KEY UPDATE：与不改主键的 UPDATE 自身取的行锁同级，后续 UPDATE 不需要升级锁。
// 不过滤 deleted：已删行也一并按序加锁，避免与并发软删的取锁顺序不一致。
func lockRowsByID(tx *gorm.DB, table string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	payload, err := json.Marshal(sortedIDs(ids))
	if err != nil {
		return fmt.Errorf("failed to marshal %s lock ids: %w", table, err)
	}
	sql := fmt.Sprintf(`
		SELECT t.id FROM %s t
		WHERE t.id IN (SELECT jsonb_array_elements_text(?::jsonb)::uuid)
		ORDER BY t.id
		FOR NO KEY UPDATE`, table)
	var locked []uuid.UUID
	if err := tx.Raw(sql, string(payload)).Scan(&locked).Error; err != nil {
		return fmt.Errorf("failed to lock %s rows: %w", table, err)
	}
	return nil
}
