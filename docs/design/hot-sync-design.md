# 帖子/圈子热度同步（hot sync）设计与优化方案

> 目标：帖子互动事件（点赞/收藏/分享/评论/评论点赞）按权重累加为**帖子热度**，并同步到**圈子热度**；帖子查询做时间加权排序。
>
> 架构基线：复用现有 `like` / `collect` / `post_statistics` 三套 **Redis Lua 原子 + Redpanda MQ 聚合 + DB 批量 upsert** 链路。帖子→ES 已有 **CDC 链路自动同步**，Go 侧只需更新 PG，无需写 ES。

---

## 一、现状盘点（已有 / 缺失）

### 已有
| 能力 | 位置 | 说明 |
|---|---|---|
| 帖子统计 Hash | `post:stats:{postID}`（[redis/constants.go](../pkg/server/storage/redis/constants.go)） | view/comment/like/collect 四计数，Lua 原子切换 |
| 圈子统计 Hash | `circle:stats:{circleID}` | member_count/post_count/**hot**，hot 字段已存在但**从未被递增** |
| 圈子 DB hot 列 | `domains.circle.hot`（[pgsql-ddl/circle.md](../pgsql-ddl/circle.md)） | 存在 |
| 帖子 DB hot 列 | `domains.post.hot`（[pgsql-ddl/post.md](../pgsql-ddl/post.md)） | 存在 |
| 帖子 ES hot 字段 + 排序公式 | [elasticsearch/post.go:570](../pkg/server/storage/elasticsearch/post.go#L570) | sortType=1 已实现 `rank_score = hot / (age_hours + 2)^0.8` runtime script |
| MQ 聚合消费框架 | [redpanda/consumer.go](../pkg/server/storage/redpanda/consumer.go) | StatisticsAggregator / PostStatisticsAggregator，map 累加 + ticker flush + `jsonb_to_recordset` 批量 UPDATE |
| 事件 publisher | like/collect/comment/post 各域 | Toggle 后已发 ±1 增量事件 |
| 帖子→ES 同步 | CDC 链路（外部） | PG `domains.post` 变更自动同步 ES，Go 无需写 ES |

### 缺失（本方案补齐）
1. **`domain.Post` 结构体无 `Hot` 字段**（[post.go:16](../pkg/domains/post/domain/post.go#L16)）→ GORM 从不读写 hot 列，CDC 也拿不到变更。**必须先加字段。**
2. **hot 增量计算与加权**：现 MQ 消息只带原始计数 ±1，无权重、无 hot 类型。
3. **评论 / 评论点赞 hot**：评论计数当前是同步落库（`IncrCommentCount`），无 hot 贡献。
4. **分享功能完全不存在**：无路由/handler/事件，`+7` 需从零搭建。（#232 已实现，见第九节 P4）
5. **圈子 hot 无写入路径**：DB+Redis 字段在，但无事件递增。
6. **上限（cap）机制**：评论 / 评论点赞需 per-post hot 贡献上限，当前无。
7. **消息计数触发**：`conf.FlushMessages` 字段存在但未接线（现仅时间触发）。

---

## 二、对原始规则的评估与优化

### 评估结论：方向正确，7 处需补齐/修正

#### O1. 反向扣减（undo）未定义 —— **必须补**
点赞/收藏是 toggle（可取消）。必须对称：取消赞 → `-2`，取消收藏 → `-5`，删评论 → `-5`，评论取消赞 → `-1`。
现有 `PublishPostLike(userID, postID, result.Int64())` 已带 ±1 方向 ✓ —— hot Δ = `权重 × 方向`。

#### O2. 上限语义与执行位置 —— **关键架构决策**
"评论 +5 上限 5000" / "评论点赞 +1 上限 25000"：定义为**hot 贡献上限（clamped）**，不是评论数上限。
- 上限 5000 = 最多 1000 条评论计入 hot；上限 25000 = 最多 25000 个评论点赞计入 hot。
- **上限必须在事件时 Redis Lua 原子执行**（check-and-incr + clamp），**不能放异步消费者**：消费者跨批次无法精确判上限，会超限。
- 结论：**producer 发的是已 clamp 的最终 hot Δ**，消费者只累加。这是本方案核心决策。

#### O3. 圈子 hot 双重计算风险 —— **必须去重**
"热度同步到吧热度" 与圈子段 "15s MQ 聚合" 描述重叠，若两条路都喂 circle hot 会**双倍**。
- 单一来源：circle hot = 同一帖子 hot Δ 的 fan-out（事件时 `INCR circle:hot:{circleID}`）。
- 现有 `circle_statistics` topic（member/post count）继续用于计数，**不喂 hot**。
- "15s MQ" 仅当产品要 circle 级 like/comment/collect **总数**时才需要（可选，Phase 2）。

#### O4. 累积 hot 永不衰减 → 老帖长期霸榜 —— **算法优化（可选 Phase 2）**
`hot` 累积只增（除 undo），`rank = hot/(age+2)^0.8`。例：2 年前爆款 hot=100000，age=17520h → rank≈36；新帖 hot=50 → rank≈29。老爆款永远压新帖，"近期热点"名不副实。
- 优化：引入**滚动衰减 hot**（如 7d/30d 半衰指数衰减），排序用 `hot_decay`；保留累积 `hot` 给"精华/全部"排序与展示。
- 默认：先按原始公式上线（ES 已实现），衰减作为 Phase 2 可选项。

#### O5. 新帖 hot=0 在热点流被埋 —— 已知边界
rank=0 → 热点流末尾。`sortType=2`（最新）覆盖新帖。无需改，记录即可。

#### O6. 分享功能不存在 + 去重模型 —— **已决策（#232）**
原文 "帖子分享 +7 因为一人只能点赞一次" 中"点赞"系笔误，应为"一人只能分享一次"。
- ~~去重：per-user ZSET（`user:share:posts:{uid}`）~~：ZSET 有 TTL / 容量上限，miss ≠ 未分享，不能做去重真值。
- 去重（#232 实施）：**PG `domains.post_share` 为真值**，`(user_id, post_id)` 唯一，`INSERT … ON CONFLICT DO NOTHING` 的影响行数即「是否首次」，
  并发同一用户只有一个请求插入成功。首次分享才加分，重复分享幂等返回。
- 不可撤销：分享单向 `+7`，无 decrement，没有取消接口。

#### O7. 权重/上限应可配置 —— **可运维性**
权重 (2/5/7/5/1) 与上限 (5000/25000) 写死在 Lua 难调。集中到 `conf.Hot`，Lua/publisher 读配置。

#### O8. ~~ES 写入路径缺失~~ —— **已解决（CDC）**
帖子→ES 走 CDC，消费者只更新 PG `domains.post.hot`，CDC 自动同步 ES hot 字段。无需 Go 写 ES。

---

## 三、优化后的热度计算规则（最终版）

| 事件 | 帖子 hot Δ | per-post 上限（hot 贡献） | 圈子 hot Δ | 方向 |
|---|---|---|---|---|
| 帖子点赞 | `±2` | 无 | `±2` | 双向 toggle |
| 帖子收藏 | `±5` | 无 | `±5` | 双向 toggle |
| 帖子分享 | `+7` | 无（per-user 去重） | `+7` | 单向 |
| 评论 | `+5` | `5000`（≈1000 条） | `+5` | 创建 `+`；删除 `-` |
| 评论点赞 | `±1` | `25000` | `±1` | 双向 toggle |
| 发帖 | `0` | — | `0` | — |

**计算与同步规则：**
1. 事件时 Redis Lua 原子算出**已 clamp 的签名 hot Δ**（capped 维度查 `post:hotcap:{postID}` 子计数器，超限 clamp 到剩余额度）。
2. producer 发 `{postID, delta}` 到 `post_hot` topic；同时 `INCR circle:hot:{circleID}` by delta（circle hot 单一来源）。
3. 消费者聚合 `postID → ΣΔ`，13min 或 500 条 flush → 批量 `UPDATE domains.post.hot = GREATEST(hot+Δ, 0)` → CDC 同步 ES。
4. 圈子 hot：`CircleHotSyncer` 34min `SCAN circle:hot:*` + `GETSET 0`（读后清零）→ 批量 `UPDATE domains.circle.hot` + 刷 `circle:stats` 缓存。key 50h TTL 自清理。
5. 排序：sortType=1 沿用现有 `hot/(age+2)^0.8`，无需改。

**hot 一致性级别：** at-least-once，容忍小幅多算（hot 是排序信号非账目）。MQ 重投递幂等性 Phase 2 再加 event-id 去重。

---

## 四、数据流架构

```
事件入口（like/collect/comment/comment-like/share service）
  │
  │  1. 各域原有原子操作（ZSET toggle / 评论落库）
  │  2. ★ Lua: ApplyHotDelta(postID, dim, dir) → 返回 clamped signed Δ
  │        - capped 维度: HINCRBY post:hotcap:{postID} <dim> 实际Δ, 超限 clamp
  │        - 返回 Δ（已含权重×方向×clamp）
  │
  ├─▶ PublishPostHot(postID, Δ)          ── Redpanda topic: post_hot
  └─▶ INCR circle:hot:{circleID} Δ       ── Redis 单 key 累加（50h TTL）
                                          │
        ┌─────────────────────────────────┴──────────────────────────┐
        ▼                                                                ▼
  PostHotAggregator（内存 map）                              CircleHotSyncer（34min ticker）
   postID → ΣΔ                                                  SCAN circle:hot:*
   13min 或 500 条 flush                                        GETSET 0（读后清零）
   batch UPDATE domains.post.hot                                batch UPDATE domains.circle.hot
        │                                                       刷 circle:stats:{cid} hot 字段
        └─▶ CDC ──▶ ES post.hot（rank 公式自动用）
```

---

## 五、Schema / 配置变更

### 5.1 domain.Post 加字段
```go
// pkg/domains/post/domain/post.go
Hot int `json:"hot" gorm:"column:hot;default:0"` // 热度分（CDC 同步 ES）
```

### 5.2 Redis 新 key
| key | 类型 | 用途 | TTL |
|---|---|---|---|
| `post:hotcap:{postID}` | Hash（`comment`/`comment_like`） | per-post hot 贡献上限计数器 | 复用 postStatsTTL |
| `circle:hot:{circleID}` | string(int) | 圈子 hot Δ 累加器 | 50h |

### 5.3 Redpanda 新 topic
- `post_hot`：`PostHotMessage{PostID, Delta int64}`，key=postID（保序）。新 producer + aggregator + consumer，独立 13min/500 条节奏。
  - 复用现有 `post_statistics` topic 也可（加 `StatisticsTypePostHot` 类型），但**独立 topic 更干净**（hot 节奏与计数不同）。

### 5.4 conf 新增
```yaml
hot:
  weight:
    post_like: 2
    post_collect: 5
    post_share: 7
    comment: 5
    comment_like: 1
  cap:
    comment: 5000
    comment_like: 25000
  post_hot_flush_interval: 13    # 分钟
  post_hot_flush_messages: 0     # 只按时间窗口 flush（#130 D3，原为 500）
  circle_hot_flush_interval: 34  # 分钟
  circle_hot_ttl: 50             # 小时
```

---

## 六、一致性 / 幂等 / 可观测

- **原子性**：hot Δ 计算与 clamp 全在 Lua（1 RTT，复用 `TogglePostLike` 脚本结构）。
- **方向对称**：所有 toggle 维度 undo 时反向 Δ；删评论反向 Δ（capped 维度同步减子计数器，floor 0）。
- **不丢不爆**：MQ at-least-once；DB UPDATE 用 `GREATEST(hot+Δ, 0)` 防负；cap 在源头 clamp 防爆。
- **可观测**：每次 publish/flush 打日志（复用现有 logger 模式）；权重/cap 可配置可热调。
- **CDC 延迟**：hot 写 PG 后到 ES 可见有 CDC 延迟（秒级），排序可接受。

---

## 七、分阶段交付

| 阶段 | 内容 | 依赖 |
|---|---|---|
| **P0 基座** | `domain.Post` 加 Hot 字段；`conf.Hot` 配置；Redis hotcap/circle:hot key；`ApplyHotDelta` Lua 脚本 | 无 |
| **P1 帖子 hot 链路** | `post_hot` producer + aggregator + consumer；接线 FlushMessages 计数触发；like/collect 发加权 Δ | P0 |
| **P2 评论维度** | comment 创建/删除发 ±5（clamp）；comment-like toggle 发 ±1（clamp 25000） | P1 |
| **P3 圈子 hot** | `CircleHotSyncer` 34min SCAN+GETSET+批量 UPDATE+刷缓存；事件时 INCR circle:hot | P1 |
| **P4 分享功能** | `POST /post/share` + Share service + `post_share` 表去重 + 发 +7（#232 已实现） | P1 |
| **P5（可选）** | hot_decay 滚动衰减（近期热点）；MQ event-id 幂等；circle 级交互总数 15s MQ | P1-P4 |

---

## 八、已确认决策（实施依据）

| 决策点 | 结论 |
|---|---|
| 分享去重模型 | ~~per-user ZSET~~ → **PG `post_share` 唯一索引**（#232，维护者 2026-11-11 确认） |
| 分享热度同步 | ~~延后~~ → #232 实现：首次分享 `ApplyHotDelta(post_share, +1)` + `PublishPostHot`，与点赞 / 收藏同一写法 |
| 分享数 | `post.share_count` 只在 PG + Redis 读路径，**不进 ES**（不登记 `es_sinks`）；列表 / 详情展示留给 #233 |
| post_hot topic | **独立 topic**（`post_hot`），独立 producer/aggregator/consumer，13min/500 条节奏 |
| hot_decay 衰减排序 | **延后 P5**。先用原始公式 `hot/(age+2)^0.8`（ES 已实现） |
| 帖子→ES 同步 | **CDC 链路自动同步**（已搭建）。Go 只更新 PG `domains.post.hot`，无需写 ES |
| 同步节奏 | 帖 13min/500 条；圈 34min；circle:hot key TTL 50h（已写入 config.yaml） |

---

## 九、已实现范围（P0-P3）

| 阶段 | 交付 |
|---|---|
| P0 | `domain.Post` 加 `Hot` 字段；`conf.Hot`（权重+上限）；Redis `post:hotcap:` / `circle:hot:` key；`ApplyHotDelta` Lua（权重×方向×clamp） |
| P1 | `post_hot` topic producer + `PostHotAggregator`（13min 或 500 条双触发）；批量 UPDATE `post.hot` + fan-out `circle:hot`；like/collect publisher 发加权 Δ |
| P2 | 评论创建 +5（`CommentEventPublisher`，clamp cap.comment=5000）；评论点赞 ±1（like publisher，clamp cap.comment_like=25000） |
| P3 | `CircleHotSyncer` 34min SCAN `circle:hot:*` + GETDEL 读后清零 → 批量 UPDATE `circle.hot` + 条件刷 stats 缓存 |
| P4 分享（#232） | `POST /post/share`（share 域）：可分享校验（已发布 + `CanViewPost`）→ `post_share` 去重插入 → 首次分享同步发布分享事件（topic `post_share_events`，失败回滚记录、返回 503）→ best-effort 发 `post_share_count` 标记到 `post_statistics` + 热度 `+hot.weight.post_share`（无上限，`HotDimPostShare`）→ Redis `post:sharecnt:{post_id}` +1（冷 key 按 `post_share` 行数 SET NX 回种）。`post.share_count` 由帖子统计消费者按 `post_share` 行数批量重算（#130 批量窗口，值不变不写行），落库后 Redis 只升不降校准。接口见 [post-share-api.md](../api/post-share-api.md) |
| P5 衰减 | **未实现**（TODO） |

**未做**：评论删除反向 -5（无删评论接口，TODO）；MQ event-id 幂等（at-least-once，容忍小幅多算）。
