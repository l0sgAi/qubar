package application

import (
	"context"
	"errors"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"

	"github.com/google/uuid"
)

// CreateComment 发评论（支持顶层评论和回复）。
//
// 与旧 controller.CreateComment 行为一致：
//  1. 校验帖子存在 + 状态(已发布) + 未锁定；
//  2. 清洗 content（SanitizeForPg，剔除 NULL 字节等非法 UTF8）；
//  3. 取根评论 / 被回复评论，经 domain.NewRootComment / NewReply 校验盖楼不变量
//     （root_id 须为同帖顶层评论、reply_to_id 须伴随 root_id 且在该楼内）并推导被回复用户ID；
//  4. 创建评论（事务内：插入评论 + 如为回复则递增根评论 reply_count）；
//  5. 实时递增帖子评论计数（Redis Hash 实时 + 帖子统计事件批量落库）。
func (s *commentServiceImpl) CreateComment(ctx context.Context, userID uuid.UUID, input CreateCommentInput) (uuid.UUID, error) {
	// 1. 校验帖子
	if s.postLookup == nil {
		return uuid.Nil, errors.New("post lookup is not configured")
	}
	post, err := s.postLookup.GetPost(ctx, input.PostID)
	if err != nil {
		return uuid.Nil, err
	}
	if post == nil {
		return uuid.Nil, domain.ErrPostNotFound
	}
	// 私密圈非成员按帖子不存在处理（先于状态 / 锁定校验，不泄露帖子信息）。
	if !input.SystemActor {
		if err := s.ensurePostVisible(ctx, userID, input.PostID, domain.ErrPostNotFound); err != nil {
			return uuid.Nil, err
		}
	}
	// 帖子状态必须为"已发布"才能评论（PostStatusPublished=1）
	if post.Status != 1 {
		return uuid.Nil, domain.ErrPostNotCommentable
	}
	if post.IsLock == 1 {
		return uuid.Nil, domain.ErrPostLocked
	}

	// 2. 清洗 PostgreSQL text 字段不接受的字符（NULL 字节 U+0000 及其它无效
	// UTF-8 字节序列），避免写入时报 "invalid byte sequence for encoding UTF8"。
	content := utils.SanitizeForPg(input.Content)
	if content == "" {
		return uuid.Nil, domain.ErrEmptyContent
	}

	// 3. 取楼层数据并经领域工厂构造评论（盖楼不变量见 domain.NewReply）
	comment, err := s.buildComment(ctx, userID, input, content)
	if err != nil {
		return uuid.Nil, err
	}
	replyToUserID := comment.ReplyToUserID

	// 4. 创建评论（事务内：插入评论 + 如为回复则递增根评论 reply_count）
	if err := s.repo.Create(ctx, comment); err != nil {
		return uuid.Nil, err
	}

	// 5. 实时递增帖子评论计数（Redis Hash + 恢复缓存 + 帖子统计事件批量落库）
	if err := s.postLookup.RestoreStatsAndIncrCommentCount(ctx, input.PostID); err != nil {
		logger.Log.Error("Failed to increment post comment count: " + err.Error())
	}

	// 6. 累积帖子热度（评论 +5，per-post 上限 cap.comment，Lua 原子 clamp；best-effort）
	// @提及：校验一次得到最终名单 → 落库（best-effort 不阻断发评论）→
	// 通知使用同一份落库名单（通知名单 == 落库名单）。
	mentionIDs := s.filterMentionUserIDs(ctx, userID, input.MentionUserIDs, post.CircleID)
	if len(mentionIDs) > 0 {
		if err := s.repo.CreateMentions(ctx, comment.ID, mentionIDs); err != nil {
			logger.Log.Error("Failed to save comment mentions: " + err.Error())
		}
	}

	if s.publisher != nil {
		if err := s.publisher.PublishCommentHot(ctx, input.PostID, 1); err != nil {
			logger.Log.Error("Failed to publish comment hot: " + err.Error())
		}
		// CF 互动：评论者对该帖写互动矩阵（weight=comment，正向）。
		if err := s.publisher.PublishCommentInteraction(ctx, userID, input.PostID); err != nil {
			logger.Log.Error("Failed to publish comment interaction: " + err.Error())
		}
		// 消息中心通知（评论/回复 + @提及，best-effort）
		s.publishCommentNotifications(ctx, userID, input.PostID, comment.ID, input.RootID == nil, replyToUserID != nil, mentionIDs, content)
	}

	// 7. AI 机器人回复触发（同步回调、实现方立即返回；机器人自身评论由实现方防回环）。
	// post.CircleID 随事件透传：圈子级机器人只在同圈帖触发（circle-agent-reply）。
	if s.agentTrigger != nil {
		s.agentTrigger.OnCommentCreated(input.PostID, post.CircleID, comment.ID, userID, comment.RootID, content)
	}

	return comment.ID, nil
}

// buildComment 按入参取楼层数据，交给领域工厂构造评论（不变量校验在 domain，这里只取数）。
//
// 只传 reply_to_id 不传 root_id 直接拒绝（不推导所属楼，避免顶层评论带任意 reply_to_id）。
// 根评论 / 被回复评论不存在或已删除分别映射为 ErrRootCommentNotFound / ErrReplyTargetNotFound。
func (s *commentServiceImpl) buildComment(ctx context.Context, userID uuid.UUID, input CreateCommentInput, content string) (*domain.Comment, error) {
	params := domain.NewCommentParams{
		PostID:    input.PostID,
		UserID:    userID,
		Content:   content,
		ExtraData: input.ExtraData,
	}
	if input.RootID == nil {
		if input.ReplyToID != nil {
			return nil, domain.ErrReplyTargetWithoutRoot
		}
		return domain.NewRootComment(params), nil
	}

	root, err := s.repo.GetByID(ctx, *input.RootID)
	if err != nil {
		if errors.Is(err, domain.ErrCommentNotFound) {
			return nil, domain.ErrRootCommentNotFound
		}
		return nil, err
	}
	var replyTo *domain.Comment
	if input.ReplyToID != nil {
		if *input.ReplyToID == root.ID {
			replyTo = root
		} else if replyTo, err = s.repo.GetByID(ctx, *input.ReplyToID); err != nil {
			if errors.Is(err, domain.ErrCommentNotFound) {
				return nil, domain.ErrReplyTargetNotFound
			}
			return nil, err
		}
	}
	return domain.NewReply(params, root, replyTo)
}

// ===== 消息中心通知辅助 =====

// noticeSnippetMaxRunes 通知快照上限（与 notification.snippet VARCHAR(200) 对齐留余量）。
const noticeSnippetMaxRunes = 100

// publishCommentNotifications 发布评论相关的消息中心通知（best-effort，不阻断主流程）。
//
// 顶层评论 → comment_post（接收人=帖子作者）；回复 → reply_comment（接收人=被回复评论作者）；
// @提及 → mention（接收人=被提及用户）。mentionIDs 为已校验并落库的最终名单
// （CreateComment 中 filterMentionUserIDs 的结果，通知名单 == 落库名单）；
// 接收人除 mention 外均由 consumer 反查解析。
func (s *commentServiceImpl) publishCommentNotifications(ctx context.Context, userID, postID, commentID uuid.UUID, isTopLevel, isReply bool, mentionIDs []uuid.UUID, content string) {
	snippet := truncateRunes(content, noticeSnippetMaxRunes)

	switch {
	case isTopLevel:
		if err := s.publisher.PublishCommentNotice(ctx, userID, postID, commentID, false, snippet); err != nil {
			logger.Log.Error("Failed to publish comment_post notification: " + err.Error())
		}
	case isReply:
		if err := s.publisher.PublishCommentNotice(ctx, userID, postID, commentID, true, snippet); err != nil {
			logger.Log.Error("Failed to publish reply_comment notification: " + err.Error())
		}
	}

	if len(mentionIDs) > 0 {
		if err := s.publisher.PublishMentionNotice(ctx, userID, &postID, &commentID, mentionIDs, snippet); err != nil {
			logger.Log.Error("Failed to publish mention notification: " + err.Error())
		}
	}
}

// truncateRunes 按 rune 截断字符串。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}
