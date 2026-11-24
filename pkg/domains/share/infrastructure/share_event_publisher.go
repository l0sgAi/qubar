package infrastructure

import (
	"context"

	"interestBar/pkg/domains/share/domain"
	"interestBar/pkg/logger"
	redispkg "interestBar/pkg/server/storage/redis"
	redpanda "interestBar/pkg/server/storage/redpanda"
)

// shareEventPublisherRedpanda 基于 Redpanda 的 ShareEventPublisher 实现。
type shareEventPublisherRedpanda struct{}

// NewShareEventPublisher 构造 ShareEventPublisher。
func NewShareEventPublisher() domain.ShareEventPublisher {
	return &shareEventPublisherRedpanda{}
}

// PublishPostShare 同步发布首次分享事件；成功后 best-effort：分享数落库标记 + 帖子热度。
//
// 不发 CF 互动、不发通知（#232 范围外）。分享不可撤销，没有负向事件。
func (p *shareEventPublisherRedpanda) PublishPostShare(_ context.Context, share *domain.PostShare, post *domain.SharedPost) error {
	if err := redpanda.PublishShareEvent(redpanda.ShareEventMessage{
		ShareID:  share.ID,
		UserID:   share.UserID,
		PostID:   post.ID,
		AuthorID: post.AuthorID,
		CircleID: post.CircleID,
	}); err != nil {
		return err
	}
	// 分享数：只发「有新分享」标记，帖子统计消费者按 post_share 重算 share_count（#130 批量窗口，不逐条 UPDATE）。
	// 标记丢失时 PG 列要等该帖下一次分享才更新；读路径以 Redis 为准，不受影响。
	if err := redpanda.PublishPostShareCount(post.ID); err != nil {
		logger.Log.Error("Failed to publish post share count: " + err.Error())
	}
	// 热度：首次分享 +weight，无上限（一人一帖只记一次）。
	if delta, err := redispkg.ApplyHotDelta(post.ID, redispkg.HotDimPostShare, 1); err != nil {
		logger.Log.Error("Failed to apply post_share hot delta: " + err.Error())
	} else if delta != 0 {
		if err := redpanda.PublishPostHot(post.ID, delta); err != nil {
			logger.Log.Error("Failed to publish post_share hot: " + err.Error())
		}
	}
	return nil
}
