package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

var (
	dialer                  *kafka.Dialer
	writer                  *kafka.Writer
	postWriter              *kafka.Writer
	likeEventWriter         *kafka.Writer
	collectEventWriter      *kafka.Writer
	historyEventWriter      *kafka.Writer
	postHotWriter           *kafka.Writer
	postInteractionWriter   *kafka.Writer
	notificationEventWriter *kafka.Writer
)

// InitRedpandaProducer 初始化Redpanda Producer
func InitRedpandaProducer() error {
	// 创建自定义Dialer，设置更长的超时时间
	dialer = &kafka.Dialer{
		Timeout:   10 * time.Second, // 连接超时
		DualStack: true,             // 启用IPv4/IPv6双栈
	}

	writer = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.Topic,
		AllowAutoTopicCreation: true,                // 自动创建topic
		Balancer:               &kafka.LeastBytes{}, // 使用LeastBytes均衡器
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne, // 只需要leader确认
		Compression:            kafka.Snappy,     // 使用Snappy压缩
		Async:                  true,             // 异步发送提升性能
		MaxAttempts:            5,                // 增加重试次数
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Redpanda producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.Topic))

	// 测试连接 - 尝试连接到Redpanda
	conn, err := dialer.DialContext(context.Background(), "tcp", conf.Config.Redpanda.Brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to redpanda: %w", err)
	}
	defer conn.Close()

	// 设置连接超时
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("failed to set deadline: %w", err)
	}

	// 检查连接是否正常
	_, err = conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get redpanda controller: %w", err)
	}

	logger.Log.Info("Redpanda connection test successful")
	return nil
}

// PublishCircleMemberCount 发布圈子成员计数变化消息
func PublishCircleMemberCount(circleID uuid.UUID, value int64) error {
	if writer == nil {
		return fmt.Errorf("redpanda writer is not initialized")
	}

	message := CircleStatisticsMessage{
		Type:     StatisticsTypeCircleCount,
		CircleID: circleID,
		Value:    value,
	}

	return publishMessage(message)
}

// PublishCirclePostCount 发布圈子帖子计数变化消息
func PublishCirclePostCount(circleID uuid.UUID) error {
	if writer == nil {
		return fmt.Errorf("redpanda writer is not initialized")
	}

	message := CircleStatisticsMessage{
		Type:     StatisticsTypePostCount,
		CircleID: circleID,
		Value:    1,
	}

	return publishMessage(message)
}

// publishMessage 发送消息到Redpanda
func publishMessage(message CircleStatisticsMessage) error {
	value, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	kafkaMsg := kafka.Message{
		Key:   []byte(message.CircleID.String()), // 使用circle_id作为key，保证同一圈子的消息有序
		Value: value,
	}

	// 异步发送
	err = writer.WriteMessages(context.Background(), kafkaMsg)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	action := "increment"
	if message.Value < 0 {
		action = "decrement"
	}
	logger.Log.Debug(fmt.Sprintf("Published %s message: type=%s, circle_id=%s, value=%d",
		action, message.Type, message.CircleID.String(), message.Value))

	return nil
}

// CloseRedpandaProducer 关闭Redpanda Producer
func CloseRedpandaProducer() error {
	if writer != nil {
		if err := writer.Close(); err != nil {
			logger.Log.Error("Failed to close redpanda writer: " + err.Error())
			return err
		}
		logger.Log.Info("Redpanda producer closed")
	}
	return nil
}

// ==================== 帖子统计消息 ====================

// InitPostStatsProducer 初始化帖子统计Producer
func InitPostStatsProducer() error {
	postWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.PostTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Post stats producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.PostTopic))

	return nil
}

// PublishPostViewCount 发布帖子浏览量变化消息
func PublishPostViewCount(postID uuid.UUID) error {
	if postWriter == nil {
		return fmt.Errorf("post stats writer is not initialized")
	}
	return publishPostMessage(PostStatisticsMessage{
		Type:   StatisticsTypePostView,
		PostID: postID,
		Value:  1,
	})
}

// PublishPostLikeCount 发布帖子点赞数变化消息
func PublishPostLikeCount(postID uuid.UUID, value int64) error {
	if postWriter == nil {
		return fmt.Errorf("post stats writer is not initialized")
	}
	return publishPostMessage(PostStatisticsMessage{
		Type:   StatisticsTypePostLike,
		PostID: postID,
		Value:  value,
	})
}

// PublishPostCollectCount 发布帖子收藏数变化消息
func PublishPostCollectCount(postID uuid.UUID, value int64) error {
	if postWriter == nil {
		return fmt.Errorf("post stats writer is not initialized")
	}
	return publishPostMessage(PostStatisticsMessage{
		Type:   StatisticsTypePostCollect,
		PostID: postID,
		Value:  value,
	})
}

// publishPostMessage 发送帖子统计消息到Redpanda
func publishPostMessage(message PostStatisticsMessage) error {
	value, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal post stats message: %w", err)
	}

	kafkaMsg := kafka.Message{
		Key:   []byte(message.PostID.String()),
		Value: value,
	}

	err = postWriter.WriteMessages(context.Background(), kafkaMsg)
	if err != nil {
		return fmt.Errorf("failed to write post stats message: %w", err)
	}

	logger.Log.Debug(fmt.Sprintf("Published post stats message: type=%s, post_id=%s, value=%d",
		message.Type, message.PostID.String(), message.Value))

	return nil
}

// ClosePostStatsProducer 关闭帖子统计Producer
func ClosePostStatsProducer() error {
	if postWriter != nil {
		if err := postWriter.Close(); err != nil {
			logger.Log.Error("Failed to close post stats writer: " + err.Error())
			return err
		}
		logger.Log.Info("Post stats producer closed")
	}
	return nil
}

// ==================== 点赞事件消息 ====================

// InitLikeEventProducer 初始化点赞事件Producer
func InitLikeEventProducer() error {
	likeEventWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.LikeEventTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.Hash{}, // 按 key(user:target) 哈希分区：同对事件同分区有序（消费者"末态为准"依赖）
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Like event producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.LikeEventTopic))
	return nil
}

// PublishCommentLikeEvent 发布评论点赞事件消息
func PublishCommentLikeEvent(userID, commentID, postID uuid.UUID, amount int64) error {
	if likeEventWriter == nil {
		return fmt.Errorf("like event writer is not initialized")
	}
	return publishLikeEvent(LikeEventMessage{
		Type:     "comment_like",
		UserID:   userID,
		TargetID: commentID,
		PostID:   postID,
		Amount:   amount,
	})
}

// PublishPostLikeEvent 发布帖子点赞事件消息
func PublishPostLikeEvent(userID, postID uuid.UUID, amount int64) error {
	if likeEventWriter == nil {
		return fmt.Errorf("like event writer is not initialized")
	}
	return publishLikeEvent(LikeEventMessage{
		Type:     "post_like",
		UserID:   userID,
		TargetID: postID,
		Amount:   amount,
	})
}

func publishLikeEvent(msg LikeEventMessage) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal like event message: %w", err)
	}
	kafkaMsg := kafka.Message{
		Key:   []byte(fmt.Sprintf("%s:%s", msg.UserID.String(), msg.TargetID.String())),
		Value: value,
	}
	if err := likeEventWriter.WriteMessages(context.Background(), kafkaMsg); err != nil {
		return fmt.Errorf("failed to write like event message: %w", err)
	}
	logger.Log.Debug(fmt.Sprintf("Published like event: type=%s, user=%s, target=%s, amount=%d",
		msg.Type, msg.UserID.String(), msg.TargetID.String(), msg.Amount))
	return nil
}

// CloseLikeEventProducer 关闭点赞事件Producer
func CloseLikeEventProducer() error {
	if likeEventWriter != nil {
		if err := likeEventWriter.Close(); err != nil {
			logger.Log.Error("Failed to close like event writer: " + err.Error())
			return err
		}
		logger.Log.Info("Like event producer closed")
	}
	return nil
}

// ==================== 收藏事件消息 ====================

// InitCollectEventProducer 初始化收藏事件Producer
func InitCollectEventProducer() error {
	collectEventWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.CollectEventTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Collect event producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.CollectEventTopic))
	return nil
}

// PublishPostCollectEvent 发布帖子收藏事件消息
func PublishPostCollectEvent(userID, postID uuid.UUID, amount int64) error {
	if collectEventWriter == nil {
		return fmt.Errorf("collect event writer is not initialized")
	}
	return publishCollectEvent(CollectEventMessage{
		Type:   CollectEventType,
		UserID: userID,
		PostID: postID,
		Amount: amount,
	})
}

func publishCollectEvent(msg CollectEventMessage) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal collect event message: %w", err)
	}
	kafkaMsg := kafka.Message{
		Key:   []byte(fmt.Sprintf("%s:%s", msg.UserID.String(), msg.PostID.String())),
		Value: value,
	}
	if err := collectEventWriter.WriteMessages(context.Background(), kafkaMsg); err != nil {
		return fmt.Errorf("failed to write collect event message: %w", err)
	}
	logger.Log.Debug(fmt.Sprintf("Published collect event: type=%s, user=%s, post=%s, amount=%d",
		msg.Type, msg.UserID.String(), msg.PostID.String(), msg.Amount))
	return nil
}

// CloseCollectEventProducer 关闭收藏事件Producer
func CloseCollectEventProducer() error {
	if collectEventWriter != nil {
		if err := collectEventWriter.Close(); err != nil {
			logger.Log.Error("Failed to close collect event writer: " + err.Error())
			return err
		}
		logger.Log.Info("Collect event producer closed")
	}
	return nil
}

// ==================== 浏览历史事件消息 ====================

// InitHistoryEventProducer 初始化浏览历史事件Producer
func InitHistoryEventProducer() error {
	historyEventWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.HistoryEventTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("History event producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.HistoryEventTopic))
	return nil
}

// PublishPostViewHistoryEvent 发布帖子浏览历史事件消息
func PublishPostViewHistoryEvent(userID, postID uuid.UUID) error {
	if historyEventWriter == nil {
		return fmt.Errorf("history event writer is not initialized")
	}
	return publishHistoryEvent(HistoryEventMessage{
		Type:   HistoryEventType,
		UserID: userID,
		PostID: postID,
	})
}

func publishHistoryEvent(msg HistoryEventMessage) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal history event message: %w", err)
	}
	kafkaMsg := kafka.Message{
		Key:   []byte(fmt.Sprintf("%s:%s", msg.UserID.String(), msg.PostID.String())),
		Value: value,
	}
	if err := historyEventWriter.WriteMessages(context.Background(), kafkaMsg); err != nil {
		return fmt.Errorf("failed to write history event message: %w", err)
	}
	logger.Log.Debug(fmt.Sprintf("Published history event: type=%s, user=%s, post=%s",
		msg.Type, msg.UserID.String(), msg.PostID.String()))
	return nil
}

// CloseHistoryEventProducer 关闭浏览历史事件Producer
func CloseHistoryEventProducer() error {
	if historyEventWriter != nil {
		if err := historyEventWriter.Close(); err != nil {
			logger.Log.Error("Failed to close history event writer: " + err.Error())
			return err
		}
		logger.Log.Info("History event producer closed")
	}
	return nil
}

// ==================== 帖子热度增量消息 ====================

// InitPostHotProducer 初始化帖子热度 Producer。
func InitPostHotProducer() error {
	postHotWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.PostHotTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Post hot producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.PostHotTopic))
	return nil
}

// PublishPostHot 发布帖子热度增量消息。
// delta 为 ApplyHotDelta 计算后的最终签名 Δ（已 clamp）。
func PublishPostHot(postID uuid.UUID, delta int64) error {
	if postHotWriter == nil {
		return fmt.Errorf("post hot writer is not initialized")
	}
	if delta == 0 {
		return nil // cap 截断或权重 0，无变化不发
	}

	value, err := json.Marshal(PostHotMessage{PostID: postID, Delta: delta})
	if err != nil {
		return fmt.Errorf("failed to marshal post hot message: %w", err)
	}

	kafkaMsg := kafka.Message{
		Key:   []byte(postID.String()), // 同帖子消息保序
		Value: value,
	}
	if err := postHotWriter.WriteMessages(context.Background(), kafkaMsg); err != nil {
		return fmt.Errorf("failed to write post hot message: %w", err)
	}

	logger.Log.Debug(fmt.Sprintf("Published post hot: post_id=%s, delta=%d",
		postID.String(), delta))
	return nil
}

// ClosePostHotProducer 关闭帖子热度 Producer。
func ClosePostHotProducer() error {
	if postHotWriter != nil {
		if err := postHotWriter.Close(); err != nil {
			logger.Log.Error("Failed to close post hot writer: " + err.Error())
			return err
		}
		logger.Log.Info("Post hot producer closed")
	}
	return nil
}

// ==================== 帖子互动事件消息（CF 灌数） ====================

// InitPostInteractionProducer 初始化帖子互动事件 Producer。
func InitPostInteractionProducer() error {
	postInteractionWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.PostInteractionTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Post interaction producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.PostInteractionTopic))
	return nil
}

// PublishPostInteraction 发布帖子互动事件（CF 灌数）。
// weight 由调用方按动作类型映射传入（见 InteractionWeight* 常量）。
func PublishPostInteraction(userID, postID uuid.UUID, action InteractionAction, weight int16) error {
	if postInteractionWriter == nil {
		return fmt.Errorf("post interaction writer is not initialized")
	}

	value, err := json.Marshal(PostInteractionMessage{
		UserID: userID,
		PostID: postID,
		Action: action,
		Weight: weight,
		Ts:     time.Now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal post interaction message: %w", err)
	}

	kafkaMsg := kafka.Message{
		Key:   []byte(postID.String()), // key=postID 保序 + 热点分片
		Value: value,
	}
	if err := postInteractionWriter.WriteMessages(context.Background(), kafkaMsg); err != nil {
		return fmt.Errorf("failed to write post interaction message: %w", err)
	}

	logger.Log.Debug(fmt.Sprintf("Published post interaction: user=%s, post=%s, action=%s, weight=%d",
		userID.String(), postID.String(), action, weight))
	return nil
}

// ClosePostInteractionProducer 关闭帖子互动事件 Producer。
func ClosePostInteractionProducer() error {
	if postInteractionWriter != nil {
		if err := postInteractionWriter.Close(); err != nil {
			logger.Log.Error("Failed to close post interaction writer: " + err.Error())
			return err
		}
		logger.Log.Info("Post interaction producer closed")
	}
	return nil
}

// ==================== 通知事件消息（消息中心） ====================

// InitNotificationEventProducer 初始化通知事件 Producer。
func InitNotificationEventProducer() error {
	notificationEventWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.NoticeEventTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Notification event producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.NoticeEventTopic))
	return nil
}

// PublishNotificationEvent 发布通知事件（消息中心）。
// key = 目标 ID（post_id 或 comment_id），同目标事件保序。
func PublishNotificationEvent(msg NotificationEventMessage) error {
	if notificationEventWriter == nil {
		return fmt.Errorf("notification event writer is not initialized")
	}
	if msg.Ts == 0 {
		msg.Ts = time.Now().UnixMilli()
	}

	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal notification event message: %w", err)
	}

	var key []byte
	if msg.CommentID != nil {
		key = []byte(msg.CommentID.String())
	} else if msg.PostID != nil {
		key = []byte(msg.PostID.String())
	} else {
		key = []byte(msg.ActorID.String())
	}

	if err := notificationEventWriter.WriteMessages(context.Background(), kafka.Message{Key: key, Value: value}); err != nil {
		return fmt.Errorf("failed to write notification event message: %w", err)
	}

	logger.Log.Debug(fmt.Sprintf("Published notification event: type=%s, actor=%s", msg.Type, msg.ActorID.String()))
	return nil
}

// CloseNotificationEventProducer 关闭通知事件 Producer。
func CloseNotificationEventProducer() error {
	if notificationEventWriter != nil {
		if err := notificationEventWriter.Close(); err != nil {
			logger.Log.Error("Failed to close notification event writer: " + err.Error())
			return err
		}
		logger.Log.Info("Notification event producer closed")
	}
	return nil
}
