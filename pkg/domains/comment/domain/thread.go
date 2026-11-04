package domain

import (
	"encoding/json"

	"github.com/google/uuid"
)

// 盖楼不变量（两级扁平楼）：
//   - 顶层评论：root_id / reply_to_id / reply_to_user_id 全为 NULL；
//   - 楼内回复：root_id 指向同帖、未删除的顶层评论；reply_to_id 为空（直接回复根评论）
//     或指向同帖、同楼（根评论本身或其楼内回复）、未删除的评论；
//     reply_to_user_id = 被回复评论作者（直接回复根评论时为根评论作者，回复通知依赖它）。
//
// 写入只能经 NewRootComment / NewReply 构造，service 只负责取数（#185）。

// NewCommentParams 构造评论的公共入参（内容须已在 application 层清洗）。
type NewCommentParams struct {
	PostID    uuid.UUID
	UserID    uuid.UUID
	Content   string
	ExtraData json.RawMessage
}

// NewRootComment 构造顶层评论。顶层评论不允许带 reply_to_id：
// 调用方收到 reply_to_id 却没有 root_id 时应返回 ErrReplyTargetWithoutRoot，而不是调用本函数。
func NewRootComment(p NewCommentParams) *Comment {
	return newComment(p)
}

// NewReply 构造楼内回复。
//
// root 为所属顶层评论（必填）；replyTo 为被回复评论，nil 表示直接回复根评论。
// 校验失败返回：
//   - ErrRootCommentNotFound：root 为 nil 或已删除；
//   - ErrRootCommentMismatch：root 不属于该帖子；
//   - ErrRootCommentNotTopLevel：root 本身是回复（防止生成任何列表都看不到的「三级楼」）；
//   - ErrReplyTargetNotFound：replyTo 已删除；
//   - ErrReplyTargetNotInThread：replyTo 不在该楼内或不属于该帖子。
func NewReply(p NewCommentParams, root, replyTo *Comment) (*Comment, error) {
	if root == nil || root.Deleted != 0 {
		return nil, ErrRootCommentNotFound
	}
	if root.PostID != p.PostID {
		return nil, ErrRootCommentMismatch
	}
	if root.RootID != nil {
		return nil, ErrRootCommentNotTopLevel
	}

	c := newComment(p)
	rootID := root.ID
	c.RootID = &rootID

	if replyTo == nil {
		// 直接回复根评论：被回复人即根评论作者。不置 nil 而解析出作者，回复通知
		// （reply_comment）才有接收人，否则通知被静默丢弃（docs/notice-design.md）。
		uid := root.UserID
		c.ReplyToUserID = &uid
		return c, nil
	}

	if replyTo.Deleted != 0 {
		return nil, ErrReplyTargetNotFound
	}
	// 被回复评论必须在同一楼内：要么就是根评论本身，要么其 root_id 等于根评论 ID；且同帖。
	inThread := replyTo.ID == root.ID || (replyTo.RootID != nil && *replyTo.RootID == root.ID)
	if !inThread || replyTo.PostID != p.PostID {
		return nil, ErrReplyTargetNotInThread
	}
	replyToID, uid := replyTo.ID, replyTo.UserID
	c.ReplyToID = &replyToID
	c.ReplyToUserID = &uid
	return c, nil
}

// newComment 构造字段公共部分（正常状态、未删除）。
func newComment(p NewCommentParams) *Comment {
	return &Comment{
		PostID:    p.PostID,
		UserID:    p.UserID,
		Content:   p.Content,
		ExtraData: p.ExtraData,
		Status:    CommentStatusNormal,
		Deleted:   0,
	}
}
