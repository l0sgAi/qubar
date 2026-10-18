// Package composition 的 wire_notice.go：notice 领域装配（Service 构造 + SSE 推流 hub + 路由注册）。
package composition

import (
	"interestBar/pkg/conf"
	noticeapp "interestBar/pkg/domains/notice/application"
	noticeinfra "interestBar/pkg/domains/notice/infrastructure"
	noticehttp "interestBar/pkg/domains/notice/interfaces/http"
	"interestBar/pkg/server/storage/redpanda"
	"interestBar/pkg/shared/routing"
)

// noticeStreamHub 包级句柄：RegisterDomainRoutes 装配时设置，供 StopNoticeStreamHub 关停。
var noticeStreamHub noticeapp.StreamHub

// StopNoticeStreamHub 停止 SSE 推流 hub 的 sweeper（server 关停序列调用，幂等安全）。
func StopNoticeStreamHub() {
	if noticeStreamHub != nil {
		noticeStreamHub.Stop()
	}
}

// newNoticeService 构造 NoticeService。
//
// user 跨领域依赖（actor 批量组装）通过 setter 注入（见 RegisterDomainRoutes）。
func newNoticeService(deps *Deps) noticeapp.NoticeService {
	repo := noticeinfra.NewNotificationRepository(deps.DB.Get())
	cache := noticeinfra.NewNoticeUnreadCache()
	return noticeapp.NewNoticeService(repo, cache)
}

// newNoticeStreamHub 构造 SSE 未读数推流 hub（设计 docs/design/sse-notification-design.md §四#5）：
// 构造 → 注入 service（MarkRead/MarkAllRead 触发）→ CountReader（推送值与
// GET /notice/unread-count 同源）→ consumer hook（Redpanda flush 触发，包级函数注入）。
func newNoticeStreamHub(noticeSvc noticeapp.NoticeService) noticeapp.StreamHub {
	streamCfg := conf.Config.NoticeStream
	streamHub := noticeapp.NewStreamHub(streamCfg.MaxConnsPerUser, streamCfg.CoalesceMs)
	noticeSvc.SetStreamHub(streamHub)
	streamHub.SetCountReader(noticeSvc.GetUnreadCount)
	redpanda.SetNoticeUnreadHook(streamHub.PublishBatch)
	noticeStreamHub = streamHub
	return streamHub
}

// registerNotice 装配 notice 领域。
func registerNotice(root routing.RouterGroup, svc noticeapp.NoticeService, authCheck routing.HandlerFunc) {
	noticehttp.RegisterRoutes(root, svc, authCheck)
}
