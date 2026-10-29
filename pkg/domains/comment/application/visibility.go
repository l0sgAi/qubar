package application

import (
	"context"

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
