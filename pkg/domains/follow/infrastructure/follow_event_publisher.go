package infrastructure

import (
	"context"

	"interestBar/pkg/domains/follow/domain"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// followEventPublisherRedpanda 基于 redpanda 的 FollowEventPublisher 实现。
type followEventPublisherRedpanda struct{}

// NewFollowEventPublisher 构造 FollowEventPublisher。
func NewFollowEventPublisher() domain.FollowEventPublisher {
	return &followEventPublisherRedpanda{}
}

// PublishFollowed 发布被关注通知事件：actor = 关注者，接收人 = 被关注者。
// 关注通知没有可供 consumer 反查的帖子 / 评论，接收人由事件自带（RecipientID）。
func (p *followEventPublisherRedpanda) PublishFollowed(_ context.Context, followerID, followeeID uuid.UUID) error {
	return redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
		Type:        redpanda.NoticeTypeFollow,
		ActorID:     followerID,
		RecipientID: &followeeID,
	})
}
