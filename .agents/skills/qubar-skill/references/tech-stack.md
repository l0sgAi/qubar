# tech-stack.md — 技术栈、中间件、配置与启动

> 本文件详述 qubar 的依赖、基础设施客户端、配置系统、启动/关停流程、后台任务。
> 改配置/加配置项/加后台 job/碰中间件前读这份。先回 `SKILL.md` 看"核心红线"。

## 一、依赖清单（go.mod）

Go module 名是 **`interestBar`**（不是 qubar），Go 版本 **1.25.4**。

| 类别 | 包 | 版本 |
|---|---|---|
| HTTP 框架 | `github.com/cloudwego/hertz` | v0.10.5 |
| ORM | `gorm.io/gorm` | v1.31.1 |
| PG 驱动 | `gorm.io/driver/postgres`（底层 `jackc/pgx/v5`） | v1.6.0 |
| Redis | `github.com/redis/go-redis/v9` | v9.19.0 |
| Elasticsearch | `github.com/elastic/go-elasticsearch/v8` | v8.19.1 |
| 消息队列 | `github.com/segmentio/kafka-go`（Redpanda = Kafka 协议） | v0.4.50 |
| 鉴权 | `github.com/sa-tokens/sa-token-go/integrations/hertz` + `storage/redis` + `stputil` | v0.2.2 |
| OAuth2 | `golang.org/x/oauth2` | v0.34.0 |
| 配置 | `github.com/spf13/viper` | v1.21.0 |
| 配置中心 | `github.com/nacos-group/nacos-sdk-go/v2` | v2.3.5 |
| 热重载 | `github.com/fsnotify/fsnotify` | v1.10.1 |
| UUID | `github.com/google/uuid` | v1.6.0 |
| 日志 | `go.uber.org/zap` | v1.27.1 |
| 对象存储 | `github.com/aws/aws-sdk-go-v2`（+config/credentials/service/s3） | v1.x |
| 压缩 | `github.com/klauspost/compress`（zstd 缓存压缩） | v1.18.3 |
| Markdown | `github.com/gomarkdown/markdown` | — |
| 密码 | `golang.org/x/crypto`（Argon2id） | v0.46.0 |

无独立 validation 库（validator/v10 经 hertz BindJSON 传递引入）。无 rate-limit 库（限流仅在 OTP/注册流按端点用 Redis key 做）。

## 二、入口与启动序列

### 2.1 flags（`cmd/main.go:10-14`）
```go
flag.StringVar(&config, "c", "configs/config.yaml", "本地兜底配置")
flag.StringVar(&bootstrap, "b", "configs/bootstrap.yaml", "Nacos 引导文件，空则跳过 Nacos")
flag.Parse()
apps.Run(config, bootstrap)
```

### 2.2 Run() 启动顺序（`cmd/apps/server.go`，行号即源码）

1. **Config** `conf.InitConfig`（`:21`）—— Nacos 优先，失败回落本地 yaml。
2. **密码参数** 注入 Argon2id（`:25`）。
3. **Logger** `logger.InitLogger()`（`:35`）。
4. **PG/GORM** `pgsql.InitDB()`（`:38`），失败 `os.Exit(1)`。
5. **Redis** `redis.InitRedis(...)`（`:42`），失败 `Fatal`。
6. **Sa-Token** `auth.InitSaToken()`（`:48`），失败 `Fatal`。
7. **S3** `s3storage.InitS3Client()`（`:53`），失败 `Fatal`。
8. **ES** `elasticsearch.InitElasticsearch()`（`:58`）—— **非致命**，失败仅 `Warn`（无搜索也能跑）。
9. **Mailtrap 邮件** `emailutil.InitEmail()`（`:64`）—— 非致命。
10. **Redpanda producers + consumers**（`:70-181`）：每个 producer 非致命（失败 `Warn`，无 MQ 也能跑）；
    成功则 `go StartXxxConsumerWithRetry()`。顺序：
    - 圈子统计 → Post 统计 → 点赞事件 → `redis.InitLikeLuaScripts()` → 收藏事件 → `InitCollectLuaScripts`
      → `InitViewLuaScripts` → 历史事件 → `InitHistoryLuaScripts` → `InitHotLuaScripts` → Post 热度
      → `StartCircleHotSyncerWithRetry()`（`:164`）→ Post 交互(CF灌数) → **`StartItemCFSyncerWithRetry()`（仅 `Recommend.CF.Enabled`，`:178`）**。
11. **Router** `router.InitRouter()`（`:184`）。
12. **Run** `r.Spin()`（`:191`）阻塞于 SIGINT/SIGTERM。

### 2.3 关停（`cmd/apps/server.go:194-206`）

`Spin()` 返回后：`redis.CloseRedis()` → `auth.CloseSaToken()` → 7 个 producer `Close*`
→ `redpanda.StopCircleHotSyncer()`（`:204`）→ `StopItemCFSyncer()`（`:205`）。
⚠️ PG/ES 客户端无显式 close；部分 consumer 读 goroutine 会随进程退出（`consumer.go:519` 注释）。

## 三、配置系统

### 3.1 全局访问（`pkg/conf/conf.go:12`）
```go
var Config *AppConfig
```
到处用 `conf.Config.Xxx`。

### 3.2 AppConfig 结构（`pkg/conf/conf.go:14-30`）
节：`Server, CORS, App, Log, Oauth, Pgsql, Redis, SaToken, S3, Elasticsearch, Redpanda, Hot, Recommend, Mailtrap, Security`。
关键节：`Hot`（`:168`，`HotWeight`+`HotCap`）、`Recommend`（`:190`，`CF`+`Feed`）、`Redpanda`（`:134`，最大，~30 字段）。
所有字段带 `mapstructure`/`json`/`yaml` 三 tag。

### 3.3 两级加载（`InitConfig` `conf.go:272`）
1. 先试 `initFromNacos(bootstrapPath)`，成功返回；`errNoBootstrap` 静默回落，其它错记日志回落。
2. 兜底 `initFromFile(fallbackPath)`（`conf.go:287`）：viper + `WatchConfig`/`OnConfigChange`（fsnotify 热重载，重 `Unmarshal`）。

### 3.4 Nacos（`pkg/conf/nacos.go`）
- `currentEnv`：读 `APP_ENV`，默认 `dev`，非 `prod` 都当 `dev`。
- `buildClient`：`NotLoadCacheAtStart: true`（避免 stale cache 掩盖失败）。
- **热重载不重建 DB/Redis/Redpanda 连接**（需重启，`nacos.go` 注释）。
- `initFromNacos` 带 `defer recover()` 优雅回落。

### 3.5 加新配置项的范式（必须三处同步）
1. `configs/config.yaml` 加 key + 默认值。
2. `pkg/conf/conf.go` 对应节结构体加字段（带 `mapstructure`/`json`/`yaml` tag）。
3. 消费处读 `conf.Config.Xxx`，**`<=0` 提供常量兜底**（仿 `circle_hot_syncer.go:40` interval 默认 34）。
参考现有节：`Hot`（`conf.go:168`）、`Recommend`（`conf.go:190`）、`Feed{PoolSize,TTLMinutes,QuotaC1..C5,...}`（`conf.go:196`）。

## 四、PostgreSQL / GORM

### 4.1 初始化（`pkg/server/storage/db/pgsql/connect.go`）
- 全局 `var DB *gorm.DB`（`:14`）；`DBHolder`（`:20`）过渡期 DI 包装，`Get()` 返回全局 DB。
- DSN 拼接；GORM 日志级别按 `log_mode`；`SetMaxIdleConns`/`SetMaxOpenConns`。
- **无 AutoMigrate**（`:55` 注释）：schema 由 `docs/pgsql-ddl/` SQL 脚本管理，运行时角色（`qubar_web_app`）无 ALTER 权限。
  **改表先改 `docs/pgsql-ddl/` 对应领域文档，由 DB-owner 执行。**

### 4.2 表名（schema 限定）
所有 `TableName()` 返回 `domains.*`：`domains.circle`/`domains.circle_member`/`domains.post`/`domains.post_like`/
`domains.comment`/`domains.comment_like`/`domains.users`/`domains.post_collect`/`domains.post_view_history`/`domains.post_interaction`。

### 4.3 查询风格
- GORM 查询构建器：`r.db.WithContext(ctx).Where(...).First/Find/Order/Create`。
- 复杂批量/聚合用 **raw SQL**：`pgsql.DB.Raw(...)` / `tx.Exec(...)`，配合 **`jsonb_to_recordset`** 批量 UPDATE。
  所有 Redpanda consumer 用此模式原子批量 upsert（`consumer.go:205`）：
  ```sql
  UPDATE domains.circle c SET member_count = GREATEST(c.member_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
  FROM (SELECT * FROM jsonb_to_recordset(?::jsonb) AS v(circle_id uuid, delta BIGINT)) v
  WHERE c.id = v.circle_id AND c.deleted = 0
  ```
  包在 `pgsql.DB.Transaction(...)` 里。
- Postgres 18，内置 `uuidv7()`；DDL 有 `DEFAULT uuidv7()` 兜底，但应用在 GORM 层生成 UUIDv7。

## 五、Redis（`pkg/server/storage/redis/`）

### 5.1 初始化（`cache.go`）
- 全局 `Client *redis.Client` 和 `ctx = context.Background()`（`:17-21`）。
- `InitRedis(addr,password,db)` + `Ping`。包级 helper（`Set/Get/Del/Exists/Expire/SetJSON/GetJSON/Incr/Decr`）。
- **压缩**：`SetJSONCompressed`/`GetJSONCompressed`（复用 zstd encoder/decoder）—— user/circle 基础信息走压缩。

### 5.2 key 规范（`constants.go`，权威清单）
| key | 类型 | TTL | 语义 |
|---|---|---|---|
| `register:code:{email}` | string | 5min | 注册验证码 |
| `circle:info:{id}` | Hash(JSON,zstd) | 24h | 圈子基础信息 |
| `circle:stats:{id}` | Hash | 24h | member/post_count, hot |
| `circle:hot:{id}` | string(int) | 50h | **热度 Δ 累加器**（34min GETDEL 清零） |
| `circle:joined:{uid}` | ZSET | 24h | 已加圈子（score=加入时间） |
| `user:info:{id}` | Hash(JSON,zstd) | 30min | 用户基础信息 |
| `user:interest_circles:{uid}` | SET | 120min | 行为兴趣圈子（C3） |
| `post:stats:{id}` | Hash | 43min(`postStatsTTL`) | view/comment/like/collect_count |
| `post:hotcap:{id}` | Hash | 43min | 热度上限子计数(comment/comment_like) |
| `post:viewdedup:{pid}:{uid}` | string | 5min | 浏览去重 |
| `comment:stats:{id}` | Hash | 43min | like_count |
| `user:like:posts:{uid}` | ZSET | 43min | 赞过的帖(score=访问ms,cap 2000；miss≠未赞，须回源 DB) |
| `user:like:comments:{uid}` | ZSET | 43min | 赞过的评论(cap 2000) |
| `user:collect:posts:{uid}` | ZSET | 43min | 收藏的帖(cap 2000) |
| `user:view:posts:{uid}` | ZSET | 43min | 浏览历史 |
| `cf:item:{postID}` | ZSET | 48h(cfg) | item-CF 相似帖(score=相似度) |
| `feed:recommend:{uid}` | LIST | 30min(cfg) | 推荐候选池 |
| `feed:recommend:token:{uid}` | string | 30min | 候选池版本 token |

⚠️ `circle:stats.hot`（读路径热值）与 `circle:hot:`（Δ 累加器）是**两回事**——前者供读，后者待落库。

### 5.3 加新 Redis key 的范式
在 `constants.go` 加：前缀 `const`（带注释：类型/语义/TTL）+ `GetXxxKey(...)` helper。
**别在 domain infra 里硬编码 key 字符串**。参考 `trending-design.md` 的 `trending:{dim}:{window}`。

### 5.4 Lua 脚本（原子多键操作）
启动时 `Client.ScriptLoad`，缓存 SHA，`EvalSha` + NOSCRIPT 重试重载：
- `hot_lua.go` `applyHotDeltaScript`（`:33`）：原子 weight×dir×clamp 热度 Δ，对 `post:hotcap`。
- `like_lua.go` `likeSetScript` / `collect_lua.go` `collectSetScript`：原子**设值**（传期望态 want；已是期望态返回 0 不改计数；否则 ZSET 增删 + HINCRBY + 超 cap 淘汰最旧）。
  调用方须先解析真实状态（ZSET miss 回源 DB 并回填），见 `like/application/service.go`。仅 `NOSCRIPT` 时重载脚本。
- `view_lua.go` / `history_lua.go`：浏览/历史原子脚本。
对应 `Init{Hot,Like,Collect,View,History}LuaScripts` 全在 bootstrap 调。

### 5.5 计数 helper（`cache.go`）
`IncrementCircle{Member,Post,Hot}` / `Decrement…`（原子 HINCRBY，负向 clamp 到 0）；
`SeedPostStatisticsIfAbsent`（`HSetNX` 逐字段，避免覆盖并发 `HINCRBY view_count`，`:552`）。

## 六、Elasticsearch（`pkg/server/storage/elasticsearch/`）

### 6.1 初始化（`init.go`）
- 全局 `var Client`（`:13`）。
- 数据同步：**Debezium CDC**（外部）把 PG 变更灌进 ES。**qubar 只管索引模板 + circle 索引**，不写文档。
- `ensureIndexTemplate()`（`:67`）：`pg_domains_template`（priority 200）保证字段映射正确（ID→keyword，全文→`ik_max_word`/`ik_smart`+`.keyword`，`*_time`→date）。
  ⚠️ 模板用 `match_pattern: "regex"`（`:91`）—— **关键**，否则 pattern 被字面对待。
- `createCircleIndex()`（`:170`）：circle 索引在启动时显式建；post/user 索引来自 CDC。

### 6.2 索引命名（`indices.go`）
`IndexCircle/Post/User/Comment = "circle"/"post"/"users"/"comment"`。
`GetIndexName(entity) = {IndexPrefix}.{entity}`（prefix 来自 `conf.Config.Elasticsearch.IndexPrefix`，如 `pg.domains`）。
→ `pg.domains.post`。helper：`GetCircleIndexName`/`GetPostIndexName`/`GetUserIndexName`/`GetCommentIndexName`。

### 6.3 查询体范式
统一 `map[string]interface{}` 序列化 JSON，`Client.Search(WithIndex, WithBody, WithTrackTotalHits(true))`。
响应从 `map[string]interface{}` 用类型断言 helper 解析（`getString`/`getInt`/`getInt16`，`post.go:482`）。
翻页用 `search_after`（keyset）on `id desc`。

### 6.4 关键函数（`post.go`）
- `SearchPosts(keyword,circleID,size,searchAfter)`（`:52`）：bool.must `deleted=0`+`status=1`，keyword `multi_match`（title^3,summary^1）。
- `SearchHomeFeed(sort,circleIDs,size,searchAfter)`（`:711`）：sort="hot" 用 **`rank_score` 运行时脚本**（`:744` Painless）：
  ```
  double ageHours = (now - create_time_ms)/3600000.0;
  emit(hot / Math.pow(ageHours + 2, 0.8));   // 时间衰减热度
  ```
  sort="latest" → `create_time desc`。
- `AggregateActiveCircles(size,offset)`（`:586`）：terms 聚合 `circle_id` over 7d 窗口 + `bucket_sort` + `cardinality`。
  这是"窗口化 ES 聚合榜"的范式蓝本（`active-circles-design.md` / `trending-design.md`）。

## 七、Redpanda / Kafka（`pkg/server/storage/redpanda/`）

### 7.1 producers（`producer.go`）
7 个包级 writer：circle stats / post stats / like / collect / history / post hot / post interaction。
共享配置：`AllowAutoTopicCreation: true`、`LeastBytes` balancer、`Snappy`、`Async:true`、`RequiredAcks:RequireOne`、
`MaxAttempts:5`。⚠️ `LeastBytes` **忽略 message key**，不保证同 key 有序。
**例外（like / collect）**：`Async:false` 同步 ack（`syncPublishTimeout` 3s），投递失败回滚缓存/流水并返回 503；
like writer 用 `kafka.Hash{}`（key=`user:target`，消费者"末态为准"依赖同分区有序）。每个 producer `InitXxxProducer()` + `PublishXxx` + `CloseXxxProducer`。
只有 `InitRedpandaProducer`（circle）真拨号 + `conn.Controller()` 验证连通（gate consumer），其余返回 nil（best-effort）。

### 7.2 topics / groups（`constants.go` + config.yaml）
`circle_statistics` / `post_statistics` / `like_events` / `collect_events` / `post_view_history` / `post_hot` / `post_interaction`。
每个有 `*_consumer_group` + `*_flush_interval`（**单位混杂**：有秒有分，看 config）。

### 7.3 consumer 范式（`consumer.go`）
`StartXxxConsumer()`：`kafka.Dialer{Timeout:10s, DualStack:true, Resolver:nil}`（Resolver nil 故意——避免 advertised-address 缓存）；
`kafka.NewReader{Brokers,Topic,GroupID,MinBytes:10KB,MaxBytes:10MB,CommitInterval:1s}`；
创建 `XxxAggregator`（`time.Ticker` @ FlushInterval）两 goroutine：`aggregator.run()`（ticker flush）+ 读循环。
读错误 → `waitAfterReadError`（`reader.go`，1s 起倍增封顶 30s，成功读取后 `Reset`）。读阻塞时无数据不会返回错误，出错即真实故障。
⚠️ `ReadMessage`+`CommitInterval` 是"读即提交"：flush 前崩溃会丢缓冲事件。
**新 consumer 优先仿 `like_consumer.go`**：`FetchMessage` + 落库成功后 `CommitMessages`、失败合并回缓冲重试、
可取消 reader ctx + `StopXxxGlobal()` 关停排干（在 `cmd/apps/server.go` 关停序列调用）。
`StartXxxConsumerWithRetry()` 线性退避重试 10 次。

### 7.4 aggregator → 批量落库
内存 `map[uuid.UUID]delta`（mutex），ticker/stop flush，单次 `pgsql.DB.Transaction` + `jsonb_to_recordset` 批量 UPDATE。

### 7.5 syncers（cron 式，非 consumer）
- `CircleHotSyncer`（`circle_hot_syncer.go:28`）：34min ticker，`SCAN circle:hot:*` + `GETDEL`（读后清零）→ 批量 UPDATE `domains.circle.hot` → `refreshCircleHotCache`（**仅当 stats Hash 已有 member_count 才 HINCRBY**，避免半截 Hash，`:95`）。
- `ItemCFSyncer`（`item_cf_syncer.go:30`）：24h ticker，算 post↔post 余弦共现相似度，top-K 写 `cf:item:{postID}` ZSET。门控 `Recommend.CF.Enabled`。

### 7.6 加新后台 job 的范式（仿 `CircleHotSyncer`）
struct `{mu sync.Mutex, ticker *time.Ticker, stopChan chan struct{}, stopped bool}`；`run()` select `ticker.C`/`stopChan`；
`Stop()` 用 `stopped` flag 幂等 + `close(stopChan)` + 关停排干；包级单例 + `StartXxxWithRetry()`/`StopXxx()`。
在 `cmd/apps/server.go` 启停点（`:164`/`:204-205` 附近）成对加 `go Start…` / `Stop…`。

## 八、鉴权（sa-token-go）

### 8.1 初始化（`pkg/server/auth/sa_token_init.go`）
`InitSaToken()`：redis URL → sa-token Redis storage → `sahertz.DefaultConfig()` → 覆盖 `TokenName`/`Timeout`(默认 259200=3d)/`ActiveTimeout`(1800=30min) → `sahertz.NewManager` → `sahertz.SetManager`（全局）。
已从 gin 迁到 hertz（integrations/gin → hertz）；`stputil` API 不变。

### 8.2 RequireLogin 中间件（`pkg/composition/auth.go:25`）
框架无关 `routing.HandlerFunc`：读 token header → 空 → 401 → `stputil.IsLogin` → 失败 401 → `stputil.GetLoginID` → `c.SetLoginID`。
**不解析 userID**（handler 懒解析）。**故意不用** `sagin.CheckLogin`（gin 中间件破坏 `c.Next()` 语义，`:20-24`）。

### 8.3 handler 取 userID
`c.LoginID()` → `uuid.Parse`（`post/interfaces/http/handler.go:210`）。`AppContext` 暴露 `UserID/SetUserID`/`LoginID/SetLoginID`/`Device/SetDevice`（`appctx/context.go:72-82`）。

### 8.4 OAuth（`pkg/server/auth/`）
`Provider` 接口（`provider.go:20`）：`Name/OAuthConfig/FetchUser/UserLookupField/ApplyProviderID/...`。注册 `google/github/azure`
（⚠️ config key 是 microsoft，provider 注册名是 azure，`azure.go`）。`golang.org/x/oauth2`。
**代理支持**（`proxy.go`）：`buildOAuthHTTPClient` 从 `conf.Config.Oauth.ProxyURL` 克隆 transport 设代理（空=直连）。
`oauthProviderAdapter` 把代理感知 `http.Client` 注入 ctx 再 `Exchange`/`FetchUser`。

## 九、HTTP server / router（CloudWeGo Hertz）

### 9.1 InitRouter（`pkg/server/router/router.go:19`）
`server.Default(WithHostPorts(":port"), WithMaxRequestBodySize(50<<20))`（body 限 4MB→50MB 适配图片上传，`:22`）。
中间件：`middleware.Logger()` → `middleware.CORS()`。
`composition.RegisterDomainRoutes(hertzadapter.ForEngine(h))`（`:33`）。**不调 Spin**，调用方调。**无 rate-limit 中间件**。

### 9.2 框架无关路由抽象（共享内核）
- `pkg/shared/routing/group.go`：`HandlerFunc = func(c appctx.AppContext)`（`:20`）、`RouterGroup`（`:26`）。
- `pkg/composition/hertzadapter/group.go`：`ForEngine`/`ForGroup`，`toHertzHandlers`（`:99`）把域 handler 包进闭包调 `h(appctxhertz.New(ctx,c))`。
- `pkg/shared/appctx/hertzadapter/adapter.go`：`AppContext` 的 hertz 实现（唯一实现）。

## 十、S3 / 日志

### 10.1 S3（`pkg/server/storage/s3/`）
`sync.Once` 守卫 `InitS3Client`；`aws-sdk-go-v2/config` + 静态凭证；设 `endpoint` 则 `UsePathStyle`（MinIO 兼容）。
预签名默认 1h；可选 CloudFront 域。storage 域用。

### 10.2 日志（`pkg/logger/logger.go`）
全局 `var Log *zap.Logger`（`:11`）。`logger.Log.Info/Error/...`（生产用 `fmt.Sprintf`）。
`InitLogger`：级别从 `conf.Config.Log.Level`（默认 Info），`zap.AddCaller()`，`ReplaceGlobals`。
**writer 只写 os.Stdout**（无 lumberjack 文件轮转，`log.director` 配置当前未用）。
