package redpanda

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/trending/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/elasticsearch"
	redispkg "interestBar/pkg/server/storage/redis"
)

// TrendingRankSyncer 热点榜单定时同步器。
//
// 每 N 分钟对 3 维度(post/circle/user) × 2 窗口(24h/7d) = 6 榜单并发跑 ES 聚合，
// ZADD 覆盖重写 Redis ZSET + 记刷新时间戳。读路径直接读 ZSET。
//
// 与 CircleHotSyncer 同形：{mu, ticker, stopChan, stopped} + 优雅关停 + 排干。
// 区别：CircleHotSyncer 处理的是 circle:hot:* 增量 Δ 累加器（GETDEL 读后清零落库）；
// 本 syncer 处理的是 trending:* 全量重算快照（DEL+ZADD 覆盖）。
type TrendingRankSyncer struct {
	mu       sync.Mutex
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

var trendingSyncer *TrendingRankSyncer

// StartTrendingSyncer 启动热点榜单定时同步器。
func StartTrendingSyncer() error {
	interval := conf.Config.Trending.FlushIntervalMinutes
	if interval <= 0 {
		interval = 5
	}
	s := &TrendingRankSyncer{
		ticker:   time.NewTicker(time.Duration(interval) * time.Minute),
		stopChan: make(chan struct{}),
	}
	trendingSyncer = s
	go s.run()
	logger.Log.Info(fmt.Sprintf("Trending rank syncer started (interval=%d min)", interval))
	return nil
}

func (s *TrendingRankSyncer) run() {
	for {
		select {
		case <-s.ticker.C:
			s.flush()
		case <-s.stopChan:
			s.ticker.Stop()
			s.flush() // 关停时排干剩余榜单
			return
		}
	}
}

// flush 并发刷新全部启用窗口的 6 个榜单（3 维度 × N 窗口）。
func (s *TrendingRankSyncer) flush() {
	windows := conf.Config.Trending.Windows
	if len(windows) == 0 {
		windows = []string{domain.Window24h, domain.Window7d}
	}
	dimensions := []string{domain.DimensionPost, domain.DimensionCircle, domain.DimensionUser}

	type job struct{ dim, window string }
	jobs := make([]job, 0, len(dimensions)*len(windows))
	for _, w := range windows {
		for _, d := range dimensions {
			jobs = append(jobs, job{dim: d, window: w})
		}
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			if err := s.syncOne(j.dim, j.window); err != nil {
				logger.Log.Error(fmt.Sprintf("trending sync (%s/%s) failed: %s", j.dim, j.window, err.Error()))
			}
		}(j)
	}
	wg.Wait()
}

// syncOne 跑一次聚合 + 覆盖写。ES 失败本轮跳过，保留上轮 ZSET（best-effort 降级）。
//
// 兜底（docs/trending-fallback-design.md §六）：窗口聚合为空时，追加一次无窗口聚合（全局热门），
// 用其结果填充同一个 ZSET。仅空才兜底——窗口有数据时不受影响，ZSET 永远是「纯窗口」或「纯兜底」之一。
func (s *TrendingRankSyncer) syncOne(dimension, window string) error {
	ctx := context.Background()
	topN := conf.Config.Trending.TopN
	if topN <= 0 {
		topN = 100
	}

	// ① 窗口聚合
	agg, err := elasticsearch.AggregateTrending(ctx, dimension, window, topN)
	if err != nil {
		return err
	}
	items := toScoredIDs(agg.Items)

	// ② 窗口为空 → 兜底：无窗口聚合（全局热门）
	if len(items) == 0 {
		fbAgg, fbErr := elasticsearch.AggregateTrending(ctx, dimension, "", topN)
		if fbErr != nil {
			// 兜底聚合失败：跳过本轮（保留上轮榜单），与窗口聚合错误的降级一致。
			return fmt.Errorf("trending fallback (%s/%s) failed: %w", dimension, window, fbErr)
		}
		items = toScoredIDs(fbAgg.Items)
		logger.Log.Info(fmt.Sprintf("trending %s/%s empty, fallback to global hot (%d items)", dimension, window, len(items)))
	}

	// ③ 即使最终仍为空也写榜单：清空 ZSET + 更新 refreshed_at。
	// 前端据此区分「跑过但确实无数据」（refreshed_at 有值、数组空）
	// 与「从未跑过 / ES 故障降级」（refreshed_at=0）。
	return rewriteTrendingBoard(ctx, dimension, window, items)
}

// toScoredIDs 把 ES 聚合的 ScoredItem 列表转为 domain.ScoredID（解析 uuid，跳过非法）。
func toScoredIDs(raw []elasticsearch.TrendingScoredItem) []domain.ScoredID {
	out := make([]domain.ScoredID, 0, len(raw))
	for _, it := range raw {
		id, parseErr := uuid.Parse(it.ID)
		if parseErr != nil {
			continue
		}
		out = append(out, domain.ScoredID{ID: id, Score: it.Score})
	}
	return out
}

// rewriteTrendingBoard 覆盖式重写 ZSET（DEL + ZADD + Set meta 原子）。
//
// 直接用 redispkg 全局客户端 + GetTrendingKey，与 CircleHotSyncer 用 GetCircleHotKey 同款风格
// （redpanda 包不 import trending infra，避免反向依赖）。
func rewriteTrendingBoard(ctx context.Context, dimension, window string, items []domain.ScoredID) error {
	key := redispkg.GetTrendingKey(dimension, window)
	metaKey := redispkg.GetTrendingMetaKey(dimension, window)

	pipe := redispkg.Client.TxPipeline()
	pipe.Del(ctx, key)

	members := make([]redis.Z, 0, len(items))
	for _, it := range items {
		members = append(members, redis.Z{Score: it.Score, Member: it.ID.String()})
	}
	pipe.ZAdd(ctx, key, members...)
	pipe.Set(ctx, metaKey, time.Now().Unix(), 0)

	_, err := pipe.Exec(ctx)
	return err
}

// Stop 停止同步器（幂等）。
func (s *TrendingRankSyncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stopChan)
}

// StartTrendingSyncerWithRetry 启动热点榜单同步器（保持与其它 consumer 一致签名）。
func StartTrendingSyncerWithRetry() {
	if err := StartTrendingSyncer(); err != nil {
		logger.Log.Error("Failed to start trending rank syncer: " + err.Error())
	}
}

// StopTrendingSyncer 停止热点榜单同步器（关停时调用）。
func StopTrendingSyncer() {
	if trendingSyncer != nil {
		trendingSyncer.Stop()
	}
}
