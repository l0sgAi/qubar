// Package composition 的 server.go：装配各领域依赖并注册路由。
package composition

import (
	"interestBar/pkg/conf"
	agentapp "interestBar/pkg/domains/aiagent/application"
	agentinfra "interestBar/pkg/domains/aiagent/infrastructure"
	agenthttp "interestBar/pkg/domains/aiagent/interfaces/http"
	authapp "interestBar/pkg/domains/auth/application"
	authinfra "interestBar/pkg/domains/auth/infrastructure"
	authhttp "interestBar/pkg/domains/auth/interfaces/http"
	categoryapp "interestBar/pkg/domains/category/application"
	categoryinfra "interestBar/pkg/domains/category/infrastructure"
	categoryhttp "interestBar/pkg/domains/category/interfaces/http"
	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	circleinfra "interestBar/pkg/domains/circle/infrastructure"
	circlehttp "interestBar/pkg/domains/circle/interfaces/http"
	collectapp "interestBar/pkg/domains/collect/application"
	collectinfra "interestBar/pkg/domains/collect/infrastructure"
	collecthttp "interestBar/pkg/domains/collect/interfaces/http"
	commentapp "interestBar/pkg/domains/comment/application"
	commentinfra "interestBar/pkg/domains/comment/infrastructure"
	commenthttp "interestBar/pkg/domains/comment/interfaces/http"
	discoverapp "interestBar/pkg/domains/discover/application"
	discoverinfra "interestBar/pkg/domains/discover/infrastructure"
	discoverhttp "interestBar/pkg/domains/discover/interfaces/http"
	historyapp "interestBar/pkg/domains/history/application"
	historyinfra "interestBar/pkg/domains/history/infrastructure"
	historyhttp "interestBar/pkg/domains/history/interfaces/http"
	likeapp "interestBar/pkg/domains/like/application"
	likeinfra "interestBar/pkg/domains/like/infrastructure"
	likehttp "interestBar/pkg/domains/like/interfaces/http"
	noticeapp "interestBar/pkg/domains/notice/application"
	noticeinfra "interestBar/pkg/domains/notice/infrastructure"
	noticehttp "interestBar/pkg/domains/notice/interfaces/http"
	postapp "interestBar/pkg/domains/post/application"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	posthttp "interestBar/pkg/domains/post/interfaces/http"
	recommendapp "interestBar/pkg/domains/recommend/application"
	recommendinfra "interestBar/pkg/domains/recommend/infrastructure"
	recommendhttp "interestBar/pkg/domains/recommend/interfaces/http"
	storageapp "interestBar/pkg/domains/storage/application"
	storageinfra "interestBar/pkg/domains/storage/infrastructure"
	storagehttp "interestBar/pkg/domains/storage/interfaces/http"
	trendingapp "interestBar/pkg/domains/trending/application"
	trendinginfra "interestBar/pkg/domains/trending/infrastructure"
	trendinghttp "interestBar/pkg/domains/trending/interfaces/http"
	userapp "interestBar/pkg/domains/user/application"
	userinfra "interestBar/pkg/domains/user/infrastructure"
	userhttp "interestBar/pkg/domains/user/interfaces/http"
	"interestBar/pkg/logger"
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

	// notice 需要 user Facade（通知列表 actor 批量组装）
	noticeSvc.SetUserFacade(&noticeUserFacade{delegate: userFacade})

	// SSE 未读数推流 hub（设计 docs/design/sse-notification-design.md §四#5）：
	// 构造 → 注入 service（MarkRead/MarkAllRead 触发）→ CountReader（推送值与
	// GET /notice/unread-count 同源）→ consumer hook（Redpanda flush 触发，包级函数注入）。
	streamCfg := conf.Config.NoticeStream
	streamHub := noticeapp.NewStreamHub(streamCfg.MaxConnsPerUser, streamCfg.CoalesceMs)
	noticeSvc.SetStreamHub(streamHub)
	streamHub.SetCountReader(noticeSvc.GetUnreadCount)
	redpanda.SetNoticeUnreadHook(streamHub.PublishBatch)
	noticeStreamHub = streamHub

	// 跨领域 Facade 注入完成。如遗漏注入，相关领域会在请求时表现为空数据/校验失败，
	// 这里打一条启动日志便于排查（强类型断言成本过高，用日志替代 panic，见 review P2-2）。
	if logger.Log != nil {
		logger.Log.Info("cross-domain facades injected: circle<-user/post, post<-user/circle, comment<-user/post, like<-post/comment, collect<-post, history<-post")
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
	agentRepo := agentinfra.NewAgentRepository(deps.DB.Get())
	agentSvc := agentapp.NewAgentService(agentRepo)
	agentSvc.SetRoleReader(&agentRoleReader{delegate: userSvc})
	agentSvc.SetBotUserCreator(&agentBotUserCreator{db: deps.DB.Get()})
	agentSvc.SetBotUserProfileUpdater(&agentBotUserUpdater{delegate: userSvc})
	agentSvc.SetBotUserScopeCleaner(&agentBotUserScopeCleaner{delegate: userSvc})

	// aiagent 圈内机器人管理：圈内角色读取（circle Facade 直查 member，权限即时生效）
	// + 机器人账号创建/资料同步/圈子绑定清理（复用全局端口桥接器）。
	circleAgentSvc := agentapp.NewCircleAgentService(agentRepo)
	circleAgentSvc.SetCircleRoleReader(&circleRoleReaderForAgent{
		delegate: circleapp.NewCircleMemberRoleReader(memberRepo),
	})
	circleAgentSvc.SetBotUserCreator(&agentBotUserCreator{db: deps.DB.Get()})
	circleAgentSvc.SetBotUserProfileUpdater(&agentBotUserUpdater{delegate: userSvc})
	circleAgentSvc.SetBotUserScopeCleaner(&agentBotUserScopeCleaner{delegate: userSvc})

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

	return streamHub
}

// registerCategory 装配 category 领域。
func registerCategory(root routing.RouterGroup, deps *Deps, authCheck, optionalCheck routing.HandlerFunc) {
	repo := categoryinfra.NewCategoryRepository(deps.DB.Get())
	cache := categoryinfra.NewCategoryCache()
	svc := categoryapp.NewCategoryService(repo, cache)
	categoryhttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}

// registerStorage 装配 storage 领域。
func registerStorage(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	storage := storageinfra.NewObjectStorage()
	svc := storageapp.NewStorageService(storage)
	storagehttp.RegisterRoutes(root, svc, authCheck)
}

// registerUser 装配 user 领域。
func registerUser(root routing.RouterGroup, svc userapp.UserService, authCheck, optionalCheck routing.HandlerFunc) {
	userhttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}

// registerAuth 装配 auth 领域。
func registerAuth(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	session := authinfra.NewSaTokenSession()
	userStore := NewUserSessionStore(deps.DB.Get())
	verify := authinfra.NewVerificationStore()
	email := authinfra.NewEmailSender()
	oauthReg := authinfra.NewOAuthProviderRegistry()
	pwdReset := authinfra.NewPasswordResetStore()
	svc := authapp.NewAuthService(session, userStore, verify, email, oauthReg, pwdReset)
	authhttp.RegisterRoutes(root, svc, authCheck)
}

// registerCircle 装配 circle 领域。
func registerCircle(root routing.RouterGroup, svc circleapp.CircleService, authCheck, optionalCheck routing.HandlerFunc) {
	circlehttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}

// registerPost 装配 post 领域。
func registerPost(root routing.RouterGroup, svc postapp.PostService, authCheck, optionalCheck routing.HandlerFunc) {
	posthttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}

// registerComment 装配 comment 领域。
func registerComment(root routing.RouterGroup, svc commentapp.CommentService, authCheck, optionalCheck routing.HandlerFunc) {
	commenthttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}

// registerLike 装配 like 领域。
func registerLike(root routing.RouterGroup, svc likeapp.LikeService, authCheck routing.HandlerFunc) {
	likehttp.RegisterRoutes(root, svc, authCheck)
}

// registerCollect 装配 collect 领域。
func registerCollect(root routing.RouterGroup, svc collectapp.CollectService, authCheck routing.HandlerFunc) {
	collecthttp.RegisterRoutes(root, svc, authCheck)
}

// registerHistory 装配 history 领域。
func registerHistory(root routing.RouterGroup, svc historyapp.HistoryService, authCheck routing.HandlerFunc) {
	historyhttp.RegisterRoutes(root, svc, authCheck)
}

// registerNotice 装配 notice 领域。
func registerNotice(root routing.RouterGroup, svc noticeapp.NoticeService, authCheck routing.HandlerFunc) {
	noticehttp.RegisterRoutes(root, svc, authCheck)
}

// newNoticeService 构造 NoticeService。
//
// user 跨领域依赖（actor 批量组装）通过 setter 注入（见 RegisterDomainRoutes）。
func newNoticeService(deps *Deps) noticeapp.NoticeService {
	repo := noticeinfra.NewNotificationRepository(deps.DB.Get())
	cache := noticeinfra.NewNoticeUnreadCache()
	return noticeapp.NewNoticeService(repo, cache)
}

// newRecommendService 构造 RecommendService。
//
// searcher/seed/feed 为 recommend 同域 infra（直构，走全局 ES/Redis 客户端）；
// circle/postMeta/hydrator/checker 为跨域桥接器（包 post/circle service）。
func newRecommendService(postSvc postapp.PostService, circleSvc circleapp.CircleService) recommendapp.RecommendService {
	return recommendapp.NewRecommendService(
		recommendinfra.NewHomeFeedSearcher(),        // HomeFeedSearcher
		&recommendCircleLookup{delegate: circleSvc}, // CircleLookup
		&recommendPostMetaReader{delegate: postSvc}, // PostMetaReader
		recommendinfra.NewSeedReader(),              // SeedReader
		&recommendPostHydrator{delegate: postSvc},   // PostHydrator
		&postInteractionChecker{delegate: postSvc},  // InteractionChecker（缓存 + DB 回源）
		recommendinfra.NewFeedCache(),               // FeedCache
		recommendinfra.NewInterestCircleCache(),     // InterestCircleCache
	)
}

// registerRecommend 装配 recommend 领域。
func registerRecommend(root routing.RouterGroup, svc recommendapp.RecommendService, authCheck, optionalCheck routing.HandlerFunc) {
	recommendhttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}

// newTrendingService 构造 TrendingService。
//
// boardStore 为 trending 同域 infra（直构，走全局 Redis 客户端）；
// hydrator/checker/circle/user 为跨域桥接器（包 post/circle/user service）。
func newTrendingService(postSvc postapp.PostService, circleRepo circledomain.CircleRepository, userFacade userapp.UserFacade) trendingapp.TrendingService {
	return trendingapp.NewTrendingService(
		trendinginfra.NewBoardStore(),              // BoardStore
		&trendingPostHydrator{delegate: postSvc},   // PostHydrator
		&postInteractionChecker{delegate: postSvc}, // InteractionChecker（缓存 + DB 回源）
		&trendingCircleLookup{repo: circleRepo},    // CircleLookup
		&trendingUserLookup{delegate: userFacade},  // UserLookup
	)
}

// registerTrending 装配 trending 领域。
func registerTrending(root routing.RouterGroup, svc trendingapp.TrendingService, authCheck, optionalCheck routing.HandlerFunc) {
	trendinghttp.RegisterRoutes(root, svc, authCheck, optionalCheck)
}

// newDiscoverService 构造 DiscoverService。
//
// pool 为 discover 同域 infra（直构，走全局 Redis 客户端）；
// hydrator/checker/circle/seed/joinedCircles 为跨域桥接器（包 post/circle service + redispkg）。
func newDiscoverService(postSvc postapp.PostService, circleRepo circledomain.CircleRepository, circleSvc circleapp.CircleService) discoverapp.DiscoverService {
	return discoverapp.NewDiscoverService(
		discoverinfra.NewDiscoverPoolStore(),             // DiscoverPoolStore
		&discoverPostHydrator{delegate: postSvc},         // PostHydrator（复用 trending/recommend 同款桥接）
		&postInteractionChecker{delegate: postSvc},       // InteractionChecker（缓存 + DB 回源，同 recommend/trending）
		&discoverCircleLookup{repo: circleRepo},          // CircleLookup（复用 trending 同款桥接）
		&discoverSeedReader{},                            // SeedReader（直接调 redispkg，同 recommend infra）
		&discoverJoinedCircleLookup{delegate: circleSvc}, // JoinedCircleLookup（复用 recommend 同款桥接）
	)
}

// registerDiscover 装配 discover 领域。
//
// discover 允许匿名访问（新用户落地页场景）：登录→反气泡个性化，匿名→纯随机退化。
// 故 authCheck 用 OptionalLogin（有 token 解析、无/坏 token 放行），而非全局 RequireLogin。
func registerDiscover(root routing.RouterGroup, svc discoverapp.DiscoverService, _ /*authCheck*/ routing.HandlerFunc) {
	discoverhttp.RegisterRoutes(root, svc, OptionalLoginFn)
}

// registerAgent 装配 aiagent 领域（全局机器人 CRUD + 圈子级机器人 CRUD + 手动触发回复；
// role=1 / 圈内 admin+ 校验都在 service 层）。
func registerAgent(
	root routing.RouterGroup,
	svc agentapp.AgentService,
	circleAgentSvc agentapp.CircleAgentService,
	replySvc agentapp.ReplyService,
	authCheck routing.HandlerFunc,
) {
	agenthttp.RegisterRoutes(root, svc, circleAgentSvc, replySvc, authCheck)
}

// ===== Service 构造函数 =====

func newUserService(deps *Deps) userapp.UserService {
	repo := userinfra.NewUserRepository(deps.DB.Get())
	cache := userinfra.NewUserCache()
	searcher := userinfra.NewUserSearcher()
	return userapp.NewUserService(repo, cache, searcher)
}

// newCircleService 构造 CircleService 并返回其依赖的 repo（供桥接器使用）。
func newCircleService(deps *Deps) (circleapp.CircleService, circledomain.CircleRepository, circledomain.MemberRepository) {
	circleRepo := circleinfra.NewCircleRepository(deps.DB.Get())
	memberRepo := circleinfra.NewMemberRepository(deps.DB.Get())
	baseCache := circleinfra.NewCircleBaseCache()
	statsCache := circleinfra.NewCircleStatsCache()
	joinedCache := circleinfra.NewJoinedCirclesCache()
	searcher := circleinfra.NewCircleSearcher()
	publisher := circleinfra.NewCircleEventPublisher()

	svc := circleapp.NewCircleService(
		circleRepo, memberRepo, baseCache, statsCache, joinedCache, searcher, publisher,
	)
	return svc, circleRepo, memberRepo
}

// newAgentReplyService 构造机器人回复执行服务（eino LLM + post/comment 跨域桥接，
// 桥接器见 facade_bridges.go；role 读取 setter 注入见 RegisterDomainRoutes）。
func newAgentReplyService(deps *Deps, postSvc postapp.PostService, commentSvc commentapp.CommentService) agentapp.ReplyService {
	agentRepo := agentinfra.NewAgentRepository(deps.DB.Get())
	replyLogRepo := agentinfra.NewReplyLogRepository(deps.DB.Get())
	llm := agentinfra.NewLLMCaller()
	svc := agentapp.NewReplyService(agentRepo, replyLogRepo, llm)
	svc.SetPostReader(&agentPostReader{delegate: postSvc})
	svc.SetCommentCreator(&agentCommentCreator{delegate: commentSvc})
	return svc
}

func newPostService(deps *Deps) postapp.PostService {
	repo := postinfra.NewPostRepository(deps.DB.Get())
	statsCache := postinfra.NewPostStatsCache()
	likeCache := postinfra.NewPostLikeCache()
	collectCache := postinfra.NewPostCollectCache()
	searcher := postinfra.NewPostSearcher()
	publisher := postinfra.NewPostEventPublisher()
	return postapp.NewPostService(repo, statsCache, likeCache, collectCache, searcher, publisher)
}

// newCommentService 构造 CommentService。
//
// user/post 跨领域依赖通过 setter 注入（见 RegisterDomainRoutes）。
func newCommentService(deps *Deps) commentapp.CommentService {
	repo := commentinfra.NewCommentRepository(deps.DB.Get())
	statsCache := commentinfra.NewCommentStatsCache()
	likeCache := commentinfra.NewCommentLikeCache()
	publisher := commentinfra.NewCommentEventPublisher()
	return commentapp.NewCommentService(repo, statsCache, likeCache, publisher)
}

// newLikeService 构造 LikeService。
//
// post/comment 跨领域依赖通过 setter 注入（见 RegisterDomainRoutes）。
func newLikeService(deps *Deps) likeapp.LikeService {
	postCache := likeinfra.NewPostLikeCache()
	commentCache := likeinfra.NewCommentLikeCache()
	publisher := likeinfra.NewLikeEventPublisher()
	return likeapp.NewLikeService(postCache, commentCache, publisher)
}

// newCollectService 构造 CollectService。
//
// post 跨领域依赖（查询端口 + 组装端口）通过 setter 注入（见 RegisterDomainRoutes）。
func newCollectService(deps *Deps) collectapp.CollectService {
	cache := collectinfra.NewPostCollectCache()
	repo := collectinfra.NewPostCollectRepository(deps.DB.Get())
	publisher := collectinfra.NewCollectEventPublisher()
	return collectapp.NewCollectService(cache, repo, publisher)
}

// newHistoryService 构造 HistoryService。
//
// post 跨领域依赖（ES 帖子组装端口）通过 setter 注入（见 RegisterDomainRoutes）。
func newHistoryService(deps *Deps) historyapp.HistoryService {
	cache := historyinfra.NewPostHistoryCache()
	repo := historyinfra.NewPostHistoryRepository(deps.DB.Get())
	publisher := historyinfra.NewHistoryEventPublisher()
	return historyapp.NewHistoryService(cache, repo, publisher)
}
