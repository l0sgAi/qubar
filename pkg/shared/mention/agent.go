// Package mention 是 post / comment 两域共用的 @提及 规则。
//
// 两域的业务层不能互相 import（跨域只走 Facade），而「哪些机器人不能被 @」必须是同一条规则，
// 所以放在共享包：只依赖标准库、uuid 与 logger，不 import 任何领域包。各域的 UserFacade 把
// AgentMentionScope 声明为 AgentScope 的别名，天然满足 AgentScopeLookup。
package mention

import (
	"context"

	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// AgentScope 机器人账号的 @提及 可见范围（user 域投影列的跨域视图）。
type AgentScope struct {
	// CircleID 绑定圈子ID；uuid.Nil=全局机器人。
	CircleID uuid.UUID
	// Mentionable 能否被 @：机器人停用或已删除时为 false。
	Mentionable bool
}

// MentionableIn 该机器人能否在 postCircleID 圈子的帖子（及其评论）里被 @：
// 未停用 / 未删除，且是全局机器人或绑定的就是这个圈子。
func (s AgentScope) MentionableIn(postCircleID uuid.UUID) bool {
	return s.Mentionable && (s.CircleID == uuid.Nil || s.CircleID == postCircleID)
}

// AgentScopeLookup 批量查机器人 @提及 可见范围的端口（post / comment 的 UserFacade 均满足）。
type AgentScopeLookup interface {
	// GetAgentMentionScopes 批量返回 userID → 机器人 @提及 可见范围（仅含圈内机器人与不可 @ 的
	// 机器人，普通用户 / 可 @ 的全局机器人不出现在 map）。
	GetAgentMentionScopes(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]AgentScope, error)
}

// StripUnmentionableAgents 从 mention 候选中剔除不可 @ 的机器人：停用 / 已删除的机器人，
// 以及绑定圈子 ≠ postCircleID 的圈内机器人静默移除；普通用户 / 可 @ 的全局机器人（map 无记录）保留，
// 候选顺序不变。
//
// 这是发帖 / 发评论时的兜底（@选人列表已在 ES 侧过滤，这里防手搓 ID 构造请求与 CDC 延迟窗口）。
// 查询失败 fail-open：记日志并原样返回候选，不阻断发帖 / 发评论。
// 调用方须在截断 MentionMax 之前调用：被剔除的机器人不占用配额。
func StripUnmentionableAgents(ctx context.Context, lookup AgentScopeLookup, candidates []uuid.UUID, postCircleID uuid.UUID) []uuid.UUID {
	if len(candidates) == 0 {
		return candidates
	}
	scopes, err := lookup.GetAgentMentionScopes(ctx, candidates)
	if err != nil {
		logger.Log.Warn("Failed to load agent mention scope, skip agent filtering: " + err.Error())
		return candidates
	}
	kept := make([]uuid.UUID, 0, len(candidates))
	for _, id := range candidates {
		if scope, ok := scopes[id]; ok && !scope.MentionableIn(postCircleID) {
			continue
		}
		kept = append(kept, id)
	}
	return kept
}
