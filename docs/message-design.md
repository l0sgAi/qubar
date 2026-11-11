# 私信（message）设计：会话与消息核心

> 目标：新增 `pkg/domains/message`，落地一对一私信的数据模型、发送、会话列表、消息历史、标记已读与删除会话（issue #238，总览 #30 的第 1 步）；
> 补充按对方用户 ID 查会话（issue #246，见第九节）、未关注发送限制（issue #239，见第十节）、发送频率限制与内容安全（issue #242，见第十二节）。
> 后续步骤建在这一步之上：私信权限设置（#240）、未读总数与实时推送（#241）、频率限制与内容安全（#242）、群聊（#31）。
>
> 基线：此前没有用户间私信。表结构见 `migrations/20261111134129_add_private_message.up.sql`，字段说明见 [pgsql-ddl/message.md](pgsql-ddl/message.md)，
> 前端对接见 [api/message-api.md](api/message-api.md)。

---

## 一、业务规则（2026-11-11 维护者确认）

| 项 | 规则 |
|---|---|
| 消息类型 | 只有文本（`msg_type=1`）；图片 / 文件、撤回、引用回复、搜索、拉黑不在本期 |
| 正文 | 1–2000 个字符（按 Unicode 字符计，与 PG `varchar` 计数一致；**issue 原文 1000，维护者 2026-11-11 决策改为 2000**）；入库前过 `utils.SanitizeForPg`，清洗后不能为空或只有空白；正文按原样保存，不去首尾空白 |
| 接收方 | 状态正常的普通用户。不能发给自己；不存在 / 已禁用 / 已删除返回 404 |
| 未关注发送限制（#239） | 对方**没关注我**、也**没在会话里发过消息**时，我在该会话累计最多发 5 条，第 6 条 403 `dm limit reached`；对方关注我立即解除，对方回复过一次永久解除。固定 5 条，不可配置。细则见第十节 |
| 发送频率（#242） | 按发送者计：每分钟 20 条、每天 500 条、每天最多主动开启 20 个新会话（配置 `message.send_rate`，`<=0` 兜底这些默认值），超限 429；正文含外链放行（维护者 2026-11-11 确认）。细则见第十二节 |
| 发送方 | 账号已禁用 / 已删除时不能发送（登录态只校验 token，发送时再查账号状态），403。细则见第十二节 |
| AI 机器人账号 | **不能收私信**（维护者 2026-11-11 决策）：发给机器人账号（`users.role=2`，全局 / 圈内）按「不可发送」处理，返回 403 |
| 会话 | 首次发送自动建会话；两人之间只有一个单聊会话（`(user_low, user_high)` 唯一），并发首发不会产生两个 |
| 会话列表 | 对方简要信息、最后一条消息摘要（前 100 个字符）与时间、我的未读数；按最后消息时间倒序，keyset 游标分页 |
| 按对方查会话（#246） | 按对方用户 ID 返回我与他的单聊会话，字段同会话列表单项；没有会话返回 `conversation: null`，不建会话；只读。细则见第九节 |
| 消息历史 | 按会话向上翻旧消息，keyset 游标分页；只有会话双方可读，其他人 404（不区分「会话不存在」与「不是成员」） |
| 标记已读 | 把会话内我的未读清零，最后已读位置前移到当前最新消息 |
| 删除会话 | **只对自己隐藏**；对方（或自己）再发消息时会话重新出现，**只显示删除之后的消息**（维护者 2026-11-11 决策，按「成员行记录隐藏截止位置」实现）；对方不受影响 |
| 对方被禁用 / 删除 | 会话与历史保留，会话列表里 `peer` 为 `null`（`peer_id` 仍在）；不能再给对方发消息 |

「接收方能否收私信」的判定与 `UserService.GetByID` 一致：message 域声明端口 `application.RecipientChecker`，由
`pkg/composition/message_bridges.go` 的 `messageRecipientChecker` 用 `UserService.GetByID` 桥接（缓存优先），`role=2` 判为机器人；
会话列表的对方信息由 `messageUserBriefReader` 经 `UserFacade.GetBriefs` 批量回填（同 notice 列表的 actor 回填），不 import user 域。

## 二、分层与文件布局

```
pkg/domains/message/
  domain/message.go                 实体 Conversation / ConversationMember / Message、类型常量、OrderPair
  domain/repository.go              端口 Repository、ConversationEntry、哨兵错误
  application/service.go            MessageService（由小接口组合）、RecipientChecker / UserBriefReader 端口、常量
  application/send.go               SendService：Send（校验 + 清洗 + 接收方判定 + 未关注发送限制）
  application/send_limit.go         未关注发送限制（#239）：UnfollowedSendLimit、FollowChecker 端口、SendVO
  application/conversation.go       ConversationService：会话列表（对方信息回填）/ 按对方查会话（#246）/ 标记已读 / 删除会话；
                                    列表项组装 toConversationItemVO 两处共用
  application/history.go            HistoryService：消息历史
  application/cursor.go             两种列表共用的 keyset 游标（带作用域校验）
  infrastructure/message_repo_pg.go Repository 的 PG 实现
  interfaces/http/handler.go        Handler + 请求 DTO + 错误映射
  interfaces/http/routes.go         路由（全部需登录）
pkg/composition/
  wire_message.go                   构造 + 路由注册
  message_bridges.go                user → message 桥接（接收方状态、对方简要信息）；follow → message 桥接（关注关系，#239）
```

## 三、数据模型

三张表，全部 `-- cdc: none`（只在 PG 内读写，不进 ES）。

| 表 | 作用 | 唯一键 / 索引 |
|---|---|---|
| `conversation` | 会话。`type` 1=单聊 / 2=群聊（#31 预留）；单聊用 `user_low` / `user_high`（双方 UUID 按字节序排好）定位 | `uk_conversation_pair (user_low, user_high)`（群聊两列为 NULL，NULL 互不相等，不受约束） |
| `conversation_member` | 每个会话每个成员一行：最新消息位置、已读位置、未读数、隐藏截止位置、发送统计 | `uk_conversation_member (conversation_id, user_id)`；`idx_conversation_member_list (user_id, last_message_id DESC) WHERE deleted = 0` |
| `message` | 消息，只追加 | `idx_message_conversation (conversation_id, id DESC) WHERE deleted = 0` |

**位置一律用消息 ID 表示**：消息主键是 UUIDv7，时间序 = 字节序 = PG `uuid` 比较序，所以「最新消息」「已读到哪」「从哪开始隐藏」都存消息 ID，
比较即先后，不需要再冗余时间列；会话列表的排序键就是成员行的 `last_message_id`（单列唯一，keyset 不需要复合键）。

**最后一条消息不冗余正文**：成员行只存 `last_message_id`，会话列表一次查询按主键 JOIN `message` 取摘要（`left(content, 100)`）和 JOIN `conversation` 取类型，
没有 N+1，也不用在发送时多写一份正文。

## 四、数据流

```
POST /message/send
  handler ─ 校验 receiver_id ─▶ Send
    ├─ 自己？→ 400
    ├─ SanitizeForPg → 空 / 只有空白 → 400；> 2000 字符 → 400
    ├─ RecipientChecker（UserService.GetByID）→ 不存在/禁用/删除 404；role=2 → 403
    ├─ FollowChecker（FollowService.GetRelations）：对方关注了我 → limit=0（不限制），否则 limit=5（#239）
    └─ Repository.SendDirect(limit)（一个事务）
         1. INSERT conversation … ON CONFLICT (user_low, user_high) DO UPDATE SET update_time … RETURNING id
            └─ 冲突时也返回已有 ID，并锁住会话行：同一会话的发送串行
         1'. limit > 0：锁后读 我的 sent_count、对方成员行 first_sent_time
             └─ 对方发过消息 → 不限制；sent_count ≥ limit → 403 dm limit reached（回滚）；否则剩余 = limit − sent_count − 1
         2. 在锁内生成消息 ID（NewID）→ INSERT message
         3. 发送方成员行 upsert：last_message_id 前移、sent_count+1、first_sent_time 首次写入
         4. 接收方成员行 upsert：last_message_id 前移、unread_count+1
         （删除过会话的成员：cleared_message_id 不动，新消息 ID 大于它 → 会话自然重现）

GET /message/conversations
  conversation_member m（idx_conversation_member_list）
    WHERE m.user_id = 我 AND m.last_message_id > COALESCE(m.cleared_message_id, -∞) [AND m.last_message_id < 游标]
    JOIN message ON id = m.last_message_id、JOIN conversation
    ORDER BY m.last_message_id DESC LIMIT size+1
  → UserFacade.GetBriefs(对方 ID) 回填 peer

GET /message/conversations/with/:user_id（#246，只读）
  自己？→ 400
  conversation c（uk_conversation_pair：OrderPair(我, 对方)）JOIN 我的成员行（uk_conversation_member）
    LEFT JOIN message ON id = m.last_message_id AND m.last_message_id > COALESCE(m.cleared_message_id, -∞)
  ─ 没有行 → conversation: null
  → UserFacade.GetBriefs([对方 ID]) 回填 peer → toConversationItemVO（与会话列表同一个函数）

GET /message/conversations/:id/messages
  GetMember(会话, 我) ─ 不是成员 → 404
  message（idx_message_conversation）WHERE conversation_id = ? [AND id > cleared] [AND id < 游标] ORDER BY id DESC LIMIT size+1

POST /message/conversations/:id/read     UPDATE 成员行 SET last_read = last_message_id, unread = 0
DELETE /message/conversations/:id        UPDATE 成员行 SET cleared = last_read = last_message_id, unread = 0
```

## 五、游标

两种列表共用一种游标：URL 安全的 base64(JSON)，对前端不透明。

```json
{"v":1,"list":"messages","scope_id":"<会话ID 或 当前用户ID>","id":"<上一页最后一条消息ID>"}
```

`list` + `scope_id` 是游标所属的列表：解析时必须与本次请求一致（会话列表的作用域是当前用户，消息历史的作用域是会话），
否则 400（做法同关注列表游标）。`has_more=false` 时 `next_cursor` 为空串。页大小不传或 ≤0 取 20，超过 50 按 50。

## 六、一致性 / 边界 / 风险

| 场景 | 处理 |
|---|---|
| 并发首发（双方同时发第一条） | 会话 upsert 在唯一索引上串行，只建一个会话；集成测试 `TestSend_ConcurrentFirstSendCreatesOneConversation` 覆盖 |
| 同一会话并发发送的消息顺序 | 会话行锁内生成消息 ID，ID 顺序与提交顺序一致，消息历史按 ID 翻页不会漏掉「晚提交、ID 更小」的消息；`last_message_id` 用 `GREATEST` 前移，多实例时钟偏差下也不回退 |
| 死锁 | 所有写同一会话两条成员行的事务都先锁会话行，串行执行；已读 / 删除是单行 UPDATE |
| 未读与已读位置 | 已读 / 删除是一条 UPDATE，取成员行当前的 `last_message_id`，与并发发送在行锁上串行，未读数与已读位置不会错开 |
| 会话列表翻页时有新消息 | 会话被顶到最前（在游标之前），本轮翻页不会再出现；下拉刷新即可看到（常规 IM 行为）。静态数据下分页无重复无遗漏（集成测试覆盖） |
| 删除后再删除 | 幂等：截止位置等于当前最新消息 |
| 删除方自己再发消息 | 会话同样重现，只显示截止位置之后的消息（包括刚发的这条） |
| 发送方自己的未读 | 发送不改发送方的未读数 / 已读位置（不默认「回复即已读」），由客户端打开会话时调标记已读 |
| 对方被禁用 / 删除 | 会话保留，`peer=null`；再发送 404 |
| 机器人账号 | 403；已有会话不受影响（当前不可能产生） |
| 未关注发送限制的并发（#239） | 判断与 `sent_count+1` 都在会话行锁内，同一会话的发送串行，并发发送不会突破 5 条；集成测试 `TestSendLimit_ConcurrentSendsNeverExceedLimit`、`TestMessageSendLimit_ConcurrentSendsNeverExceedLimit` 覆盖 |

## 七、给后续 issue 预留的位置

| issue | 预留 |
|---|---|
| #239 未关注发送限制 | **已实现**，见第十节 |
| #240 私信权限设置 | 接收方判定集中在 `RecipientChecker`（`application/send.go`）；权限设置可在 `user_setting` 加列（该表注释已预告），桥接器里一并判定，或新增端口 |
| #241 未读总数与实时推送 | `conversation_member.unread_count`（按成员、按会话），未读总数 = `SUM(unread_count) WHERE user_id = ?`（可复用 `idx_conversation_member_list` 的前缀）；推送可在 `Send` 事务提交后发事件 |
| #242 频率限制与内容安全 | **已实现**，见第十二节 |
| #31 群聊 | `conversation.type=2`、`user_low/high` 为 NULL；`conversation_member.peer_id` 为 NULL、`deleted` 表示退出群聊；成员行的已读 / 未读 / 隐藏机制直接复用 |

## 八、范围外 / 未做

- 权限设置（#240）。AI 内容审核、举报私信、拉黑（#242 范围外）。
- 图片 / 文件消息、撤回、引用回复、消息搜索、拉黑、群聊（#31）。
- 进入会话时自动创建空会话（会话仍只在首次发送时创建，#246 范围外）。
- 「能否给对方发私信」的判定与置灰原因（`can_send`）：#246 的接口不返回；#239 只在发送时判定（发送响应带 `remaining_count`，满额时 403），
  进入会话前预先展示「还能发几条」不在 #239 范围内。

## 九、按对方用户 ID 查会话（#246）

> 前端从用户主页点「私信」进入会话页时，需要与该用户的 `conversation_id` 才能拉消息历史。维护者 2026-11-11 决定另开 #246 跟进。

`GET /message/conversations/with/:user_id`（需登录），响应 `data` 为 `{"conversation": <会话列表单项> | null}`。

| 场景 | 行为 |
|---|---|
| 有会话 | `conversation` 为会话列表单项（`conversation_id` / `type` / `peer_id` / `peer` / `last_message` / `unread_count`），由与会话列表相同的 `toConversationItemVO` 组装，口径一致 |
| 两人之间没有会话 | 200，`conversation: null`；**不建会话**（会话仍只在首次发送时创建） |
| `user_id` 是自己 | 400 `Cannot have a conversation with yourself`（`domain.ErrConversationWithSelf`） |
| `user_id` 不是合法 UUID | 400 `Invalid user id` |
| **对方用户不存在 / 已禁用 / 已删除 / 是机器人** | **不校验对方状态，只看会话是否存在**：没有会话时 `conversation: null`；已有会话（对方后来被禁用 / 删除）时照常返回会话，`peer` 为 `null`、`peer_id` 仍在。理由：与 #238 会话列表口径一致（会话与历史保留，`peer=null`），查询不额外读用户状态；能否发送由 `POST /message/send` 判定（不存在 / 禁用 / 删除 404、机器人 403），前端按主页信息决定是否显示「私信」按钮 |
| **我删除过该会话（隐藏中），之后没有新消息** | 返回会话（`conversation_id`、`type`、`peer_id`、`peer` 照常），**`last_message` 为 `null`**、`unread_count` 为 `0`（删除时已清零）。理由：删除之前的消息对我不可见（与 #238 的隐藏截止位置 `cleared_message_id` 一致），摘要也不能暴露；用这个 `conversation_id` 拉历史只返回删除之后的消息（此时为空）。会话列表里仍然看不到它 |
| 我删除过、之后又有新消息 | 与会话列表单项完全相同（会话已重新出现在列表里） |

- **只读**：一条 SELECT，不改未读数、已读位置、隐藏截止位置（集成测试比对查询前后成员行 `update_time`）。
- `last_message` 在会话列表里一定非 `null`（列表只含有可见消息的会话）；字段类型从对象改为可空对象，对列表的 JSON 没有影响。
- 查询：`conversation` 按 `(user_low, user_high)` 命中 `uk_conversation_pair`，成员行按 `uk_conversation_member`，最后一条消息按主键；不需要新索引或迁移。
- 群聊（#31）不走本接口。

## 十、未关注发送限制（#239）

> 防骚扰：对方没关注我、也没回复过我时，我在这个会话里最多发 5 条。维护者 2026-11-11 决策：**对方关注我，或对方在会话里回复过我**，都解除限制；
> 回复过一次后永久解除；固定 5 条，不做成配置（`application.UnfollowedSendLimit`）。不需要迁移：用 #238 预留的成员行字段。

| 场景 | 行为 |
|---|---|
| 对方没关注我，且对方从未在该会话发过消息 | 我在该会话累计最多 5 条；第 6 条 403 `dm limit reached`（`domain.ErrSendLimitReached`），不写消息、不改任何计数 |
| 对方关注了我 | 立即解除（每次发送实时查关注关系，不缓存） |
| 对方在会话里发过消息（对方成员行 `first_sent_time` 非 NULL） | 永久解除：之后对方取关、删除会话都不再限制 |
| 对方取关我，且从未回复 | 恢复限制，按我在该会话的**累计**已发条数（`sent_count`，包括关注期间发的）计算；已满 5 条则直接拒绝 |
| 对方先给我发消息 | 我天然不受限制（对方发过消息）；对方在我回复前同样受 5 条限制 |
| 我关注了对方 | 不影响（看的是对方是否关注我） |
| 删除会话（隐藏） | 不影响 `sent_count` 与 `first_sent_time` |

**判断时机与并发**：关注关系在应用层查（`FollowChecker`，桥接 `FollowService.GetRelations`，直接读关系行），决定传给仓储的 `limit`
（关注了 → 0 不限制，否则 5）；「对方是否回复过」和「我已发几条」在 `SendDirect` 事务里**锁住会话行之后**读（`sqlSendLimitState`），
判断与 `sent_count+1` 在同一把锁内，同一会话的并发发送串行，不会突破 5 条。关注关系在事务外读：发送与关注 / 取关并发时，结果按其中任一先后顺序都合理，不需要加锁。

**发送响应**：`POST /message/send` 的 `data` 在原有消息字段（平铺，与 #238 一致）之外新增 `remaining_count`：

- 限制生效时：本条发出后还能再发的条数（`4, 3, 2, 1, 0`；`0` 表示下一条会被拒绝）；
- 不受限制（对方关注了我，或对方回复过）时：`null`。

旧客户端忽略该字段即可，不受影响。被拒时 HTTP 403，`message` 固定为 `dm limit reached`，前端据此展示「对方关注你或回复后可继续发送」
（与机器人账号的 403 `This user cannot receive private messages` 通过 `message` 区分）。

**与 #240 叠加**：#240 的私信权限设置在本限制之前判定（先看权限，再看 5 条），建议放在 `Send` 里 `RecipientChecker` 之后、`sendLimitFor` 之前。

## 十二、发送频率限制与内容安全（#242）

> 维护者 2026-11-11 确认：每分钟 20 条、每天 500 条、每天最多主动开启 20 个新会话，做成新配置项（`<=0` 兜底默认值），超限 429；
> 正文包含外链放行。项目没有通用限流中间件（#29 / #217 范围外），参考 auth 验证码频控（[design-rate-limit-otp-hardening.md](design/design-rate-limit-otp-hardening.md)）的 Redis 计数写法。

### 12.1 规则

| 项 | 规则 |
|---|---|
| 发送条数 | 每个发送者每分钟最多 `per_minute` 条、每天最多 `per_day` 条；只统计**发送成功**的消息 |
| 新会话数 | 每个发送者每天最多**主动开启** `new_conversations_per_day` 个新会话：本次发送**新建了会话**才计数；会话已存在（包括对方先发起的、我删除过的）不计。额度用完后仍可在已有会话里发 |
| 窗口 | 固定窗口，**从窗口内第一次计数起算** 1 分钟 / 24 小时，到期整体清零；不是自然日（不依赖时区） |
| 被拒请求 | 不落库、不计入 #239 的 `sent_count`、不触发 #241 的推送、不占频率额度 |
| 内容校验 | 不变（#238 `normalizeContent`）：`SanitizeForPg` 清洗 → 至少一个非空白字符 → 至多 2000 个字符；正文含外链**放行** |
| 发送方状态 | 发送时用 `RecipientChecker`（与接收方同一判定，`UserService.GetByID`）校验发送方：不存在 / 已禁用 / 已删除 → 403 `ErrSenderUnavailable`；AI 机器人账号不在此拦截（行为不变） |
| 接收方状态 | 不变（#238）：不存在 / 已禁用 / 已删除 404，机器人 403 |

### 12.2 配置（`pkg/conf/conf_message.go`，段名 `message`）

| key | 默认 / 兜底 | 说明 |
|---|---|---|
| `message.send_rate.per_minute` | 20 | 每分钟最多发送条数 |
| `message.send_rate.per_day` | 500 | 每天（24h 窗口）最多发送条数 |
| `message.send_rate.new_conversations_per_day` | 20 | 每天（24h 窗口）最多主动开启的新会话数 |

各项 `<=0` 或缺省时兜底为默认值（`PerMinuteOrDefault` 等），生产 Secret 不加这一段也能跑。每次发送时读 `conf.Config`（composition `messageSendRateLimits`），
配置热更新即时生效；调小上限时已占用的计数不变，超出部分直到窗口到期都会被拒。

### 12.3 Redis key（`pkg/server/storage/redis/keys_message.go`）

| key | 类型 | TTL | 语义 |
|---|---|---|---|
| `message:rate:min:{sender_id}` | String 计数 | 60s，自窗口内第一次计数起算 | 每分钟发送条数 |
| `message:rate:day:{sender_id}` | String 计数 | 24h，同上 | 每天发送条数 |
| `message:rate:newconv:{sender_id}` | String 计数 | 24h，自窗口内第一次预占起算 | 每天主动开启的新会话数 |

Lua（`message_rate_lua.go`）：

- **占用** `acquireMessageSendRateScript`：每分钟计数 ≥ 上限 → 返回 1；每天计数 ≥ 上限 → 返回 2（超限什么都不写）；否则两者各 `INCR`，
  新会话计数 < 上限时也 `INCR` 并返回「已预占」。`INCR` 后 key 没有 TTL（第一次计数或异常残留）才 `PEXPIRE`，**不续期**
  （续期会让持续发送时窗口永不到期，见 otp 设计文档 §2.5）。一次 `EVALSHA` 完成「检查 → 占用」，多实例下并发请求不会突破上限。
- **退还** `releaseMessageSendRateScript`：key 存在且 > 0 才 `DECR`，不改 TTL；窗口已过期时不写，避免 `DECR` 建出 -1 且没有 TTL 的 key。

### 12.4 发送流程

```
Send
 ├─ 发给自己？ → 400
 ├─ normalizeContent（SanitizeForPg / 空白 / 2000 字符）→ 400
 ├─ RecipientChecker(receiver) → 404 / 403（机器人）
 ├─ checkSender：RecipientChecker(sender) 为 NotFound → 403 ErrSenderUnavailable        ← #242
 ├─ （#240 权限设置）
 ├─ sendLimitFor：FollowChecker → limit（#239）
 ├─ acquireSendRate                                                                       ← #242
 │    ├─ limiter.Acquire：每分钟 / 每天超限 → 429 ErrSendRateLimited（不占额度）
 │    └─ 新会话名额已用完（未预占）→ repo.GetDirectConversation(sender, receiver)
 │         ├─ 会话不存在（本次会新建）→ 退还条数额度，429 ErrNewConversationLimited
 │         └─ 会话已存在 → 继续
 ├─ repo.SendDirect（事务：会话 upsert → #239 检查 → 写消息 → 成员行）
 ├─ settleSendRate
 │    ├─ SendDirect 失败（含 #239 的 403）→ 退还条数额度与预占的新会话名额
 │    └─ 成功但 res.Created = false → 退还预占的新会话名额
 └─ （#241 afterSend：未读缓存 / 推送）
```

**「新建了会话」怎么判断（不多一次查询）**：`SendDirect` 的会话 upsert 冲突时 `RETURNING` 已有行的 ID，所以返回的 ID 等于本次生成的 ID 即为新建，
结果追加到 `domain.SendResult.Created`（仓储签名不变）。新会话名额采用「先预占、没新建再退还」：正常路径不多查询；
只有当天新会话额度已用完的发送者，每次发送多一次按 `uk_conversation_pair` 的会话查询（判断这次会不会新建会话），被拒时不建会话。

### 12.5 一致性 / 边界 / 风险

| 场景 | 处理 |
|---|---|
| 多实例 | 计数在 Redis，一次 Lua 原子检查并占用，所有实例共享；集成测试 `TestSendRateLimiter_ConcurrentNeverExceeds`（30 并发、上限 5）覆盖 |
| **Redis 不可用** | **拒绝发送**（`Acquire` 出错 → 返回错误，handler 500 `Failed to process message request`），与 auth 验证码频控一致（`CheckSendRateLimit` 出错直接返回错误）。登录态本身也在 Redis，Redis 不可用时登录接口已不可用，拒绝不会额外降低可用性 |
| 退还失败 | 尽力而为，只记 Warn 日志；代价是该发送者当前窗口少几条额度，窗口到期自愈 |
| 新会话名额边界的并发 | 名额只剩 1 个时，发往已有会话的请求会短暂预占再退还，同时发起的新会话请求可能看到「已用完」而被拒（偏严，不会超发）。只在额度临界、同一发送者并发时出现 |
| 额度用完后的会话查询与新建之间 | 查到会话存在才放行，会话不会被删除（删除只是成员行隐藏），所以放行后的 `SendDirect` 不会新建会话 |
| 固定窗口边界 | 窗口切换的瞬间最多可连发 2 倍每分钟上限（固定窗口的固有特性）；每天上限兜底 |
| 发送方状态的实时性 | 与接收方同一判定（`UserService.GetByID`，`user:info` 缓存 30 分钟）：在库里禁用 / 删除账号后，缓存过期前仍按旧状态判断。项目目前没有应用内的禁用 / 删除入口（运维直接改库），改库时需同时删除 `user:info:{id}`；登录 token 也不会因禁用失效 |
| 重放请求（#248 `client_msg_id`） | 本 issue 不处理幂等。当前口径：重放请求与普通请求一样先占用频率额度；`SendDirect` 返回原消息（成功）时不退还，即重放也计入频率。#248 合入后如需不计，可在其「命中重放」标志上退还 |
| 被 #239 拒绝（403 `dm limit reached`） | `SendDirect` 失败 → 全部退还，不占频率额度 |
| 配置热更新调小上限 | 已有计数不变，超出部分直到窗口到期都被拒 |

### 12.6 错误

| 错误 | HTTP（信封 code） | message |
|---|---|---|
| `domain.ErrSendRateLimited` | 429（208） | `message rate limit exceeded: at most {N} messages per minute` / `... per day` |
| `domain.ErrNewConversationLimited` | 429（208） | `new conversation limit exceeded: at most {N} new conversations per day` |
| `domain.ErrSenderUnavailable` | 403（203） | `Your account cannot send private messages` |
