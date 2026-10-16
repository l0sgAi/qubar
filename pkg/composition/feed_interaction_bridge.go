// Package composition 的 feed_interaction_bridge.go：recommend / trending / discover 共用的交互态桥接器（post → 信息流 is_liked / is_collected）。
package composition

import (
	"context"

	postapp "interestBar/pkg/domains/post/application"

	"github.com/google/uuid"
)

// postInteractionChecker 把 post.application.PostService.BatchCheckInteractions 适配为
// recommend / trending / discover 三个 InteractionChecker 端口（签名一致，共用一个桥接器）。
//
// 旧实现各自直接调 redispkg ZSET，miss 一律当 false：ZSET 有 TTL 与容量上限，
// 已赞帖被显示为未赞会诱发重复点赞（#46）。现统一走 post 的"缓存 → miss 回源 DB → 回填"。
type postInteractionChecker struct {
	delegate postapp.PostService
}

func (c *postInteractionChecker) BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (liked, collected map[uuid.UUID]bool, err error) {
	return c.delegate.BatchCheckInteractions(ctx, userID, postIDs)
}
