package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// 帖子 AI 审核事件（#117）。
//
// CreatePost（moderation 启用时）落库为审核中后同步发一条事件；审核 worker 逐条消费，
// 回调 post 应用层处理（调 Jev → 分流 → 条件更新状态 → 发布副作用），处理成功后才提交 offset（at-least-once）。
// 重复投递由应用层幂等保证（WHERE status = 2 条件更新 + 审核日志存在性检查）。

// 未配置 topic / group 时的兜底名称（生产配置不在仓库里，漏配不应让审核链路起不来）。
const (
	defaultPostModerationTopic = "post_moderation"
	defaultPostModerationGroup = "post_moderation_consumer_group"
)

const (
	postModerationWriteTimeout  = 5 * time.Second
	postModerationCommitTimeout = 10 * time.Second
	postModerationMaxWait       = time.Second // 单条事件也要尽快送达，不等凑批
)

// PostModerationMessage 帖子审核事件消息（只带 post_id，worker 回源读库）。
type PostModerationMessage struct {
	PostID uuid.UUID `json:"post_id"`
	Ts     int64     `json:"ts"` // 事件时间 Unix 毫秒
}

// PostModerationHandler 处理一条审核事件。返回错误表示需要重试（不提交 offset，退避后重试同一条）；
// 帖子不存在 / 已处理等「无需处理」的情况应返回 nil。
type PostModerationHandler func(ctx context.Context, postID uuid.UUID) error

func postModerationTopic() string {
	if t := conf.Config.Redpanda.PostModerationTopic; t != "" {
		return t
	}
	return defaultPostModerationTopic
}

func postModerationGroup() string {
	if g := conf.Config.Redpanda.PostModerationConsumerGroup; g != "" {
		return g
	}
	return defaultPostModerationGroup
}

// ==================== 生产者 ====================

var postModerationWriter *kafka.Writer

// InitPostModerationProducer 初始化帖子审核事件 Producer。
//
// 与统计类 writer 不同，这里同步写入（Async=false）：发帖接口要知道事件是否真的发出，
// 发不出去时按 fail_open 兜底，避免帖子永远卡在审核中。
func InitPostModerationProducer() error {
	postModerationWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  postModerationTopic(),
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.Hash{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Async:                  false,
		MaxAttempts:            3,
		ReadTimeout:            postModerationWriteTimeout,
		WriteTimeout:           postModerationWriteTimeout,
	}
	logger.Log.Info(fmt.Sprintf("Post moderation producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, postModerationTopic()))
	return nil
}

// PublishPostModerationEvent 同步发布一条帖子审核事件（key = post_id）。
// 不跟随请求取消（客户端断开不应让已落库的帖子丢事件），单独限时。
func PublishPostModerationEvent(ctx context.Context, postID uuid.UUID) error {
	if postModerationWriter == nil {
		return fmt.Errorf("post moderation writer is not initialized")
	}
	value, err := json.Marshal(PostModerationMessage{PostID: postID, Ts: time.Now().UnixMilli()})
	if err != nil {
		return fmt.Errorf("failed to marshal post moderation message: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postModerationWriteTimeout)
	defer cancel()
	if err := postModerationWriter.WriteMessages(ctx, kafka.Message{Key: []byte(postID.String()), Value: value}); err != nil {
		return fmt.Errorf("failed to write post moderation message: %w", err)
	}
	return nil
}

// ClosePostModerationProducer 关闭帖子审核事件 Producer（未初始化时为空操作）。
func ClosePostModerationProducer() error {
	if postModerationWriter == nil {
		return nil
	}
	if err := postModerationWriter.Close(); err != nil {
		logger.Log.Error("Failed to close post moderation writer: " + err.Error())
		return err
	}
	logger.Log.Info("Post moderation producer closed")
	return nil
}

// ==================== 消费者（审核 worker） ====================

// moderationReader 审核消费者用到的 reader 能力（便于测试替换）。
type moderationReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// postModerationConsumer 逐条处理审核事件：处理成功（或毒消息）才提交 offset。
// 同一时刻只处理一条，Jev 单次约 1s，量级足够；需要更高吞吐时再加分区 + 实例。
type postModerationConsumer struct {
	reader moderationReader
	handle PostModerationHandler
	cancel context.CancelFunc
	done   chan struct{} // 读循环退出后关闭
}

var (
	postModerationMu       sync.Mutex
	postModerationRunning  *postModerationConsumer
	postModerationRetryMax = 30 * time.Second
)

// StartPostModerationConsumer 启动帖子审核 worker（重复调用时先停掉旧实例）。
func StartPostModerationConsumer(handle PostModerationHandler) {
	topic, group := postModerationTopic(), postModerationGroup()
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        conf.Config.Redpanda.Brokers,
		Topic:          topic,
		GroupID:        group,
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        postModerationMaxWait,
		CommitInterval: 0, // 同步提交，仅在处理成功后调用
		Dialer:         &kafka.Dialer{Timeout: 10 * time.Second, DualStack: true},
	})
	c := &postModerationConsumer{reader: r, handle: handle, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	postModerationMu.Lock()
	prev := postModerationRunning
	postModerationRunning = c
	postModerationMu.Unlock()
	if prev != nil {
		prev.stop()
	}

	go c.run(ctx)
	logger.Log.Info(fmt.Sprintf("Post moderation consumer created: topic=%s, group=%s", topic, group))
}

// StopPostModerationConsumerGlobal 停止审核 worker（幂等）：取消拉取与进行中的处理，
// 未处理完的事件不提交，重启后重投。须早于 CloseRedis（发布副作用写 Redis）。
func StopPostModerationConsumerGlobal() {
	postModerationMu.Lock()
	c := postModerationRunning
	postModerationRunning = nil
	postModerationMu.Unlock()
	if c != nil {
		c.stop()
	}
}

func (c *postModerationConsumer) stop() {
	c.cancel()
	<-c.done
	if err := c.reader.Close(); err != nil {
		logger.Log.Error("Failed to close post moderation reader: " + err.Error())
	}
}

func (c *postModerationConsumer) run(ctx context.Context) {
	defer close(c.done)
	backoff := &readBackoff{}
	for {
		km, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if !waitAfterReadError(ctx, backoff, "post moderation", err) {
				return
			}
			continue
		}
		backoff.Reset()

		var msg PostModerationMessage
		if err := json.Unmarshal(km.Value, &msg); err != nil || msg.PostID == uuid.Nil {
			logger.Log.Error(fmt.Sprintf("Skip invalid post moderation message at offset %d: %v", km.Offset, err))
			c.commit(km) // 毒消息：跳过
			continue
		}
		if !c.handleWithRetry(ctx, msg.PostID) {
			return // 关停：不提交，重启后重投
		}
		c.commit(km)
	}
}

// handleWithRetry 处理一条事件，失败按有界指数退避重试同一条，直到成功或关停（返回 false）。
func (c *postModerationConsumer) handleWithRetry(ctx context.Context, postID uuid.UUID) bool {
	b := &readBackoff{}
	for {
		err := c.safeHandle(ctx, postID)
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		d := min(b.Next(), postModerationRetryMax)
		logger.Log.Warn(fmt.Sprintf("Failed to moderate post %s, retrying in %v: %s", postID, d, err.Error()))
		if !sleepOrDone(ctx, d) {
			return false
		}
	}
}

// safeHandle 调用处理函数；panic 视为不可重试的坏数据，记日志后跳过（返回 nil），避免卡死整个 topic。
func (c *postModerationConsumer) safeHandle(ctx context.Context, postID uuid.UUID) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error(fmt.Sprintf("Panic while moderating post %s, skipped: %v", postID, r))
			err = nil
		}
	}()
	return c.handle(ctx, postID)
}

func (c *postModerationConsumer) commit(km kafka.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), postModerationCommitTimeout)
	defer cancel()
	if err := c.reader.CommitMessages(ctx, km); err != nil {
		logger.Log.Warn("Failed to commit post moderation offset (next commit supersedes): " + err.Error())
	}
}
