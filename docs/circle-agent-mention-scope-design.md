# 圈内机器人 @提及 可见范围设计文档

> 目标：圈子级 AI 机器人（`ai_agent.circle_id` 非 NULL）只能在**对应圈子内**被 @——
> @选人列表（`GET /user/search`）按圈子作用域过滤，发帖/评论 mention 落库前服务端兜底剔除越圈机器人。
> 基线：
> ① 圈内机器人 CRUD 已落地（`/circle/agent/*`，`ai_agent.circle_id/creator_id` 两列已加，见 [circle-agent-manage-design.md](circle-agent-manage-design.md)）；
> ② 防泄漏护栏已在触发侧生效（`ListEnabled` 只加载 `circle_id IS NULL`，[agent_repo_pg.go:197](../pkg/domains/aiagent/infrastructure/agent_repo_pg.go#L197)）；
> ③ @选人走 ES user 索引（CDC 外部链路同步，Go 无 ES 写路径，同 post.hot 先例）；
> ④ mention 精确绑定已落库（post/comment_mention，名单前端传入、后端校验存在性/去自/截断）。
> 本文所有 file:line 基于当前 develop 分支，动手前先 grep 校准。
>
> **已确认决策**：① ~~软删机器人时 users 行只清 `agent_circle_id` 列，不动 status~~
> （**#171 推翻**：删除不再清 `agent_circle_id`，改为写 `users.agent_mention_disabled=1`；停用同样写 1、启用写 0；
> 仍不动 status。见 §九）；② mention 服务端兜底剔除纳入本期。

---

## 一、现状盘点（新开发者必读）

| 资产 | 位置 | 说明 |
|---|---|---|
| @选人接口 | [handler.go:96](../pkg/domains/user/interfaces/http/handler.go#L96) | `GET /user/search` → ES user 索引；过滤仅 `deleted=0 + status=1`（[user.go:78](../pkg/server/storage/elasticsearch/user.go#L78)），**无 role/圈子概念** |
| 机器人=用户 | [agent_bridges.go:58](../pkg/composition/agent_bridges.go#L58) | 机器人以 `linked_user_id`（role=2）身份落在 users 表 → **圈内机器人当前在全站@列表可见**，即本期要堵的泄漏点 |
| 圈子绑定权威 | [agent.go:27](../pkg/domains/aiagent/domain/agent.go#L27) | `AiAgent.CircleID *uuid.UUID`（nil=全局）。绑定关系只在 PG `ai_agent` 表，ES user 文档无此字段 |
| ES user 索引 | [indices.go:15](../pkg/server/storage/elasticsearch/indices.go#L15) | `pg.public.users`，CDC 外部链路全列同步（先例：`post.hot` 加列后 CDC 自动同步 ES，Go 侧零 ES 写代码，见 [hot-sync-design.md:71](design/hot-sync-design.md#L71)） |
| user 实体 | [user.go:18](../pkg/domains/user/domain/user.go#L18) | `SysUser` 无机器人绑定字段 |
| 发帖 mention 校验 | [service.go:466](../pkg/domains/post/application/service.go#L466) | `filterMentionUserIDs`：去重 → 去自己/Nil → 存在性（UserFacade）→ 截断；**无圈子作用域校验，手搓 ID 可绕过列表过滤** |
| 评论 mention 校验 | [service.go:697](../pkg/domains/comment/application/service.go#L697) | 同名函数同语义；评论经 `PostLookup.GetPost`（[service.go:211](../pkg/domains/comment/application/service.go#L211)）拿帖子，但 `PostInfo`（[service.go:43](../pkg/domains/comment/application/service.go#L43)）**无 CircleID 字段** |
| 圈内机器人创建 | [circle_service.go:154](../pkg/domains/aiagent/application/circle_service.go#L154) | `CreateBotUser(name, email, avatar)` 建 role=2 users 行 → `CreateInCircle` 插 ai_agent。**users 行当前不写任何圈子标识** |
| 圈内机器人删除 | [circle_service.go:252](../pkg/domains/aiagent/application/circle_service.go#L252) | `DeleteCircleAgent` → `repo.SoftDelete`（只动 ai_agent）；全局删除同（`AgentService.DeleteAgent` → 同一 SoftDelete） |
| 触发链护栏 | [agent_repo_pg.go:197](../pkg/domains/aiagent/infrastructure/agent_repo_pg.go#L197) | `ListEnabled` 已 `circle_id IS NULL` → 圈内机器人被@也**不回复**（回复触发改造是后续阶段，不在本期） |

**关键既有结论**（勿重新论证）：users.username 无唯一约束（机器人名可重复）；email 全局唯一（uuid 派生）；
userinfo Redis 缓存由 `UpdateProfile` 链路刷新（[agent_bridges.go:82](../pkg/composition/agent_bridges.go#L82) 注释）；
跨域只走 Facade/Port + composition 桥接，禁止包级 import 兄弟域。

---

## 二、方案选型（已定：DDL 加列 + ES 过滤）

| 候选 | 机制 | 结论 |
|---|---|---|
| A. 应用层排除名单 | aiagent 端口出"圈内机器人 user_id 名单"，Redis 缓存，ES 查询 `must_not terms id` | ❌ 否决。多一条跨域依赖 + 缓存一致性负担；规模上去后 terms 名单膨胀；后期还得迁回列方案 |
| **B. users 加列 + CDC 同步 ES** | `users.agent_circle_id` 列 → CDC → ES 文档字段 → `exists/term` 作用域过滤 | ✅ **本期采用**。数据模型一步到位防二次迁移；搜索路径零跨域调用；存量文档无该字段=天然落入"非圈内机器人"桶，**零重建索引** |

列放 **users 而非 ai_agent**：ES user 文档来自 users 表 CDC，ai_agent 无 ES 索引。
users 列是 `ai_agent.circle_id` 的**投影**（权威仍在 ai_agent），双写由应用层同事务保证。

---

## 三、数据模型与 Schema 变更

### 3.1 DDL（[user.md](../docs/pgsql-ddl/user.md)，交 DB-owner 执行）

```sql
ALTER TABLE domains.users ADD COLUMN agent_circle_id uuid NULL;
COMMENT ON COLUMN domains.users.agent_circle_id IS
    '机器人绑定圈子ID(domains.circle.id,无FK应用层保证);NULL=普通用户或全局机器人;仅 role=2 行有意义';
```

- 加列本身对存量行零行为变化；**存量圈内机器人需一次性回填**（应用层只在创建时写该列，
  上线前已建的圈内机器人无补写路径，不回填则作用域对它们永不生效），
  幂等 SQL 见 [user.md](../docs/pgsql-ddl/user.md) 迁移段。
- **不加索引**：过滤全在 ES 做；PG 侧只按主键回查（mention 兜底批量 IN 主键），无扫描场景。
- 格式对齐 user.md 既有"存量表迁移"节，由 DB-owner 执行。

### 3.2 ES mapping（ES-owner 执行，**先于代码上线**）

`pg.public.users` 索引加字段：`agent_circle_id: {"type": "keyword"}`。

- 显式声明 keyword：依赖 dynamic mapping 会落成 text+keyword 双字段，term 查询就得写
  `agent_circle_id.keyword` 后缀，易漏。
- **上线顺序硬约束**：先加 mapping，再部署 Go 代码。字段无 mapping 时 `exists` 查询行为
  依赖 ES 版本（可能全不命中→圈内机器人泄漏依旧），不可赌。

### 3.3 GORM 实体变更（[user.go:18](../pkg/domains/user/domain/user.go#L18)）

```go
// AgentCircleID 机器人绑定圈子ID（ai_agent.circle_id 的投影，供 @提及 作用域过滤）。
// nil=普通用户或全局机器人；仅 role=2 行有意义。
AgentCircleID *uuid.UUID `json:"-" gorm:"column:agent_circle_id;type:uuid"`
```

`json:"-"`：用户详情接口不回显（内部机制，不暴露）。

ES `UserDocument`（[user.go:12](../pkg/server/storage/elasticsearch/user.go#L12)）**不加字段**——
该字段只用于过滤，无需解析进响应。

---

## 四、分层实施设计

### 4.1 ES 层（[user.go](../pkg/server/storage/elasticsearch/user.go)）

`SearchUsers` 与 `buildUserSearchQuery` 加参数 `circleID uuid.UUID`（Nil=全站场景）：

```
circleID == Nil（全站@/用户搜索页）:
  must 追加 → {"bool": {"must_not": {"exists": {"field": "agent_circle_id"}}}}

circleID 非 Nil（圈内@）:
  must 追加 → {"bool": {"should": [
                  {"bool": {"must_not": {"exists": {"field": "agent_circle_id"}}}},
                  {"term": {"agent_circle_id": "<circleID>"}}
               ], "minimum_should_match": 1}}
```

- 两路都放 `must`（filter 语义，不算分）：无关键字分支追加到 `mustConditions`，
  关键字分支追加到 `searchConditions`（[user.go:96-158](../pkg/server/storage/elasticsearch/user.go#L96)）。
- 排序（id/_score）与 `search_after` 游标不动，前后页兼容。
- 全局机器人（agent_circle_id NULL）两路均可见——现状保留。

### 4.2 user 域

**application**（[service.go](../pkg/domains/user/application/service.go)）：

- `UserService.Search` 与 `UserSearcher.Search` 签名加 `circleID uuid.UUID`（Nil=全站）。
  service 层纯透传，无新逻辑——过滤全部下沉 ES。
- `UserService` 新增：

```go
// GetAgentCircleIDs 批量返回 userID → 机器人绑定圈子ID（仅含 agent_circle_id 非 NULL 的行）。
// 直查 repo.GetByIDs 不走缓存（mention 校验低频，数据新鲜度优先）。
// 供 post/comment 域 mention 兜底剔除越圈机器人。
GetAgentCircleIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
```

- `UserFacade`（[service.go:40](../pkg/domains/user/application/service.go#L40)）加同名方法，
  `userFacadeAdapter` 薄转发。

**infrastructure**（[user_searcher_es.go:20](../pkg/domains/user/infrastructure/user_searcher_es.go#L20)）：
透传 circleID 给 `elasticsearch.SearchUsers`。

### 4.3 aiagent 域（写路径：保持 users 列与 ai_agent.circle_id 一致）

**端口签名变更**（[service.go:38](../pkg/domains/aiagent/application/service.go#L38)）：

```go
// CreateBotUser 创建机器人系统用户。circleID 非 nil 时写入 users.agent_circle_id
//（全局链路传 nil）。其余语义不变。
CreateBotUser(ctx context.Context, username, email, avatarURL string, circleID *uuid.UUID) (uuid.UUID, error)
```

**新端口**（同文件，与 BotUserProfileUpdater 并列）：

> ⚠️ #171 已替换为 `BotUserMentionSyncer`（停用 / 启用 / 删除投影可 @ 状态，删除不再清绑定），见 §九。

```go
// BotUserScopeCleaner 跨域端口：机器人软删时清 users.agent_circle_id（恢复为"全局可见"语义）。
// 决策①：只清列，不动 users.status（已删全局机器人仍可被@的存量行为不变）。
type BotUserScopeCleaner interface {
    ClearBotCircleScope(ctx context.Context, userID uuid.UUID) error
}
```

**调用点**：
- `CircleAgentService.CreateCircleAgent`（[circle_service.go:154](../pkg/domains/aiagent/application/circle_service.go#L154)）：
  `CreateBotUser(..., &circleID)`。users 行写入与 `CreateInCircle` 插入**非同一事务**
  （跨库行已存在先例：BotUserCreator 本就是独立写），失败补偿沿用既有语义——
  CreateInCircle 失败时 users 孤儿行问题本期不扩大讨论（与全局链路现状一致）。
- `AgentService.CreateAgent`（全局）：传 nil。
- 两条删除路径（`AgentService.DeleteAgent` 全局 / `DeleteCircleAgent` 圈内，
  [circle_service.go:252](../pkg/domains/aiagent/application/circle_service.go#L252)）：
  `SoftDelete` 成功后调 `ClearBotCircleScope(linkedUserID)`。**失败只记日志不回滚**
  （软删已生效；列未清的最坏后果=已删机器人仍在原圈@列表可见，CDC 追平前窗口同级，接受；
  幂等可重试/人工清）。
- `SetBotUserScopeCleaner` setter 注入，fail-open（nil 时跳过清列 + log warn）——
  与 `botUserUpdater` 的 nil 处理范式一致（[circle_service.go:70](../pkg/domains/aiagent/application/circle_service.go#L70)）。

### 4.4 mention 服务端兜底（防手搓 ID 越圈@）

**post 域**（[service.go:466](../pkg/domains/post/application/service.go#L466)）：
`filterMentionUserIDs` 存在性校验后加一步——
`s.userFacade.GetAgentCircleIDs(候选IDs)` → 命中且 `circleID != input.CircleID` 的**静默剔除**
（对齐去自/截断的静默风格，不报错）。facade 未注入或出错 → 跳过剔除 + log warn
（fail-open 同现状；列表过滤已挡住正常用户，兜底只防构造请求）。

**comment 域**（[service.go:697](../pkg/domains/comment/application/service.go#L697)）：
同逻辑。前置依赖：`PostInfo`（[service.go:43](../pkg/domains/comment/application/service.go#L43)）
加 `CircleID uuid.UUID` 字段，`PostLookup` 桥接器回填（post 侧 `GetPostBrief` 已有 CircleID
数据，桥接器多赋一个字段）。`filterMentionUserIDs` 加 `postCircleID uuid.UUID` 参数。

### 4.5 interfaces/http 层

[handler.go:59](../pkg/domains/user/interfaces/http/handler.go#L59) `SearchUsersRequest` 加：

```go
CircleID string `query:"circle_id" binding:"omitempty,uuid"`
```

`SearchUsers` handler 解析为 `uuid.UUID`（空=Nil=全站），透传 service。
评论框@复用同接口，前端在圈上下文传 post 的 circle_id。

### 4.6 composition 装配

| 桥接器 | 端口 → 实现 | 备注 |
|---|---|---|
| `agentBotUserCreator` 改造 | `CreateBotUser` 加 circleID 写列 | [agent_bridges.go:58](../pkg/composition/agent_bridges.go#L58)，直写 users 行时多赋一字段 |
| `agentBotUserScopeCleaner`（新） | `BotUserScopeCleaner` → user 域 | 走 user Service 新方法（UpdateFields 清列 + userinfo 缓存失效），**不直持 DB**——缓存必须同步失效，UpdateProfile 链路已有刷新范式 |
| user facade 扩展 | post/comment `UserFacade.GetAgentCircleIDs` → user `userFacadeAdapter` | facade_bridges 既有适配器加方法，无新桥接器 |
| comment PostLookup 回填 | `PostInfo.CircleID` | 既有桥接器改一行 |

---

## 五、数据流

### 圈内@选人（查询路径）

```
用户 ──► GET /user/search?keyword=x&circle_id=<C>
  │ handler: bind + 解析 circle_id（空=Nil）
  ▼
UserService.Search(kw, size, after, circleID=C) ──透传──► ES SearchUsers
  ▼
must: [deleted=0, status=1,
       bool should: [must_not exists agent_circle_id,  ← 普通用户+全局机器人
                     term agent_circle_id=C]]           ← 本圈机器人
```

### 创建/删除（写路径，保持投影一致）

```
创建: CreateCircleAgent ─► CreateBotUser(..., &C)   ─► users.agent_circle_id = C
                        ─► CreateInCircle           ─► ai_agent.circle_id = C（权威）
                                          └── CDC 秒级同步 ES user 文档

删除: Delete*/DeleteCircleAgent ─► ai_agent SoftDelete（deleted=1,status=0）
                                ─► SetBotUserMentionable(false) ─► users.agent_mention_disabled = 1
                                   （#171：agent_circle_id 保留）     + userinfo 缓存刷新
停用/启用: Update*/UpdateCircleAgent(status) ─► SetBotUserMentionable(status==1)
```

### mention 兜底（防构造请求）

```
发帖/评论 ─► filterMentionUserIDs: 去重 → 去自 → 存在性 →
            GetAgentCircleIDs(IDs) → 剔除 circleID≠本帖圈子 的机器人 → 截断 → 落库
```

---

## 六、一致性 / 边界 / 风险

| 主题 | 决策 | 备注 |
|---|---|---|
| 双写漂移 | `ai_agent.circle_id` 权威，users 列投影；创建/删除同流程写入 | 极端漂移（人工改库）P1 加对账脚本，本期不做 |
| CDC 延迟 | 新圈内机器人秒级窗口内全站@列表可见（列未到 ES） | 同 post.hot 既有延迟口径，接受 |
| 删除清列失败 | ~~已删机器人短时仍在原圈@列表可见~~ #171 起删除不清列，改为投影不可 @；投影失败见 §九 | fail-open + 异步重试 |
| 上线顺序 | **ES mapping 先于 Go 部署**；DDL 先于 mapping | 字段无 mapping 时 exists 行为不可赌 |
| userinfo 缓存旧实体 | 老缓存 JSON 无新字段 → nil = 非圈内机器人，方向安全；清列走 Service 方法强制缓存失效 | 创建路径新行无旧缓存问题 |
| 翻页漂移 | 翻页中途机器人增删 → 结果集轻微漂移 | 软信号接受（ES search_after 语义本就弱一致） |
| 被@后是否回复 | **不回复**（ListEnabled 护栏既定） | 圈内触发回复属后续阶段，届时匹配 post.circle_id==agent.circle_id |
| 全局机器人 | 全站可@（agent_circle_id=NULL），现状保留 | — |
| 已删全局机器人仍可被@ | ~~存量怪癖，本期不动~~ **#171 已修**：停用 / 删除写 `agent_mention_disabled=1`，ES 与 mention 兜底都排除 | 不动 users.status（`GetBriefs` 跳过非正常用户，改 status 会让历史评论丢失作者信息） |

---

## 七、分阶段交付

### P0（本期，@可见范围闭环）

| # | 交付物 | 位置 |
|---|---|---|
| 1 | DDL 段（users + agent_circle_id） | [user.md](../docs/pgsql-ddl/user.md)（交 DB-owner） |
| 2 | ES mapping 操作说明（keyword 字段，先上线） | 文档本节 §3.2（交 ES-owner） |
| 3 | `SysUser.AgentCircleID` + `Search`/`UserSearcher` 加 circleID + ES 查询过滤 | user 域 domain/application/infrastructure + elasticsearch/user.go |
| 4 | handler `circle_id` query 参数 | user/interfaces/http/handler.go |
| 5 | `BotUserCreator` 加参 + `BotUserScopeCleaner` 端口 + 两处删除路径清列 | aiagent/application + composition/agent_bridges.go |
| 6 | user Service `GetAgentCircleIDs`（含缓存失效的清列方法）+ facade 扩展 | user/application + composition/facade_bridges.go |
| 7 | post/comment mention 兜底剔除 + `PostInfo.CircleID` | post/application、comment/application、composition 桥 |
| 8 | 单测：ES 过滤两路（全站排除/圈内可见）、兜底剔除（越圈剔除/本圈保留/全局机器人保留）、清列缓存失效、fail-open 降级 | 各层 _test.go |
| 9 | 前端契约更新（`GET /user/search` 加 circle_id） | docs/ 既有 API 文档 |

### P1（后续，非本期）

| 项 | 说明 |
|---|---|
| 圈内机器人回复触发 | 触发链匹配 post.circle_id == agent.circle_id（含 @提及）；移除对应护栏分支 |
| 双写对账脚本 | ai_agent.circle_id vs users.agent_circle_id 周期对账 |
| 已删机器人用户禁用 | #171 已用 `agent_mention_disabled` 解决可 @ 问题，不改 status（见 §九） |

---

## 八、明确非目标（本期不做）

- ❌ 圈内机器人的回复触发（护栏保证其被@也不回复）
- ❌ ES 重建索引（存量文档无该字段天然落入"非圈内机器人"桶，正确）；
  ⚠️ 数据回填**不做全量重建，但迁移段含一次性幂等回填**（存量圈内机器人 + 建圈失败
  孤儿行，见 [user.md](../docs/pgsql-ddl/user.md)）——原"存量行全 NULL 天然正确"仅在
  部署前不存在圈内机器人时成立，不可假设
- ❌ 全局机器人可见范围调整（仍全站可@）
- ❌ @选人接口本身的性能/召回优化

---

## 九、#171 变更：停用 / 删除的机器人不可被 @

> 现象：停用只改 `ai_agent.status`、删除只软删 `ai_agent`，`users` 行不变，ES @选人照样搜得到；
> 删除圈内机器人时 `ClearBotCircleScope` 还把 `users.agent_circle_id` 清成 NULL，它落进"全局机器人"桶，
> 在所有圈子都能被 @（最严重）。

### 9.1 方案：再加一列投影 `users.agent_mention_disabled`

| 项 | 决策 |
|---|---|
| 列 | `agent_mention_disabled SMALLINT NOT NULL DEFAULT 0`（迁移 `20261029191406`）。0=正常（普通用户恒为 0），1=机器人已停用或已删除 |
| 为什么是"禁用标记"而不是 `agent_mentionable DEFAULT 1` | 零值必须落在安全一侧：GORM 对带 `default` tag 的零值字段会省略写入，`SysUser` 的 userinfo 缓存 JSON 不含该列（`json:"-"`），两处零值都等于"可被 @" |
| 写入（aiagent 投影，权威仍是 `ai_agent`） | 创建：`CreateBotUser(..., mentionable = status==1)` 随建号写入；停用 / 启用：`Update*` 改了 status 就按更新后实体写；删除：软删后写 1。端口 `BotUserMentionSyncer` → user `SetAgentMentionable`（写列 + 刷新 userinfo 缓存） |
| 删除圈内机器人 | **不再清 `agent_circle_id`**（`BotUserScopeCleaner` / `ClearAgentCircleScope` 删除）。即使投影失败，删除的圈内机器人也只会留在原圈 |
| `users.status` | 恒为 1：`GetBriefs` 跳过非正常用户，改 status 会让机器人历史评论 / 帖子丢失作者信息（集成测试回归覆盖） |
| ES @选人 | `userBaseFilterConditions` 追加 `must_not term agent_mention_disabled: 1`（全站、圈内、有无关键字都生效）。存量文档缺该字段天然命中，零重建索引；动态 mapping 推断为 long 即可 |
| mention 兜底 | `GetAgentCircleIDs` 扩展为 `GetAgentMentionScopes`（`{CircleID, Mentionable}`），post / comment 的 `stripUnmentionableAgents` 剔除不可 @ 与越圈机器人：不落 mention、不发提醒、不触发回复（三者共用同一份过滤后名单） |
| CDC | `deploy/ansible/group_vars/all.yml` 的 `es_sinks.users` 字段白名单加 `agent_mention_disabled`（source connector 按表全列采集，无列白名单） |

### 9.2 投影写入失败策略：fail-open + 日志 + 异步重试

投影与 `ai_agent` 更新不在同一事务。写失败**不回滚**已生效的状态变更 / 软删（删除后再调接口会 404，报错让调用方重试也做不到），
记 Error 日志并起后台重试（3 次，指数退避 1s / 4s / 16s）。重试**每次从 `ai_agent` 重新推导目标值**（已删除 = 不可 @，
否则看 status），不重放旧值，避免和随后的启用 / 停用互相覆盖。重试耗尽记 Error，按 user.md 回填 SQL 人工修正
（无 outbox，进程退出即丢）。端口未注入只记 Warn（与 `botUserUpdater` 一致）。

已知竞态：同一机器人的"启用"和"删除"并发时，后写的投影胜出，极端情况下已删除机器人被写成可 @；
管理端低频操作，接受，回填 SQL 可修正。

### 9.3 存量回填：发版后一次性脚本（不进迁移）

发布流水线是 `migrate → deploy → register-connectors.sh`：迁移执行时 ES sink 还是旧字段白名单，迁移里的回填 UPDATE
产生的 CDC 事件会丢掉新列，ES 永远收不到。所以迁移只加列，回填 SQL 放在
[pgsql-ddl/user.md](pgsql-ddl/user.md)「agent_mention_disabled 存量回填」，**connector 重新注册之后**执行（幂等）。
回填同时把旧版删除链路清掉的 `agent_circle_id` 按 `ai_agent.circle_id` 补回，并把孤儿机器人账号（无对应 ai_agent 行）标为不可 @。

### 9.4 上线顺序

1. 迁移加列（旧版本应用不读不写，兼容）；2. infra 下发新 sink 白名单 → deploy 新应用 → `register-connectors.sh`；
3. 执行回填 SQL；4. 核对 SQL 返回 0 行。步骤 2→3 之间已停用 / 删除的存量机器人仍可见（与现状相同）。
