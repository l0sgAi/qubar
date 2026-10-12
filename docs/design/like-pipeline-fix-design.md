# 点赞链路（like pipeline）正确性 / 持久性修复方案

> 目标：修复 [l0sgAi/qubar#46](https://github.com/l0sgAi/qubar/issues/46) 列出的 4 个问题，并补齐本次深入排查新发现的同源问题。
> 修复后需满足：点赞计数**不漂移**；事件**不丢**；重复投递**幂等**；消费者**不长睡**。
>
> 架构基线：保留现有 **Redis Lua 原子（读路径真值）+ Redpanda MQ + 聚合器批量落库** 的 Write-Behind 链路（见
> `qubar-skill/references/domain-conventions.md` §五），**不引入新存储**（ScyllaDB/Cassandra 评估结论见 #46 背景）。
> 不改 DDL：全部依赖现有唯一索引 `uk_post_like_user_post` / `uk_comment_like_user_comment`。
>
> 基线提交：`develop@84b3a80`。本文所有 `file:line` 均指该提交。**先文档后编码，待审批准后再实施。**

---

## 一、现状盘点

### 1.1 写路径（点赞/取消点赞）

```
POST /like/toggle {type, target_id}
  └─ likeService.togglePostLike                 like/application/service.go:115
       1. postTarget.Exists                     → DB
       2. postTarget.RestoreStats               → post:stats:{id} 不存在则从 DB 回填
       3. postCache.Toggle → Lua likeToggleScript  redis/like_lua.go:12-48
            ZSCORE user:like:posts:{uid} target
              命中 → ZREM + HINCRBY like_count -1 → return -1
              未命中 → ZADD + HINCRBY like_count +1 + 超 2000 按 rank 淘汰 → return +1
       4. publisher.PublishPostLike(±1)         like/infrastructure/like_event_publisher.go:30
            ├─ like_events            {type,user,target,post,amount=±1}   (Async writer)
            ├─ ApplyHotDelta(±2) → post_hot
            ├─ amount>0: post_interaction (CF 灌数)
            └─ amount>0: notification (帖子被赞 → 作者)
```

### 1.2 落库路径（like_events 消费者）

```
kafka.Reader{GroupID, CommitInterval:1s}            redpanda/like_consumer.go:54-62
  └─ ReadMessage(ctx.Background())  ← 读即自动提交 offset   :79
       err 含 "timeout"/"no data" → Sleep(30min)          :81-88
  └─ aggregator.addMessage: map[type:user:target].Amount += amount   :108-127
  └─ ticker(10s) flush                                    :142-175
       batchUpdatePostLikes / batchUpdateCommentLikes     :178 / :237
         for each delta: SELECT First → Create / Update(deleted)     ← 逐行 2 RTT
         按 target 聚合 ΣAmount → jsonb_to_recordset UPDATE like_count  ← 与行状态无关
       err → 仅 log，批次丢弃
```

### 1.3 读路径（is_liked 回显）

| 入口 | 位置 | miss 处理 |
|---|---|---|
| 帖子详情 | `post/application/service.go:682` `checkLiked` | ✅ miss → DB `IsLiked` → 回填 ZSET |
| 评论列表 / 详情 | `comment/application/service.go:671` / `:716` | ✅ miss → DB 批量 → 回填 |
| 首页推荐流 | `recommend/infrastructure/interaction_checker_redis.go:21` | ❌ miss 直接当 false |
| 热点榜 | `composition/facade_bridges.go:439` `trendingInteractionChecker` | ❌ miss 直接当 false |
| 发现页 | `composition/facade_bridges.go:528` `discoverInteractionChecker` | ❌ miss 直接当 false |

`user:like:posts:{uid}` ZSET：TTL = `postStatsTTL` 43min（`redis/cache.go:446`，访问续期），cap 2000（`like_lua.go:87`）。
ZMSCORE 返回 0 同时代表"未点赞"和"缓存已丢失"，**两者不可区分**。

### 1.4 可复用的现成范式

| 能力 | 位置 | 本方案用途 |
|---|---|---|
| 可取消 reader ctx + `sleepOrDone` + Stop 排干 | `redpanda/notification_consumer.go:82-84,141,194` | 问题 2/4 的模板 |
| `ON CONFLICT` 批量 upsert | `redpanda/interaction_consumer.go:175`、`notification_consumer.go:465` | 问题 3 的模板 |
| cache→DB→回填 的单条 is_liked | `post/application/service.go:682` | 问题 1 写/读两侧复用 |
| 失败补偿回切 Redis | `collect/application/service.go:133-139` | 问题 2 生产端补偿 |

---

## 二、问题评估（#46 四项 + 本次新发现）

### 问题 1：Toggle 依赖有损缓存判方向 → `like_count` 只增不减地漂移 ——【正确性，高】

**1a（#46 原述）**：Lua 仅凭 `ZSCORE` 判方向（`like_lua.go:21`）。ZSET 因 TTL(43min) / cap(2000) 丢失成员时，
已赞被判成"未赞" → `+1`。消费者（`like_consumer.go:241-255`）找到 `deleted=0` 的已有行、行不变，
但计数仍按 `ΣAmount` `+1`（`:266-288`）。

**1b（新发现）触发面比 #46 描述更大**：不回源的 checker 有 **3 处**（推荐 / 热点 / 发现，见 §1.3），
用户在这三个页面上看到的已赞帖都会显示为未赞，点一下即触发 1a。

**1c（新发现）Toggle 非幂等**：`POST /like/toggle` 无期望状态。客户端超时重试 / 双击 → 翻转两次。

**1d（新发现）级联污染**：虚假 `+1` 同时触发（`like_event_publisher.go:35-56`）：
- 帖子 hot `+2` 并 fan-out 到 circle hot（不可逆追溯）；
- 通知 upsert 命中 `uk_notice_dedup` → **把作者已读通知重置为未读** + 未读计数 `+1`（`notification_consumer.go:465-477`）；
- `post_interaction`：`ON CONFLICT GREATEST` 幂等，无害。

**1e（新发现）收藏同病**：`collect_lua.go:25` 同构脚本；`collect.Toggle` 虽同步落行（`SetCollected` 幂等），
但 `collect_count` 事件仍按 `±1` 累加（`collect_consumer.go:170-196`）→ `collect_count` 同样漂移，hot `+5`。

### 问题 2：事件丢失 ——【持久性，高】

| # | 丢失窗口 | 位置 |
|---|---|---|
| 2a | `ReadMessage`+GroupID 读后约 1s 自动提交，flush 每 10s；崩溃 → 已提交未落库的事件永久丢失 | `like_consumer.go:61,79` |
| 2b | flush 事务失败只 log，已换出的 `deltas` 被丢弃 | `like_consumer.go:166-174` |
| 2c（新） | **优雅关停也丢**：`server.go` 关停序列未停 like 聚合器（只停了 notification），每次发布/重启丢最多 10s 的点赞 | `cmd/apps/server.go:207-226` |
| 2d（新） | 生产端 `Async:true` 且无 `Completion` 回调，`WriteMessages` 立即返回，投递失败被静默吞掉 → Redis 已赞、DB 永无；43min 后 Redis 从 DB 回填，赞"消失" | `redpanda/producer.go:242-256` |
| 2e（新） | writer `Balancer: LeastBytes` **忽略 message key**，同一 (user,target) 的事件不保证同分区有序。现"求和"语义与序无关所以没暴露；改为"末态为准"（§三 F2）后**必须**改为按 key 哈希 | `producer.go:246` |

### 问题 3：flush 逐行 SELECT + INSERT/UPDATE ——【性能，中】

`like_consumer.go:180-205`（评论）/ `:239-263`（帖子）：每个 delta 2 次往返，全在一个长事务里。
附带缺陷：激活/取消的 `Update(...)` 未检查 `.Error`（`:197,:200,:255,:258`）；靠 `strings.Contains(err, "duplicate key")` 判重。

### 问题 4：读错误含 "timeout" 即睡 30 分钟 ——【可用性，中】

`ReadMessage(context.Background())` 无数据时阻塞不返回，所以匹配到 `timeout`/`deadline` 的只会是**真实的 broker/网络错误**（如 Redpanda 重启时拨号超时）。
命中即 `Sleep(30min)`，该 topic 停摆半小时。共 **7 个读循环**：
`consumer.go:91`、`consumer.go:380`、`hot_consumer.go:82`、`interaction_consumer.go:82`、`like_consumer.go:87`、`collect_consumer.go:81`、`history_consumer.go:81`
（`consumer.go` 两处 + 其余 5 个文件；`notification_consumer.go:108-118` 已修为 5s `sleepOrDone`，作为模板）。

### 为什么不照搬收藏的"同步落行"

收藏 Toggle 同步写 `post_collect`（列表需即时可见）。点赞没有"我的点赞列表"强一致需求，且频率远高于收藏，
保留 Write-Behind 更合适；只需让**方向判定**和**计数推导**都以行状态为准。

---

## 三、修复方案

### F1 写路径：以真实状态为准的"设值"语义（解决 1a/1c/1d）

**F1.1 Lua：toggle → set**（`redis/like_lua.go`）
新脚本 `likeSetScript(statsKey, zsetKey, targetId, now, maxSize, ttl, want)`，`want ∈ {1=赞, 0=取消}`：

```
score = ZSCORE zsetKey targetId
want=1: score 存在 → 续期 TTL, return 0           -- 已赞，no-op
        否则 ZADD + HINCRBY +1 + 淘汰 + 续期, return 1
want=0: score 不存在 → 续期 TTL, return 0          -- 已取消，no-op
        否则 ZREM + HINCRBY -1(clamp 0) + 续期, return -1
```
导出 `SetPostLike(userID, postID, liked bool)` / `SetCommentLike(...)`，保留 EvalSha + NOSCRIPT 重载。
旧 `likeToggleScript` 删除（无其它调用方）。

**F1.2 应用层：先解析真实状态，再设值**（`like/application/service.go`）

```
current, err := postTarget.IsLiked(ctx, uid, pid)   // cache → miss 回源 DB → DB 已赞则回填 ZSET
if err != nil → 返回错误（不猜）
want := !current                                   // 未传 action 时（兼容旧客户端）
if input.Action != "" { want = (Action == "like") } // 显式期望状态（F1.3）
r := postCache.Set(ctx, uid, pid, want)            // -1 / 0 / +1
if r != 0 { publisher.PublishPostLike(..., r) }     // r==0 不发任何事件：无 MQ / hot / 通知
return {is_liked: want}
```
- 回填后 ZSET 对该 (user,target) 准确，Lua 的判定即正确；Lua 自身原子，并发双击最多一次生效。
- 端口扩展（跨域走 Facade，composition 桥接）：
  - `PostTarget.IsLiked(ctx, userID, postID) (bool, error)` → 桥接到 post 新导出方法 `PostService.IsLikedByUser`
    （复用 `checkLiked` 逻辑，但**把 DB 错误返回**而非吞成 false）；
  - `CommentTarget.IsLiked(ctx, userID, commentID) (bool, error)` → `CommentService.IsLikedByUser`（同理）。
- `domain.ToggleResult` 新增 `ToggleResultUnchanged = 0`。

**F1.3 API：可选期望状态**（`like/interfaces/http/handler.go`）
`ToggleLikeRequest` 增加 `Action string \`json:"action" binding:"omitempty,oneof=like unlike"\``。
缺省=toggle（兼容现有前端）；前端改为显式传 `action` 后，重试/双击天然幂等。响应结构不变。

**F1.4 读路径回显补回源**（解决 1b）
- post 新增 `PostService.BatchCheckInteractions(ctx, userID, postIDs) (liked, collected map, err)`：
  ZSET 批查 → miss 批量 DB（新 repo 方法 `BatchIsLiked` / `BatchIsCollected`：
  `WHERE user_id=? AND post_id IN ? AND deleted=0`，走唯一索引）→ 回填 ZSET。
- composition 新增一个桥接器 `postInteractionChecker{delegate postapp.PostService}`，同时满足
  recommend / trending / discover 三个 `InteractionChecker` 端口（签名一致），替换：
  `recommend/infrastructure/interaction_checker_redis.go`（删除）、`trendingInteractionChecker`、`discoverInteractionChecker`。
- 代价：每页 ≤2 次索引 IN 查询（仅在有 miss 时）。注意"未赞"本身也是 miss，故多数页都会回源——见 D5。

**F1.5 收藏同改**（解决 1e）
`collect_lua.go` 同样改 set 脚本；`collect.Toggle` 先用同域 repo 解析真实状态；`SetCollected` 改为返回 `changed bool`，
`changed=false` 时不发 `collect_count` 事件和 hot。

### F2 消费端：末态为准 + 落库后提交 + 关停排干（解决 2a/2b/2c/2e）

**F2.1 聚合语义：ΣAmount → 末态为准**
`LikeEventMessage` **结构不变**：`Amount=+1` 解释为"期望态=已赞"，`-1` 为"期望态=未赞"。
聚合器 `map[type:user:target] → {liked bool, postID}`，后到覆盖先到。计数不再由事件求和，而由 F3 的**行状态迁移**推导 → 重复投递 / 旧实例的虚假 `+1` 都变成 no-op。

**F2.2 保序：writer 改按 key 哈希**
`InitLikeEventProducer`：`Balancer: &kafka.Hash{}`（key 已是 `user:target`，`producer.go:295`），保证同一对事件同分区有序、落在同一消费者实例。

**F2.3 提交时机：FetchMessage + 落库成功后 CommitMessages**
- reader：`CommitInterval: 0`（同步提交），读循环改 `FetchMessage(readerCtx)`。
- 聚合器额外持有 `pending map[partition]kafka.Message`（每分区只留最大 offset）。
- flush：换出 `{states, pending}` → 落库事务 → 成功才 `CommitMessages(pending...)`。
- 失败：把换出的状态**合并回**（仅当 key 在新 map 中不存在时放回，保留更新的末态），pending 保留，下个 tick 重试；ERROR 日志带批大小。
- 提交失败（落库已成功）→ 重启后重投 → F3 幂等，无副作用。

**F2.4 关停排干**（仿 notification）
reader 用可取消 ctx；新增 `StopLikeEventConsumerGlobal()`：cancel reader → 停 ticker → 最后一次 flush + commit → 关 reader。
**reader 的 Close 移到最终 commit 之后**（现 `defer r.Close()` 在读 goroutine 退出时即关，会导致最终 commit 失败）。
`cmd/apps/server.go` 关停序列在 `StopNotificationEventConsumerGlobal()` 旁调用（需在 `CloseRedis` 之前无强依赖，但放一起便于维护）。

**F2.5 生产端投递确认**（解决 2d，见 D2）
推荐：仅 like / collect 两个 writer 改 `Async:false`（其它 hot/interaction/notification 仍异步 best-effort）。
`PublishPostLike` 的 like_events 写失败 → 应用层**反向 Set 回滚 Redis**（仿 `collect/application/service.go:133-139`）并返回 503，
客户端可重试（配合 F1.3 天然幂等）。延迟代价：单次 `WriteMessages` 同步 ack，`BatchTimeout` 10ms 内。

### F3 落库：单条集合式 SQL，计数由行迁移推导（解决问题 3 + 1a 的 DB 侧）

帖子（评论同构，多 `post_id` 冗余列）：

```sql
WITH v AS (
    SELECT * FROM jsonb_to_recordset(?::jsonb)
    AS v(id uuid, user_id uuid, post_id uuid, liked boolean)
),
ins AS (   -- 0→1：新建 或 从 deleted=1 复活
    INSERT INTO domains.post_like (id, user_id, post_id, deleted)
    SELECT id, user_id, post_id, 0 FROM v WHERE liked
    ON CONFLICT (user_id, post_id) DO UPDATE
        SET deleted = 0, update_time = CURRENT_TIMESTAMP
        WHERE domains.post_like.deleted = 1
    RETURNING post_id
),
del AS (   -- 1→0：仅当前为有效赞
    UPDATE domains.post_like l
    SET deleted = 1, update_time = CURRENT_TIMESTAMP
    FROM v
    WHERE NOT v.liked AND l.user_id = v.user_id AND l.post_id = v.post_id AND l.deleted = 0
    RETURNING l.post_id
),
d AS (
    SELECT post_id, SUM(delta) AS delta
    FROM (SELECT post_id, 1 AS delta FROM ins UNION ALL SELECT post_id, -1 FROM del) x
    GROUP BY post_id
)
UPDATE domains.post p
SET like_count = GREATEST(p.like_count + d.delta, 0), update_time = CURRENT_TIMESTAMP
FROM d
WHERE p.id = d.post_id AND p.deleted = 0 AND d.delta <> 0;
```

- `v` 每个 (user,post) 仅一行（聚合器已去重），满足 `ON CONFLICT DO UPDATE` 单语句不得重复命中同一行的约束；`ins` 与 `del` 行集互斥。
- `id` 由 Go 侧 `sharedomain.NewID()` 生成（遵守 UUIDv7 约定；冲突时被忽略）。
- 已赞再赞 / 未赞再取消 / 重复投递 → 0 行迁移 → 计数不变。**这是计数正确性的最终保证**。
- 每批固定 1 条语句（每种 target 各 1），删除 `First`/`Create`/`Update` 循环与 `strings.Contains` 判重。

### F4 读循环退避（解决问题 4）

新文件 `redpanda/reader.go`：
- `readBackoff`：1s 起倍增、上限 30s，成功读取后重置；
- 复用 `sleepOrDone(ctx, d)`（从 `notification_consumer.go:141` 原位保留，同包可直接用）。
7 个读循环的 `"no data"/"timeout" → Sleep(30min)` 与 `Sleep(5s)` 两个分支合并为：`ctx 已取消 → return；否则 WARN + backoff`。
**仅改退避**；非 like 消费者的"读即提交"问题（同 2a）列入后续（§七 P2），本次不扩大范围。

---

## 四、数据流（修复后）

```
POST /like/toggle {type, target_id, action?}
  └─ likeService
       1. Exists + RestoreStats                           (不变)
       2. current = target.IsLiked()  ── ZSET 命中 ──────────┐
                                    └─ miss → DB → 已赞则回填 ZSET
       3. want = action ?? !current
       4. r = Lua Set(want) ∈ {-1, 0, +1}
       5. r == 0 → 直接返回（不发事件）
          r != 0 → like_events (同步 ack, Hash 分区)  ── 失败 → Lua Set(!want) 回滚 → 503
                 → hot / interaction / notification (异步 best-effort，不变)

like_events ──FetchMessage──► aggregator: map[key]=末态, pending[partition]=max offset
                                  │ ticker 10s / Stop
                                  ▼
                   1 条 CTE：行迁移(ins/del) → Σ迁移 → UPDATE like_count
                                  │ 成功                         │ 失败
                                  ▼                              ▼
                        CommitMessages(pending)        合并回 map，保留 pending，下轮重试

feed / trending / discover ──► postInteractionChecker ──► PostService.BatchCheckInteractions
                                                           ZSET → miss 批量 DB → 回填
```

---

## 五、Schema / 配置 / 接口变更

| 类别 | 变更 |
|---|---|
| DDL | **无**。依赖现有 `uk_post_like_user_post`、`uk_comment_like_user_comment`、`uk_post_collect_user_post`（实施前在生产 `\d` 确认存在）。 |
| Redis key | **无新增**。`like_lua.go` / `collect_lua.go` 脚本内容替换。 |
| MQ 消息 | `LikeEventMessage` / `CollectEventMessage` 结构不变，语义由"增量"改为"期望末态"（±1 编码不变）。 |
| MQ writer | like / collect writer：`Balancer: Hash`；`Async:false`（D2）。 |
| 配置 | **无新增**（退避上限、批量参数用包内常量）。 |
| HTTP | `POST /like/toggle` 请求体新增可选 `action: "like"\|"unlike"`；响应不变。`POST /collect/toggle` 同步新增（F1.5）。 |
| 跨域端口 | like `PostTarget/CommentTarget` +`IsLiked`；post `PostService` +`IsLikedByUser` +`BatchCheckInteractions`；post repo +`BatchIsLiked` +`BatchIsCollected`；comment `CommentService` +`IsLikedByUser`；`CollectRepository.SetCollected` 返回 `(changed bool, err)`。 |

### DDD 分层改动清单

| 层 | 文件 | 改动 |
|---|---|---|
| like/domain | `like.go`、`repository.go` | `ToggleResultUnchanged`；`PostLikeCache/CommentLikeCache.Toggle` → `Set(ctx,uid,id,liked)` |
| like/application | `service.go` | F1.2 流程；`ToggleInput.Action`；端口 `IsLiked`；发布失败回滚（F2.5） |
| like/infrastructure | `like_cache_redis.go` | 调 `redispkg.SetPostLike/SetCommentLike` |
| like/interfaces/http | `handler.go` | `Action` 字段 |
| post/application | `service.go` | 导出 `IsLikedByUser`、`BatchCheckInteractions` |
| post/domain + infrastructure | `repository.go`、`post_repo_pg.go` | `BatchIsLiked`、`BatchIsCollected` |
| comment/application | `service.go` | 导出 `IsLikedByUser` |
| collect/* | `collect_lua.go`、`service.go`、`collect_repository.go` | F1.5 |
| recommend/infrastructure | `interaction_checker_redis.go` | **删除**，改由 composition 注入 |
| composition | `facade_bridges.go`、`server.go`/`deps.go` | `likePostTarget/likeCommentTarget.IsLiked`；`postInteractionChecker` 替换 3 个 checker |
| storage/redis | `like_lua.go`、`collect_lua.go` | set 脚本 |
| storage/redpanda | `like_consumer.go`（重写聚合+读循环+SQL）、`collect_consumer.go`、`producer.go`、新 `reader.go`、其余 5 个 consumer 读循环 | F2/F3/F4 |
| cmd/apps | `server.go` | 关停调用 `StopLikeEventConsumerGlobal`（collect 同） |
| docs | 本文、`docs/api/`（toggle `action` 字段）、`qubar-skill/references/tech-stack.md` | 同步：like ZSET cap 实为 2000（文档写 500）；§7.3 consumer 范式去掉 30min sleep |

---

## 六、一致性 / 边界 / 风险

| 场景 | 行为 | 结论 |
|---|---|---|
| ZSET 丢失 + 已赞 + 再点 | `IsLiked` 回源 DB 得 true → 回填 → Set(false) → `-1` | ✅ 正确取消 |
| 客户端重试 `action=like` | Lua 返回 0，不发事件 | ✅ 幂等 |
| 旧实例（滚动发布中）发虚假 `+1` | 消费端末态=已赞，行已 active → 0 迁移 | ✅ DB 不漂；Redis stats 仍可能短暂 +1，TTL 后从 DB 自愈 |
| DB 落后于 Redis（flush 窗口 10s） | 仅当 ZSET 在 10s 内丢失该成员才可能误判；TTL 43min、cap 2000 下可忽略 | 可接受 |
| 消费者长时间积压 | ZSET 仍持有近期赞（访问续期），DB 回源只在 ZSET 丢失时发生 | 可接受，靠 ERROR 日志告警 |
| 崩溃于 fetch 与 flush 之间 | offset 未提交 → 重投 → F3 幂等 | ✅ 不丢不重 |
| 落库成功但 commit 失败 | 重投 → 0 迁移 | ✅ |
| 永久失败批次（坏数据） | 每 tick 重试、offset 不前进、日志持续报错 | ⚠️ 入 map 前校验 nil UUID；无 FK，失败面很小 |
| 分区数变更 / 迁移期旧消息 | Hash 重映射期间可能短暂乱序 | ⚠️ 迁移窗口内极低概率；末态以最后处理为准，下次操作即纠正 |
| 生产端同步 ack 增加延迟 | ≤ BatchTimeout(10ms) + broker RTT | 可接受（D2） |
| feed 回显多 2 次 DB 查询 | 唯一索引 IN 查询，每页一次 | 可接受；若成为热点见 D5 |
| 历史已漂移的 `like_count` / `collect_count` | 修复只阻止新增漂移 | 见 §7 P2 一次性对账 SQL |
| 历史已污染的 `hot` / circle hot | 无法精确回溯 | 接受；hot 为软信号 |

---

## 七、分阶段交付

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P0**（本分支首个 PR） | F2（末态聚合、Hash 分区、FetchMessage+落库后提交、失败合并回、关停排干）· F3（集合式 CTE）· F1.1/F1.2（like set 脚本 + 真实状态解析 + no-op 不发事件）· F4（7 个读循环退避） | #46 验收项 1、2、3、4、5、6 |
| **P1** | F1.3（`action` 参数 + API 文档）· F1.4（feed/trending/discover 回源，删 recommend checker）· F1.5（收藏同改）· F2.5（like/collect writer 同步 ack + 回滚） | #46 验收项 7；前端切换到显式 `action` |
| **P2**（运维/后续） | 一次性对账 SQL（下）· 其余 5 个 consumer 迁移到"落库后提交"· 若 D5 触发则做 ZSET 完整预热 | 对账后抽样 `like_count = count(deleted=0)` |

P2 对账 SQL（P0 上线、消费者追平后低峰执行；会批量触发 CDC → ES 重索引，建议按 id 区间分批）：

```sql
WITH s AS (
    SELECT post_id, count(*) AS cnt FROM domains.post_like WHERE deleted = 0 GROUP BY post_id
)
UPDATE domains.post p
SET like_count = COALESCE(s.cnt, 0), update_time = CURRENT_TIMESTAMP
FROM domains.post p0 LEFT JOIN s ON s.post_id = p0.id
WHERE p.id = p0.id AND p.deleted = 0 AND p.like_count <> COALESCE(s.cnt, 0);
-- comment.like_count / post.collect_count 同构
```

### 测试计划

项目测试稀疏且无 PG/Redis/Redpanda 测试夹具，因此：
- **单元测试（纯 Go，随 PR 提交）**
  - 聚合器：同 key 末态覆盖；失败合并回不覆盖更新的末态；pending 每分区只保留最大 offset。
  - `readBackoff`：1s→2s→…→30s 封顶，成功后重置。
  - like service（fake cache/target/publisher）：miss+DB 已赞+toggle → 取消；`action=like` 且已赞 → 不发布；DB 错误 → 返回错误不调 Lua；发布失败 → 回滚调用 `Set(!want)`。
  - F3 行构建：去重、`liked` 映射、nil UUID 过滤。
- **手工集成验证（本地 docker PG/Redis/Redpanda）**：逐条跑 #46 验收清单；`kill -9` 消费者于 fetch 后、flush 前 → 重启后事件恰好落库一次；在临时 schema 上跑 F3 CTE 的 5 种迁移用例（新赞、再赞、取消、取消不存在、复活）核对 `like_count`。
- `go build ./... && go vet ./... && go test ./pkg/...` 全绿。

---

## 八、决策（已确认：全部采用推荐项）

| # | 问题 | 选项 | 推荐 |
|---|---|---|---|
| D1 | 显式期望状态的 API 形态 | A. `POST /like/toggle` 加可选 `action`（兼容）；B. 新增 `PUT/DELETE /like` | **A**：零破坏，前端逐步切换 |
| D2 | 生产端丢失（2d） | A. like/collect writer 同步 ack + 失败回滚 Redis 返回 503；B. 保持异步，仅加 `Completion` 回调记日志 | **A**：B 只能发现不能挽回 |
| D3 | 收藏修复（F1.5）放哪 | A. 本分支 P1 独立提交；B. 另开 issue | **A**：同一脚本同一病，改法一致 |
| D4 | 是否执行对账 SQL | 执行 / 不执行 | **执行**，P0 上线后低峰分批 |
| D5 | feed 回显回源成本 | A. 有 miss 即批量回源（简单，多 ≤2 次索引查询/页）；B. ZSET 过期时整表预热 top-2000 + 完整性标记，miss 才算权威 | **A**，上线后观察 DB QPS 再定是否做 B |
| D6 | P0 / P1 是否拆成两个 PR | 拆 / 合 | **拆**：P0 纯后端无接口变化，可先上线止血 |

---

## 九、实施记录（`fix/like-pipeline-20260925`）

### 9.1 提交清单

| 阶段 | 提交 | 内容 |
|---|---|---|
| P0 | `fix(redpanda): replace 30-minute consumer sleep…` | F4：`redpanda/reader.go`（`readBackoff` + `waitAfterReadError`），6 个非 like 读循环改用 |
| P0 | `fix(like): make like persistence idempotent…` | F2 + F3：`like_consumer.go` 重写（末态缓冲、`FetchMessage`/落库后 `CommitMessages`、失败合并回、`StopLikeEventConsumerGlobal`、单条 CTE）；like writer `kafka.Hash{}` |
| P0 | `fix(like): resolve real like state before setting it` | F1.1 + F1.2：`likeSetScript`；post/comment `IsLikedByUser`；like 端口 `IsLiked`；no-op 不发事件；仅 `NOSCRIPT` 重载 |
| P1 | `feat(like): accept optional action…` | F1.3：`action=like\|unlike`，`domain.ResolveWant` |
| P1 | `fix(feed): fall back to DB for is_liked/is_collected…` | F1.4：`PostService.BatchCheckInteractions` + repo `BatchIsLiked/BatchIsCollected`；composition `postInteractionChecker` 替换 3 个 checker |
| P1 | `fix(collect): resolve real collect state…` | F1.5：`collectSetScript`、`SetCollected` 返回 `changed`、`action=collect\|uncollect` |
| P1 | `fix(like,collect): confirm event delivery…` | F2.5：like/collect writer 同步 ack（`syncPublishTimeout` 3s），失败回滚 → 503 |

D6（拆 PR）：P0 = 前 3 个代码提交，即 `181a34f`–`4c93a6b`（基于 `e17423e`）再 cherry-pick `fix(like): don't carry failed offset commits…`（仅改 `like_consumer.go`，无冲突），可单独开 PR 先上线；P1 在其后。

### 9.2 与方案的差异

| 项 | 方案 | 实际 | 原因 |
|---|---|---|---|
| 收藏写入顺序（F1.5） | Redis 设值后同步落行（沿用原顺序 + 补偿） | **先落行（权威、返回 changed）→ 再 Redis 设值** | DB 失败时 Redis 未动，无需补偿；Redis 失败仅记日志，缓存随 TTL/回源自愈 |
| 收藏投递失败（F2.5） | 回滚 Redis | 回滚**流水行 + Redis** | 流水已同步落库，只回滚 Redis 会让 DB 与计数不一致 |
| collect writer 分区 | — | 保持 `LeastBytes` | 收藏消费者仍按 ±1 求和；现仅在真实迁移时发事件，与顺序无关 |
| 信息流回显失败语义 | — | best-effort：DB 失败保留缓存结果，不报错 | 回显是软信号，不应让整页失败 |
| `post_like` / `comment_like` DDL | 无变更 | 无变更 | 依赖已有唯一索引（上线前在生产确认存在） |
| offset 提交失败（F2.3） | 保留 pending，下轮重试提交 | **只记日志、不合并回** | 同分区后续更大 offset 的提交会覆盖；未覆盖则重投幂等。合并回在再均衡后可能让失效 offset 拖累之后每次提交 |

### 9.3 验证

- `go build ./... && go vet`（改动包）通过；`go test ./pkg/...` 全绿。新增单测：
  `redpanda/reader_test.go`、`redpanda/like_consumer_test.go`（末态、合并回、offset、落库后提交、提交失败重试、停机丢弃、行构建）、
  `like/application/service_test.go`（缓存 miss 取消、no-op 不发布、状态解析失败不猜、显式 action 幂等、投递失败回滚）、
  `post/application/interactions_test.go`（miss 回源 + 回填、缓存/DB 故障降级）、`collect/application/service_test.go`。
- **本地 PostgreSQL 16** 执行 F3 两条 CTE：已赞再赞 no-op、复活 +1、新建 +1、取消不存在 no-op、取消 −1、**整批重放计数不变**、评论复活补齐 `post_id`；
  `SetCollected` upsert：新建 1 行、重复 0 行、复活 1 行。
- **本地 Redis** 执行 `likeSetScript` / `collectSetScript`：设值 ±1、重复 0、计数 clamp 0、超 cap 淘汰最旧、TTL 续期。
- 未做：真实 Redpanda 下的 `kill -9` 重投演练（容器内无 broker），上线前在预发按 §七 测试计划执行。

### 9.4 遗留（P2）

- 一次性对账 SQL（§七），P0 上线、消费者追平后低峰分批执行。
- 其余 consumer（circle/post statistics、collect、history、hot、interaction）仍为"读即提交"，flush 前崩溃会丢缓冲；按 `like_consumer.go` 范式逐个迁移。
- 若信息流回源导致 DB QPS 明显上升，评估 D5-B（ZSET 完整预热 + 完整性标记）。
- 已知极端边界：落库失败的批次被合并回后恰逢消费组再均衡，本实例可能把已被转移分区的旧末态晚于新 owner 写入。
  需"落库失败 + 再均衡"同时发生；下次对同一对象操作即纠正，接受。

