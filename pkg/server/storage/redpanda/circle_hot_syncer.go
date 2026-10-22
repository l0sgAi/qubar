package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CircleHotSyncer 圈子热度定时同步器。
//
// post_hot 消费者把帖子 hot Δ fan-out 到 circle:hot:{circleID} 累加器（string int）。
// 本同步器每 N 分钟 SCAN 全部 circle:hot:*，GETDEL 读后清零：
//   - 批量 UPDATE domains.circle.hot（GREATEST 防负）
//   - 若 circle:stats:{circleID} 已存在则同步 HINCRBY hot，保持读路径热值与 DB 收敛
//     （冷圈子 stats hash 不存在时不刷，避免写出只有 hot 字段的半截 Hash）
type CircleHotSyncer struct {
	mu       sync.Mutex
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

var circleHotSyncer *CircleHotSyncer

// StartCircleHotSyncer 启动圈子热度定时同步器。
func StartCircleHotSyncer() error {
	interval := conf.Config.Redpanda.CircleHotFlushInterval
	if interval <= 0 {
		interval = 34
	}
	s := &CircleHotSyncer{
		ticker:   time.NewTicker(time.Duration(interval) * time.Minute),
		stopChan: make(chan struct{}),
	}
	circleHotSyncer = s
	go s.run()
	logger.Log.Info(fmt.Sprintf("Circle hot syncer started (interval=%d min)", interval))
	return nil
}

func (s *CircleHotSyncer) run() {
	for {
		select {
		case <-s.ticker.C:
			s.flush()
		case <-s.stopChan:
			s.ticker.Stop()
			s.flush() // 关停时排干剩余 Δ
			return
		}
	}
}

// flush 扫描所有 circle:hot:* 累加器，读后清零，批量落库 + 刷 stats 缓存。
func (s *CircleHotSyncer) flush() {
	deltas, err := s.collectDeltas()
	if err != nil {
		logger.Log.Error("Failed to collect circle hot deltas: " + err.Error())
		return
	}
	if len(deltas) == 0 {
		return
	}
	logger.Log.Info(fmt.Sprintf("Circle hot syncer flushing %d circles", len(deltas)))

	if err := s.batchUpdateCircleHot(deltas); err != nil {
		logger.Log.Error("Failed to batch update circle hot: " + err.Error())
		return
	}

	// 刷 stats 缓存 hot 字段（仅当 stats hash 已存在，与 DB 收敛）
	ctx := context.Background()
	for circleID, delta := range deltas {
		if delta == 0 {
			continue
		}
		refreshCircleHotCache(ctx, circleID, delta)
	}
}

// refreshCircleHotCache 仅当 circle stats hash 已有 member_count（正常 populate 过）才 HINCRBY hot，
// 避免给冷圈子写出只有 hot 字段的半截 Hash。
func refreshCircleHotCache(ctx context.Context, circleID uuid.UUID, delta int64) {
	statsKey := redispkg.GetCircleStatsKey(circleID)
	populated, err := redispkg.Client.HExists(ctx, statsKey, "member_count").Result()
	if err != nil || !populated {
		return
	}
	if delta > 0 {
		_ = redispkg.IncrementCircleHot(circleID, delta)
	} else {
		_ = redispkg.DecrementCircleHot(circleID, -delta)
	}
}

// collectDeltas SCAN circle:hot:* 并 GETDEL 读后清零。
//
// GETDEL 原子读取并删除：读到本周期累积 Δ；期间并发新增量进入下一周期，不丢不重。
func (s *CircleHotSyncer) collectDeltas() (map[uuid.UUID]int64, error) {
	deltas := make(map[uuid.UUID]int64)
	ctx := context.Background()
	pattern := redispkg.CircleHotPrefix + "*"
	var cursor uint64
	for {
		keys, c, err := redispkg.Client.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			return nil, fmt.Errorf("scan circle hot keys: %w", err)
		}
		for _, key := range keys {
			val, err := redispkg.Client.GetDel(ctx, key).Result()
			if err != nil {
				continue // key 已过期/被并发删除 → 跳过
			}
			circleID, perr := uuid.Parse(strings.TrimPrefix(key, redispkg.CircleHotPrefix))
			if perr != nil {
				continue
			}
			delta, perr := strconv.ParseInt(val, 10, 64)
			if perr != nil || delta == 0 {
				continue
			}
			deltas[circleID] += delta
		}
		cursor = c
		if cursor == 0 {
			break
		}
	}
	return deltas, nil
}

// batchUpdateCircleHot 批量更新 domains.circle.hot（按 id 升序取锁，见 batch_lock.go）。
func (s *CircleHotSyncer) batchUpdateCircleHot(deltas map[uuid.UUID]int64) error {
	type updateRow struct {
		CircleID uuid.UUID `json:"circle_id"`
		Delta    int64     `json:"delta"`
	}
	rows := make([]updateRow, 0, len(deltas))
	ids := make([]uuid.UUID, 0, len(deltas))
	for circleID, delta := range deltas {
		if delta != 0 {
			rows = append(rows, updateRow{CircleID: circleID, Delta: delta})
			ids = append(ids, circleID)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	sortByID(rows, func(r updateRow) uuid.UUID { return r.CircleID })

	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockRowsByID(tx, "domains.circle", ids); err != nil {
			return err
		}
		sql := `
		UPDATE domains.circle c
		SET hot = GREATEST(c.hot + v.delta, 0),
		    update_time = CURRENT_TIMESTAMP
		FROM (
		    SELECT * FROM jsonb_to_recordset(?::jsonb)
		    AS v(circle_id uuid, delta BIGINT)
		) v
		WHERE c.id = v.circle_id AND c.deleted = 0
		`
		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal circle hot rows: %w", err)
		}
		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to execute circle hot batch update: %w", err)
		}
		logger.Log.Info(fmt.Sprintf("Successfully updated %d circle hot", len(rows)))
		return nil
	})
}

// Stop 停止同步器。
func (s *CircleHotSyncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stopChan)
}

// StartCircleHotSyncerWithRetry 启动圈子热度同步器（保持与其他 consumer 一致签名）。
func StartCircleHotSyncerWithRetry() {
	if err := StartCircleHotSyncer(); err != nil {
		logger.Log.Error("Failed to start circle hot syncer: " + err.Error())
	}
}

// StopCircleHotSyncer 停止圈子热度同步器（关停时调用）。
func StopCircleHotSyncer() {
	if circleHotSyncer != nil {
		circleHotSyncer.Stop()
	}
}
