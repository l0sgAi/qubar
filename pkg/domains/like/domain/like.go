// Package domain 存放 like 领域的纯领域模型。
//
// like 领域是横跨 post 和 comment 两个聚合的"点赞"用例聚合点。
// 它本身不持有独立的聚合根表（PostLike/CommentLike 表分别属于 post/comment 领域），
// 但统一管理"点赞/取消点赞"这个原子操作 + 异步事件发布。
package domain

import (
	"errors"
)

// TargetType 点赞目标类型。
type TargetType string

const (
	// TargetTypeComment 评论点赞。
	TargetTypeComment TargetType = "comment"
	// TargetTypePost 帖子点赞。
	TargetTypePost TargetType = "post"
)

// ToggleResult 点赞设值结果（与 redispkg.LikeSetResult 值一致）。
type ToggleResult int

const (
	// ToggleResultLiked 点赞成功（+1）。
	ToggleResultLiked ToggleResult = 1
	// ToggleResultUnliked 取消点赞（-1）。
	ToggleResultUnliked ToggleResult = -1
	// ToggleResultUnchanged 已处于期望状态，未变化（不发任何事件）。
	ToggleResultUnchanged ToggleResult = 0
)

// Int64 返回 ToggleResult 的 int64 值（用于事件发布的 amount 字段）。
func (r ToggleResult) Int64() int64 { return int64(r) }

// 哨兵错误。
var (
	// ErrPostNotFound 帖子未找到。
	ErrPostNotFound = errors.New("post not found")
	// ErrCommentNotFound 评论未找到。
	ErrCommentNotFound = errors.New("comment not found")
	// ErrInvalidTargetType 无效的点赞目标类型。
	ErrInvalidTargetType = errors.New("invalid target type, must be 'comment' or 'post'")
)
