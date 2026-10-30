# 机器人自动回复高热度帖子 / 评论设计（agent-auto-reply，#99）

> **目标**：全局机器人与圈内机器人在被动回复（关键词 / @提及 / 手动）之外，新增一个**定时扫描入口**：
> 按热度门槛挑出高热度的帖子 / 评论自动触发回复，每个机器人对同一目标最多一次。
> **基线**：回复执行链路 `ReplyService.executeReply`（分类器、生成、限频、写日志、楼层挂载）在被动触发上运行稳定
>（[agent-reply-design.md](agent-reply-design.md)、[agent-reply-filter-design.md](agent-reply-filter-design.md)）；
> 圈子作用域规则沿用 [circle-agent-reply-design.md](circle-agent-reply-design.md) §2.1（全局机器人全站，圈内机器人只管本圈）。
> 总览 issue #105；后续自动发帖（#101）复用 `ai_agent.auto_config` 的 `auto_post` 兄弟键。

---

## 一、现状盘点

| 资产 | 位置 | 说明 |
|---|---|---|
| 回复执行核心 | `pkg/domains/aiagent/application/reply_service.go` `executeReply` | 帖子门槛 → 限频 → 解密 → 分类器 → 生成 → 评论落库 → 日志；本次只新增入口与占位 |
| 作用域规则 | 同文件 `agentInScope` | 全局机器人全站；圈内机器人仅同圈；圈子未知 fail-closed |
| 帖子热度 | `domains.post.hot` | 互动加权累积，热度链路异步落库（分钟级滞后） |
| 评论热度 | `domains.comment.like_count / reply_count` | 评论表**没有 hot 列**，也没有 `create_time` 索引（主键是 UUIDv7） |
| 日志表 | `domains.ai_agent_reply_log` | 2026-08-26 移除了唯一约束；没有"触发来源 / 目标评论"列，无法保证"最多一次" |
| 定时任务范式 | `pkg/server/storage/redpanda/trending_syncer.go` | `{mu, ticker, stopChan, stopped}`；依赖 Service 的 syncer 在 `composition/server.go` 启动（discover 先例） |

## 二、方案

### 2.1 配置

**每个机器人**：`ai_agent.auto_config`（jsonb 对象，与 `trigger_mode` 互不影响），本期只用 `auto_reply` 子对象：

| 字段 | 含义 | 校验（开启时） |
|---|---|---|
| `enabled` | 开关 | — |
| `targets` | `post` / `comment` | 至少一个，不重复 |
| `post_hot_min` | 帖子 `hot >=` | 含 post 时 ≥1 |
| `comment_like_min` / `comment_reply_min` | 评论点赞数 / 回复数，满足任一 | 含 comment 时至少一个 ≥1 |
| `window_hours` | 只看最近 N 小时发布的内容 | 1-168 |
| `max_per_hour` | 自动回复每小时上限（0=不单独限制） | 0-1000 |

门槛全 0 等于"窗口内全部内容"，属于 `trigger_mode=1` 的语义，不在本功能范围，校验拒绝（400）。

写入走**顶层 key 合并**：application 把 `auto_reply` 打成 `domain.AutoConfigPatch`，仓储翻译为
`auto_config || ?::jsonb`（单条 UPDATE 原子执行），不读改写整列，并发修改不同子对象（`auto_reply` / `auto_post`）互不覆盖。
圈内机器人的 `auto_config` 属运营字段（圈子 admin+，与限流字段同级）。

**平台**：`aiagent.autonomy`（`pkg/conf/conf_aiagent_autonomy.go`，`<=0` 兜底默认值）：

```yaml
aiagent:
  autonomy:
    enabled: false               # 总开关；关闭时扫描器不启动，运行中关闭（热更新）后每轮直接返回
    scan_interval_sec: 300       # 扫描周期
    max_agents_per_target: 1     # 同一目标最多被几个机器人自动回复（软限）
    max_candidates_per_run: 20   # 每轮每个机器人每种目标按热度取的候选上限
```

### 2.2 日志与去重

`ai_agent_reply_log` 新增：

- `trigger_source smallint NOT NULL DEFAULT 0`：1=关键词 2=@提及 3=手动 4=自动回复；所有新写入的行都带来源。
  默认值 0 只出现在「迁移后、发版前」窗口内旧版本写入的行（未知来源）；存量行由迁移按回复形态回填。
- `target_comment_id uuid NULL`：回复挂在该评论楼层内时非空（关键词触发 / 自动回复评论）。
- 部分唯一索引 `idx_ai_reply_auto_target (post_id, COALESCE(target_comment_id, 全零), agent_id) WHERE trigger_source = 4`。
  列顺序 `post_id` 在前，同一索引服务每目标计数；`CREATE INDEX CONCURRENTLY` 单独一个迁移文件。

执行流程（`executeReply` 内，`source=4` 时）：

```
帖子门槛（已发布未锁定 + 按帖子真实圈子再校验作用域）
  → 机器人限频（max_replies_per_hour / min_interval_sec，原样）
  → claimAutoTarget：
       auto_reply.max_per_hour（自动回复行数，排除 status=2/3）
       每目标已有自动回复机器人数 >= max_agents_per_target → 跳过（软限，排除 status=2）
       插 status=4 占位行，ON CONFLICT DO NOTHING；没插进去 = 本机器人已触发过 → 跳过
  → 解密 / 分类器（配置了 filter_prompt 时）/ 生成 / 评论落库
  → 终态（0 失败 / 1 成功 / 2 分类器判定不回复）原地更新占位行
```

- 占位必须在限频检查**之后**：占位行计入限频统计，先占位会把自己挡掉；限频跳过不占位，下一轮还能再试。
- 失败、分类器判定不回复都算已触发，不重试（沿用 agent-reply 决策 C）；进程中断留下的 `status=4` 同样视为已触发。
- 分类器超时降级（status=3）是中间事件：被动触发照旧单独落行，自动回复只记 zap（同一目标只能有一行）。

### 2.3 扫描

`AgentAutoReplySyncer`（`pkg/server/storage/redpanda/agent_auto_reply_syncer.go`）每轮调用
`ReplyService.RunAutoReplyScan`（`pkg/domains/aiagent/application/auto_reply.go`）：

1. `ListAutoReplyEnabled`：未删、启用、`auto_config.auto_reply.enabled = true` 的机器人（全局 + 圈内）。
2. 一次加载最大时间窗口内**全部机器人**的自动回复目标，按机器人算排除名单：本机器人触发过的 + 已达 `max_agents_per_target` 的。
   排除在候选 SQL 里做，避免已处理目标长期占住 top-N 名额。
3. 逐个机器人、逐种目标经 Facade 取候选（composition 桥 `agentCandidateReader`）：
   - 帖子 `post.ListHotForAutoReply`：`status=1`、未锁定、未删、`hot >= 门槛`、窗口内、作者非 `users.role=2`、圈子范围、排除名单，`hot` 降序 top-N；
   - 评论 `comment.ListHotForAutoReply`：评论正常未删、窗口内（UUIDv7 主键下界 + `create_time`，评论表无时间索引）、
     点赞 / 回复数达到任一门槛、作者非机器人、所属帖子可回复且在圈子范围、排除名单，`like_count + reply_count` 降序 top-N（JOIN post 同 collect 仓储先例）。
4. 逐个候选 `executeReply(source=4)`：帖子发顶层评论；评论挂在该评论所在楼层（与关键词触发相同）。
   命中限频即结束该机器人本轮；奇偶轮交替帖子 / 评论优先，防止一种目标长期饿死。

一轮内**顺序执行**，阻塞获取共享回复信号量：最多占一个并发槽，不挤占关键词 / @提及 的实时触发；
受 `min_interval_sec` 约束每个机器人每轮本就至多回复一次，顺序执行不损失吞吐。上一轮未结束时 ticker 信号被丢弃，不堆积。
关停时取消进行中的一轮并等待退出（≤10s），不再排干一轮；在 `shutdown()` 最先执行（回复会写评论并发事件）。

## 三、一致性 / 边界 / 风险

| 项 | 分析 | 对策 |
|---|---|---|
| 多实例并发 | 现有定时任务都无分布式锁 | 每机器人每目标最多一次由部分唯一索引兜底（集成测试覆盖并发占位只有一个成功）；每目标机器人数是软限，接受极小概率超限 |
| 机器人回复机器人 | 机器人评论也会变热 | 候选 SQL 剔除 `users.role=2` 作者（帖子与评论） |
| 越圈回复 | 候选查询语义漂移 | 候选 SQL 按圈收口 + 扫描循环 `agentInScope` + `executeReply` 按帖子真实圈子再校验，三道 |
| 计数滞后 | `hot` / `like_count` 异步落库 | 分钟级滞后对定时扫描无影响 |
| 成本 | 自动回复调用 LLM | 计入机器人原有限频；`max_per_hour` 单独限；每轮候选上限；失败不重试 |
| 迁移窗口期 | 旧版本写入的日志行 `trigger_source=0` | 读路径不依赖来源列；`0` 记为未知，不回填 |
| 进程中断 | 占位行停在 `status=4` | 视为已触发（不重试），计入限频统计 |

## 四、分阶段交付

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0（#99） | 迁移、`aiagent.autonomy`、`auto_config.auto_reply` 读写校验、扫描器、Facade、去重 / 每目标上限、API 文档 | 本次 |
| P1 | 门槛按分位数 / 圈子规模自适应；回复日志管理端查询 | 待办 |
| — | 自动发帖（#101，复用 `auto_config.auto_post`） | 另有 issue |
