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

// PublishCommentCount 发布评论数变化事件（帖子统计 topic，消费者按明细重算 comment_count）。
func (p *postEventPublisherRedpanda) PublishCommentCount(ctx context.Context, postID uuid.UUID) error {
	return redpanda.PublishPostCommentCount(postID, 1)
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

// PublishReviewResultNotice 发布人工审核结果通知事件（消息中心，best-effort 不阻断主流程）。
func (p *postEventPublisherRedpanda) PublishReviewResultNotice(ctx context.Context, operatorID, postID uuid.UUID, approved bool) error {
	_ = ctx
	noticeType := redpanda.NoticeTypePostRejected
	if approved {
		noticeType = redpanda.NoticeTypePostApproved
	}
	if err := redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
		Type:    noticeType,
		ActorID: operatorID,
		PostID:  &postID,
	}); err != nil {
		logger.Log.Error("Failed to publish post review result notification: " + err.Error())
	}
	return nil
}

// PublishModeratorNotice 发布圈管通知事件（消息中心，best-effort 不阻断主流程）。
func (p *postEventPublisherRedpanda) PublishModeratorNotice(ctx context.Context, kind domain.ModeratorNoticeKind, actorID, postID uuid.UUID) error {
	_ = ctx
	noticeType := redpanda.NoticeTypePostReported
	if kind == domain.ModeratorNoticeSelfHarm {
		noticeType = redpanda.NoticeTypePostSelfHarm
	}
	if err := redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
		Type:    noticeType,
		ActorID: actorID,
		PostID:  &postID,
	}); err != nil {
		logger.Log.Error("Failed to publish post moderator notification: " + err.Error())
	}
	return nil
}

// PublishPostRemovedNotice 发布帖子下架通知事件（消息中心，best-effort 不阻断主流程）。
func (p *postEventPublisherRedpanda) PublishPostRemovedNotice(ctx context.Context, operatorID, postID uuid.UUID) error {
	_ = ctx
	if err := redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
		Type:    redpanda.NoticeTypePostRemoved,
		ActorID: operatorID,
		PostID:  &postID,
	}); err != nil {
		logger.Log.Error("Failed to publish post removed notification: " + err.Error())
	}
	return nil
}

// PublishModeration 发布帖子审核事件（同步写入，失败由调用方按 fail_open 兜底）。
func (p *postEventPublisherRedpanda) PublishModeration(ctx context.Context, postID uuid.UUID) error {
	return redpanda.PublishPostModerationEvent(ctx, postID)
}
