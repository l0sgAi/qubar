package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/segmentio/kafka-go"
)

// 通用「落库后提交」批量消费者（l0sgAi/qubar#50）。
//
// 统计类消费者（圈子统计 / 帖子统计 / 收藏计数 / 浏览历史 / 帖子热度 / 互动灌数）共用本骨架，
// 语义与点赞消费者（like_consumer.go，#46）一致：
//   - FetchMessage 拉取，CommitInterval=0；flush 落库成功后才 CommitMessages（at-least-once）。
//   - 落库失败：本批聚合结果与 offset 合并回缓冲，下轮重试，不提交 → 不丢。
//   - 提交失败：只记日志，同分区后续更大 offset 的提交会覆盖它。
//   - 无法解析的毒消息：只推进 offset，随下次成功 flush 一并提交跳过。
//   - 关停排干：停止拉取 → 末次 flush + commit → 关闭 reader。
//
// 取舍：增量类 SQL（x = x + delta）非幂等。"落库成功、提交前进程被杀" 或 "再均衡" 时该批会被重投，
// 可能重复计数一次；相比旧实现（读即提交，flush 窗口内全部丢失）风险窗口从分钟级缩到毫秒级。

const batchCommitTimeout = 10 * time.Second

// batchConsumerSpec 描述一个批量消费者：消息类型 T，聚合缓冲类型 B（map 或指针，便于原地修改）。
type batchConsumerSpec[T any, B any] struct {
	name          string           // 日志用名称
	newBuf        func() B         // 新建空缓冲
	add           func(b B, m T)   // 合并一条消息（无效消息自行记日志并忽略）
	size          func(b B) int    // 待落库条目数（0 = 无需落库）
	merge         func(dst, src B) // 落库失败时把 src 合并回 dst（dst 可能已有新消息）
	persist       func(b B) error  // 单事务落库
	flushMessages int              // >0：累计消息数达到阈值即触发 flush
}

// batchConsumer 按 spec 聚合消息、定时/定量 flush、落库成功后提交 offset。
type batchConsumer[T any, B any] struct {
	spec      batchConsumerSpec[T, B]
	committer messageCommitter

	mu      sync.Mutex
	buf     B
	offsets map[int]kafka.Message // 每分区待提交的最大 offset 消息
	count   int                   // 自上次 flush 累计消息数（计数触发用）
	stopped bool

	ticker   *time.Ticker
	flushNow chan struct{} // 计数阈值触发的即时 flush 信号（缓冲 1，不阻塞读循环）
	stopChan chan struct{}
	done     chan struct{}      // run() 退出（末次 flush + commit 完成）后关闭
	cancel   context.CancelFunc // 停止 reader 的 FetchMessage
	readDone chan struct{}      // 读循环退出后关闭；nil 表示无读循环（测试）
}

func newBatchConsumer[T any, B any](spec batchConsumerSpec[T, B], committer messageCommitter, interval time.Duration) *batchConsumer[T, B] {
	return &batchConsumer[T, B]{
		spec:      spec,
		committer: committer,
		buf:       spec.newBuf(),
		offsets:   make(map[int]kafka.Message),
		ticker:    time.NewTicker(interval),
		flushNow:  make(chan struct{}, 1),
		stopChan:  make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// startBatchConsumer 创建 reader 并启动聚合 + 读循环，注册到全局关停列表。
func startBatchConsumer[T any, B any](spec batchConsumerSpec[T, B], topic, group string, interval time.Duration) {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing %s consumer with brokers: %v", spec.name, brokers))

	// CommitInterval=0：CommitMessages 同步提交，仅在 flush 落库成功后调用。
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        group,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: 0,
		Dialer: &kafka.Dialer{
			Timeout:   10 * time.Second,
			DualStack: true,
			Resolver:  nil, // 禁用 resolver 缓存，避免使用 advertised 地址
		},
	})

	c := newBatchConsumer(spec, r, interval)
	readerCtx, readerCancel := context.WithCancel(context.Background())
	c.cancel = readerCancel
	c.readDone = make(chan struct{})
	registerBatchConsumer(c.Stop)

	// reader 在末次 flush + commit 之后才关闭（关闭后无法再提交）。
	go func() {
		c.run()
		if err := r.Close(); err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to close %s reader: %s", spec.name, err.Error()))
		}
	}()

	go func() {
		defer close(c.readDone)
		backoff := &readBackoff{}
		for {
			km, err := r.FetchMessage(readerCtx)
			if err != nil {
				if !waitAfterReadError(readerCtx, backoff, spec.name, err) {
					return // 关停：cancel 已触发
				}
				continue
			}
			backoff.Reset()

			var msg T
			if err := json.Unmarshal(km.Value, &msg); err != nil {
				logger.Log.Error(fmt.Sprintf("Failed to unmarshal %s message: %s", spec.name, err.Error()))
				c.addMessage(nil, km) // 毒消息：仅推进 offset
				continue
			}
			c.addMessage(&msg, km)
		}
	}()

	logger.Log.Info(fmt.Sprintf("%s consumer created: topic=%s, group=%s, flush=%v", spec.name, topic, group, interval))
}

// addMessage 合并一条消息并记录 offset。msg 为 nil 时仅推进 offset。
func (c *batchConsumer[T, B]) addMessage(msg *T, km kafka.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return // 未提交，重启后重投
	}
	if msg != nil {
		c.spec.add(c.buf, *msg)
	}
	c.trackOffset(km)
	c.count++

	if t := c.spec.flushMessages; t > 0 && c.count >= t {
		select {
		case c.flushNow <- struct{}{}:
		default: // 已有待处理 flush 信号
		}
	}
}

func (c *batchConsumer[T, B]) trackOffset(km kafka.Message) {
	if prev, ok := c.offsets[km.Partition]; !ok || km.Offset > prev.Offset {
		c.offsets[km.Partition] = km
	}
}

func (c *batchConsumer[T, B]) run() {
	defer close(c.done)
	for {
		select {
		case <-c.ticker.C:
			c.flush()
		case <-c.flushNow:
			c.flush()
		case <-c.stopChan:
			c.ticker.Stop()
			c.flush()
			return
		}
	}
}

// flush 落库 → 提交 offset。
func (c *batchConsumer[T, B]) flush() {
	c.mu.Lock()
	if c.spec.size(c.buf) == 0 && len(c.offsets) == 0 {
		c.count = 0
		c.mu.Unlock()
		return
	}
	buf, offsets := c.buf, c.offsets
	c.buf, c.offsets, c.count = c.spec.newBuf(), make(map[int]kafka.Message), 0
	c.mu.Unlock()

	if n := c.spec.size(buf); n > 0 {
		if err := c.spec.persist(buf); err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to persist %d %s updates, will retry: %s", n, c.spec.name, err.Error()))
			c.mu.Lock()
			c.spec.merge(c.buf, buf)
			for _, km := range offsets {
				c.trackOffset(km)
			}
			c.mu.Unlock()
			return
		}
	}

	if len(offsets) == 0 {
		return
	}
	msgs := make([]kafka.Message, 0, len(offsets))
	for _, km := range offsets {
		msgs = append(msgs, km)
	}
	ctx, cancel := context.WithTimeout(context.Background(), batchCommitTimeout)
	defer cancel()
	if err := c.committer.CommitMessages(ctx, msgs...); err != nil {
		logger.Log.Warn(fmt.Sprintf("Failed to commit %s offsets (next commit supersedes): %s", c.spec.name, err.Error()))
	}
}

// Stop 优雅排干（幂等）：停止拉取 → 末次 flush + commit → 返回。
func (c *batchConsumer[T, B]) Stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.readDone != nil {
		<-c.readDone // 读循环退出后不会再有 addMessage
	}
	close(c.stopChan)
	<-c.done
}

// ===== 全局关停 =====

var (
	batchConsumersMu sync.Mutex
	batchConsumers   []func()
)

func registerBatchConsumer(stop func()) {
	batchConsumersMu.Lock()
	batchConsumers = append(batchConsumers, stop)
	batchConsumersMu.Unlock()
}

// StopBatchConsumersGlobal 并行排干所有已启动的批量消费者（幂等）。
// 在 server 关停序列中调用，须早于 CloseRedis（帖子热度 fan-out 依赖 Redis）。
func StopBatchConsumersGlobal() {
	batchConsumersMu.Lock()
	stops := batchConsumers
	batchConsumers = nil
	batchConsumersMu.Unlock()

	var wg sync.WaitGroup
	for _, stop := range stops {
		wg.Add(1)
		go func(stop func()) {
			defer wg.Done()
			stop()
		}(stop)
	}
	wg.Wait()
}

// flushIntervalOr 把配置值换算为时长，<=0 时用兜底值。
func flushIntervalOr(v int, fallback int, unit time.Duration) time.Duration {
	if v <= 0 {
		v = fallback
	}
	return time.Duration(v) * unit
}
