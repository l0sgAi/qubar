// Package application 提供 notice 领域的应用服务层。
//
// 职责：
//   - 通知列表（DB keyset 游标分页 + actor 批量回填）
//   - 未读数（Redis 计数器，miss 回源 DB）
//   - 标记已读（单批/全部 + 计数器校正）
//
// 写路径不在本层：通知由 notification_events Redpanda consumer 落库。
package application

import (
	"context"
	"time"

	"interestBar/pkg/domains/notice/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// UserBrief 与 user.application.UserBrief 字段一致，独立定义避免跨领域 import。
type UserBrief struct {
	ID        string
	Username  string
	AvatarURL string
}

// UserFacade notice 领域需要的 user 查询接口（actor 展示信息批量回填）。
type UserFacade interface {
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
}

// ===== DTO =====

// ActorBrief 触发人展示信息（列表项内嵌）。
type ActorBrief struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
}

// NoticeItem 通知列表项（供 handler 序列化）。
type NoticeItem struct {
	ID         string      `json:"id"`
	NoticeType int16       `json:"notice_type"`
	Actor      *ActorBrief `json:"actor,omitempty"`
	PostID     string      `json:"post_id,omitempty"`
	CommentID  string      `json:"comment_id,omitempty"`
	Snippet    string      `json:"snippet"`
	IsRead     bool        `json:"is_read"`
	CreateTime time.Time   `json:"create_time"`
}

// NoticeListResult 通知列表结果。
type NoticeListResult struct {
	Notices []NoticeItem `json:"notices"`
	Size    int          `json:"size"`
	Cursor  string       `json:"cursor"` // 下一页游标，无更多为 ""
}

// ===== Service 接口 =====

// NoticeService 是 notice 领域的应用服务接口。
type NoticeService interface {
	// ListNotifications 获取当前用户通知列表（keyset 游标分页，id DESC）。
	// noticeTypes 为空=全部；非空按类型集合过滤（如「赞」tab 传 [1,2]）。
	ListNotifications(ctx context.Context, userID uuid.UUID, noticeTypes []int16, size int, cursor string) (*NoticeListResult, error)
	// GetUnreadCount 获取当前用户未读通知数。
	GetUnreadCount(ctx context.Context, userID uuid.UUID) (int64, error)
	// MarkRead 批量标记已读（仅本人通知）。
	MarkRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error
	// MarkAllRead 全部标记已读。
	MarkAllRead(ctx context.Context, userID uuid.UUID) error

	// SetUserFacade 注入 user Facade（actor 展示信息回填用）。
	SetUserFacade(f UserFacade)
	// SetStreamHub 注入未读数推送 hub（MarkRead/MarkAllRead 成功后触发 SSE 推送）。
	// 为 nil 时跳过推送（保测试兼容）。
	SetStreamHub(h StreamHub)
}

type noticeServiceImpl struct {
	repo       domain.NotificationRepository
	cache      domain.NoticeUnreadCache
	userFacade UserFacade
	hub        StreamHub
}

// NewNoticeService 构造 NoticeService。
//
// userFacade 是跨领域依赖，通过 setter 注入（composition 层负责连接）。
func NewNoticeService(repo domain.NotificationRepository, cache domain.NoticeUnreadCache) NoticeService {
	return &noticeServiceImpl{repo: repo, cache: cache}
}

// SetUserFacade 注入 user Facade。
func (s *noticeServiceImpl) SetUserFacade(f UserFacade) { s.userFacade = f }

// SetStreamHub 注入未读数推送 hub。
func (s *noticeServiceImpl) SetStreamHub(h StreamHub) { s.hub = h }

// ListNotifications 获取当前用户通知列表。
func (s *noticeServiceImpl) ListNotifications(ctx context.Context, userID uuid.UUID, noticeTypes []int16, size int, cursor string) (*NoticeListResult, error) {
	for _, t := range noticeTypes {
		if t < domain.NoticeTypeLikePost || t > domain.NoticeTypeMax {
			return nil, errInvalidNoticeType
		}
	}
	if size <= 0 || size > 100 {
		size = 20
	}

	notices, nextCursor, err := s.repo.ListByCursor(ctx, userID, noticeTypes, size, cursor)
	if err != nil {
		return nil, err
	}

	briefs := s.loadActorBriefs(ctx, notices)

	items := make([]NoticeItem, 0, len(notices))
	for _, n := range notices {
		item := NoticeItem{
			ID:         n.ID.String(),
			NoticeType: n.NoticeType,
			Snippet:    n.Snippet,
			IsRead:     n.IsRead == domain.NoticeRead,
			CreateTime: n.CreateTime,
		}
		if n.PostID != nil {
			item.PostID = n.PostID.String()
		}
		if n.CommentID != nil {
			item.CommentID = n.CommentID.String()
		}
		if b, ok := briefs[n.ActorID.String()]; ok && !domain.HidesActor(n.NoticeType) {
			item.Actor = &ActorBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
		}
		items = append(items, item)
	}

	return &NoticeListResult{
		Notices: items,
		Size:    len(items),
		Cursor:  nextCursor,
	}, nil
}

// loadActorBriefs 批量回填 actor 展示信息（失败降级为空 map，不阻断列表）。
func (s *noticeServiceImpl) loadActorBriefs(ctx context.Context, notices []domain.Notification) map[string]UserBrief {
	if s.userFacade == nil || len(notices) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(notices))
	actorIDs := make([]string, 0, len(notices))
	for _, n := range notices {
		if _, ok := seen[n.ActorID]; !ok {
			seen[n.ActorID] = struct{}{}
			actorIDs = append(actorIDs, n.ActorID.String())
		}
	}
	briefs, err := s.userFacade.GetBriefs(ctx, actorIDs)
	if err != nil {
		logger.Log.Error("Failed to batch get notice actor briefs: " + err.Error())
		return nil
	}
	return briefs
}

// GetUnreadCount 获取当前用户未读通知数（缓存优先，miss 回源 DB + 回填）。
func (s *noticeServiceImpl) GetUnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	if count, ok, err := s.cache.Get(ctx, userID); err != nil {
		logger.Log.Error("Failed to get notice unread count from cache: " + err.Error())
	} else if ok {
		return count, nil
	}

	count, err := s.repo.CountUnread(ctx, userID)
	if err != nil {
		return 0, err
	}
	if err := s.cache.Set(ctx, userID, count); err != nil {
		logger.Log.Error("Failed to backfill notice unread count: " + err.Error())
	}
	return count, nil
}

// MarkRead 批量标记已读。计数器按实际更新行数扣减。
func (s *noticeServiceImpl) MarkRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return errEmptyNoticeIDs
	}
	affected, err := s.repo.MarkRead(ctx, userID, ids)
	if err != nil {
		return err
	}
	if affected > 0 {
		if err := s.cache.DecrBy(ctx, userID, affected); err != nil {
			logger.Log.Error("Failed to decr notice unread count: " + err.Error())
		} else if s.hub != nil {
			s.hub.Publish(userID)
		}
	}
	return nil
}

// MarkAllRead 全部标记已读。计数器直接置 0。
func (s *noticeServiceImpl) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	if err := s.repo.MarkAllRead(ctx, userID); err != nil {
		return err
	}
	if err := s.cache.Set(ctx, userID, 0); err != nil {
		logger.Log.Error("Failed to reset notice unread count: " + err.Error())
	} else if s.hub != nil {
		s.hub.Publish(userID)
	}
	return nil
}
