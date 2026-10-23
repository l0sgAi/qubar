package infrastructure

import (
	"context"

	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/logger"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// postEventPublisherRedpanda 基于 redpanda 的 PostEventPublisher 实现。
type postEventPublisherRedpanda struct{}

// NewPostEventPublisher 构造 PostEventPublisher。
func NewPostEventPublisher() domain.PostEventPublisher {
	return &postEventPublisherRedpanda{}
}

// PublishViewCount 发布浏览量变化事件。
func (p *postEventPublisherRedpanda) PublishViewCount(ctx context.Context, postID uuid.UUID) error {
	return redpanda.PublishPostViewCount(postID)
}

// PublishMentionNotice 发布 @提及 通知事件（消息中心，best-effort 不阻断主流程）。
func (p *postEventPublisherRedpanda) PublishMentionNotice(ctx context.Context, actorID, postID uuid.UUID, mentionUserIDs []uuid.UUID, snippet string) error {
	_ = ctx
	if len(mentionUserIDs) == 0 {
		return nil
	}
	if err := redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
		Type:           redpanda.NoticeTypeMention,
		ActorID:        actorID,
		PostID:         &postID,
		MentionUserIDs: mentionUserIDs,
		Snippet:        snippet,
	}); err != nil {
		logger.Log.Error("Failed to publish post mention notification: " + err.Error())
	}
	return nil
}

// PublishModeration 发布帖子审核事件（同步写入，失败由调用方按 fail_open 兜底）。
func (p *postEventPublisherRedpanda) PublishModeration(ctx context.Context, postID uuid.UUID) error {
	return redpanda.PublishPostModerationEvent(ctx, postID)
}
