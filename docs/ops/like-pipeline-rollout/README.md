# 点赞/收藏链路修复（#46）上线 Runbook

> 对应 PR：l0sgAi/qubar#47；设计：[like-pipeline-fix-design.md](../../design/like-pipeline-fix-design.md)。
> 本目录脚本均已在按 `docs/pgsql-ddl/` 真实 DDL 搭建的 PostgreSQL 16 副本 + Redis 上演练通过（见文末）。

## 结论先行

- **无 DDL 变更、无配置变更、无新 Redis key、无 topic 变更。**
- 新代码的 `INSERT … ON CONFLICT` **依赖已有唯一索引**（DDL 文档里本就有），上线前确认生产确实存在且有效。
- 上线后需要做一次**历史计数对账**（修复前的 bug 已让计数漂移）+ 清理对应 Redis 统计缓存。

| 步骤 | 时机 | 脚本 | 角色 | 必需 |
|---|---|---|---|---|
| 1 预检 | 部署前 | `01_preflight.sql` | DB-owner（只读） | ✅ |
| 2 补索引 / 授权 | 部署前，仅预检非 OK 时 | `02_fix_unique_indexes.sql` / `GRANT` | DB-owner | 视情况 |
| 3 Redpanda 检查 | 部署前 | `rpk` 命令 | 运维 | ✅ |
| 4 部署 | — | 常规滚动发布 | — | ✅ |
| 5 等消费追平 | 部署后 | `rpk group describe` | 运维 | ✅ |
| 6 计数对账 | 低峰 | `03_reconcile_counts.sql` | DB-owner | ✅（一次性） |
| 7 清 Redis 统计缓存 | 紧接 6 | `04_invalidate_redis_stats.sh` | 运维 | ✅ |
| 8 验证 | 之后 | 03 干跑 + 日志 | — | ✅ |

---

## 1. 预检（部署前，只读）

```bash
psql "$PG_URI" -v app_role=qubar_web_app -f 01_preflight.sql
```

`app_role` = `configs/config.yaml` 的 `pgsql.username`。逐段看 `status`：

| 段 | 期望 | 非 OK 时 |
|---|---|---|
| 1 唯一索引 | 3 行全 `OK` | `MISSING` / `INVALID` / `PARTIAL ONLY` → 第 2 步 |
| 2 重复行 | 3 行全 `OK` | 只要唯一索引有效就不可能有重复；索引缺失时由第 2 步处理 |
| 3 角色权限 | 全 `OK` | 按 `status` 列给出的 `GRANT …` 语句执行（DB-owner） |
| 4 漂移预估 | 任意 | 仅供了解规模，第 6 步处理 |

> 为什么唯一索引是硬前提：缺失时 `ON CONFLICT (user_id, post_id)` 直接报
> `there is no unique or exclusion constraint matching the ON CONFLICT specification`，点赞/收藏无法落库。

## 2. 补唯一索引（仅当第 1 段非 OK）

```bash
psql "$PG_URI" -f 02_fix_unique_indexes.sql     # 不要加 -1 / --single-transaction
```

- 已有效 → 跳过；失败残留的 `INVALID` 索引 → `DROP INDEX CONCURRENTLY`；
- 重复行 → **先整行备份**到 `ops.dedupe_backup_<table>`，再删除多余行（保留：有效行优先 → `update_time` 最新 → `id` 最大）；
- `CREATE UNIQUE INDEX CONCURRENTLY`（不锁写）。并发写入导致失败时直接重跑。
- 完成后重跑第 1 步确认全 `OK`。

## 3. Redpanda 检查（部署前）

```bash
rpk topic describe like_events            # 记下分区数
rpk group describe like_events_consumer_group
rpk group describe collect_events_consumer_group
```

- 无需改 topic / 分区 / 消费组配置。
- like 生产者分区策略由 `LeastBytes` 改为按 key（`user:target`）哈希。多分区 topic 上线瞬间同一对象的事件可能落到不同分区，
  新消费者按行状态迁移落库，**乱序最多造成一次暂时的末态偏差，下次操作即纠正**，无需处理。
- like / collect 事件改为**同步 ack**：Redpanda 不可用时接口在 ≤3s 内返回 503（此前是静默丢失）。

## 4. 部署

常规滚动发布即可，接口向后兼容（`action` 字段可选），前端可晚于后端上线。

上线后关注日志关键字：

| 关键字 | 含义 | 处理 |
|---|---|---|
| `Failed to persist N like states, will retry` | 落库失败，批次已保留下轮重试 | 持续出现 → 查 DB（多为权限 / 索引，见第 1 步） |
| `Failed to commit like event offsets` | 提交失败；重投幂等 | 偶发忽略；持续出现 → 查 broker |
| `like event publish failed` / `collect event publish failed` | 投递失败，已回滚并返回 503 | 持续出现 → 查 Redpanda 可用性 |
| `Failed to read … message, retrying in` | 读错误退避重试（1s→30s） | 持续出现 → 查 broker |

> 旧版本实例下线时仍按旧逻辑丢弃最多 10s 的缓冲点赞（最后一次），新版本起关停会排干。

## 5. 等消费追平

```bash
rpk group describe like_events_consumer_group      # 所有分区 LAG = 0
rpk group describe collect_events_consumer_group
```

## 6. 历史计数对账（低峰，一次性）

```bash
# 先干跑：只出报告（各计数器偏差行数 / 净多计 / 最大偏差 + 偏差最大的 20 个帖子）
psql "$PG_URI" -f 03_reconcile_counts.sql

# 确认后执行（默认 2000 行/批，批间 100ms）
psql "$PG_URI" -v apply=1 -f 03_reconcile_counts.sql
# 可调：-v batch_size=1000 -v sleep_ms=200
```

- 把 `post.like_count` / `comment.like_count` / `post.collect_count` 校正为流水表 `deleted = 0` 行数；已删除（`deleted = 1`）的帖子/评论不动。
- 按主键分批、每批先 `FOR UPDATE` 锁住本批行再计数：与线上消费者串行化，**不会吞掉并发的 ±1**（已演练）。
- 只更新不一致的行，并 bump `update_time` → CDC 同步 ES（ES 会有一波按改动行数的重索引，这也是分批限流的原因）。
- 每处修改写入 `ops.count_reconcile_log`（`run_id, entity, id, old_value, new_value`）；输出末尾打印 `run_id`。
- 幂等：中途失败（如锁等待）直接重跑。

## 7. 清 Redis 统计缓存（紧接第 6 步）

```bash
RUN_ID=<第 6 步输出的 run_id> \
PG_URI="$PG_URI" \
REDIS_CLI_ARGS="-h <redis-host> -p 6379 -n <db> --user <user> --pass <pass>" \
./04_invalidate_redis_stats.sh              # 先加 DRY_RUN=1 看数量
```

热门帖的 `post:stats:{id}` 每次点赞都续期 TTL，不清理会长期显示旧的（偏大的）计数。脚本只删本次被校正对象的
`post:stats:*` / `comment:stats:*`，下次访问时从 DB 重新加载。代价：被删 Hash 里尚未落库的浏览量增量短暂少显示，DB 不受影响。

## 8. 验证

```bash
psql "$PG_URI" -f 03_reconcile_counts.sql   # 干跑：rows_off 应为 0（刚好在 10s flush 窗口内的少量偏差重跑即消失）
```

- 抽查：任选帖子，`post.like_count` = `SELECT count(*) FROM domains.post_like WHERE post_id = … AND deleted = 0`。
- 此后 rows_off 应长期保持 0；若再次增长说明仍有漂移源，提 issue。

## 回滚

- **代码**：直接回滚到旧版本即可（消息格式、表结构、Redis key 均未变）。回滚后旧 bug 回归，计数会重新开始漂移。
- **对账数据**（一般不需要）：
  ```sql
  UPDATE domains.post p SET like_count = l.old_value
  FROM ops.count_reconcile_log l
  WHERE l.run_id = '<run_id>' AND l.entity = 'post.like_count' AND p.id = l.id;
  -- comment.like_count / post.collect_count 同构
  ```
- **去重删除的行**（第 2 步，仅当执行过）：原行在 `ops.dedupe_backup_<table>`，可 `INSERT … SELECT` 回灌（需先删唯一索引）。

## 清理

确认无误（建议保留 ≥ 7 天）后：

```sql
DROP SCHEMA ops CASCADE;   -- 对账日志、去重备份、ops.reconcile_counter 过程
```

---

## 演练记录（2026-09，本地）

按 `docs/pgsql-ddl/{post,comment,interaction}.md` 真实 DDL 在 PostgreSQL 16 搭副本（`uuidv7()` 用 shim），Redis 7：

- `01`：健康时全 OK；删除 `UPDATE ON domains.comment` 后第 3 段给出对应 `GRANT`；缺索引 / INVALID 索引 / 重复行均被识别。
- `02`：健康 → 全跳过；缺索引 + 3 条重复 → 保留有效且最新的 1 条、2 条进备份表、建索引；
  INVALID 残留索引 → DROP 后重建；执行后 `01` 全 OK。
- `03`：5000 帖（1666 行 like 多计 2、500 行 collect 多计 1）+ 300 评论（多计 3）+ 1 个已删帖（不动）：
  干跑无副作用；执行后三类 rows_off 全 0、已删帖不变、二次执行 0 行；
  **并发演练**：执行期间另一事务对批内某帖插入点赞并 `+1` 且持锁 4s → 对账等待其提交后计数，最终 stored = actual。
- `04`：DRY_RUN 只报数量；实跑只删对账涉及的 `post:stats` / `comment:stats`，无关 key 保留。
