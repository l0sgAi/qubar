# 帖子计数与 CDC 解耦设计（#130）

> 目标：计数 / 热度更新不再每次都让 `domains.post` 整行进 WAL、经 Debezium 触发 ES 整篇重建索引。
>
> 基线（`docs/loadtest-report-20261022.md` 发现 3）：写场景 2.9 万次发帖对应 `domains.post` **76 万次 UPDATE**，
> WAL 复制槽积压峰值 540 MB，ES post sink 积压 10 万条；混合流量下 ES 是第一瓶颈，重建索引是 ES CPU 大头。
>
> 范围：本文只做方案比较与分期，代码改动见各阶段 PR。排序 / 热度算法调整不在范围内。

---

## 一、现状盘点

### 1.1 写 `domains.post` 计数 / 热度列的路径

| # | 来源 | 写入点 | 触发 | 窗口 | 改的列 |
|---|---|---|---|---|---|
| W1 | 评论 / 回复创建 | `RestoreStatsAndIncrCommentCount` → `postRepoPG.IncrCommentCount`（`pkg/domains/post/application/service.go:1470`、`pkg/domains/post/infrastructure/post_repo_pg.go:161`） | 每条评论同步 1 次单行 UPDATE | 无聚合 | `comment_count` |
| W2 | 帖子点赞 / 取消 | `persistLikeStates` → `postLikeApplySQL`（`pkg/server/storage/redpanda/like_consumer.go:372`、`:435`） | `like_events` 批量 | 10 s（`like_event_flush_interval`） | `like_count`, `update_time` |
| W3 | 收藏 / 取消 | `batchUpdatePostCollects`（`pkg/server/storage/redpanda/collect_consumer.go:53`） | `collect_events` 批量 | 10 s（`collect_event_flush_interval`） | `collect_count`, `update_time` |
| W4 | 热度 | `updatePostHotDB`（`pkg/server/storage/redpanda/hot_consumer.go:86`） | `post_hot` 批量；点赞 / 评论点赞 / 收藏 / 评论都会发 | 13 min **或 500 条**（`post_hot_flush_messages`，先到先 flush） | `hot`, `update_time` |
| W5 | 浏览 | `batchUpdatePostStats`（`pkg/server/storage/redpanda/consumer.go:218`） | `post_statistics` 批量 | 15 min（`post_flush_interval`） | `view_count`（另有 `comment/like/collect_delta` 列，但一直没人发） |

补充：

- 所有批量写入方已按 `id` 升序取锁（`lockRowsByID`，`pkg/server/storage/redpanda/batch_lock.go`，#124），本设计的任何新写入方必须沿用。
- `domains.post` 是 `REPLICA IDENTITY FULL`（`migrations/20261015000000_baseline.up.sql:366`）：每次 UPDATE 除新行外还要把**整条旧行**写进 WAL，
  PG 会把旧行里被 TOAST 的列（`content` 正文、长 `summary` / `media_extra`）展开后写入。帖子越长，一次计数 UPDATE 的 WAL 越大。
- 读路径：详情 / 列表的四个计数以 Redis `post:stats:{id}` 为真值（详情 `service.go:764`，列表 `applyStatsOverlay` `service.go:1183`），
  缓存未命中时详情回落 PG 行、列表回落 ES 文档。**四个计数只用于展示**：ES 排序 / 聚合与 PG 查询只用 `hot`
  （热门流 `rank_score`、热榜 `hot_sum`、帖子自动回复候选 `hot >= ?`），没有任何查询按 view / comment / like / collect 排序或过滤。

### 1.2 CDC 与 ES sink

- Debezium source（`deploy/ansible/roles/stack/templates/pg-source.json.j2`）：`cdc_tables` = users, category, circle, circle_member, **post**, comment, comment_like, post_like（`deploy/ansible/group_vars/all.yml:28`），pgoutput、单 task。
- ES post sink（`es-sink.json.j2` + `es_sinks.post`，`all.yml:31`）：取 `after` → `ReplaceField` 白名单 → `write.method: upsert`。白名单：
  `id,circle_id,user_id,type,title,summary,media_extra,view_count,comment_count,like_count,collect_count,hot,is_pinned,is_essence,is_lock,status,deleted,create_time,update_time,last_reply_time`。
- 白名单只裁字段，**不过滤事件**：任何一列变化（哪怕只有 `like_count`）都会产出一条事件、一次 ES update。
  ES 的 update 是「取 `_source` → 合并 → 整篇重建索引」，IK 分词的 `title` / `summary` 每次都重新分析，局部字段更新并不比全量便宜。
  ES 侧成本只和**事件条数**成正比，与改了几个字段无关。

### 1.3 76 万次 UPDATE 从哪来（估算）

写场景脚本（`loadtest/scenarios.js` `write()`）：帖子点赞 40%、评论点赞 15%、收藏 20%、评论 10%、回复 10%、发帖 5%；目标帖子池约 3000 篇。
发帖 2.9 万次（5%）→ 写请求约 56 万次，约 560 s，平均约 1000 RPS。

| 来源 | 事件数 | 每窗口去重后 UPDATE | 估算 UPDATE | 占比 |
|---|---|---|---|---|
| W4 热度 | 点赞 + 评论点赞 + 收藏 + 评论 ≈ 95% × 56 万 ≈ 53 万 | 500 条触发一次 flush，500 条落在 3000 篇里约 460 篇不同帖子，约 0.9 次 UPDATE / 事件 | **40–49 万** | **≈ 55–60%** |
| W1 评论同步 | 20% × 56 万 | 每条 1 次 | ≈ 11 万 | ≈ 15% |
| W2 点赞 | 40% × 56 万，约 400/s | 10 s 窗口 4000 条 → 约 2200 篇不同帖子 × 56 窗口 | ≈ 12 万 | ≈ 15% |
| W3 收藏 | 20% × 56 万，约 200/s | 10 s 窗口 2000 条 → 约 1460 篇 × 56 窗口 | ≈ 8 万 | ≈ 10% |
| W5 浏览 | 写场景无浏览 | — | ≈ 0 | — |
| 合计 | | | ≈ 71–80 万 | 实测 76 万 |

结论：

1. **最大来源是热度**：`post_hot_flush_messages: 500` 在高写入下让热度几乎逐事件落库，13 min 时间窗口基本没起作用。
2. 评论同步 UPDATE 只占约 15%，但它在请求路径上，热帖会成为热点行（评论创建等锁最长 0.9 s，见报告发现 1）。
3. 点赞 / 收藏的 10 s 窗口在帖子池大时去重效果差；真实流量更稀疏，去重更差，接近「每次互动一次 UPDATE」。
4. 每次 UPDATE 都是：WAL 写整行新旧两份 → Debezium 解码 → Kafka 一条事件 → ES 整篇重建索引。

---

## 二、方案比较

三个方案都要回答同一个问题：**ES 里的计数怎么办**。既然四个计数只用于展示、读路径已经以 Redis 为真值，
本文建议的处理是「ES 不再承载计数」：列表 Redis 未命中时回落 PG 批量主键查询，而不是读 ES 文档里的计数。
另一种做法是 Go 定时批量把计数写进 ES，但仓库目前刻意没有 Go → ES 写路径（帖子入 ES 只走 CDC），新增写路径的代价更大，作为备选列出。

### 方案 1：计数拆到不进 CDC 的独立表 `domains.post_stats`

```
post_stats(post_id uuid PK, view_count, comment_count, like_count, collect_count, update_time)   -- cdc: none
```

- 计数消费者（W2 / W3 / W5 与 B1 后的评论计数）改写 `post_stats`（`INSERT ... ON CONFLICT (post_id) DO UPDATE`，按 `post_id` 升序取锁）。
- `post.hot` 留在 `post`：ES 排序与 PG 自动回复候选都要用，必须进 ES；热度改为只按时间窗口 flush（见方案 3）。
- 读路径：详情 Redis 未命中 → 读 `post_stats`（主键）；列表 Redis 未命中 → `post_stats` 批量主键查询（一次 `IN`），不再用 ES 文档里的计数。
- ES：`es_sinks.post` 白名单去掉 `view_count,comment_count,like_count,collect_count`。

| 维度 | 估算 / 说明 |
|---|---|
| post UPDATE 次数 | 计数类（W1–W3、W5）全部移走，只剩热度 + 真实内容编辑。按 1.3 的拆分，配合方案 3 的热度时间窗口，写场景下 post UPDATE 约 76 万 → **≤ 1 万**（−98%） |
| WAL | 计数 UPDATE 落在窄表上：`post_stats` 一行约 70 B，`REPLICA IDENTITY DEFAULT` 不写旧行；对比 `post` 整行新旧两份（≥ 1 KB，长帖数 KB）。计数相关 WAL 字节约 **−90% 以上**。`post_stats` 不在 publication，Debezium 解码到它直接跳过，不产出事件 |
| ES | 计数事件归零；ES post 事件只剩热度 flush 与内容变更。写场景估算 **−98%** |
| 排序 / 热度 | 不受影响：`hot` 仍在 `post` 并经 CDC 进 ES。ES 文档里的四个计数冻结在切换时的值，读路径不再用它们 |
| 读路径成本 | 列表页 Redis 未命中的帖子多一次 PG 批量主键查询（≤ 20 行）；详情改读 `post_stats` 主键 |

迁移（expand / contract，先迁移后发版，每一步兼容旧版本应用）：

1. **M1（expand）**：新建 `post_stats`（`-- cdc: none`），`INSERT ... SELECT` 从 `post` 回填。旧应用不感知新表。
2. **R1（双写）**：计数消费者在同一事务内同时写 `post` 与 `post_stats`（按 `id` 升序先锁 `post` 再锁 `post_stats`，所有写入方顺序一致）；读路径不变。
   M1 回填到 R1 上线之间旧应用写进 `post` 的增量，用 R1 上线后的一次校准补齐（comment / like / collect 由明细表重算，view 从 `post` 复制，浏览是软信号可接受小误差）。
3. **R2（切读 + 停写 post 计数）**：读路径改读 `post_stats`；消费者不再写 `post` 的计数列。**WAL / ES 收益从这一步开始。**
4. **D1（deploy）**：`es_sinks.post` 白名单去掉四个计数列，重新注册 sink connector。可以和 R2 同一次发布，也可以晚于 R2（晚做只是多一些无用的白名单字段，没有正确性问题）。
5. **M2（contract，可选，至少隔一个发布周期）**：删 `post` 的四个计数列。需要先从 `domain.Post`、`es_sinks`、`docs/pgsql-ddl/` 移除引用；也可以永久保留冻结的列，不做 contract。

| 项 | 内容 |
|---|---|
| `deploy/` 改动 | `es_sinks.post` 白名单（`all.yml`）；`cdc_tables` 不变 |
| CDC 改动 | 无（新表不登记 CDC） |
| 回滚 | R2 → R1：读路径切回 `post`，但 R2 期间 `post` 计数没更新，回滚前先跑一次 `post_stats → post` 的分批回写（会产生一次性的 CDC 事件，按 1000 行一批限速）；M1 可保留不回滚 |
| 风险 | 改动面最大（迁移 + 消费者 + 读路径 + deploy，全部 `risk:high`）；R1 双写期间写放大反而略增；切换窗口的计数校准要写对；`post_stats` 缺行（新帖）要在发帖事务内插入或读路径按 0 处理 |

### 方案 2：CDC 层丢掉只改计数列的事件，计数单独同步

两种实现：

- **2a ES sink 加过滤 SMT**：`io.debezium.transforms.Filter` + Groovy / JS 表达式，比较 `before` / `after` 中 ES 关心的字段，全部相同就丢弃。
  需要给 `deploy/images/kafka-connect` 加 `debezium-scripting` 与脚本引擎 jar。
- **2b Debezium source 原生跳过**：`pg-source` 加 `column.exclude.list`（排除 `domains.post` 的四个计数列、`content`）+ `skip.messages.without.change=true`。
  Debezium 文档说明该选项要求表是 `REPLICA IDENTITY FULL`（`post` 已满足）。被排除的列全部没变时 source 直接不发事件，Kafka 与 ES 都看不到。
  前提：计数 UPDATE 不能再顺带改 `update_time`（否则 `update_time` 变化会让事件照常发出）。要么把 `update_time` 也排除出 CDC（ES 里的 `update_time` 只在插入时写入），要么计数 SQL 去掉 `update_time = CURRENT_TIMESTAMP`。

| 维度 | 估算 / 说明 |
|---|---|
| post UPDATE 次数 | **不变**（76 万） |
| WAL | **生成量不变**（每次 UPDATE 照样写整行新旧两份）。2b 下 Debezium 少发约 40–45% 的消息，复制槽确认可能更快，积压可能缓解，需实测 |
| ES | 只改计数的事件（W1–W3、W5，写场景约 40–45%）被丢弃；热度事件仍然会发（`hot` 必须进 ES）。单独做约 −40%；配合方案 3 的热度时间窗口约 −95% |
| 排序 / 热度 | 不受影响（`hot` 不排除）。ES 计数冻结，读路径同方案 1 改为 Redis → PG 回落 |
| 读路径成本 | 同方案 1，但回落查的是 `post` 行 |

| 项 | 内容 |
|---|---|
| 迁移 | 无 |
| `deploy/` 改动 | 2a：connect 镜像 + sink 配置；2b：`pg-source.json.j2` 两个参数（重新注册 source connector，不需要重做快照） |
| 应用改动 | 读路径回落 PG；2b 需要计数 SQL 不再改 `update_time`（或 CDC 排除 `update_time`） |
| 回滚 | 去掉参数重新注册 connector；之后的计数变化恢复同步，已冻结的 ES 计数要等下一次该帖变更才更新（或手动重放快照） |
| 风险 | 字段清单是隐式契约：以后给 ES 加字段、或把某个计数改成 ES 排序依据时，忘了改排除清单 / 过滤表达式就会静默不同步；2a 在 Connect 里跑脚本引擎，有性能与安全面；2b 依赖 `REPLICA IDENTITY FULL`，以后想把 `post` 改成 DEFAULT 省 WAL 时两者冲突；PG 侧的行膨胀、热点行锁、WAL 体积都没有改善 |

### 方案 3：只降频（批量聚合 + 更长 flush 窗口）

不改表、不改 CDC，只把每篇帖子每个窗口的 UPDATE 压到至多 1 次，并拉长窗口：

| 改动 | 配置 / 代码 | 写场景估算 |
|---|---|---|
| 评论计数改批量（B1） | 代码：评论计数并入 `post_statistics`，15 min 窗口，与浏览量同一条 UPDATE | W1 ≈ 11 万 → ≤ 活跃帖数 / 15 min，写场景约 0.3 万（有浏览量时与之合并成同一条，不额外增加） |
| 热度只按时间 flush | 配置：`post_hot_flush_messages: 0`（关闭条数触发，代码已支持 `<=0` 不触发），保留 13 min | W4 ≈ 40–49 万 → ≤ 活跃帖数 / 13 min，写场景约 0.3–0.6 万 |
| 点赞 / 收藏窗口拉长 | 配置：`like_event_flush_interval` / `collect_event_flush_interval` 10 s → 60 s | W2 + W3 ≈ 20 万 → ≈ 5 万（帖子池 3000，60 s 内几乎每篇都会被碰到，接近上限 3000 × 9 × 2） |
| 合计 | | 76 万 → **≈ 6–7 万（−90% 左右）** |

| 维度 | 说明 |
|---|---|
| WAL / ES | 与 UPDATE 次数同比下降（每次 UPDATE 的成本不变：整行新旧两份 WAL + 一次 ES 整篇重建） |
| 排序 / 热度 | ES `hot` 最多滞后 13 min（现在高负载下几乎实时，低负载下本来就是 13 min）；热门流 `rank_score` 的时间衰减是小时级，影响小 |
| 计数可见性 | 读路径以 Redis 为真值，不受窗口影响；PG / ES 计数最多滞后一个窗口。Redis 回填偏低的问题由落库后校准兜底（点赞 #128 已有，B1 给评论补上） |
| 迁移 / deploy | 无迁移、无 `deploy/` 改动；但生产 `config.yaml` 来自 GitHub Secret，改配置要维护者更新 Secret |
| 回滚 | 改回配置值重启 |
| 风险 | 上限受「活跃帖子数 × 窗口频率」约束：帖子池变大、活跃帖变多时收益线性回落，不能根治；点赞 / 收藏窗口变长后，`post_like` / `post_collect` 明细行落库也会延后（状态读取以 Redis ZSET 为准，ZSET TTL 43 min，影响小），进程被杀时重投窗口变大（落库后才提交 offset，不丢，但 collect 增量重投可能重复计一次） |

### 2.4 汇总

| | 方案 1 独立表 | 方案 2 CDC 过滤 | 方案 3 降频 |
|---|---|---|---|
| post UPDATE（写场景） | −98%（配合热度时间窗口） | 0% | −90% |
| WAL 字节 | −90% 以上（计数部分） | 0%（槽积压可能缓解） | ≈ −90%（随次数） |
| ES 事件 | −98% | −40%；配合热度时间窗口 −95% | −90% |
| 随帖子规模增长 | 计数完全脱离 CDC，不随规模回落 | ES 侧不回落，PG 侧问题仍在 | 收益随活跃帖数增长回落 |
| 迁移 | 有（新表 + 回填 + 可选 contract） | 无 | 无 |
| `deploy/` | ES 白名单 | source / sink connector（2a 还要镜像） | 无（生产配置 Secret） |
| 应用改动 | 消费者 + 读路径 | 读路径 | 配置（B1 是代码） |
| 回滚难度 | 中（R2 回滚要回写计数） | 低 | 低 |
| 主要风险 | 改动面大、切换校准 | 隐式字段契约、PG 侧无改善 | 治标，窗口变长后数据滞后 |

---

## 三、推荐方案与分期

推荐：**先做方案 3（含 B1），再做方案 1**；方案 2 不推荐作为长期方案（PG 侧 WAL 与热点行问题不变，字段清单是隐式契约），
只在维护者不想加迁移、又需要尽快压 ES 时作为 1 的替代。

| 阶段 | 内容 | 改动范围 | 风险 | 状态 |
|---|---|---|---|---|
| **B1** | `comment_count` 改为与浏览量同一条批量 UPDATE（`post_statistics`，15 min），落库时按明细表重算（幂等），落库后校准 Redis | 代码（post / comment 域、`redpanda` 消费者），无迁移、无 `deploy/` | 中 | 本次 PR |
| **B2-a** | 热度关闭条数触发：`post_hot_flush_messages: 0`；点赞 / 收藏窗口 10 s → 30–60 s | 生产配置 Secret（维护者操作），`configs/*.yaml` 默认值同步 | 低 | 待决策（窗口取值） |
| **B2-b** | 方案 1：`post_stats` 独立表 + 读路径回落 + ES 白名单去计数 | 迁移 + 代码 + `deploy/`，全部 `risk:high`，拆成 M1 / R1 / R2 / D1 多个 PR | 中高 | 待决策 |
| **B2-c**（可选） | `post` 从 `REPLICA IDENTITY FULL` 改 DEFAULT：每次 UPDATE 不再写整条旧行 | 迁移；需确认 Debezium / sink 不依赖 `before`（当前 sink 只取 `after`） | 中 | 待决策 |
| 验收 | 生产写场景压测对比：`pg_stat_user_tables.n_tup_upd`（post / post_stats）、复制槽积压（`pg_current_wal_lsn() - confirmed_flush_lsn`）、ES post sink 消费积压、ES indexing rate / CPU | 生产 | — | 需维护者安排，agent 不执行 |

B1 的设计要点（详见 B1 PR）：

- 评论创建不再同步 `UPDATE post`；只做 Redis `post:stats` 实时 +1 和发一条 `post_comment_count` 事件到 `post_statistics`。
- 消费者对评论计数**不累加 ±1**，只记录「哪些帖子的评论变了」；落库时 `comment_count = 明细表 deleted=0 行数`。
  重投、flush 失败重试、评论删除都只是再算一次，天然幂等（`collect_count` 那种 delta 累加在提交 offset 前进程被杀时会重复计数，评论计数没有这个问题）。
- 与浏览量合并成同一条 UPDATE，并且重算结果与当前值相同的行不写，不产生新的 WAL / CDC 事件。
- 落库后把 Redis 中低于 PG 的 `comment_count` 抬到 PG 值，与点赞 #128 同一口径（只升不降）。

---

## 四、需要维护者决策的点

| # | 问题 | 选项 | 建议 |
|---|---|---|---|
| D1 | B2 走哪条路 | 方案 1（独立表）/ 方案 2b（Debezium 跳过）/ 只做方案 3 | 方案 3 先上，方案 1 跟进 |
| D2 | ES 里的计数怎么处理 | ES 不再承载计数（列表 Redis 未命中回落 PG）/ Go 定时批量写 ES（新增 Go → ES 写路径）| 不再承载；计数只用于展示 |
| D3 | 热度窗口 | `post_hot_flush_messages` 500 → 0（只按 13 min）或调大到某个值 | 0；热门流时间衰减是小时级 |
| D4 | 点赞 / 收藏窗口 | 保持 10 s / 30 s / 60 s | 60 s；读路径不受影响 |
| D5 | `post` 的 `REPLICA IDENTITY FULL` 是否保留 | 保留 / 改 DEFAULT | 先查清当初设为 FULL 的原因；若只是 Debezium 默认习惯，改 DEFAULT 能直接减半以上的单次 UPDATE WAL（与方案 2b 互斥） |
| D6 | 方案 1 的 contract | 删 `post` 四个计数列 / 永久保留冻结列 | 至少隔一个发布周期再决定 |
| D7 | 浏览量是否一起进 `post_stats` | 一起 / 留在 `post` | 一起；浏览量是最高频的计数 |

---

## 五、一致性 / 边界 / 风险

| 场景 | 影响 | 处理 |
|---|---|---|
| Redis `post:stats` 过期后由 PG 回填，PG 尚未落库窗口内的增量 | 缓存偏低 | 落库后只升不降的校准（点赞已有，B1 补评论）；缓存 TTL 43 min > 统计窗口 15 min，评论会刷新 TTL，被评论过的帖子在落库前缓存一般不会过期 |
| flush 失败（死锁、PG 不可用） | 本批合并回缓冲重试，offset 不提交 | 评论计数是重算，重试不重复；增量类（collect / view）维持现状 |
| 落库成功、提交 offset 前进程被杀 | 该批重投 | 评论计数重算幂等；点赞按行状态迁移幂等；collect / view / hot 可能重复一次（现状） |
| ES 计数冻结（方案 1 / 2 之后） | 读 ES 文档计数会看到旧值 | 读路径统一 Redis → PG，不再读 ES 计数；在代码里删除对 ES 计数字段的读取，防止回归 |
| 未来需要按计数排序 / 过滤（如「最多评论」） | ES 里没有实时计数 | 届时按需把该计数以低频窗口同步进 ES，或用 PG 查询；属于新需求 |
| 方案 1 切换窗口 | M1 回填与 R1 之间的增量缺失 | R1 上线后一次校准（明细表重算 + view 复制） |

## 六、未验证

- 1.3 的拆分是按脚本比例和窗口去重的估算，压测时没有逐来源统计 UPDATE 次数。
- 方案 2b 的 `skip.messages.without.change` 在当前 Debezium 版本（3.2.6）上的行为没有在本地 CDC 栈验证（开发栈没有 CDC）。
- WAL 字节的下降比例按行宽估算，没有实测；生产帖子正文长度分布会影响 `REPLICA IDENTITY FULL` 的旧行大小。
- 所有收益都需要生产写场景压测对比确认，由维护者安排。
