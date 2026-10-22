# 未读通知数 SSE 实时推送 — 后端设计文档

> 目标：新增 `GET /notice/stream` SSE 通道，未读数变化时服务端主动推送全量未读数；前端保留 30s 轮询降级。
> 基线：前端需求文档（2026-09-01 版，`qubar-frontend` `src/api/notice.js`）；本仓 notice 域现状见 `docs/notice-design.md`。
> 关联代码：`pkg/domains/notice/`、`pkg/server/storage/redpanda/notification_consumer.go`、`pkg/composition/server.go`。

## 一、现状盘点

| 环节 | 现状 | 文件 |
|---|---|---|
| 未读数读取 | `GET /notice/unread-count`，Redis String 缓存优先，miss 回源 DB COUNT 回填 | `pkg/domains/notice/application/service.go:165` |
| 未读数变更-新通知 | Redpanda consumer 聚合 flush：upsert DB → `incrNoticeUnread`（INCRBY+EXPIRE 管道） | `notification_consumer.go:204,450` |
| 未读数变更-已读 | `MarkRead`（DecrBy floor 0）/ `MarkAllRead`（Set 0），DB 更新成功后改缓存 | `service.go:183,200` |
| 鉴权 | `RequireLogin` 中间件只读 header `satoken`，stputil 校验，loginID 写入 AppContext | `pkg/composition/auth.go:25` |
| 路由抽象 | 领域 handler 只拿 `appctx.AppContext`（框架无关），hertz 适配在 `composition/hertzadapter` | `pkg/shared/appctx/context.go` |
| SSE 能力 | hertz v0.10.5 **内置** `pkg/protocol/sse`：`NewWriter(c)` 自动 hijack chunked writer、设 `Content-Type: text/event-stream; charset=utf-8` 与 `Cache-Control: no-cache`，每次 Write 后 Flush；`WriteComment("ping")` 发注释行 | 无需引新依赖 |
| CORS | 全局中间件 `Allow-Headers` 已含 `satoken` | `pkg/composition/middleware/cors.go:47` |

关键缺口：

1. **无服务端推送通道**——所有变更只落 Redis 计数器，前端只能轮询。
2. **AppContext 抽象不暴露流式写**——SSE 需 hijack response writer，现有 `appctx.AppContext`（`context.go:23`）无此能力，而红线禁止领域层 import hertz。
3. **触发点分散两处**：consumer（`notification_consumer.go:flush`）与 service（MarkRead/MarkAllRead），需统一收口到推送入口。
4. **登出/过期无感知**——长连接建立后 token 失效无任何清理路径。

## 二、对原始需求评估

| 需求条目 | 评估 | 处置 |
|---|---|---|
| 3.1 query 传 token（`?satoken=`） | 合理。`RequireLogin` 只读 header，SSE 传输 handler 在 composition 层可自行加 query 兜底 | 采纳 |
| 3.1 `X-Accel-Buffering: no` | hertz `sse.NewWriter` 不设此头，手动补 | 采纳 |
| 3.2 事件 id 单调递增 + `retry: 5000` | id 用「毫秒时间戳-per 用户序号」；retry 只在首事件写一次（hertz `Event.SetRetry`） | 采纳 |
| 3.2 心跳 25s `: ping` | `Writer.WriteComment("ping")`，每次 Write 自带 Flush | 采纳 |
| 3.3 推全量 + 1s 合并 | 计数器真值在 Redis（INCRBY/DECRBY 先行），推送时直接读缓存得全量；hub 内 per-user 合并窗口 | 采纳 |
| 3.4 重连不补历史、只推当前值 | 连接建立即推全量，`Last-Event-ID` 收到忽略 | 采纳 |
| 3.4 连接存续期 token 过期 → 推 `auth-expired` 后断 | 见决策 D2：心跳节拍复检 token | 变体实现 |
| 3.5 每用户连接上限 5 | 见决策 D1：超限踢最旧（#127 由拒新 429 改） | 变体实现 |
| 3.5 登出主动断开 | 同 D2，心跳复检覆盖登出（≤25s 延迟） | 变体实现 |
| 4.2 多实例 Redis pub/sub | 当前单实例。注册表按 `user_id → connections` 设计，P1 接广播层 | P0 预留，P1 实施 |

### 待用户拍板的决策

- **D1 超限策略**：需求允许「拒新」或「踢最旧」。~~原选拒新（429）~~，#127 改为**踢最旧**：新连接总能建立，
  该用户 connID 最小的连接收到 `conn-evicted` 事件后被服务端断开，前端收到即 `es.close()` 回落轮询（不自动重连，避免互踢抖动）。
  改的理由（2026-10-22 压测发现 6）：浏览器 `EventSource` 遇非 200 永久放弃重连，429 会让页面实时通知静默失效；
  而超限时最旧的连接最可能是已断开但未被感知的僵尸（半开 TCP 写不报错、代理未透传断开），踢掉它恰好回收名额。
  「返回可重试响应」不可行：EventSource 只对 200 `text/event-stream` 按 `retry` 重连，名额不释放则只是空转重试。
- **断开感知（#127）**：`server.WithSenseClientDisconnection(true)` 让 netpoll 在对端断开（hup）时取消连接 ctx，
  SSE 泵循环 select 该信号即 `hub.Remove`，不再等下次写失败（最长一个心跳周期）。该开关全局生效，
  由最先挂的 `middleware.DetachClientCancel` 收口：其余接口拿 `context.WithoutCancel`（行为不变），
  只有 SSE handler 经 `middleware.ClientGone(ctx)` 订阅断开信号。netpoll 的 `CloseCallback` 要等 handler 返回才执行，不可用。
- **D2 登出/过期断连**：需求要「主动断开」。两案：
  - (a) **心跳节拍复检**（推荐 P0）：连接持有 token 串，每 25s 心跳时 `stputil.IsLogin(token)`，失效则推 `auth-expired` 后关连接。覆盖登出+过期+Kickout，零跨域耦合；代价是最长 25s 延迟。
  - (b) 即时断开：auth 域 Logout/Kickout 后经 composition 桥接调 hub。零延迟但引入 auth→notice 新跨域依赖，且 Kickout 有找回密码等多入口易漏。
  - 推荐 (a)，(b) 列 P1 增强。
- **D3 SSE handler 落点**：AppContext 无流式写能力。两案：
  - (a) **composition 层裸 hertz handler**（推荐）：`pkg/composition/notice_stream.go` 直接拿 `*app.RequestContext`，业务状态全在 notice application 的 hub，handler 只做 auth+IO 泵。composition 已有 import hertz 先例（`hertzadapter/group.go`）。
  - (b) 给 AppContext 加 `SSEWriter()` 方法：污染框架无关抽象，为单端点不值得。

## 三、推送架构

### 3.1 组件

```
┌─────────────────────────── notice/application ───────────────────────────┐
│ StreamHub（纯 Go，新文件 stream_hub.go）                                  │
│   users: map[uuid]→userEntry{ conns: map[connID]chan StreamEvent,        │
│                               pending int64, dirty bool,                  │
│                               lastSent int64, hasSent bool, seq uint64 }  │
│   Add(userID) (conn, ok)      // 上限 5，超限 ok=false                    │
│   Remove(userID, connID)                                                 │
│   Publish(userID)             // 标 dirty，不立即发（合并窗口）           │
│   sweeper goroutine           // 每 coalesce_ms 扫 dirty：                │
│                               //   读 CountReader 得全量 → 与 lastSent    │
│                               //   比对，变了才投递到各 conn channel       │
│ CountReader func(ctx,userID)(int64,error)  // setter 注入 =               │
│                               //   noticeSvc.GetUnreadCount（缓存优先）   │
└──────────────────────────────────────────────────────────────────────────┘
        ▲ Publish                       ▲ Add/Remove + chan 消费
        │                               │
┌───────┴─────────────┐   ┌─────────────┴──────────────────────────────┐
│ 触发源（2+1 处）     │   │ SSE 传输 handler（composition，裸 hertz）   │
│ ① consumer.flush    │   │  auth(header→query 兜底) → sse.NewWriter →  │
│   upsert→INCRBY 后  │   │  立即推全量 → for select:                   │
│   调 hook(userIDs)  │   │    conn chan → WriteEvent(unread-count)     │
│ ② MarkRead/         │   │    25s ticker → WriteComment("ping") +      │
│   MarkAllRead 成功后│   │      stputil.IsLogin 复检 → auth-expired    │
│   svc.hub.Publish   │   │    写错 → Remove + return                   │
│ ③ (P1) Redis 订阅   │   │  Last-Event-ID 忽略（连接即推全量）          │
└─────────────────────┘   └────────────────────────────────────────────┘
```

### 3.2 推送时序（以新通知为例）

```
like/comment 发生 → 事件进 Redpanda
  → consumer flush 窗口（现有 5s）：upsert 事务完成 → INCRBY 计数器
  → hook: hub.Publish(recipient1, recipient2, ...)
  → sweeper（≤1s）：CountReader 读缓存全量 → 与 lastSent 不同才写 conn chan
  → handler WriteEvent → 客户端 ≤1s 收到（叠加 consumer flush 窗口，端到端 ≤6s，
    满足"1s 内"的验收口径指推送段；flush 窗口已由 notice-event 链路固有，
    如需压缩可调 notice_event_flush_interval）
```

一致性要点：

- **先落库/先改计数，后 Publish**——三处触发源都满足「推送在状态变更之后」，客户端收到推送即拉详情读得到数据。
- **推全量 + lastSent 去重**——未变不推（满足 3.5「无消息不推」）；1s 窗口内多次变化只推最终值。
- **计数器漂移自愈沿用现状**——推送值与 `GET /notice/unread-count` 同源（同一 `GetUnreadCount`），语义完全一致。

### 3.3 事件格式

```text
event: unread-count
id: 1756713600123-7        // {unixMilli}-{perUserSeq}，单调递增
retry: 5000                // 仅首事件携带
data: {"unread_count": 3}

: ping                     // WriteComment("ping")，25s 一次
```

`auth-expired`：`WriteEvent("", "auth-expired", nil)`（无 id 无 data），随后关连接。

## 四、文件改动清单

| # | 文件 | 改动 |
|---|---|---|
| 1 | `pkg/domains/notice/application/stream_hub.go`（新） | `StreamHub` 接口 + impl + `StreamEvent{ID, Count}` + sweeper goroutine + `Stop()` |
| 2 | `pkg/domains/notice/application/service.go` | `NoticeService` 加 `SetStreamHub(h)`；`MarkRead`/`MarkAllRead` 缓存更新成功后 `hub.Publish(userID)`（hub 为 nil 跳过，保测试兼容） |
| 3 | `pkg/server/storage/redpanda/notification_consumer.go` | 加包级 `SetNoticeUnreadHook(func([]uuid.UUID))`；`flush()` 在 `incrNoticeUnread` 成功后调 hook（keys of unreadDeltas）；nil 跳过 |
| 4 | `pkg/composition/notice_stream.go`（新） | 裸 hertz handler `ServeNoticeStream(hub, countReader)`：auth（header→query）、超限踢最旧（conn-evicted）、SSE 泵循环、心跳+token 复检、auth-expired、客户端断开即退出 |
| 5 | `pkg/composition/server.go` | `newNoticeService` 处构造 hub；`noticeSvc.SetStreamHub(hub)`；`hub.SetCountReader(noticeSvc.GetUnreadCount)`；`redpanda.SetNoticeUnreadHook(hub.PublishBatch)`；返回 hub 供路由层用 |
| 6 | `pkg/server/router/router.go` | `InitRouter` 内拿 engine 注册 `h.GET("/notice/stream", ...)`（挂全局 CORS 之后；鉴权在 handler 内自做，不走 `RequireLogin` 包装） |
| 7 | `pkg/conf/conf.go` + `configs/config.yaml` | 新增 `notice_stream` 配置节（见 §五） |
| 8 | `cmd/apps/server.go` | 关停序列加 `hub.Stop()`（排干 sweeper） |

不改：`GET /notice/unread-count` 保留不动（降级通道）；notice 域 routes.go 不动（SSE 路由挂 composition 层）；无 DDL 变更。

跨域红线核对：hub 在 notice application 纯 Go；redpanda 包与 notice 的耦合走函数注入（包级 hook），不 import notice application；composition handler import hertz 有先例。✔

## 五、配置变更

```yaml
# configs/config.yaml 新增
notice_stream:
  enabled: true            # false 时不注册路由（灰度开关）
  heartbeat_sec: 25        # <=0 兜底 25
  max_conns_per_user: 5    # <=0 兜底 5
  coalesce_ms: 1000        # 合并推送窗口，<=0 兜底 1000
  retry_ms: 5000           # 写给客户端的重连建议，<=0 兜底 5000
```

`conf.go` 加 `NoticeStream` 结构体 + `Config` 字段，所有数值字段 `<=0` 兜底默认值（项目惯例）。

## 六、一致性 / 边界 / 风险

| 场景 | 行为 | 依据 |
|---|---|---|
| 未认证/坏 token | handler 内校验失败直接 401 JSON 信封，不建立连接 | 需求 3.1 |
| 第 6 个标签页 | 建连成功；最旧连接收到 `conn-evicted` 后被断开，该页前端回落轮询 | D1（#127） |
| 客户端断开（关页 / 断网重连） | netpoll hup 取消连接 ctx → 泵循环立即退出 `hub.Remove`，名额即时释放 | #127 |
| 批量已读 100 条 | 1s 合并窗口只推最终值 | 需求 3.3 |
| 断网重连带 `Last-Event-ID` | 忽略 id，连接建立即推当前全量 | 需求 3.4 |
| 登出/过期/Kickout | ≤25s 内心跳复检发现 → `auth-expired` → 断连 | D2(a) |
| Redis 计数 miss（TTL 到期） | sweeper 读 `GetUnreadCount` 自动回源 DB，推送值仍正确 | 复用现有回源 |
| 推送写失败（客户端假死） | Write 返回 err → Remove 连接，资源即释放 | hertz Flush 感知 |
| 多实例 | P0 单实例进程内 hub；注册表 `user_id→conns` 结构已为 P1 Redis pub/sub（channel `notice:unread:push`）预留 | 需求 4.2 |
| Nginx 缓冲 | 部署侧按需求 4.1 配置 `proxy_buffering off`；响应头补 `X-Accel-Buffering: no` 双保险 | 需求 4.1 |
| 关停 | `hub.Stop()` 停 sweeper；存量连接由 hertz 关停流程随连接关闭回收 | §四 #8 |

已知限制（记录不阻塞）：新通知推送的端到端延迟 = consumer flush 窗口（默认 5s）+ 合并窗口（1s）。需求验收「1s 内」针对推送段；若需压端到端，调 `redpanda.notice_event_flush_interval` 配置即可，无需改代码。

## 七、分阶段交付

| 阶段 | 内容 | 验收映射 |
|---|---|---|
| **P0** | §四全部：hub + 两处触发 + SSE 端点 + 配置 + 关停。单实例 | 需求 §6 验收清单全项（多实例除外） |
| **P1** | Redis pub/sub 跨实例广播；登出即时断开（D2b，如需）；`new-notice` 完整通知对象推送（需求 §7，事件结构已预留） | — |

P0 自验清单：

- [ ] 登录连接立即收到一次全量 `unread-count`（含 id + retry）
- [ ] 无/坏 `satoken`（header 与 query 均试）→ 401 JSON，无 SSE 头
- [ ] 他端点赞/评论/提及 → flush 后 ≤1s 收到推送，值与 `GET /notice/unread-count` 一致
- [ ] `POST /notice/read`、`/read-all` 后收到推送（read-all 为 0）
- [ ] 心跳 `: ping` ≤30s 一帧，挂 5min 不断
- [ ] 断网重连带 `Last-Event-ID` → 立即收到最新全量
- [ ] 登出后 ≤25s 收到 `auth-expired` 并断连
- [ ] 第 6 个连接建立成功，最旧连接收到 `conn-evicted` 并断开；其余不受影响
- [ ] 断开后立即重连成功（名额即时释放，不 429、不踢人）
- [ ] `go build ./... && go vet ./...` 通过；hub 单测（Add 上限/合并去重/lastSent 不重复推）
