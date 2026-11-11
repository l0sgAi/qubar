// Package application 是 message（私信）领域的应用层：发送、会话列表、按对方查会话、消息历史、标记已读、删除会话。
//
// 文件按职责拆分：send.go（发送）、send_limit.go（未关注发送限制）、send_idempotent.go（发送幂等 client_msg_id 校验）、conversation.go（会话列表 / 按对方查会话 / 已读 / 删除）、history.go（消息历史）、
// cursor.go（两种列表的 keyset 游标）、unread.go（未读总数与实时推送，#241）。MessageService 由各文件里的小接口组合而成。
// 设计见 docs/message-design.md。
package application

import (
	"context"

	"interestBar/pkg/domains/message/domain"

	"github.com/google/uuid"
)

const (
	// MaxContentChars 正文字符上限（按 Unicode 字符计，与 PG varchar 的计数方式一致）。
	// 维护者 2026-11-11 决策：上限 2000（issue 原文为 1000）。
	MaxContentChars = 2000
	// PreviewChars 会话列表里最后一条消息摘要的字符数。
	PreviewChars = 100
	// DefaultPageSize / MaxPageSize 列表页大小：不传或 ≤0 取默认值，超过上限按上限。
	DefaultPageSize = 20
	MaxPageSize     = 50
)

// RecipientStatus 接收方能否收私信。
type RecipientStatus int

const (
	// RecipientOK 正常用户，可以收私信。
	RecipientOK RecipientStatus = iota
	// RecipientNotFound 用户不存在、已禁用或已删除。
	RecipientNotFound
	// RecipientBot AI 机器人账号：不接收私信（维护者 2026-11-11 决策）。
	RecipientBot
)

// RecipientChecker 跨域端口：判断接收方状态。由 composition 用 user 领域的 UserService 桥接
// （判定与 UserService.GetByID 一致，机器人 = role 2）。
type RecipientChecker interface {
	CheckRecipient(ctx context.Context, userID uuid.UUID) (RecipientStatus, error)
}

// UserBrief 会话列表里对方的简要信息。
type UserBrief struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
}

// UserBriefReader 跨域端口：批量读取用户简要信息。
// 只返回状态正常的用户：不存在 / 已禁用 / 已删除的 ID 不出现在 map 里。由 composition 用 UserFacade.GetBriefs 桥接。
type UserBriefReader interface {
	GetBriefs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]UserBrief, error)
}

// MessageService 是 message 领域的应用服务接口。
type MessageService interface {
	SendService
	ConversationService
	HistoryService
	UnreadService
}

type messageServiceImpl struct {
	repo       domain.Repository
	recipients RecipientChecker
	users      UserBriefReader
	// follows 关注关系查询（#239 未关注发送限制，见 send_limit.go）。
	follows FollowChecker
	// unreadCache / notifier 由 setter 注入（#241，见 unread.go），nil 时只读 DB、不推送。
	unreadCache domain.UnreadCache
	notifier    UnreadNotifier
}

// NewMessageService 构造 MessageService。
func NewMessageService(repo domain.Repository, recipients RecipientChecker, users UserBriefReader, follows FollowChecker) MessageService {
	return &messageServiceImpl{repo: repo, recipients: recipients, users: users, follows: follows}
}

// normalizePageSize ≤0 取默认值，超过上限按上限。
func normalizePageSize(size int) int {
	switch {
	case size <= 0:
		return DefaultPageSize
	case size > MaxPageSize:
		return MaxPageSize
	default:
		return size
	}
}
