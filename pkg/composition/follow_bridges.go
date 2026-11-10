// Package composition 的 follow_bridges.go：注入 follow 领域的跨域桥接器（user → follow）。
package composition

import (
	"context"

	followapp "interestBar/pkg/domains/follow/application"
	userapp "interestBar/pkg/domains/user/application"

	"github.com/google/uuid"
)

// ===== user → follow（目标用户存在性）=====

// followUserChecker 把 user.application.UserFacade 适配为 follow.application.UserChecker。
type followUserChecker struct {
	delegate userapp.UserFacade
}

var _ followapp.UserChecker = (*followUserChecker)(nil)

// Exists 复用 UserFacade.GetBrief（内部走 UserService.GetByID：缓存优先，
// 不存在 / 已禁用 / 已删除都返回 nil），AI 机器人账号（role=2）同样视为存在。
func (c *followUserChecker) Exists(ctx context.Context, userID uuid.UUID) (bool, error) {
	brief, err := c.delegate.GetBrief(ctx, userID.String())
	if err != nil {
		return false, err
	}
	return brief != nil, nil
}
