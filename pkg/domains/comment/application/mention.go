package application

import (
	"context"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// filterMentionUserIDs 校验 @提及用户列表：去重 → 去自己 → 存在性（UserFacade）→
// 剔除不可 @ 的机器人 → 截断上限。
// 机器人兜底：越圈的圈内机器人（绑定圈子 ≠ 帖子圈子）与停用/已删除的机器人
// （GetAgentMentionScopes 命中）静默剔除，防手搓 ID 构造请求与 CDC 延迟窗口（@选人列表已在 ES 侧过滤）。校验失败（如 user 服务不可用）
// 降级为不发 mention 通知，不影响评论主流程。
func (s *commentServiceImpl) filterMentionUserIDs(ctx context.Context, actorID uuid.UUID, mentionUserIDs []uuid.UUID, postCircleID uuid.UUID) []uuid.UUID {
	if len(mentionUserIDs) == 0 || s.userFacade == nil {
		return nil
	}

	seen := make(map[uuid.UUID]struct{}, len(mentionUserIDs))
	ordered := make([]uuid.UUID, 0, len(mentionUserIDs))
	candidates := make([]string, 0, len(mentionUserIDs))
	for _, id := range mentionUserIDs {
		if id == uuid.Nil || id == actorID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
		candidates = append(candidates, id.String())
	}
	if len(candidates) == 0 {
		return nil
	}

	briefs, err := s.userFacade.GetBriefs(ctx, candidates)
	if err != nil {
		logger.Log.Error("Failed to validate mention user ids: " + err.Error())
		return nil
	}

	// 按去重后的原始顺序筛选，不存在的用户静默过滤；重复提及既不重复入列，
	// 也不占用 MentionMax 配额（截断在剔除之后，见下）。
	max := conf.Config.Notice.MentionMax
	if max <= 0 {
		max = 10
	}
	valid := make([]uuid.UUID, 0, len(briefs))
	for _, id := range ordered {
		if _, ok := briefs[id.String()]; !ok {
			continue // 不存在的用户静默过滤
		}
		valid = append(valid, id)
	}
	// 机器人剔除先于截断：不可 @ 的机器人不占用 MentionMax 配额。
	valid = stripUnmentionableAgents(ctx, s.userFacade, valid, postCircleID)
	if len(valid) > max {
		valid = valid[:max]
	}
	return valid
}

// stripUnmentionableAgents 从 mention 候选中剔除不可 @ 的机器人：停用/已删除的机器人，
// 以及绑定圈子 ≠ postCircleID 的圈内机器人静默移除；普通用户/可 @ 的全局机器人（map 无记录）保留。
// 查询失败 fail-open 跳过剔除。
// post/comment 两域同构逻辑（各自 UserFacade 名义类型不同，无法共用实现）。
func stripUnmentionableAgents(ctx context.Context, facade UserFacade, candidates []uuid.UUID, postCircleID uuid.UUID) []uuid.UUID {
	if len(candidates) == 0 {
		return candidates
	}
	scopes, err := facade.GetAgentMentionScopes(ctx, candidates)
	if err != nil {
		logger.Log.Warn("Failed to load agent mention scope, skip agent filtering: " + err.Error())
		return candidates
	}
	kept := make([]uuid.UUID, 0, len(candidates))
	for _, id := range candidates {
		if scope, ok := scopes[id]; ok &&
			(!scope.Mentionable || (scope.CircleID != uuid.Nil && scope.CircleID != postCircleID)) {
			continue
		}
		kept = append(kept, id)
	}
	return kept
}
