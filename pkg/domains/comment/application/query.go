package application

import (
	"context"
	"errors"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// GetRootComments 获取帖子的顶层评论列表（游标分页）。
func (s *commentServiceImpl) GetRootComments(ctx context.Context, userID, postID uuid.UUID, sort int, cursor string) (*CommentListResult, error) {
	if err := s.ensurePostVisible(ctx, userID, postID, domain.ErrPostNotFound); err != nil {
		return nil, err
	}
	comments, nextCursor, hasMore, err := s.repo.GetRootCommentsByCursor(ctx, postID, rootCommentPageSize, sort, cursor)
	if err != nil {
		return nil, err
	}
	vos := s.buildCommentVOs(ctx, userID, comments)
	return &CommentListResult{
		Items: vos, HasMore: hasMore, NextCursor: nextCursor,
	}, nil
}

// GetReplies 获取某条评论的子回复列表（游标分页）。
func (s *commentServiceImpl) GetReplies(ctx context.Context, userID, rootID uuid.UUID, limit, sort int, cursor string) (*CommentListResult, error) {
	// 校验根评论可见且是顶层评论（根评论不可见时整楼不可见）
	rootComment, err := s.getVisibleComment(ctx, rootID)
	if err != nil {
		return nil, err
	}
	if rootComment.RootID != nil {
		return nil, errNotRootComment
	}
	if err := s.ensurePostVisible(ctx, userID, rootComment.PostID, domain.ErrCommentNotFound); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultReplyPageSize
	}
	comments, nextCursor, hasMore, err := s.repo.GetRepliesByCursor(ctx, rootID, limit, sort, cursor)
	if err != nil {
		return nil, err
	}
	vos := s.buildCommentVOs(ctx, userID, comments)
	return &CommentListResult{
		Items: vos, HasMore: hasMore, NextCursor: nextCursor,
	}, nil
}

// LocateComment 定位评论（通知点击直达）。
//
// 流程：取目标（不可见 → ErrCommentNotFound）→ 是回复则解析根评论（根不可见则其下回复
// 在列表中不可达，同样按不存在处理）→ 根评论在顶层列表的定位游标 → 回复时另算
// 回复列表定位游标与页码。游标语义：目标所在页的「起始游标」（上一页末条 keyset），
// 首页为 nil（JSON null），前端不传 cursor 直接拉首页。
func (s *commentServiceImpl) LocateComment(ctx context.Context, viewerID, commentID uuid.UUID, sort, replySort int) (*CommentLocateResult, error) {
	target, root, err := s.getVisibleThread(ctx, commentID)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePostVisible(ctx, viewerID, target.PostID, domain.ErrCommentNotFound); err != nil {
		return nil, err
	}

	listCursor, err := s.repo.LocateRootCursor(ctx, root.PostID, sort, root, rootCommentPageSize)
	if err != nil {
		return nil, err
	}

	result := &CommentLocateResult{
		CommentID: target.ID,
		PostID:    target.PostID,
		RootID:    root.ID,
		IsRoot:    1,
	}
	if listCursor != "" {
		result.ListCursor = &listCursor
	}
	if target.RootID == nil {
		return result, nil
	}

	result.IsRoot = 0
	replyCursor, replyPage, err := s.repo.LocateReplyCursor(ctx, root.ID, replySort, target, defaultReplyPageSize)
	if err != nil {
		return nil, err
	}
	if replyCursor != "" {
		result.ReplyCursor = &replyCursor
	}
	result.ReplyPage = replyPage
	return result, nil
}

// GetCommentDetail 获取单条评论详情（评论或其根评论不可见 → ErrCommentNotFound）。
func (s *commentServiceImpl) GetCommentDetail(ctx context.Context, userID, commentID uuid.UUID) (*CommentVO, error) {
	comment, _, err := s.getVisibleThread(ctx, commentID)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePostVisible(ctx, userID, comment.PostID, domain.ErrCommentNotFound); err != nil {
		return nil, err
	}

	vo := &CommentVO{
		ID:         comment.ID,
		PostID:     comment.PostID,
		UserID:     comment.UserID,
		RootID:     comment.RootID,
		ReplyToID:  comment.ReplyToID,
		Content:    comment.Content,
		ExtraData:  comment.ExtraData,
		LikeCount:  comment.LikeCount,
		ReplyCount: comment.ReplyCount,
		Status:     comment.Status,
		CreateTime: comment.CreateTime.Format("2006-01-02 15:04:05"),
	}

	// 获取当前用户点赞状态（缓存优先，miss 回源 DB + 回填）
	vo.Liked = s.checkLiked(ctx, userID, commentID)

	// 填充评论者信息
	if s.userFacade != nil {
		if author, err := s.userFacade.GetBrief(ctx, comment.UserID.String()); err == nil && author != nil {
			vo.AuthorName = author.Username
			vo.AuthorAvatar = author.AvatarURL
		}
	}

	// 填充被回复人信息
	if comment.ReplyToUserID != nil {
		vo.ReplyToUserID = comment.ReplyToUserID
		if s.userFacade != nil {
			if replyUser, err := s.userFacade.GetBrief(ctx, comment.ReplyToUserID.String()); err == nil && replyUser != nil {
				vo.ReplyToName = replyUser.Username
			}
		}
	}

	// 填充 @提及 用户列表（落库名单 → GetBrief 过滤未注销；失败置空数组，前端回退文本反查）
	vo.Mentions = []MentionVO{}
	if mentionIDsMap, err := s.repo.GetMentionUserIDsByCommentIDs(ctx, []uuid.UUID{commentID}); err == nil {
		if ids := mentionIDsMap[commentID]; len(ids) > 0 && s.userFacade != nil {
			if briefs, err := s.userFacade.GetBriefs(ctx, toStrings(ids)); err == nil {
				vo.Mentions = buildMentionVOs(ids, briefs)
			}
		}
	}

	return vo, nil
}

// GetCommentMeta 获取评论元信息（供 like 领域校验用）。
// 返回评论所属帖子ID（冗余字段，供 like 事件消费者更新 comment.post_id）。
// 未找到返回 nil, nil。
func (s *commentServiceImpl) GetCommentMeta(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, error) {
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, domain.ErrCommentNotFound) {
			return nil, nil
		}
		return nil, err
	}
	postID := comment.PostID
	return &postID, nil
}

// buildCommentVOs 批量构建 CommentVO（包含评论者信息和被回复人信息）。
//
// 与旧 controller.buildCommentVOs 行为一致：
//  1. 收集所有需要查询用户信息的用户ID（评论者 + 被回复人）；
//  2. 批量查询所有用户信息；
//  3. 批量查询当前用户点赞状态（Redis 优先，miss 回源 DB + 回填）；
//  4. 组装 VO。
func (s *commentServiceImpl) buildCommentVOs(ctx context.Context, userID uuid.UUID, comments []domain.Comment) []CommentVO {
	// 本页评论的提及名单（一次 IN 查询；mention 用户并入 userIDSet 共用一次 GetBriefs）
	commentIDs := make([]uuid.UUID, 0, len(comments))
	for _, cm := range comments {
		commentIDs = append(commentIDs, cm.ID)
	}
	mentionIDsMap := make(map[uuid.UUID][]uuid.UUID, len(comments))
	if len(commentIDs) > 0 {
		var err error
		mentionIDsMap, err = s.repo.GetMentionUserIDsByCommentIDs(ctx, commentIDs)
		if err != nil {
			logger.Log.Error("Failed to get comment mentions: " + err.Error())
		}
	}

	// 收集所有需要查询用户信息（评论者/被回复人/@提及用户）的用户ID
	userIDSet := make(map[uuid.UUID]struct{})
	for _, cm := range comments {
		userIDSet[cm.UserID] = struct{}{}
		if cm.ReplyToUserID != nil {
			userIDSet[*cm.ReplyToUserID] = struct{}{}
		}
	}
	for _, ids := range mentionIDsMap {
		for _, id := range ids {
			userIDSet[id] = struct{}{}
		}
	}

	// 批量查询所有用户信息（保留 string-keyed briefs 供提及组装复用）
	briefs := make(map[string]UserBrief, len(userIDSet))
	userMap := make(map[uuid.UUID]UserBrief, len(userIDSet))
	if s.userFacade != nil && len(userIDSet) > 0 {
		idStrs := make([]string, 0, len(userIDSet))
		for id := range userIDSet {
			idStrs = append(idStrs, id.String())
		}
		if fetched, err := s.userFacade.GetBriefs(ctx, idStrs); err == nil {
			briefs = fetched
			for idStr, b := range briefs {
				if uid, parseErr := uuid.Parse(idStr); parseErr == nil {
					userMap[uid] = UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询当前用户点赞状态
	likedMap := s.batchLikedStatus(ctx, userID, comments)

	// 组装
	vos := make([]CommentVO, 0, len(comments))
	for _, cm := range comments {
		vo := CommentVO{
			ID:         cm.ID,
			PostID:     cm.PostID,
			UserID:     cm.UserID,
			RootID:     cm.RootID,
			ReplyToID:  cm.ReplyToID,
			Content:    cm.Content,
			ExtraData:  cm.ExtraData,
			LikeCount:  cm.LikeCount,
			ReplyCount: cm.ReplyCount,
			Status:     cm.Status,
			CreateTime: cm.CreateTime.Format("2006-01-02 15:04:05"),
			Liked:      likedMap[cm.ID],
		}

		// 填充评论者信息
		if author, exists := userMap[cm.UserID]; exists {
			vo.AuthorName = author.Username
			vo.AuthorAvatar = author.AvatarURL
		}

		// 填充被回复人信息
		if cm.ReplyToUserID != nil {
			vo.ReplyToUserID = cm.ReplyToUserID
			if replyUser, exists := userMap[*cm.ReplyToUserID]; exists {
				vo.ReplyToName = replyUser.Username
			}
		}

		// 填充 @提及 用户列表（空名单给非 nil 空切片，序列化为 [] 而非 null）
		vo.Mentions = buildMentionVOs(mentionIDsMap[cm.ID], briefs)

		vos = append(vos, vo)
	}

	return vos
}

// buildMentionVOs 由提及用户ID组装 MentionVO 列表（保持提及写入顺序≈正文出现顺序）。
//
// briefs 为 GetBriefs 的返回（内部已过滤未注销用户），未命中的提及ID被静默跳过。
// 空名单返回非 nil 空切片，序列化为 [] 而非 null。
func buildMentionVOs(ids []uuid.UUID, briefs map[string]UserBrief) []MentionVO {
	mentions := make([]MentionVO, 0, len(ids))
	for _, id := range ids {
		if b, ok := briefs[id.String()]; ok {
			mentions = append(mentions, MentionVO(b))
		}
	}
	return mentions
}

// ListAutoReplyCandidates 机器人自动回复的评论候选（纯 DB 查询；like_count 由点赞链路
// 异步落库，分钟级滞后对定时扫描无影响）。
func (s *commentServiceImpl) ListAutoReplyCandidates(ctx context.Context, q AutoReplyCommentQuery) ([]AutoReplyCommentCandidate, error) {
	rows, err := s.repo.ListHotForAutoReply(ctx, domain.AutoReplyCommentFilter{
		CircleID:   q.CircleID,
		LikeMin:    q.LikeMin,
		ReplyMin:   q.ReplyMin,
		Since:      q.Since,
		ExcludeIDs: q.ExcludeIDs,
		Limit:      q.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AutoReplyCommentCandidate, 0, len(rows))
	for i := range rows {
		out = append(out, AutoReplyCommentCandidate{
			ID:           rows[i].ID,
			PostID:       rows[i].PostID,
			PostCircleID: rows[i].PostCircleID,
			UserID:       rows[i].UserID,
			RootID:       rows[i].RootID,
			Content:      rows[i].Content,
			LikeCount:    rows[i].LikeCount,
			ReplyCount:   rows[i].ReplyCount,
		})
	}
	return out, nil
}

// toStrings 批量转换 uuid 列表为字符串列表。
func toStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, id.String())
	}
	return result
}
