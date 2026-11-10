package application

import (
	"context"

	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// notifyFollowed 关系从「未关注」变为「已关注」后发布被关注通知事件（#219）。
//
// 调用方保证只在仓储返回 changed == true 时调用：重复关注（幂等命中）不发，取关不发也不撤回。
// 取关后再次关注会再发一条，通知 consumer 复用同一行并重新置为未读（与重复点赞一致）。
// best-effort：发布失败只记日志，不影响关注接口返回（通知链路没有 outbox，与点赞通知相同）。
func (s *followServiceImpl) notifyFollowed(ctx context.Context, followerID, followeeID uuid.UUID) {
	if err := s.events.PublishFollowed(ctx, followerID, followeeID); err != nil {
		logger.Log.Error("Failed to publish follow notification: " + err.Error())
	}
}
