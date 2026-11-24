package domain

import (
	"context"

	"github.com/google/uuid"
)

// FollowEventPublisher follow 领域的事件发布端口（Redpanda 实现见 infrastructure/follow_event_publisher.go）。
type FollowEventPublisher interface {
	// PublishFollowed 发布「被关注」通知事件（消息中心 notice_type = 12，#219）：followerID 关注了 followeeID。
	// 只在关系从「未关注」变为「已关注」时调用；重复关注与取关都不发布（取关不撤回已有通知）。
	// 通知链路是 best-effort：调用方对返回的错误只记日志，不影响关注结果。
	PublishFollowed(ctx context.Context, followerID, followeeID uuid.UUID) error
}
