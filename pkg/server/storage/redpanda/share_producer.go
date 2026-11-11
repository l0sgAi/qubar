package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// defaultShareEventTopic 分享事件 topic 兜底名（redpanda.share_event_topic 为空时使用，生产配置未更新也能发布）。
const defaultShareEventTopic = "post_share_events"

var shareEventWriter *kafka.Writer

// shareEventTopic 分享事件 topic（配置为空回落默认值）。
func shareEventTopic() string {
	if t := conf.Config.Redpanda.ShareEventTopic; t != "" {
		return t
	}
	return defaultShareEventTopic
}

// InitShareEventProducer 初始化分享事件 Producer（#232）。
// 同步 ack：投递失败时 share 域删除刚插入的分享记录并返回可重试错误，保证「首次分享」与事件一一对应。
func InitShareEventProducer() error {
	topic := shareEventTopic()
	shareEventWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  topic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  false,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}
	logger.Log.Info(fmt.Sprintf("Share event producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, topic))
	return nil
}

// PublishShareEvent 同步发布帖子首次分享事件（key = post_id）。Ts 为 0 时取当前时间。
func PublishShareEvent(msg ShareEventMessage) error {
	if shareEventWriter == nil {
		return fmt.Errorf("share event writer is not initialized")
	}
	msg.Type = ShareEventType
	if msg.Ts == 0 {
		msg.Ts = time.Now().UnixMilli()
	}
	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal share event message: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncPublishTimeout)
	defer cancel()
	if err := shareEventWriter.WriteMessages(ctx, kafka.Message{Key: []byte(msg.PostID.String()), Value: value}); err != nil {
		return fmt.Errorf("failed to write share event message: %w", err)
	}
	logger.Log.Debug(fmt.Sprintf("Published share event: share=%s, user=%s, post=%s",
		msg.ShareID.String(), msg.UserID.String(), msg.PostID.String()))
	return nil
}

// PublishPostShareCount 发布「帖子有新分享」标记到帖子统计 topic（消费者按 post_share 重算 share_count，不累加 value）。
func PublishPostShareCount(postID uuid.UUID) error {
	if postWriter == nil {
		return fmt.Errorf("post stats writer is not initialized")
	}
	return publishPostMessage(PostStatisticsMessage{
		Type:   StatisticsTypePostShare,
		PostID: postID,
		Value:  1,
	})
}

// CloseShareEventProducer 关闭分享事件 Producer。
func CloseShareEventProducer() error {
	if shareEventWriter != nil {
		if err := shareEventWriter.Close(); err != nil {
			logger.Log.Error("Failed to close share event writer: " + err.Error())
			return err
		}
		logger.Log.Info("Share event producer closed")
	}
	return nil
}
