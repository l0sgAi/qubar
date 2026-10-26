# architecture.md — qubar 架构与分层规范

> 本文件详述 qubar 的 DDD 分层、跨域 Facade 模式、composition 装配根、共享内核。
> 写代码前先回 `SKILL.md` 看"核心红线"。

## 一、整体结构

`pkg/` 是**模块化单体（modular monolith）**：每个限界上下文一个 domain 目录，内部固定 4 层 DDD；
外加一个小型**共享内核**和一个**装配根（composition root）**。

```
pkg/
  domains/<name>/                 # 一个限界上下文一个目录（11 个域）
    domain/                       # 实体、值对象、端口接口 —— 纯 Go，禁 import gorm/redis/兄弟域
    application/                  # Service(接口+impl) + 跨域 Facade/Port 接口 + DTO + errors
    infrastructure/               # 适配器: *_repo_pg.go / *_cache_redis.go / *_searcher_es.go / *_event_publisher.go
    interfaces/http/              # 入站适配器: handler.go + routes.go
  shared/                         # 共享内核（耦合点，保持克制）
    domain/base.go                # BaseModel + NewID() + BeforeCreate
    appctx/                       # 框架无关的请求上下文接口（+ hertz 实现）
    routing/                      # RouterGroup 抽象
    httputil/                     # 响应助手
  composition/                    # 装配根: server.go（顺序清单）/ wire_<domain>.go / <domain>_bridges.go / deps.go / auth.go / hertzadapter/
  server/                         # 基础设施（DB/Redis/ES/Redpanda/S3 全局客户端）+ router + auth
  logger/ conf/ util/ enums/      # 横切
```

设计意图（`pkg/composition/deps.go:6-11` 注释）：模块化单体，**未来拆微服务时，把某个领域包复制出去 +
在新服务里重写一份 composition 即可**。所以"领域不 import 兄弟域"是不可动摇的硬约束。

11 个域：`auth, category, circle, collect, comment, history, like, post, recommend, storage, user`。

## 二、domain 层（纯 Go，无任何 infra 依赖）

### 2.1 实体定义

实体是带 `json:`+`gorm:` tag 的普通结构体 + `TableName() string`。所有表在 `domains.*` schema 下。
**关键细节：实体并不嵌入 `BaseModel`，而是把 `ID/CreateTime/UpdateTime` 内联**（为精确控制 gorm tag），
因此每个 repo 在 insert 前**必须显式调 `sharedomain.NewID()`**（内联字段不触发 `BeforeCreate` 钩子）。

示例（`pkg/domains/circle/domain/circle.go:17,38-40`）：
```go
type Circle struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	Name string `json:"name" gorm:"..."`
	Hot  int    `json:"hot" gorm:"column:hot;default:0"`
	Deleted int16 `json:"deleted" gorm:"column:deleted;default:0"`
	// ...
}
func (Circle) TableName() string { return "domains.circle" }
```

状态/角色/类型枚举用无类型 `const` 块紧贴实体：`CircleJoinType*` / `CircleStatus*` /
`MemberRoleMember/Admin/Owner = 10/20/30` / `PostType*` / `PostStatus*` 等。
（另有一个 `pkg/enums/` 包，但各域大多内联重定义，未完全采用它。）

### 2.2 端口接口（Repository / Cache / EventPublisher）

所有端口都是 `domain/` 里声明的接口，**首参 `context.Context`**，返回领域实体/DTO + error。
按关注点拆分多个小接口（接口隔离）。`pkg/domains/circle/domain/repository.go`：
- `CircleRepository`（`:18`）—— `GetByID/GetByIDs/ExistsByName/ExistsBySlug/Create`
- `MemberRepository`（`:32`）
- `CircleBaseCache`（`:46`）—— 基础信息
- `CircleStatsCache`（`:54`）—— 带原子计数器
- `JoinedCirclesCache`（`:78`）—— ZSET
- `CircleEventPublisher`（`:94`）

跨域 Facade DTO（用 string ID 避免强加 uuid 耦合）：`CircleBrief`（`:104`）。
哨兵错误：`ErrCircleNotFound`/`ErrMemberNotFound`（`:12,15`）。

> 普通值对象/缓存 DTO（无 gorm tag）：`CircleBaseInfo`、`CircleStatistics`。

### 2.3 自定义 GORM 类型示例

`MediaExtraJSON`（`post/domain/post.go:63-88`）实现 `sql.Scanner`+`driver.Valuer` 映射 `jsonb` 图片 URL 列，
带向后兼容把旧 `"{}"` 转成空切片。

## 三、application 层

### 3.1 Service = 接口 + 私有 impl + 构造器

规范形态（`pkg/domains/circle/application/service.go:261,286,302`）：
```go
type CircleService interface {
	CreateCircle(ctx, userID uuid.UUID, input CreateCircleInput) error
	GetCircleDetail(...) (*CircleDetailVO, error)
	SetUserFacade(f UserFacade)        // setter 注入跨域依赖
	SetPostFetcher(f PostMediaFetcher)
	// ...
}

type circleServiceImpl struct {
	repo domain.CircleRepository
	// ... 只放同域依赖
	userFacade  UserFacade  // 可能为 nil（注入前）
	postFetcher PostMediaFetcher
}

func NewCircleService(repo, memberRepo, baseCache, statsCache, joinedCache, searcher, publisher) CircleService {
	return &circleServiceImpl{...}   // 构造器只接同域依赖
}
```

命名铁律：导出接口 `XxxService` / 未导出 impl `xxxServiceImpl` / 构造器 `NewXxxService`。
**构造器只接同域依赖；跨域依赖一律 setter 注入**（见 §五）。

### 3.2 Searcher 接口放 application，不放 domain

因为 Searcher 返回 application DTO，所以接口定义在 `application/`（`circle/application/service.go:252` `CircleSearcher`）。
ES 适配器在 infrastructure 实现它。

### 3.3 跨域 Facade / Port 接口在 application 重新声明

**消费域自己重新声明**它需要的简视图结构 + Facade 接口（避免 import 生产者的 application 包）。
同形结构在各域重复出现：
- circle 声明自己的 `UserBrief`+`UserFacade`、`PostMediaFetcher`（`service.go:51,59`）
- post 声明 `UserBrief`/`UserFacade`、`CircleBrief`/`CircleFacade` + port `CircleMemberChecker`/`CircleStatusChecker`/`CirclePostCountPort`
- comment 声明 `UserBrief`/`UserFacade` + port `PostLookup`

注释原话（`circle/application/service.go:43`）："与 user.application.UserBrief 字段一致，独立定义避免跨领域 import"。

### 3.4 errors.go（两层错误）

- **domain 层哨兵**：`ErrXxxNotFound`/`ErrPostLocked`/`ErrInvalidCursor`（在 `<domain>/domain/`）。
- **application 层**：未导出哨兵 `errFoo` + 导出谓词 `IsFooErr(err)`（`<domain>/application/errors.go`），
  让 handler 能 `switch` 不泄露哨兵。`circle/application/errors.go:9-32`。
- 参数化错误用结构体类型：如 `post/application/errors.go` 的 `mutedError`（带时间）+ `IsMutedErr(err) (time.Time, bool)`。
- 历史字符串错误的桥接：`mapJoinLeaveError`（`circle/application/errors.go:38`）把 repo 的 `fmt.Errorf` 串映射成类型化哨兵。

## 四、infrastructure 层

### 4.1 文件命名（载重约定，后缀即技术）

- `*_repo_pg.go` —— GORM/PG repo 实现（非 PG 专用用 `*_repository.go`，如 collect/history）
- `*_cache_redis.go` —— Redis 缓存实现（一个文件可含多个 cache impl）
- `*_searcher_es.go` —— ES searcher 实现
- `*_event_publisher.go` —— Redpanda 事件发布实现

### 4.2 Repo PG 实现约定（`circle/infrastructure/circle_repo_pg.go:21-41`）

```go
type circleRepoPG struct { db *gorm.DB }

func NewCircleRepository(db *gorm.DB) domain.CircleRepository {  // 返回 domain 接口（编译期保证）
	return &circleRepoPG{db: db}
}

func (r *circleRepoPG) GetByID(ctx context.Context, circleID uuid.UUID) (*domain.Circle, error) {
	var c domain.Circle
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", circleID, 0).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCircleNotFound   // gorm 错误 → domain 哨兵
		}
		return nil, err
	}
	return &c, nil
}
```

约定：
- struct 持 `*gorm.DB`；构造器**返回 domain 接口类型**（编译期满足检查）。
- 所有查询 `r.db.WithContext(ctx)`。
- 软删除手动 `deleted = 0`（不用 gorm 插件）。
- `gorm.ErrRecordNotFound` → domain 哨兵。
- insert 前 `sharedomain.NewID()`（`circle_repo_pg.go:87`）。
- 事务：`r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {...})`。
- **跨域表访问不 import 实体**：用 `.Table("domains.post_collect")` 原始表名（`post_repo_pg.go:87`，注释"避免跨领域 import 实体"）。
- keyset 游标：`comment_repo_pg.go` 的 `applyCursorCondition`/`applyOrderBy`，`(like_count < ?) OR (like_count = ? AND id < ?)`，靠 UUIDv7 字典序。

### 4.3 Cache Redis 实现

- impl struct 通常**无状态**（`struct{}`），委托 `pkg/server/storage/redis` 全局客户端/helper。
- TTL 写成未导出 `const`（如 `circleBaseCacheTTL = 24 * time.Hour`）。
- miss 返回 `nil, nil`（非 error），非 `redis.Nil` 错才记日志。
- 原子批量写用 `TxPipeline`：joined ZSET Rebuild = `Del` + 分块(500)`ZAdd` + `Expire`。

### 4.4 ES searcher 实现

薄适配器：调 `pkg/server/storage/elasticsearch` 的函数，再用 `toXxx` helper 把 ES 响应结构**逐字段拷贝**成 application DTO。
`marshalSearchAfter([]interface{}) string` 把 ES search-after 数组 JSON 成不透明游标串。

### 4.5 Event publisher 实现

`post/infrastructure/post_event_publisher.go`：`struct{}` 委托 `redpanda.PublishPostViewCount(postID)`，
构造器返回 **domain 接口**（publisher 是纯 infra 端口）。

## 五、跨域 Facade 模式（最重要的一条架构规则）

文档来源：`pkg/composition/doc.go`（包注释）。

三步：
1. **生产者域**在 `application/` 暴露 Facade 接口（如 `user.application.UserFacade`），并提供构造器
   `NewUserFacade(svc) UserFacade`（ backed by 未导出 `userFacadeAdapter`）。
2. **消费者域**在自己的 `application/` 重新声明同形接口（同名 DTO + 同形方法）。
3. **composition** 写**桥接适配器 struct**，包生产者 Facade，逐字段拷贝 DTO（两个 `UserBrief` 结构同形但名义不同）。

桥接器按**被注入方**归档到 `pkg/composition/<domain>_bridges.go`（注入 post 的放 `post_bridges.go`；
recommend/trending/discover 共用的 `postInteractionChecker` 在 `feed_interaction_bridge.go`）。

桥接器示例（`circle_bridges.go` `circleUserFacade`）：
```go
type circleUserFacade struct { delegate userapp.UserFacade }

func (f *circleUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]circleapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	// ...
	result := make(map[string]circleapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = circleapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}  // 字段拷贝
	}
	return result, nil
}
```

桥接器既可包 Facade，也可直接包 repo/service（如 `circleMemberCheckerForPost` 包 `circledomain.MemberRepository`）。

### setter 注入顺序（`pkg/composition/server.go` `RegisterDomainRoutes`）

构造完所有 service 后，统一调 setter：
```go
circleSvc.SetUserFacade(&circleUserFacade{delegate: userFacade})
circleSvc.SetPostFetcher(&postMediaFetcherForCircle{delegate: postSvc})
postSvc.SetUserFacade(&postUserFacade{delegate: userFacade})
postSvc.SetCircleFacade(&postCircleFacade{delegate: circleFacade})
postSvc.SetMemberChecker(&circleMemberCheckerForPost{memberRepo: memberRepo})
// ...
```
注入遗漏不会 panic，只会请求时空数据/校验失败，启动日志打一条便于排查（`cross-domain facades injected: ...`）。

## 六、interfaces/http 层

### 6.1 routes.go — `RegisterRoutes` 统一签名

每个域都暴露（`circle/interfaces/http/routes.go:20-39`）：
```go
func RegisterRoutes(rg routing.RouterGroup, svc application.CircleService, authCheck routing.HandlerFunc) {
	h := NewHandler(svc)
	cir := rg.Group("/circle", authCheck)   // authCheck 传入而非 import，保持框架无关
	{ cir.POST("/create", h.CreateCircle); cir.GET("/list", h.GetCircles) /* ... */ }
}
```
签名统一：`(rg routing.RouterGroup, svc <Domain>Service, authCheck routing.HandlerFunc)`。
`routing.RouterGroup`/`HandlerFunc` 来自共享内核（`pkg/shared/routing/group.go:26,20`），`HandlerFunc = func(c appctx.AppContext)`。

### 6.2 handler.go — Handler + Request DTO + 绑定 + 错误映射

```go
type Handler struct { svc application.CircleService }
func NewHandler(svc application.CircleService) *Handler { return &Handler{svc: svc} }
```

请求绑定用 `appctx.AppContext` 的方法（`pkg/shared/appctx/context.go:23-83`）：
- **JSON body**：`c.BindJSON(&req)`，配 `json:`+`binding:` tag。
- **Query**：`c.BindQuery(&req)`，配 **`query:` tag（不是 form!）**。
- **Path**：`c.Param("id")` → `uuid.Parse`。

`AppContext` 内嵌 `context.Context`，handler 直接把 `c` 当 ctx 传给 service（`handler.go:52 h.svc.CreateCircle(c, userID, ...)`）。

### 6.3 响应格式（绝不用 c.JSON）

统一用 `pkg/shared/httputil` 助手（`httputil/response.go`）：`Success/Created/BadRequest/Unauthorized/
Forbidden/NotFound/Conflict/TooManyRequests/InternalError/ServiceUnavailable/Pagination`。
信封 `{code, message, data}`；`ResponseCode`（`:16`）从 200 起镜像 HTTP；`httpStatusMap`（`:122`）映射业务码→HTTP 状态。
**错误助手内部已 `c.Abort()`**（`:192`）防双写。

### 6.4 错误→HTTP 映射

每个 handler 写 `write<Domain>Error(c, err)`，`switch application.Is…Err(err)` / `errors.Is(err, domain.Err…)` 映射到对应 httputil 助手，
未知错误落到 `InternalError`（先日志）。见 `circle/interfaces/http/handler.go:347-377`。

### 6.5 鉴权

两层：
- **路由级**：`authCheck`（= `composition.RequireLogin`，`pkg/composition/auth.go:25`）读 token header → `authenticate`（一次 GET 校验 + 按阈值续期）→ `c.SetLoginID(loginID)`，失败写 401。
  **故意不用** `sagin.CheckLogin`（gin 专用中间件破坏 `c.Next()` 语义，`auth.go:20-24`）。
- **handler 级**：本地 `requireUserID(c)`（读 `c.LoginID()` → `uuid.Parse`，失败 401/400）。
  匿名可读端点用 `requireUserIDAllowAnon`（返回 `uuid.Nil, true`）。

## 七、composition 装配根

### 7.1 Deps（`pkg/composition/deps.go:21-30`）
```go
type Deps struct { DB *pgsql.DBHolder }   // 目前只有 DB；Redis/ES/Redpanda 仍走全局
func NewDeps() *Deps { return &Deps{DB: &pgsql.DBHolder{}} }
```
`DBHolder.Get()` 返回全局 `pgsql.DB`（过渡期 DI 包装，`deps.go:18-29` 注释）。

### 7.2 文件布局（按领域拆分，减少并行开发冲突）

| 文件 | 内容 |
|---|---|
| `server.go` | 只有 `RegisterDomainRoutes`：构造顺序 + 跨域 setter 注入 + 路由注册顺序三张清单 |
| `wire_<domain>.go` | 该领域的 `new<Domain>Service` 构造器 + `register<Domain>` 路由注册（及其 import 别名） |
| `<domain>_bridges.go` | 注入该领域的跨域桥接器（按被注入方归档）+ 编译期 guard |
| `doc.go` | 包注释：布局约定 + Facade 桥接模式说明 |
| `deps.go` / `auth.go` / `notice_stream.go` / `user_session_store_bridge.go` | Deps、RequireLogin/OptionalLogin、SSE 裸 handler、auth→user 会话存储桥 |

加领域 = 新建 `wire_foo.go` + `foo_bridges.go`，`server.go` 只按依赖顺序插几行调用（见 `add-domain.md` §6）。

### 7.3 RegisterDomainRoutes 流程（`pkg/composition/server.go`）

1. `deps := NewDeps()`；`authCheck := RequireLogin`。
2. 逐域构造 service（`new<Service>(deps)` 助手，在 `wire_<domain>.go`：建 repo/cache/searcher/publisher + 调 `New<Service>Service`）。
   `newCircleService` 额外返回 repo（post 域桥接器要用）。
3. 建 Facade：`userFacade := userapp.NewUserFacade(userSvc)`、`circleFacade := circleapp.NewCircleFacade(circleRepo)`。
4. **跨域 setter 注入**（集中在 `RegisterDomainRoutes`，不散落到 wire 文件；`newNoticeStreamHub` 在 `wire_notice.go`）。
5. 只读编排域 recommend / trending / discover 与 aiagent 在注入之后构造（依赖 post+circle 等）。
6. 逐域 `register<Domain>(root, svc, authCheck)` 注册路由；最后起 Discover pool syncer。

### 7.4 new<Service> 助手范式（`wire_post.go` newPostService）
```go
func newPostService(deps *Deps) postapp.PostService {
	repo := postinfra.NewPostRepository(deps.DB.Get())
	statsCache := postinfra.NewPostStatsCache()
	// ...
	return postapp.NewPostService(repo, statsCache, likeCache, collectCache, searcher, publisher)
}
```
Redis/ES infra 构造器**无参**（读全局客户端）。category/storage/auth 因无跨域依赖，直接在 `register<Domain>`（`wire_category.go` 等）内联装配。

## 八、共享内核（`pkg/shared/`）—— 保持克制

- `domain/base.go`：`BaseModel`+`NewID()`+`BeforeCreate`。注释强调"共享内核是耦合点，应保持克制"。
- `appctx/context.go:23`：`AppContext` 接口（Method/Path/Param/Query/Header/BindJSON/BindQuery/JSON/UserID/LoginID/Device/...）。
  hertz 实现 `pkg/shared/appctx/hertzadapter/adapter.go`（**唯一**实现，带编译期 guard `var _ appctx.AppContext = (*hertzAppContext)(nil)`）。
  ⚠️ `BindQuery` 只认 `query:` tag（`adapter.go:89-98`）。
- `routing/group.go:26`：`RouterGroup` 接口。**两个** hertzadapter 包（有意的）：
  - `pkg/shared/appctx/hertzadapter` 适配**上下文**（`*app.RequestContext` → `AppContext`）。
  - `pkg/composition/hertzadapter` 适配**路由器**（`*server.Hertz` → `routing.RouterGroup`），`ForEngine(e)`/`ForGroup(g)`，
    `toHertzHandlers`（`group.go:99`）把每个域 handler 包进闭包调 `h(appctxhertz.New(ctx, c))`。
- `httputil/response.go`：响应助手（见 §6.3）。

## 九、横切约定

- **Logger**：全局 `logger.Log *zap.Logger`（`pkg/logger/logger.go:11`）。`logger.Log.Error/Warn/Debug(...)`。
  service 记 infra 失败但**一般不返回给调用方**（缓存 best-effort）。
- **SanitizeForPg**：所有入 PG 的用户文本在 application 层过 `utils.SanitizeForPg`（剥 NULL 字节/非法 UTF8）。
- **防御性游标解析**：用户可控游标用逗号 ok 类型断言 + `fmt.Errorf("%w: ...", domain.ErrInvalidCursor, ...)`，**绝不 panic**（`comment_repo_pg.go:203-265`）。
- **异步 fire-and-forget**：post 浏览计数自启 goroutine + 自带 `recover()` + 用 `context.Background()`（`post/application/service.go:447`）。
- **编译期接口满足 guard**：大适配器都带 `var _ Iface = (*Impl)(nil)`。

## 十、命名速查表

| 角色 | 导出 | 未导出 | 构造器 |
|---|---|---|---|
| Service | `XxxService` | `xxxServiceImpl` | `NewXxxService` |
| Facade | `XxxFacade` | — | `NewXxxFacade` |
| Repository | `XxxRepository` | `xxxRepoPG` | `NewXxxRepository` |
| Cache | `XxxCache` | `xxxCacheRedis` | `NewXxxCache` |
| Searcher | `XxxSearcher` | `xxxSearcherES` | `NewXxxSearcher` |
| EventPublisher | `XxxEventPublisher` | `xxxEventPublisherRedpanda` | `NewXxxEventPublisher` |
| Handler | — | `Handler` | `NewHandler` |
