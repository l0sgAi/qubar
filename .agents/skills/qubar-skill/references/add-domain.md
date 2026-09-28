# add-domain.md — 新增领域分步指南

> 新建一个领域是最大改动。本文件给完整分步清单 + 可抄模板。先回 `SKILL.md` 看"核心红线"。
> 范例参照 `recommend` 域（跨域编排器，无聚合根）与 `circle` 域（有聚合根）。

## 0. 先决策

- **要不要新域？** 大多数需求是给现有域加方法/路由（见 `domain-guide.md` 先查现成）。只有职责独立、
  跨多域编排、且现有域装不下时才新建域（如 trending 聚合 3 类榜单）。
- **有没有聚合根？** 有 → `<name>/domain/<name>.go` 放实体；纯编排器（如 recommend/trending）→ 只在 `domain/ports.go` 放端口+DTO。
- **是否跨域？** 是 → 跨域依赖一律 application 层 Facade/Port 接口 + composition 桥接器 + setter 注入。

## 1. 建目录与包（4 层）

```
pkg/domains/<name>/
  domain/<name>.go          # 实体(若有)+TableName()；或 ports.go(纯编排器)
  domain/repository.go      # 端口接口 Repository/Cache/EventPublisher + 哨兵错误 + DTO
  application/service.go    # XxxService 接口 + xxxServiceImpl + NewXxxService + Facade/Port 接口 + DTO/VO
  application/errors.go     # errFoo 哨兵 + IsFooErr 谓词
  infrastructure/<name>_repo_pg.go       # GORM 实现
  infrastructure/<name>_cache_redis.go   # Redis 实现
  infrastructure/<name>_searcher_es.go   # ES 实现（若需）
  infrastructure/<name>_event_publisher.go  # Redpanda（若需）
  interfaces/http/handler.go # Handler + NewHandler + Request DTO + writeXxxError
  interfaces/http/routes.go  # RegisterRoutes(rg, svc, authCheck)
```

包名固定：`domain` / `application` / `infrastructure` / `http`。

## 2. domain 层（纯 Go，禁 infra import）

### 2.1 实体（有聚合根时）—— 仿 `circle/domain/circle.go:17`
```go
package domain

import ("time"; "github.com/google/uuid")

type Foo struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	Name       string    `json:"name" gorm:"column:name;..."`
	Deleted    int16     `json:"deleted" gorm:"column:deleted;default:0"`
}
func (Foo) TableName() string { return "domains.foo" }   // ★ domains.* schema
```
状态枚举紧贴实体（无类型 `const`）。

### 2.2 端口接口 —— 仿 `circle/domain/repository.go`
```go
type FooRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*Foo, error)
	Create(ctx context.Context, f *Foo) error
	// ...
}
type FooCache interface { Get/Set/... }
type FooEventPublisher interface { Publish... }

var ErrFooNotFound = errors.New("foo not found")   // 哨兵
```
首参 `context.Context`，返回领域实体/DTO+error。接口隔离（多个小接口）。

### 2.3 纯编排器（无聚合根）—— 仿 `recommend/domain/ports.go`
只有端口接口 + DTO（`FeedPostItem`/`FeedPage` 风格），无实体。

### 2.4 schema 改动
**改表先改 `docs/pgsql-ddl/` 对应领域文档**（DDL 权威来源，DB-owner 执行）。运行时角色无 ALTER 权限，**禁止 AutoMigrate**。

## 3. application 层

### 3.1 Service —— 仿 `circle/application/service.go:261,286,302`
```go
package application

type FooService interface {
	CreateFoo(ctx context.Context, userID uuid.UUID, input CreateFooInput) error
	SetUserFacade(f UserFacade)   // 跨域依赖 setter 注入
	// ...
}
type fooServiceImpl struct {
	repo      domain.FooRepository
	cache     domain.FooCache
	userFacade UserFacade  // 注入前 nil
}
func NewFooService(repo domain.FooRepository, cache domain.FooCache) FooService {
	return &fooServiceImpl{repo: repo, cache: cache}   // 构造器只接同域依赖
}
```

### 3.2 Searcher 接口放 application（因返回 application DTO）—— 仿 `circle/application/service.go:252`
```go
type FooSearcher interface {
	Search(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*FooSearchResult, error)
}
```

### 3.3 跨域 Facade/Port —— 在本域重新声明，仿 `circle/application/service.go:51`
```go
// 与 user.application.UserBrief 字段一致，独立定义避免跨领域 import。
type UserBrief struct { ID string; Username string; AvatarURL string }
type UserFacade interface { GetBriefs(ctx, []string) (map[string]UserBrief, error) }
```
post 域还声明 port：`CircleMemberChecker`/`CircleStatusChecker`/`CirclePostCountPort`（`post/application/service.go:24`）。

### 3.4 errors.go —— 仿 `circle/application/errors.go:9-32`
```go
var errFooExists = errors.New("foo already exists")
func IsFooExistsErr(err error) bool { return errors.Is(err, errFooExists) }
```

## 4. infrastructure 层

### 4.1 Repo PG —— 仿 `circle/infrastructure/circle_repo_pg.go:21-41`
```go
package infrastructure

type fooRepoPG struct{ db *gorm.DB }
func NewFooRepository(db *gorm.DB) domain.FooRepository { return &fooRepoPG{db: db} }

func (r *fooRepoPG) GetByID(ctx context.Context, id uuid.UUID) (*domain.Foo, error) {
	var f domain.Foo
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", id, 0).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) { return nil, domain.ErrFooNotFound }
	return &f, err
}
func (r *fooRepoPG) Create(ctx context.Context, f *domain.Foo) error {
	f.ID = sharedomain.NewID()   // ★ 内联字段必须显式生成 UUIDv7
	return r.db.WithContext(ctx).Create(f).Error
}
```
跨域表访问用 `.Table("domains.post_collect")` 原始表名（避免 import 实体）。批量 upsert 用 `jsonb_to_recordset`。

### 4.2 Cache Redis —— 无状态 struct + 委托全局客户端
```go
type fooCacheRedis struct{}
func NewFooCache() domain.FooCache { return &fooCacheRedis{} }
```
新 Redis key 加到 `pkg/server/storage/redis/constants.go`（前缀 const + `GetXxxKey` helper + 注释含类型/TTL/语义）。
miss 返 `nil,nil`；非 `redis.Nil` 错才记日志。

### 4.3 ES searcher —— 薄适配器，调 `pkg/server/storage/elasticsearch` 函数 + `toXxx` 字段拷贝。

### 4.4 Event publisher —— `struct{}` 委托 `redpanda.PublishXxx`。

## 5. interfaces/http 层

### 5.1 routes.go —— 仿 `post/interfaces/http/routes.go:16`
```go
package http

func RegisterRoutes(rg routing.RouterGroup, svc application.FooService, authCheck routing.HandlerFunc) {
	h := NewHandler(svc)
	f := rg.Group("/foo", authCheck)   // authCheck 传入而非 import
	{
		f.POST("/create", h.CreateFoo)
		f.GET("/list", h.ListFoos)
	}
}
```

### 5.2 handler.go —— 仿 `circle/interfaces/http/handler.go`
```go
type Handler struct{ svc application.FooService }
func NewHandler(svc application.FooService) *Handler { return &Handler{svc: svc} }

type CreateFooRequest struct {
	Name string `json:"name" binding:"required,min=1,max=50"`
}
func (h *Handler) CreateFoo(c appctx.AppContext) {
	userID, ok := requireUserID(c); if !ok { return }
	var req CreateFooRequest
	if err := c.BindJSON(&req); err != nil { httputil.BadRequest(c, err.Error()); return }
	if err := h.svc.CreateFoo(c, userID, application.CreateFooInput{Name: req.Name}); err != nil {
		writeFooError(c, err); return
	}
	httputil.Created(c, nil)
}

func writeFooError(c appctx.AppContext, err error) {
	switch {
	case application.IsFooExistsErr(err): httputil.Conflict(c, err.Error())
	case errors.Is(err, domain.ErrFooNotFound): httputil.NotFound(c, "foo not found")
	default: logger.Log.Error("foo service error: " + err.Error()); httputil.InternalError(c)
	}
}
```
- query 绑定用 **`query:` tag**（不是 form！），`c.BindQuery(&req)`。
- path 用 `c.Param("id")` → `uuid.Parse`。
- 响应**只用 httputil 助手**（禁 `c.JSON`）。
- `size` 用本地 `normalizeSize`。

## 6. composition 装配（`pkg/composition/`）

### 6.1 import 别名（`server.go` 顶部）—— 仿 `:11-14`
```go
fooapp  "interestBar/pkg/domains/foo/application"
fooinfra "interestBar/pkg/domains/foo/infrastructure"
foohttp "interestBar/pkg/domains/foo/interfaces/http"
```

### 6.2 newFooService 助手 + registerFoo —— 仿 `newRecommendService`（`server.go:181`）/`registerRecommend`（`:195`）
```go
func newFooService(deps *Deps, postSvc postapp.PostService) fooapp.FooService {
	repo := foonfra.NewFooRepository(deps.DB.Get())
	cache := foonfra.NewFooCache()
	svc := fooapp.NewFooService(repo, cache)
	svc.SetPostFetcher(&fooPostFetcher{delegate: postSvc})  // 跨域桥接注入
	return svc
}
func registerFoo(root routing.RouterGroup, svc fooapp.FooService, authCheck routing.HandlerFunc) {
	foohttp.RegisterRoutes(root, svc, authCheck)
}
```

### 6.3 RegisterDomainRoutes 里挂接 —— 仿 `server.go:100,113`
```go
fooSvc := newFooService(deps, postSvc)   // 构造（跨域依赖的 service 先于它构造）
// ... 其它 setter 注入 ...
registerFoo(root, fooSvc, authCheck)     // 注册
```

### 6.4 跨域桥接器 —— 加到 `facade_bridges.go`，仿 `circleUserFacade`（`:31`）
```go
type fooPostFetcher struct{ delegate postapp.PostService }
func (f *fooPostFetcher) GetMediaByPostIDs(ctx context.Context, ids []uuid.UUID) ([]fooapp.PostMedia, error) {
	// 委托 + DTO 字段拷贝
}
```
编译期 guard：`var _ fooapp.PostFetcher = (*fooPostFetcher)(nil)`。

## 7. 配置（若需新配置项）

三处同步（仿 `Hot`/`Recommend` 节）：
1. `configs/config.example.yaml` 加 `foo:` 节 + 默认值（模板入库；本地 `config.yaml` 不入库）。
2. `pkg/conf/conf.go` 加 `Foo` 结构体（mapstructure/json/yaml tag）+ `AppConfig` 加字段（`:14`）。
3. 消费处读 `conf.Config.Foo.Xxx`，`<=0` 提供常量兜底。

## 8. 后台 job（若需）

仿 `CircleHotSyncer`（`redpanda/circle_hot_syncer.go:28`）：struct `{mu,ticker,stopChan,stopped}` + `run()` select + `Stop()` 幂等排干 + 包级 `StartFooSyncerWithRetry()`/`StopFooSyncer()`。
在 `cmd/apps/server.go` 成对加 `go redpanda.StartFooSyncerWithRetry()`（`:164` 附近）+ `redpanda.StopFooSyncer()`（`:204` 附近）。

## 9. 文档

较大改动先写 `docs/foo-design.md`（仿 `active-circles-design.md`/`trending-design.md` 范式：
目标 blockquote / 现状盘点 / 数据流 ASCII / schema+配置变更 / 风险表 / 分阶段交付 P0-Pn）。
**caveman mode 先文档后编码，待批准再写代码。**

## 10. 验收 checklist

- [ ] `domain/` 不 import gorm/redis/es/hertz/兄弟域
- [ ] 跨域走 Facade/Port + composition 桥接 + setter 注入（无直接 import 兄弟域）
- [ ] handler 用 appctx + httputil（无 c.JSON），query 用 `query:` tag
- [ ] 实体内联字段 → repo insert 前显式 `sharedomain.NewID()`
- [ ] 软删除 `deleted = 0` 手动过滤（无 gorm.DeletedAt）
- [ ] 用户文本过 `utils.SanitizeForPg`
- [ ] Redis key 加到 `constants.go`（前缀 const + helper + 注释）
- [ ] 改表先改 `docs/pgsql-ddl/` 对应领域文档（无 AutoMigrate）
- [ ] 新配置项三处同步 + 兜底默认值
- [ ] `go build ./...` + `go vet ./...` 通过
- [ ] 纯函数补单测（游标/解析/规整）

## 完整范例索引
- 有聚合根 + 跨域 Facade 生产者：`circle/`
- 有聚合根 + 多跨域端口消费：`post/`
- keyset 游标 + 域哨兵：`comment/`
- 无聚合根跨域编排器：`recommend/`（最佳新域模板，特别是 `recommend/domain/ports.go` 与 composition 的 `newRecommendService`）
- 新域设计文档范例：`docs/trending-design.md`、`docs/active-circles-design.md`
