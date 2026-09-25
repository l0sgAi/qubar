# domain-guide.md — 11 域速查（避免重复造轮子）

> 实施前先翻这份：你要加的能力可能某域已实现（如 `circleRepo.GetByIDs`、`UserFacade.GetBriefs`、
> `PostHydrator`）。**复用优先于新建**。先回 `SKILL.md` 看"开工检查清单"。

每域都遵循 4 层 DDD（`domain/application/infrastructure/interfaces/http`），路由由各自 `routes.go` 挂到 `rg.Group("/<domain>", authCheck)` 下。

## 速查：全部路由

| 域 | 方法 | 路径 | 说明 |
|---|---|---|---|
| auth | GET | `/auth/{google\|github\|azure}/login` | OAuth 登录跳转 |
| auth | GET | `/auth/{...}/callback` | OAuth 回调 |
| auth | POST | `/auth/register/send-code` | 发注册验证码 |
| auth | POST | `/auth/register/verify` | 校验验证码 |
| auth | POST | `/auth/register/complete` | 完成注册 |
| auth | POST | `/auth/login` | 邮箱密码登录 |
| auth | POST | `/auth/logout` | 登出（需登录） |
| user | GET | `/user/get` | 当前用户 |
| user | PUT | `/user/update` | 改资料 |
| user | GET | `/user/search` | 搜用户(ES) |
| user | GET | `/user/detail/:id` | 用户详情 |
| category | GET | `/category/get` | 分类列表 |
| circle | POST | `/circle/create` | 建圈 |
| circle | GET | `/circle/list` | 搜圈子(ES) |
| circle | GET | `/circle/active` | 近期活跃圈子(ES聚合) |
| circle | GET | `/circle/detail/:id` | 圈子详情 |
| circle | GET | `/circle/my` | 我的圈子 |
| circle | GET | `/circle/user` | 某用户的圈子 |
| circle | POST | `/circle/join` | 加圈 |
| circle | POST | `/circle/leave` | 退圈 |
| circle | GET | `/circle/posts` | 圈内帖子 |
| post | POST | `/post/create` | 发帖 |
| post | GET | `/post/list` | 帖子列表(ES) |
| post | GET | `/post/my` | 我的帖子 |
| post | GET | `/post/user/:user_id` | 某用户帖子 |
| post | GET | `/post/detail/:id` | 帖子详情 |
| recommend | GET | `/post/home` | 首页信息流(复用/post) |
| comment | POST | `/comment/create` | 发评论 |
| comment | GET | `/comment/list` | 根评论(cursor) |
| comment | GET | `/comment/replies` | 回复(cursor) |
| comment | GET | `/comment/detail/:id` | 评论详情 |
| like | POST | `/like/toggle` | 赞切换(post/comment) |
| collect | POST | `/collect/toggle` | 收藏切换 |
| collect | GET | `/collect/posts` | 我的收藏 |
| history | GET | `/history/posts` | 浏览历史 |
| storage | POST | `/upload/image` | 上传单图 |
| storage | POST | `/upload/post-images` | 上传帖子图(多) |
| storage | POST | `/upload/video` | 上传视频 |
| storage | DELETE | `/upload/delete` | 删文件 |
| storage | GET | `/upload/presign` | 预签名URL |

## 各域要点

### auth（无聚合根，编排登录/注册/OAuth）
- Service（`auth/application/service.go:66`）：`Login/SendCode/VerifyCode/Register/OAuthLoginURL/OAuthCallback/LogoutByToken`。
- 公开组 `/auth`（OAuth + register + login）；登出子组挂 authCheck。
- Infra：`sa_token_session.go`、`verification_store_redis.go`（`register:code:{email}` 5min）、`email_sender.go`（Mailtrap）、`oauth_provider_adapter.go`。
- OAuth provider 注册名 `google/github/azure`（⚠️ config key 是 microsoft，provider 是 azure）。
- 错误：`errInvalidCredentials`/`errRateLimitExceeded`/`errOAuthProviderUnavailable`→503。

### user（`SysUser`，`domains.users`）
- Service：`GetCurrentUser/UpdateProfile/Search(ES)/GetByID(cache)/GetBrief/GetBriefs`。
- **跨域生产者**：`UserFacade`（`application/service.go:40`）+ `NewUserFacade(svc)`（`:128`），`GetBriefs(ctx,userIDs) (map[string]UserBrief,error)`（`:43`）—— 被多域复用的核心。
- 缓存：`user:info:{id}`（zstd, 30min）。

### category（`Category`，`domains.category`）
- 层级（parent_id），种子 ID 固定（`docs/pgsql-ddl/circle.md`）。Service：`GetCategories`（status=shown）。无跨域依赖。

### circle ⭐（`Circle`+`CircleMember`）
- Service（`application/service.go:261`）：`CreateCircle/GetCircleDetail(cache+stats+member)/JoinCircle/LeaveCircle/
  SearchCircles(ES)/GetMyCircles/GetUserCircles(joined ZSET+rank cursor)/GetCirclePosts(ES+assemble)/
  ListActiveCircles(ES terms 聚合)/ListJoinedCircleIDs(recommend C1 用)/IncrPostCount`。
- 缓存：`circle:info:`(24h) / `circle:stats:`(Hash) / `circle:joined:{uid}`(ZSET 24h) / `circle:hot:`(Δ 累加器 50h)。
- **跨域生产者**：`CircleFacade`（`:36`）+ `NewCircleFacade(repo)`（`:333`）。
- repo 暴露给桥接：`circleRepo.GetByIDs`（`circle_repo_pg.go:43`，`map[uuid.UUID]*Circle`）—— post/trending 回填圈子用。
- errors：join/leave 哨兵 + `mapJoinLeaveError`（`:38`）。

### post ⭐（`Post`，`domains.post`，含 `Hot int`）
- Service（`application/service.go:193`）：`CreatePost(成员+状态校验,sanitize,摘要,incr圈子post_count)/
  GetPostDetail(异步浏览+RestoreStats+is_liked/is_collected)/SearchPosts/GetMyPosts/GetUserPosts(ES)/
  GetPostsByIDs/SearchPostsByIDs/SearchPostsByIDsAndKeyword(跨域给 collect/history)/
  ListCircleIDsByPostIDs(recommend C3)/GetPostMeta/RestoreStats/RestoreStatsAndIncrCommentCount`。
- **⚠️ 评论数同步落库**（`:967`），不像浏览/赞/藏走 MQ。
- 跨域端口（setter 注入）：`UserFacade/CircleFacade/CircleMemberChecker/CircleStatusChecker/CirclePostCountPort/PostCollectCache/HistoryRecorder`。
- publisher：`PublishViewCount`。
- repo `IsCollected` 用 `.Table("domains.post_collect")` 原始表名（避免跨域 import 实体）。

### comment ⭐（`Comment` 二级扁平 `root_id` + `CommentLike`）
- Service（`application/service.go:110`）：`CreateComment(状态/锁校验,root_id/reply_to_id 线程校验,sanitize,incr post comment_count,
  发 hot+5 + CF interaction)/GetRootComments(cursor sort 0=赞/1=时间,固定20)/GetReplies(cursor,默认10)/GetCommentDetail/GetCommentMeta/RestoreCommentStats`。
- **DB keyset 游标翻页**（`comment_repo_pg.go:186`）。CommentLike 冗余存 `post_id`。
- domain 哨兵：`ErrPostLocked/ErrRootCommentMismatch/ErrReplyTargetNotInThread/ErrInvalidCursor`（`comment.go:67`）。

### like ⭐（无自带实体，`like/domain/like.go` 只有 `TargetType`+`ToggleResult{±1,0}`+`ResolveWant`）
- Service（`application/service.go`）：`Toggle(input{Type,TargetID,Action})` → `togglePostLike`/`toggleCommentLike`。
  每路：校验存在 → `RestoreStats` → `Target.IsLiked`(桥接 post/comment `IsLikedByUser`：缓存 → DB 回源 + 回填) →
  `ResolveWant`(action 显式 或 取反) → Lua **设值** → 仅变化时发 like 事件（同步 ack；失败回滚缓存 → `ErrEventPublishFailed`/503）。
- ** owns post & comment 的原子设值**；流水表分别在 post/comment 域。

### collect ⭐（`PostCollect`，`domains.post_collect`，仅 post 有收藏）
- Service（`application/service.go`）：`Toggle`(校验→RestoreStats→解析真实状态→**同步 upsert** post_collect(返回 changed)→Lua 设值→仅 changed 发 collect_count 事件，投递失败回滚)/
  `ListCollectedPosts`(DB keyset `(create_time,id)`)。
- 缓存：`user:collect:posts:{uid}` ZSET。

### history ⭐（`PostViewHistory`，`domains.post_view_history`）
- **DB 只由 MQ consumer 写**，repo 是冷启动兜底。
- Service（`application/service.go:56`）：`RecordView`(Redis ZSET `user:view:posts:{uid}` 立即 + MQ 异步，由 post 详情异步 goroutine 调)/
  `ListHistoryPosts`(ZSET offset 翻页，keyword 走 ES multi_match)。ZSET cap 500。

### recommend ⭐（跨域编排器，**无聚合根**）
- Ports/DTO 全在 `recommend/domain/ports.go`：`HomeFeedSearcher/PostHydrator/PostMetaReader/CircleLookup/SeedReader/
  InteractionChecker/FeedCache/InterestCircleCache`；`FeedPostItem`(镜像 PostListItem + IsLiked/IsCollected)、`FeedPage`。
- Service（`application/service.go:21`）：`GetHomeFeed` 按 tab 分发 → `getRecommend`(池 offset) / `getSimpleFeed`(hot/latest search_after) / `getFollowing`(已加圈子 latest)。
- 单端点 `GET /post/home?tab=…`（复用 `/post` 前缀）。
- Infra：`home_feed_searcher_es.go`、`feed_cache_redis.go`(`feed:recommend:{uid}` LIST + token)、
  `seed_reader_redis.go`(like/collect/view ZSET + cf:item 聚合)、`interest_circle_cache_redis.go`(`user:interest_circles:{uid}` SET)。
- **`PostHydrator`/`InteractionChecker` 是可复用只读端口**（trending/discover 复用）。`InteractionChecker` 由 composition
  `postInteractionChecker` 桥接 `PostService.BatchCheckInteractions`（ZSET 批查 → miss 批量回源 DB → 回填），三个信息流共用。

### storage（无实体，文件上传 DTO 在 `storage/domain/storage.go`）
- Service：`UploadImage/UploadPostImages/UploadVideo/DeleteFile/PresignedURL`。Infra：`s3_storage.go`（AWS S3 + 预签名）。

## 跨域可复用资产速查（建新功能优先用这些）

| 需求 | 已有资产 | 位置 |
|---|---|---|
| 批量取用户简视图 | `UserFacade.GetBriefs(ctx,[]string) (map[string]UserBrief,error)` | `user/application/service.go:43` |
| 单个用户简视图 | `UserFacade.GetBrief` | `user/application/service.go:45` |
| 批量取圈子(全字段,保序) | `circleRepo.GetByIDs(ctx,[]uuid) map[uuid]*Circle` | `circle/infrastructure/circle_repo_pg.go:43` |
| 帖子 hydrate(作者/圈子/图片/统计) | `PostHydrator.Hydrate(ctx,[]uuid) ([]FeedPostItem,error)` | `recommend/domain/ports.go:64` |
| 批量回填 is_liked/is_collected | `InteractionChecker.BatchCheck` | `recommend/domain/ports.go:89` |
| 帖子→circle_id 反查 | `PostMetaReader.ListCircleIDsByPostIDs` | `recommend/domain/ports.go:70` |
| 用户已加圈子 IDs | `CircleLookup.ListJoinedCircleIDs` | `recommend/domain/ports.go:75` |
| ES 窗口化 terms 聚合榜 | `AggregateActiveCircles`（范式蓝本） | `elasticsearch/post.go:586` |
| ES 时间衰减热度排序 | `SearchHomeFeed`（rank_score 脚本） | `elasticsearch/post.go:711` |
| Redis 原子计数 helper | `IncrementCircle{Member,Post,Hot}` 等 | `redis/cache.go:234+` |
| 批量 PG upsert | `jsonb_to_recordset` 模式 | 各 redpanda consumer |
