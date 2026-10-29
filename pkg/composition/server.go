// Package composition 的 server.go：按固定顺序装配各领域并注册路由。
//
// 本文件只保留"构造顺序 + 跨领域 setter 注入 + 路由注册顺序"三张清单；
// 各领域的 Service 构造 / 路由注册在 wire_<domain>.go，桥接器在 <domain>_bridges.go。
package composition

import (
	"interestBar/pkg/conf"
	agentapp "interestBar/pkg/domains/aiagent/application"
	categoryinfra "interestBar/pkg/domains/category/infrastructure"
	circleapp "interestBar/pkg/domains/circle/application"
	circleinfra "interestBar/pkg/domains/circle/infrastructure"
	noticeapp "interestBar/pkg/domains/notice/application"
	userapp "interestBar/pkg/domains/user/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/redpanda"
	"interestBar/pkg/shared/routing"
)

// RegisterDomainRoutes 把所有"已搬迁到 domains/"的领域路由挂到 Web server 上。
//
// root 是框架无关的 RouterGroup（由入口层用 composition/hertzadapter.ForEngine
// 从 *server.Hertz 包装而来）。这样本函数彻底不感知底层框架。
//
// 返回 SSE 未读推流 hub（nil 不可用），供路由层注册裸 hertz 的 /notice/stream
// （SSE 需 hijack writer，不走 AppContext 抽象）。
func RegisterDomainRoutes(root routing.RouterGroup) noticeapp.StreamHub {
	deps := NewDeps()
	authCheck := RequireLogin

	// 先构造各领域 Service（不立即注册路由，因为要互注 Facade）
	userSvc := newUserService(deps)
	userFacade := userapp.NewUserFacade(userSvc)

	circleSvc, circleRepo, memberRepo := newCircleService(deps)
	circleFacade := circleapp.NewCircleFacade(circleRepo)
	circleVisibility := circleapp.NewCircleVisibility(circleRepo, memberRepo, circleinfra.NewPrivateCircleCache())

	postSvc := newPostService(deps)

	commentSvc := newCommentService(deps)
	likeSvc := newLikeService(deps)
	collectSvc := newCollectService(deps)
	historySvc := newHistoryService(deps)
	noticeSvc := newNoticeService(deps)

	// 互注跨领域 Facade
	// circle 需要 user Facade + post 媒体查询器
	circleSvc.SetUserFacade(&circleUserFacade{delegate: userFacade})
	circleSvc.SetPostFetcher(&postMediaFetcherForCircle{delegate: postSvc})

	// post 需要 user Facade + circle Facade + 成员/状态校验器 + 帖子计数端口
	postSvc.SetUserFacade(&postUserFacade{delegate: userFacade})
	postSvc.SetCircleFacade(&postCircleFacade{delegate: circleFacade})
	postSvc.SetMemberChecker(&circleMemberCheckerForPost{memberRepo: memberRepo})
	postSvc.SetStatusChecker(&circleStatusCheckerForPost{circleRepo: circleRepo})
	postSvc.SetPostCountPort(&circlePostCountPortForPost{svc: circleSvc})
	// post 统一判断私密圈可见性（comment / like / collect 经 CanViewPost 复用，见 post_bridges.go）
	postSvc.SetCircleVisibility(circleVisibility)
	postSvc.SetModerationCircleReader(&postModerationCircleReader{
		circleRepo:   circleRepo,
		categoryRepo: categoryinfra.NewCategoryRepository(deps.DB.Get()),
	})

	// comment 需要 user Facade + post 查询端口（帖子校验 + 评论计数）
	commentSvc.SetUserFacade(&commentUserFacade{delegate: userFacade})
	commentSvc.SetPostLookup(&commentPostLookup{delegate: postSvc})

	// like 需要 post 查询端口 + comment 查询端口（目标存在性校验 + 统计缓存恢复）
	likeSvc.SetPostTarget(&likePostTarget{delegate: postSvc})
	likeSvc.SetCommentTarget(&likeCommentTarget{delegate: commentSvc})

	// collect 需要 post 查询端口（存在性校验 + 统计缓存恢复）+ post 组装端口（「我的收藏」列表）
	collectSvc.SetPostTarget(&collectPostTarget{delegate: postSvc})
	collectSvc.SetPostFetcher(&collectPostFetcher{delegate: postSvc})

	// history 需要 post 组装端口（「最近浏览」列表 ES 查询）;post 需要 history 记录器（详情页 async 回调）
	historySvc.SetPostFetcher(&historyPostFetcher{delegate: postSvc})
	postSvc.SetHistoryRecorder(&postHistoryRecorder{delegate: historySvc})

	// notice 需要 user Facade（通知列表 actor 批量组装）+ post Facade（审核类通知 circle_id 回填）
	noticeSvc.SetUserFacade(&noticeUserFacade{delegate: userFacade})
	noticeSvc.SetPostFacade(&noticePostFacade{delegate: postSvc})

	// SSE 未读数推流 hub：注入 notice service + Redpanda consumer hook（见 wire_notice.go）。
	streamHub := newNoticeStreamHub(noticeSvc)

	// 跨领域 Facade 注入完成。如遗漏注入，相关领域会在请求时表现为空数据/校验失败，
	// 这里打一条启动日志便于排查（强类型断言成本过高，用日志替代 panic，见 review P2-2）。
	if logger.Log != nil {
		logger.Log.Info("cross-domain facades injected: circle<-user/post, post<-user/circle, comment<-user/post, like<-post/comment, collect<-post, history<-post, notice<-user/post")
	}

	// recommend 需要 post（hydrate + circle_id 反查）+ circle（joined IDs），均为只读消费者，无反向注入。
	recommendSvc := newRecommendService(postSvc, circleSvc)

	// trending 需要 post（hydrate + 交互态）+ circle（GetByIDs）+ user（GetBriefs），均为只读消费者。
	trendingSvc := newTrendingService(postSvc, circleRepo, userFacade)

	// discover 需要 post（hydrate + 交互态）+ circle（GetByIDs + joined IDs）+ seed（反气泡已交互帖），
	// 均为只读消费者。syncer 复用其 RebuildPool（反气泡重建逻辑）。
	discoverSvc := newDiscoverService(postSvc, circleRepo, circleSvc)

	// aiagent 跨域依赖：role 读取（user 缓存）+ 机器人账号创建（role=2）。
	// 全局/圈内两个 Service 共享同一仓储实例（无状态薄封装）。
	agentRepo, agentSvc := newAgentService(deps)
	agentSvc.SetRoleReader(&agentRoleReader{delegate: userSvc})
	agentSvc.SetBotUserCreator(&agentBotUserCreator{db: deps.DB.Get()})
	agentSvc.SetBotUserProfileUpdater(&agentBotUserUpdater{delegate: userSvc})
	agentSvc.SetBotUserMentionSyncer(&agentBotUserMentionSyncer{delegate: userSvc})

	// aiagent 圈内机器人管理：圈内角色读取（circle Facade 直查 member，权限即时生效）
	// + 机器人账号创建/资料同步/可 @ 状态投影（复用全局端口桥接器）。
	circleAgentSvc := agentapp.NewCircleAgentService(agentRepo)
	circleAgentSvc.SetCircleRoleReader(&circleRoleReaderForAgent{
		delegate: circleapp.NewCircleMemberRoleReader(memberRepo),
	})
	circleAgentSvc.SetBotUserCreator(&agentBotUserCreator{db: deps.DB.Get()})
	circleAgentSvc.SetBotUserProfileUpdater(&agentBotUserUpdater{delegate: userSvc})
	circleAgentSvc.SetBotUserMentionSyncer(&agentBotUserMentionSyncer{delegate: userSvc})

	// circle -> aiagent：可管理圈子列表的 agent_count 回填（方向反转桥接，失败降级 0）。
	circleSvc.SetAgentCounter(&circleAgentCounterForCircle{repo: agentRepo})

	// aiagent 回复执行链路：LLM(eino) + 帖子摘要(post) + 评论创建(comment)。
	// 触发链按帖子所在圈收口候选集（全局机器人 + 本圈机器人，circle-agent-reply）；
	// 圈内手动触发需圈主鉴权（复用 circleRoleReaderForAgent 桥）。
	replySvc := newAgentReplyService(deps, postSvc, commentSvc)
	replySvc.SetRoleReader(&agentRoleReader{delegate: userSvc})
	replySvc.SetCircleRoleReader(&circleRoleReaderForAgent{
		delegate: circleapp.NewCircleMemberRoleReader(memberRepo),
	})
	// comment -> aiagent：评论创建后触发关键词机器人（同步回调、内部异步执行）。
	commentSvc.SetAgentTrigger(&commentAgentTrigger{delegate: replySvc})
	// post -> aiagent：发帖 @机器人 触发回复（同步回调、内部异步执行）。
	postSvc.SetAgentTrigger(&postAgentTrigger{delegate: replySvc})

	// 注册路由
	registerCategory(root, deps, authCheck, OptionalLoginFn)
	registerStorage(root, deps, authCheck)
	registerUser(root, userSvc, authCheck, OptionalLoginFn)
	registerAuth(root, deps, authCheck)
	registerCircle(root, circleSvc, authCheck, OptionalLoginFn)
	registerPost(root, postSvc, authCheck, OptionalLoginFn)
	registerComment(root, commentSvc, authCheck, OptionalLoginFn)
	registerLike(root, likeSvc, authCheck)
	registerCollect(root, collectSvc, authCheck)
	registerHistory(root, historySvc, authCheck)
	registerNotice(root, noticeSvc, authCheck)
	registerRecommend(root, recommendSvc, authCheck, OptionalLoginFn)
	registerTrending(root, trendingSvc, authCheck, OptionalLoginFn)
	registerDiscover(root, discoverSvc, authCheck)
	registerAgent(root, agentSvc, circleAgentSvc, replySvc, authCheck)

	// 启动 Discover pool syncer（需要 discoverSvc 复用 RebuildPool；其它无依赖 syncer 在 apps/server.go）。
	go redpanda.StartDiscoverSyncerWithRetry(discoverSvc)

	// 启动机器人自动回复扫描器（需要 replySvc 执行扫描；aiagent.autonomy.enabled=false 时不启动）。
	go redpanda.StartAgentAutoReplySyncerWithRetry(replySvc)

	// 启动帖子 AI 审核 worker（#117）：仅 moderation 启用时；未启用时发帖直接发布，不需要 worker。
	if conf.Config.Moderation.Active() {
		redpanda.StartPostModerationConsumer(postSvc.ModeratePost)
	} else {
		logger.Log.Info("Post moderation worker disabled (moderation.enabled=false or api_key empty)")
	}

	return streamHub
}
