package application

import (
	"context"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
)

// ensurePostVisible 校验评论所属帖子对查看者可见（私密圈仅成员可见，访客视为非成员）。
//
// 不可见（含帖子不存在）返回 hidden，由调用方传入与"不存在"一致的哨兵，保证 404 不暴露帖子存在。
// 判断本身在 post 域（PostLookup.CanView → PostService.CanViewPost），本域不重复实现。
func (s *commentServiceImpl) ensurePostVisible(ctx context.Context, viewerID, postID uuid.UUID, hidden error) error {
	if s.postLookup == nil {
		return nil
	}
	visible, err := s.postLookup.CanView(ctx, viewerID, postID)
	if err != nil {
		return err
	}
	if !visible {
		return hidden
	}
	return nil
}

// getVisibleComment 取对读者可见的评论（domain.Comment.Visible，#208）。
//
// 不存在、已删除、审核中、折叠隐藏统一返回 domain.ErrCommentNotFound，调用方按 404 处理，不暴露评论存在。
func (s *commentServiceImpl) getVisibleComment(ctx context.Context, commentID uuid.UUID) (*domain.Comment, error) {
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		return nil, err
	}
	if !comment.Visible() {
		return nil, domain.ErrCommentNotFound
	}
	return comment, nil
}

// getVisibleThread 取可见评论及其楼层根评论（目标是顶层评论时 root == target）。
//
// 楼层规则：根评论不可见（不存在 / 已删除 / 审核中 / 隐藏）时整楼不可见，楼内回复同样按「评论不存在」处理，
// 与回复列表（GetReplies 先校验根评论）口径一致。
func (s *commentServiceImpl) getVisibleThread(ctx context.Context, commentID uuid.UUID) (target, root *domain.Comment, err error) {
	target, err = s.getVisibleComment(ctx, commentID)
	if err != nil {
		return nil, nil, err
	}
	if target.RootID == nil {
		return target, target, nil
	}
	root, err = s.getVisibleComment(ctx, *target.RootID)
	if err != nil {
		return nil, nil, err
	}
	return target, root, nil
}
