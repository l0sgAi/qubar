---
name: qubar-skill
description: >
  How to build and modify the qubar (interestBar) backend — a Go modular-monolith
  interest-community platform using DDD layered domains, Hertz, GORM/Postgres, Redis,
  Elasticsearch, and Redpanda. Use whenever the user asks to add/change/implement
  features, endpoints, domains, services, caches, search, or background jobs in the
  qubar codebase — even if they don't name the framework. Covers architecture rules,
  coding conventions, and the exact step-by-step for adding a new domain.
---

# qubar-skill — qubar 项目实施约束与指南

qubar（Go module `interestBar`）是一个 DDD 模块化单体的兴趣社区后端。本 skill
是该项目所有实施工作的**强制性前置阅读**：它告诉你架构怎么分层、命名怎么定、跨域怎么调、
配置怎么加。**写任何代码前先读这里的"开工检查清单"，再按需翻 references/**。

> 一句话架构：`pkg/domains/<name>/{domain,application,infrastructure,interfaces/http}` 四层 DDD，
> 跨域只走 Facade 接口（setter 注入，在 `pkg/composition` 装配），业务层绝不 import 兄弟域或 hertz。

## 何时触发本 skill

当用户在 qubar 仓库里要求做以下任何一件事时，**先读本 SKILL.md 全文**，再决定翻哪个 reference：
- 新增/修改一个领域（domain）、接口（endpoint）、Service 方法
- 涉及数据库、Redis 缓存、ES 搜索、Redpanda 消息、后台定时任务
- 改配置、加配置项、改启动流程
- 任何写 Go 代码的任务

## 渐进式披露：按需读 references/

不要一次性读全部 reference。**先看下面的"开工检查清单"和"核心红线"，判断任务类型，
再只读相关的那份 reference。**

| 任务类型 | 先读这份 reference |
|---|---|
| 新建一个领域（最大改动） | `references/add-domain.md`（分步清单 + 完整模板） |
| 不确定某层该放什么 / 跨域怎么调 / 怎么装配 | `references/architecture.md` |
| 涉及具体中间件（GORM/Redis/ES/Redpanda）或配置/启动 | `references/tech-stack.md` |
| 写实体/缓存/分页/错误/ID/响应格式 等编码细节 | `references/domain-conventions.md` |
| 需要查某域有哪些路由/方法/缓存键（避免重复造轮子） | `references/domain-guide.md` |

## 开工检查清单（每次实施前过一遍）

1. **有没有现成的？** 先翻 `references/domain-guide.md` 或 grep，确认要加的能力不是某个域已实现的方法（如 `circleRepo.GetByIDs`、`UserFacade.GetBriefs`）。**复用优先于新建。**
2. **改动落在哪一层？** 每层职责固定（见"四层职责"）。别把 infra 接口写进 application，别把 ES/Redis import 进 domain。
3. **要不要跨域？** 跨域必须走 Facade/Port 接口 + composition 桥接器，**禁止** import 兄弟域包。
4. **新配置项？** 同步改 `configs/config.yaml` + `pkg/conf/conf.go` 结构体，并提供 `<=0` 兜底默认值。
5. **新后台 job？** 仿 `circle_hot_syncer.go`（`{mu,ticker,stopChan,stopped}` + 优雅排干），在 `cmd/apps/server.go` 启停。
6. **新 Redis key？** 前缀常量 + `GetXxxKey` helper 加到 `pkg/server/storage/redis/constants.go`，写明类型/TTL/语义。
7. **设计文档先写？** 较大改动（新子系统/新聚合接口）参照 `docs/active-circles-design.md` 范式先写设计文档，caveman mode 规划待审。

## 核心红线（违反即返工）

- ❌ **domain/ 包 import gorm / redis / elasticsearch / hertz / 兄弟 domain 包。** domain 是纯 Go，只放实体、值对象、端口接口。
- ❌ **跨域直接 import。** 如 post 要拿 user 信息：在 `post/application/` 重新声明 `UserFacade` 接口，由 `pkg/composition/facade_bridges.go` 写桥接器，setter 注入。
- ❌ **业务层 import hertz/gin。** handler 用 `appctx.AppContext`；响应用 `httputil.Success/BadRequest/...`，**禁止** `c.JSON(...)`。
- ❌ **handler 里写查询逻辑 / domain 里写 DB 操作。** handler 只 bind + 调 service + 映射错误；DB/Redis/ES 只在 `infrastructure/`。
- ❌ **AutoMigrate。** schema 由 `docs/pgsql-ddl/` 的 SQL 脚本管理（DB-owner 角色），运行时角色无 ALTER 权限。改表先改 docs/pgsql-ddl/。
- ❌ **GORM 软删除插件。** 用 `deleted = 0` 手动过滤（`Deleted int16`），不用 `gorm.DeletedAt`。
- ❌ **用 `form:` tag 绑 query。** hertz 的 `BindQuery` 只认 `query:` tag（gin 时代用 form，迁移后必须改）。
- ❌ **用户文本直接入库。** 必须先过 `utils.SanitizeForPg`（防 PG UTF8 错误），在 application 层调用。
- ❌ **随机生成主键。** 用 `sharedomain.NewID()`（UUIDv7，时间序=字典序，支撑 keyset 翻页与"最新在前"排序）。

## 四层职责速记

```
domain/            实体（gorm tag + TableName）+ 值对象 + 端口接口（Repository/Cache/EventPublisher）。纯 Go。
application/       Service（接口+impl+NewXxx 构造器）+ 跨域 Facade/Port 接口 + DTO/Input/VO + errors.go。
                   Searcher 接口也在这里（因返回 application DTO）。跨域依赖用 setter 注入。
infrastructure/    *_repo_pg.go / *_cache_redis.go / *_searcher_es.go / *_event_publisher.go 适配器实现。
                   命名后缀即技术。构造器返回 domain/application 接口（编译期保证满足）。
interfaces/http/   handler.go（Handler + Request DTO + writeXxxError）+ routes.go（RegisterRoutes(rg,svc,authCheck)）。
```

包名固定：`domain` / `application` / `infrastructure` / `http`。composition 用别名导入（`circleapp`/`circledomain`/...），
共享内核 `pkg/shared/domain` 别名 `sharedomain` 避免与各域 `domain` 包冲突。

## 响应与错误（写 handler 必看）

- 统一信封 `{code, message, data}`，统一用 `pkg/shared/httputil` 的助手：`Success/Created/BadRequest/Unauthorized/Forbidden/NotFound/Conflict/TooManyRequests/InternalError/ServiceUnavailable/Pagination`。错误助手内部已 `c.Abort()`。
- 错误两层：domain 层哨兵 `ErrXxxNotFound`（`<domain>/domain/`）+ application 层 `errFoo` + 导出 `IsFooErr(err)` 谓词（`<domain>/application/errors.go`）。
- handler 里写 `writeXxxError(c, err)`，`switch application.Is…Err(err)` / `errors.Is(err, domain.Err…)` 映射到对应 httputil 助手，未知错误落到 `InternalError`。
- 鉴权：路由组挂 `authCheck`（= composition.RequireLogin）；handler 内用本地 `requireUserID(c)`（读 `c.LoginID()` → `uuid.Parse`），匿名可读端点用 `requireUserIDAllowAnon`。

## 分页（按数据源选，别发明新风格）

- ES 列表 → `search_after`（不透明 JSON 数组游标，`HasMore = len==size`）
- DB 评论/收藏 → keyset cursor（base64 JSON，复合 `(sort_col, id)`，UUIDv7 序）
- Redis ZSET（已加圈子/历史）→ rank/offset
- 推荐候选池 → `offset + pool_token`（token 不匹配=池重建，回 offset=0）
- ES terms 聚合榜 → `offset`（`bucket_sort`）
- `size` 一律 `normalizeSize`：`<=0 || >100` 回落 20。

## 缓存/统计写法（Write-Behind）

- 读路径：cache-aside（miss → 查 DB → 回填，best-effort 不返回缓存错）。
- 计数（浏览/赞/藏）：**Redis Lua 原子为读路径真值** → 发 Redpanda 事件 → 聚合器批量 `jsonb_to_recordset` 落库。评论数是**例外**（同步落库）。
- 赞/藏是**二元状态**（`docs/design/like-pipeline-fix-design.md`）：用户 ZSET 有 TTL + cap，**miss ≠ 未赞**，必须回源 DB；
  写路径"先解析真实状态 → Lua 设值（非 toggle）→ 状态真变才发事件"；落库/计数由**行状态迁移**推导（幂等），不对事件 ±1 求和。
- 计数 Hash 用 `SeedXxxIfAbsent`（`HSetNX` 逐字段）避免覆盖并发 `HINCRBY`。无分布式锁/单飞（stats 是软信号，接受竞态）。

## 设计文档范式（大改动先写文档）

复杂子系统/聚合接口参照 `docs/active-circles-design.md` 与 `docs/trending-design.md` 的结构：开篇 blockquote 写目标+基线；
章节用 `## 一、现状盘点` / `## 二、对原始规则评估` / `## 三、优化规则` / `## 四、数据流`（ASCII 图）/
`## 五、Schema/配置变更` / `## 六、一致性/边界/风险`（表格） / `## 七、分阶段交付`（P0/P1 表）。
全程 `file:line` 引用。**先文档后编码（caveman mode），待用户批准再动手。**

## 常用命令

```bash
go build ./...              # 编译
go vet ./...                # 静态检查
go test ./pkg/...           # 测试（注意：测试很稀疏，仅 cursor/sanitize/password/adapter/cors）
go run ./cmd -c configs/config.yaml -b configs/bootstrap.yaml   # 本地启动（-b 空=跳过 Nacos 用本地 yaml）
```

## 速查：关键文件位置

- 入口：`cmd/main.go`（flags `-c` `-b`）、`cmd/apps/server.go`（启动顺序 + 关停）
- 装配根：`pkg/composition/server.go`（RegisterDomainRoutes）、`facade_bridges.go`、`deps.go`、`auth.go`
- 共享内核：`pkg/shared/domain/base.go`（NewID/BeforeCreate）、`appctx/context.go`、`routing/group.go`、`httputil/response.go`
- 基础设施全局：`pkg/server/storage/{db/pgsql,redis,elasticsearch,redpanda,s3}`
- 配置：`pkg/conf/conf.go`（Config 结构体）、`configs/config.yaml`、`configs/bootstrap.yaml`
- schema：`docs/pgsql-ddl/`（DDL 权威来源，按领域拆分，入口 README.md；docs/db.md 仅作跳转入口）
- 路由抽象适配：`pkg/composition/hertzadapter/group.go`、`pkg/shared/appctx/hertzadapter/adapter.go`
