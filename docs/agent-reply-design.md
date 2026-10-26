# AI 机器人回复执行链路设计（agent-reply）

> **目标**：基于 eino 框架，让「全局回复机器人」（`domains.ai_agent`，CRUD 已交付）按配置的触发条件调用大模型生成帖子评论。评论走 comment 域现有链路，天然可查看、可点赞；调用失败静默不回复，但照常插入 `ai_agent_reply_log` 审计行。
> **基线（2026-08-25 已确认决策）**：
> - A：eino-ext 四协议组件（openai/claude/gemini/ollama 对应包）全部安装；**本期代码只实现 openai 与 anthropic 协议**（gemini/ollama 触发时记失败日志）。
> - B：**不做**「全部新帖」触发（trigger_mode=1 本期不生效，跳过待办，无 post_created 事件）。
> - C：LLM 调用失败**不重试**，终态写失败日志行。
> - D：本期只做**手动触发（mode=3）**与**评论关键词触发（mode=2）**。
> - E：手动触发一并交付管理端接口；触发模式在 domain 层用**类型化枚举**（`TriggerMode`）表示。
> - F：限频统计口径 = `ai_agent_reply_log` **全部行（含失败）**。
> - G：Prompt 组装 = `system_prompt` + 帖子 `title` + `summary`；评论关键词触发时**追加评论原文**。
> - H：新增配置节 `aiagent`（timeout_sec / max_content_chars / reply_concurrency）。
>
> **圈内机器人阶段注记（2026-08-31）**：圈子级 AI 机器人管理已交付（见
> [circle-agent-manage-design.md](circle-agent-manage-design.md)）——圈主/管理员可维护本圈机器人
>（每圈 ≤5），但本期**不参与任何回复触发**：`ListEnabled` 加 `circle_id IS NULL` 守卫（关键词/
> @提及候选集不含圈内机器人），`ManualReply` 校验 `agent.CircleID == nil`（手动入口返回不支持）。
> 圈内回复触发（触发链匹配 `post.circle_id == agent.circle_id`）为后续演进项，届时移除/改造上述守卫。

---

## 一、现状盘点

| 已有 | 位置 | 说明 |
|---|---|---|
| Agent 配置 CRUD、api_key AES-GCM 加解密 | `pkg/domains/aiagent/application/service.go` | `crypto.Encrypt/Decrypt` + `conf.Security.DataKey` |
| 日志表 DDL（append-only，`UNIQUE(agent_id, post_id)`） | `docs/pgsql-ddl/ai-agent.md` | ⚠️ `comment_id` 列误标 `NOT NULL`，与「失败时为 NULL」注释矛盾，本次修正 |
| 评论创建链路 | `pkg/domains/comment/application/service.go` `CreateComment` | 校验帖子状态/锁定、SanitizeForPg、事务插入、评论计数、热度/CF 事件 |
| 机器人系统账号 role=2 | `CreateBotUser` 端口 | 机器人以 `linked_user_id` 身份发评论 |
| eino 依赖 | go.mod | `eino v0.9.15` + `eino-ext` openai/claude/gemini 组件 |
| **缺口** | — | 无回复执行服务、无日志表实体/仓库、无触发钩子、无 LLM 适配器 |

## 二、对原始规则的评估

1. **触发源选择**：全帖触发需要新事件（post_created topic 或轮询），本期砍掉（决策 B）后，仅剩两个触发源：
   - **mode=2 关键词**：挂在评论创建后（同步调用端口、异步执行），评论内容命中任一关键词即触发。
   - **mode=3 手动**：管理端同步接口 `POST /agent/:id/reply/:postId`。
2. **同步 vs 异步**：关键词触发若同步调 LLM（秒级~30s）会拖慢用户发评论请求，不可接受 → aiagent 侧收到事件后**立即返回**，goroutine + 信号量限并发执行；手动触发是管理员操作，**同步**执行便于即时反馈。
3. **失败语义**：「静默」指不影响评论创建主流程与用户体验；但 `reply_log` 必须落一行（status=0, error_msg, latency）。`UNIQUE(agent_id, post_id)` 保证失败也是终态（决策 C），同帖不再重试。
4. **机器人评论回环**：机器人评论本身走 `CreateComment` → 再次触发关键词钩子，可能自激循环 → 钩子入口按 `linked_user_id` 反查 agent 表，**机器人自己的评论不触发**。
5. **限频口径**（决策 F）：按 log 全部行统计。失败也消耗额度，防止失败风暴反复打供应商 API。

## 三、最终方案

### 3.1 触发与执行规则

| 规则 | mode=2 关键词 | mode=3 手动 |
|---|---|---|
| 触发源 | 用户评论创建后（comment 域端口回调） | 管理端 `POST /agent/:id/reply/:postId` |
| 匹配条件 | agent 启用 + mode=2 + 评论内容含任一关键词（不区分大小写子串） | agent 启用 + mode=3 + 操作者 role=1 |
| 帖子门槛 | status=1（已发布）且未锁定，否则静默跳过（不记日志） | 同左，但返回 400 错误 |
| 防重 | 无（2026-08-26 调整：同帖可多次触发，用量仅靠限频约束） | 同左 |
| 限频 | 最近 1h 行数 ≥ max_replies_per_hour（>0 时）或距最新一行 < min_interval_sec（>0 时）→ 静默跳过（不记日志） | 同左，返回 429 |
| 协议 | openai / anthropic 走 eino；gemini/ollama → 失败日志「protocol not implemented」 | 同左，返回 502 |
| 失败 | 不重试，写失败日志行 | 写失败日志行 + 返回 502（含原因） |

> 「跳过不记日志」与「失败记日志」的分界：跳过 = 未发起 LLM 调用（前置条件不满足）；失败 = 已尝试调用链路（LLM 报错 / 空回复 / 评论落库失败）。日志表只记录真实调用尝试。

### 3.2 Prompt 组装（决策 G）

```
system = agent.system_prompt（空时用默认人设兜底）
user   = "帖子标题：{title}\n帖子摘要：{summary}"
       + 触发源为评论时追加："\n用户评论：{comment_content}"
       + "\n请以{agent.name}的身份对上述帖子写一条回复。"
```
- `title`/`summary`/`comment_content` 各自按 `aiagent.max_content_chars` 截断。
- LLM 输出经 `utils.SanitizeForPg` 清洗，空内容视为失败（记日志）。

### 3.3 评论落库形态

- 关键词触发：回复**挂在触发评论楼层内**——`root_id` = 触发评论的 root_id（无则为其自身 ID），`reply_to_id` = 触发评论 ID。
- 手动触发：顶层评论（root_id/reply_to_id 均 NULL）。
- 复用 `CommentService.CreateComment(ctx, linked_user_id, …)`：帖子校验、清洗、计数、热度/CF 事件全部免费获得；机器人评论与普通评论同构，可查看可点赞。

### 3.4 触发模式枚举（决策 E）

```go
type TriggerMode int16 // 1=全部新帖(本期不生效) 2=关键词 3=手动
func (m TriggerMode) String() string  // "all_post"/"keyword"/"manual"/"unknown"
func (m TriggerMode) Valid() bool
```

## 四、数据流

```
用户发评论                          管理员
   │                                 │ POST /agent/:id/reply/:postId
   ▼                                 ▼
CommentService.CreateComment    aiagent.Handler.ManualReply
   │ 成功后回调端口(同步调用,实现内立即返回)   │
   ▼                                 ▼
ReplyService.OnCommentCreated   ReplyService.ManualReply
   │ ┌─ 机器人评论? → 丢弃(防回环)           │
   │ ├─ go {信号量, ctx超时, recover}        │
   │ ▼                                     │
   │ ListEnabledAgents → 过滤 mode=2 + 关键词命中 │
   ▼                                       ▼
        executeReply(agent, postID, trigger?)  [共用核心]
   ├─ PostReader.GetPostBrief: status=1 && !lock, 否则跳过
   ├─ ReplyLogRepo: 1h 计数 / 最小间隔（限频）
   ├─ crypto.Decrypt(api_key) → LLMCaller.Generate (eino)
   ├─ CommentCreator.CreateComment(linked_user_id, …)
   └─ ReplyLogRepo.Create(status=1|0, latency, tokens, error)
        成功: comment_id 回填 ── 失败: 静默(手动路径回 502), 日志照写
```

## 五、Schema / 配置变更

1. **DDL 修正**（`docs/pgsql-ddl/ai-agent.md`）：`ai_agent_reply_log.comment_id` 去掉 `NOT NULL`（失败行无评论）。既有表需 DBA 执行 `ALTER TABLE domains.ai_agent_reply_log ALTER COLUMN comment_id DROP NOT NULL;`
2. **新增配置节**（三处同步 + `<=0` 兜底）：

```yaml
aiagent:
  timeout_sec: 30         # LLM 单次调用超时(秒), 默认 30
  max_content_chars: 4000 # prompt 各段(title/summary/评论)截断长度, 默认 4000
  reply_concurrency: 3    # 关键词触发异步执行并发上限, 默认 3
```

3. **新增文件**：`aiagent/domain/reply_log.go`、`aiagent/application/reply_service.go`、`aiagent/infrastructure/reply_log_repo_pg.go`、`aiagent/infrastructure/llm_eino.go`。
4. **改动文件**：`aiagent/domain/agent.go`（TriggerMode 枚举化 + repo 接口加 `ListEnabled`/`ExistsByLinkedUserID`）、`aiagent/application/service.go`（适配枚举）、`aiagent/interfaces/http/{handler,routes}.go`（手动触发）、`comment/application/service.go`（`AgentReplyTrigger` 端口）、`post/application/service.go`（`GetPostBrief`）、`pkg/composition/*`（装配）、`pkg/conf/conf.go`。

## 六、一致性 / 边界 / 风险

| 项 | 分析 | 对策 |
|---|---|---|
| 防重并发 | 同帖并发多次触发均会执行 | 接受（2026-08-26 起不设防重）：`min_interval_sec` + `max_replies_per_hour` 软限兜底用量 |
| 进程重启丢任务 | 异步 goroutine 在内存，未执行完即丢 | 接受：本功能是尽力而为的增强，日志表可见已处理记录 |
| 回环 | 机器人评论再触发关键词 | 入口按 `linked_user_id` 反查 agent 表丢弃 |
| 限频竞态 | 并发下 1h 计数可能瞬时超限 | 接受（软限）；`min_interval_sec` 主导节流 |
| key 解密失败（换 data_key） | LLM 必然失败 | 记失败日志，error_msg 含原因；管理员可经日志发现 |
| 日志表膨胀 | append-only 只增不减 | 失败也占额度自然限流；P1 可加清理 job |
| eino 组件内存 | 每次调用按 agent 配置新建 ChatModel | 调用频率低（限频兜底），不做连接池；组件内部 http.Client 每次新建可接受 |
| gemini/ollama 触发 | 未实现协议 | 记失败日志「protocol not implemented」，agent 可见可修 |
| 机器人评论热度 | 机器人评论也 +5 热度、写 CF 互动矩阵 | 接受（决策：机器人评论即普通评论）；后续如需剔除再议 |

## 七、分阶段交付

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0（本期） | TriggerMode 枚举、ReplyLog 实体/仓库、ReplyService（关键词+手动）、eino LLM 适配（openai/claude）、comment 触发钩子、手动触发接口、装配与配置、DDL 修正、API 文档更新 | ✅ |
| P1 | 回复日志管理端查询接口、日志清理 job | 待办 |
| P2 | mode=1 全帖触发（post_created 事件）、gemini/ollama 协议实现、一帖多机器人上限 `max_agents_per_post` | 待办（决策 B/D/A） |

---

## 八、后续演进：两阶段回复（分类器 + 生成器）

> 2026-08-26 起「特定条件才回复」不再依赖 system prompt 输出空值，改为两阶段硬流程：
> 关键词触发且 `filter_prompt` 非空时，先由分类器 LLM 判定（JSON 输出），通过才生成回复；
> 判定不通过写 `status=2` 日志行，限频口径排除 skipped。详见 [agent-reply-filter-design.md](agent-reply-filter-design.md)。

## 九、后续演进：自动回复高热度帖子 / 评论

> 2026-10-23 起（#99）新增定时扫描入口：机器人按 `auto_config.auto_reply` 的热度门槛自动回复帖子 / 评论，
> 复用本文 `executeReply` 全链路；日志表新增 `trigger_source` / `target_comment_id` 与自动回复部分唯一索引
>（每机器人每目标最多一次）。详见 [agent-auto-reply-design.md](agent-auto-reply-design.md)。
