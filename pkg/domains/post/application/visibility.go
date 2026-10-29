package application

import (
	"context"
	"errors"

	"interestBar/pkg/domains/post/domain"

	"github.com/google/uuid"
)

// CircleVisibility 私密圈内容可见性端口（composition 注入 circle.application.CircleVisibility）。
//
// 帖子对查看者可见 ⇔ 所属圈子不是私密圈，或查看者是该圈正常 / 禁言成员；访客一律视为非成员。
type CircleVisibility interface {
	// CanView 查看者能否看到该圈内容。circleID 为 uuid.Nil 视为可见。
	CanView(ctx context.Context, viewerID, circleID uuid.UUID) (bool, error)
}

// SetCircleVisibility 注入私密圈可见性端口（未注入时不做私密圈过滤，仅单测场景）。
func (s *postServiceImpl) SetCircleVisibility(v CircleVisibility) { s.visibility = v }

// canViewCircle 查看者能否看到该圈的帖子（post 域内所有可见性判断的唯一入口）。
func (s *postServiceImpl) canViewCircle(ctx context.Context, viewerID, circleID uuid.UUID) (bool, error) {
	if s.visibility == nil {
		return true, nil
	}
	return s.visibility.CanView(ctx, viewerID, circleID)
}

// CanViewPost 查看者能否看到该帖子所属圈子的内容（实现 PostService）。
//
// 只判断私密圈可见性，不判断帖子状态（状态由各调用方按自身语义校验）。帖子不存在返回 false。
func (s *postServiceImpl) CanViewPost(ctx context.Context, viewerID, postID uuid.UUID) (bool, error) {
	post, err := s.repo.GetByID(ctx, postID)
	if errors.Is(err, domain.ErrPostNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.canViewCircle(ctx, viewerID, post.CircleID)
}
