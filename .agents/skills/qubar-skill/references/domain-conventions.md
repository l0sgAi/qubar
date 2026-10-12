# domain-conventions.md — 编码细节规范

> 本文件详述实体/缓存/分页/错误/ID/响应/文本处理等编码细节规范。
> 写具体代码前读这份。先回 `SKILL.md` 看"核心红线"。

## 一、ID 与时间约定

### 1.1 UUIDv7 主键（核心约定）
`pkg/shared/domain/base.go`：
```go
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }                    // :47

func (b *BaseModel) BeforeCreate(tx *gorm.DB) error {                        // :53
	if b.ID == uuid.Nil { b.ID = NewID() }
	return nil
}
```
**为什么 UUIDv7**：前 48 位是毫秒时间戳 → **字典序 == 时间序** → 免额外排序列即可"最新在前" +
keyset 翻页（`(sort_col, id)` 复合游标）。注释见 `base.go:24-34`、`docs/pgsql-ddl/README.md:4`。

**关键坑**：实体大多**内联** `ID/CreateTime/UpdateTime`（不嵌入 `BaseModel`），内联字段**不触发 `BeforeCreate` 钩子**，
所以每个 repo 在 insert 前**必须显式调 `sharedomain.NewID()`**（`circle_repo_pg.go:87`、`post_repo_pg.go:38`、
`comment_repo_pg.go:36`）。`NewID` 注释（`base.go:41`）已预见到这点。

**为什么 GORM 层生成而非 DB `DEFAULT uuidv7()`**：GORM 会把 `uuid.Nil` 当有效值发送，覆盖 DB 默认；
且 GORM 层生成后 `Create` 返回的 ID 立即可用（如建圈后立即读 `circle.ID` 建成员）。

### 1.2 软删除
`Deleted int16`，`0=normal / 1=deleted`。**手动 `WHERE deleted = 0` 过滤，不用 gorm.DeletedAt 插件**。
（不用插件是为了与 raw SQL / jsonb_to_recordset 批量更新的 `deleted = 0` 条件保持一致。）

### 1.3 时间字段
`CreateTime`（gorm `autoCreateTime`）、`UpdateTime`（`autoUpdateTime`）。DB 是 Postgres 18，列类型 `timestamptz`。

## 二、实体定义规范

实体 = 普通结构体 + `json:`+`gorm:`(+`binding:`) tag + `TableName()`。表名一律 `domains.*`。

值对象/缓存 DTO：**无 gorm tag** 的纯结构体（如 `CircleBaseInfo`、`PostStatistics`、`CircleBrief`）。
跨域 Facade DTO 用 **string ID**（避免强加 uuid 耦合给调用方），如 `user/domain/user.go:58` `UserBrief{ID,Username,AvatarURL}`。

自定义 GORM 类型实现 `sql.Scanner`+`driver.Valuer` 映射 jsonb（`MediaExtraJSON`，`post/domain/post.go:63`）。

枚举：紧贴实体用无类型 `const`（如 `PostStatus*`/`MemberRoleMember/Admin/Owner=10/20/30`）。`pkg/enums/` 包存在但各域大多内联重定义。

## 三、分页（5 种，按数据源选，别发明新风格）

`size` 一律先过 `normalizeSize`：`if size <= 0 || size > 100 { return 20 }; return size`。
⚠️ 每个 handler 包**各自重复定义** normalizeSize（`post handler:223`、`circle handler:326`），未抽共享。

### 3.1 ES `search_after`（主列表风格）
用于 post/circle/user 列表、recommend hot/latest/following tab。不透明 JSON 数组游标。
**HasMore 约定**：ES 仅在 `len(documents) == size`（满页）时返回 search_after（`post.go:542`）。
recommend `getSimpleFeed` 显式 `HasMore: len(ids) == size`（`recommend/application/service.go:213`）。
handler 用 `parseSearchAfter` 解析（`post handler:230`）。

### 3.2 DB keyset cursor（base64 JSON，复合 `(sort_col, id)`）
- **评论**（`comment_repo_pg.go:186-234`）：`buildNextCursor` 编 `{like_count,id}`(sort 0) 或 `{id}`(sort 1)；
  `applyCursorCondition` 发 `WHERE (like_count < ?) OR (like_count = ? AND id < ?)`，靠 UUIDv7 字典序。
- **收藏**（`collect_repository.go:84`）：keyset `(create_time, id) DESC`，索引 `idx_pcollect_user_active`。
- 游标是用户可控 → **防御性解析**：`parseCursorValues` 字段缺失/类型错返回 `fmt.Errorf("%w: ...", domain.ErrInvalidCursor, ...)`，**绝不 panic**（`comment_repo_pg.go:206,234`）。
- HasMore：repo 多取 1 行判断（`size+1` 式）。

### 3.3 ZSET rank/offset（已加圈子、历史）
- **已加圈子**（`circle/application/service.go:641`）：不物化全表——`decodeJoinedCursor` 返回 rank int（base64url JSON `{"r":N}`），
  `PageByRank` 做 `ZREVRANGE start start+limit-1`。浏览模式=1 页；keyword 模式分批(100/批, maxScan 5000, `Truncated` 标志，`:686`）。
  HasMore = `rank+size < card`（`:677`）。
- **历史**（`history/application/service.go:182`）：纯 `offset` over ZSET，`NextOffset` 仅 `offset+size < total` 时返回。

### 3.4 候选池 `offset + pool_token`（仅 recommend tab）
`recommend/application/service.go:83-148`。池是 Redis LIST `feed:recommend:{uid}`（TTL 30min）+ 版本 token。
客户端回传 `pool_token`；不匹配（池已重建）→ 服务端重建 + 回 `offset=0` + `pool_refreshed:true`（`:88,109`）。
HasMore = `offset+size < total`。

### 3.5 ES terms 聚合 `offset`（活跃圈子榜）
`circle/application/service.go:550` + ES `bucket_sort` 切片；offset 翻页 ranked 桶，触 `maxScan=500` → `Truncated`。
**趋势榜翻页排名漂移可接受**（`active-circles-design.md` 已论证）。

## 四、缓存模式

### 4.1 Cache-aside（miss 回填 DB，best-effort）
统一：读缓存 → miss 查 DB → 回填缓存（best-effort，缓存错只记日志不返回）。
例：circle 基础信息（`circle/application/service.go:425`）、post 详情统计（`post/application/service.go:482`）、user（`user/application/service.go`）。

### 4.2 无防穿透锁 —— "restore-if-absent" + HSetNX
**无分布式锁/单飞**。计数器用 lazy restore-once。关键：`SeedPostStatisticsIfAbsent`（`redis/cache.go:552`）
用 `HSetNX` **逐字段**，避免异步浏览自增 goroutine 的 `HINCRBY view_count` 被普通 `HSet` 覆盖（`:549` 注释）。
模式：`Exists` → 缺则 `restorePostStats(from DB)` → 再 `HINCRBY`。竞态窗口被接受（stats 是软信号）。

### 4.3 失效
**主要靠 TTL**，无显式更新删除。例外：退圈 `joinedCache.Remove`（`circle/service.go:533`）；
joined ZSET `Rebuild` = `DEL`+分块`ZADD`（`circle_cache_redis.go:158`）。用户资料更新 fire-and-forget（TTL 限界 staleness）。

### 4.4 压缩
user/circle 基础信息走 `SetJSONCompressed`/`GetJSONCompressed`（zstd，`redis/cache.go:179`）。

### 4.5 TTL 速查
`post:stats` 43min(`postStatsTTL`) / `circle:stats` 24h / `circle:info` 24h / `user:info` 30min /
`circle:joined` 24h / `comment:stats` 43min / like/collect/view ZSET 43min(访问续期) /
`post:hotcap` 43min / `circle:hot` 50h / `feed:recommend`+token 30min(cfg) / `cf:item` 48h(cfg) /
`user:interest_circles` 120min(cfg) / `register:code` 5min。完整表见 `tech-stack.md` §5.2。

## 五、Write-Behind 统计（计数器真值在 Redis）

读路径真值在 **Redis Lua 原子操作**；落库经 MQ 异步批量。流程：
1. 写事件（赞/藏/评论/评论赞）service 先做原子的主操作，再 `redis.ApplyHotDelta(postID,dim,dir)`。
2. 发 Redpanda 事件（如 `post_statistics`/`like_events`）。
3. aggregator 内存 `map[id]Δ` + ticker/数量双触发 flush → 批量 `jsonb_to_recordset` UPDATE 落库。
   **例外：点赞**按 (type,user,target) 保留**末态**，单条 CTE 做行迁移（`INSERT … ON CONFLICT … WHERE deleted=1` / `UPDATE … WHERE deleted=0`
   `RETURNING`），`like_count` 由迁移行数推导 → 重复投递幂等（`like_consumer.go`）。收藏流水同步落库，`SetCollected` 返回 `changed`，只在真迁移时发事件。
4. CDC 把 PG 同步进 ES。

**评论数是例外**：同步落库（`post/application/service.go:967`），不走 MQ。

所有 hot/interaction 事件发布**失败只记日志继续**（best-effort fan-out，`like_event_publisher.go:37`）。

## 六、热度子系统（`docs/hot-sync-design.md`，P0-P3 done）

### 6.1 热度 Δ 流
1. 事件入口（like/collect/comment/comment-like service）做完主操作后调 `redis.ApplyHotDelta(postID,dim,dir)`（`hot_lua.go:99`）。
2. `applyHotDeltaScript`（Lua `hot_lua.go:33`）：**无上限维**（post_like/post_collect/post_share）直接 `weight*dir`；
   **带上限维**（comment/comment_like）对 `post:hotcap:{postID}` Hash 子计数器 check-and-incr，clamp 到剩余预算；undo 落地 0。不变式 `cap % weight == 0`。
3. producer 发 `{PostID,Delta}` 到 `post_hot` topic（Δ≠0 才发），且 `INCR circle:hot:{circleID}` by Δ。
4. `PostHotAggregator`（`hot_consumer.go:31`）：内存 `map[postID]ΣΔ`，双触发 flush（13min ticker OR 500 msgs）→
   批量 `UPDATE domains.post.hot = GREATEST(hot+Δ,0)` → `resolveCircleDeltas` + `fanoutCircleHot` 到 `circle:hot:`。
5. `CircleHotSyncer`（34min）：`SCAN circle:hot:*` + `GETDEL` → 批量 UPDATE `domains.circle.hot` → 条件 HINCRBY `circle:stats` hot（仅当 stats Hash 已存在）。
6. Post→ES 经 CDC。

### 6.2 权重与上限（`conf.Hot` `conf.go:167`）
| 事件 | weight | cap | dim |
|---|---|---|---|
| post_like | 2 | — | post_like |
| post_collect | 5 | — | post_collect |
| post_share | 7 | — (TODO 未实现) | post_share |
| comment | 5 | 5000 | comment |
| comment_like | 1 | 25000 | comment_like |

### 6.3 ES rank_score（`post.go:744`）
sort="hot"：`emit(hot / (ageHours + 2)^0.8)`。P5 时间衰减 `hot_decay` 未实现 → 当前 `hot` 是累积值，老爆款可霸榜（已知限制，`hot-sync-design.md` O4）。

## 七、推荐子系统（`docs/home-feed-api.md` + `cf-item-based-design.md`）

单端点 `GET /post/home?tab=…` 4 tab。5 路召回（`recommend/application/recall.go:38`）：

| 路 | 来源 | 排序 | 默认配额 |
|---|---|---|---|
| C1 兴趣圈 | 已加圈子 | rank_score(hot) | 35% |
| C2 全局热 | 全局 | rank_score | 25%（**也是不足时兜底**） |
| C3 行为圈 | seed 帖→circle_id−已加(缓存) | rank_score | 15% |
| C4 最新 | 全局 | create_time desc | 10% |
| C5 CF 相似 | seed 帖→`cf:item:{seed}` ZSET 聚合 Σsim | sim desc | 15% |

`interleave` 轮询合并保每路序 → `dedupPreserveOrder` → 可选排除已交互 → C2 兜底 → 截 `poolSize`(默认150)。
**每路失败 log+返回空；ES 全挂只 C5；C5 空走 C2；feed 永不空。匿名 → 401。**

CF item-based：`domains.post_interaction(user,post,weight,ts)` 由 5 事件灌数 → `ItemCFSyncer`(24h) 算余弦共现相似度 top-K=50 → `cf:item:{postID}` ZSET。

## 八、响应与错误（写 handler 必看）

### 8.1 统一信封 + httputil 助手
`{code, message, data}`（`httputil/response.go:40`）。`ResponseCode`（`:16`）从 200 起镜像 HTTP（CodeBadRequest=201...）。
`httpStatusMap`（`:122`）映射业务码→HTTP 状态。**禁 `c.JSON`**，统一用助手：
`Success`/`Created`/`BadRequest`/`Unauthorized`/`Forbidden`/`NotFound`/`Conflict`/`TooManyRequests`/
`InternalError`/`ServiceUnavailable`/`Pagination`（`:157-321`）。**错误助手内部已 `c.Abort()`**（`:192`）防双写。

### 8.2 两层错误 + 谓词
- domain 层哨兵：`ErrXxxNotFound`/`ErrPostLocked`/`ErrInvalidCursor`（`<domain>/domain/`）。
- application 层：未导出 `errFoo` + 导出 `IsFooErr(err)` 谓词（`<domain>/application/errors.go`）。
- 参数化错误用结构体：`mutedError`+`IsMutedErr(err)(time.Time,bool)`（`post/application/errors.go:20`）。

handler 写 `write<Domain>Error(c, err)`：`switch application.Is…Err(err)` / `errors.Is(err, domain.Err…)` → 对应 httputil 助手；未知落 `InternalError`（先日志）。见 `circle handler:347`、`comment handler:202`。

### 8.3 HTTP 状态约定
400（坏 JSON/参数/坏游标/不支持的 tab）/ 401（缺/坏 token）/ 403（非成员/禁言/私圈）/ 404（未找到）/ 409（已存在）/ 429（OTP 限流）/ 503（OAuth 不可用）/ 500（兜底，zap 记录）。

### 8.4 鉴权两层
- 路由级 `authCheck`（composition.RequireLogin，`auth.go:25`）。
- handler 级 `requireUserID(c)`（读 `c.LoginID()`→`uuid.Parse`）；匿名可读用 `requireUserIDAllowAnon`。

## 九、文本处理与防御

- **SanitizeForPg**（`pkg/server/utils/sanitize.go:18`）：剥 NULL 字节/非法 UTF8/控制字符，入 PG 前在 application 层调（`post/service.go:358`、`comment/service.go:234`），防 `invalid byte sequence for encoding UTF8`。有回归测试 `sanitize_test.go`（对应生产事故）。
- **GenerateSummary**：同包，生成摘要。
- **防御性游标**：用户可控游标全用逗号 ok 断言 + `%w` 包装 ErrInvalidCursor，绝不 panic。
- **异步 fire-and-forget**：自启 goroutine + `recover()` + `context.Background()`（`post/service.go:447`）。

## 十、测试现状（稀疏）

仅 5 个 `*_test.go`：`comment/cursor_test.go`（游标防御性解析回归）、`composition/cors_test.go`、
`utils/sanitize_test.go`、`appctx/hertzadapter/adapter_test.go`、`util/password/password_test.go`。
**纯 stdlib testing**，无 testify/mock，无集成测试/DB/Redis/ES fixture。service/MQ consumer 基本未测。
跑：`go test ./pkg/...`。加新功能**至少给纯函数（游标/解析/规整）补单测**。
