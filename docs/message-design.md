# 私信（message）设计：会话与消息核心

> 目标：新增 `pkg/domains/message`，落地一对一私信的数据模型、发送、会话列表、消息历史、标记已读与删除会话（issue #238，总览 #30 的第 1 步）；
> 补充按对方用户 ID 查会话（issue #246，见第九节）、未关注发送限制（issue #239，见第十节）、私信权限设置（issue #240，见第十二节）。
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
| 私信权限设置（#240） | 接收方设置谁可以给自己发私信：所有人（默认）/ 仅我关注的人 / 仅互相关注 / 关闭；**已有会话同样受约束，对方回复过也不豁免**；先看权限再看 5 条限制。细则见第十二节 |
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
  application/permission.go         私信权限设置（#240）：DMPermission、DMPermissionReader 端口、checkDMPermission、can_send（sendPermission）
  application/conversation.go       ConversationService：会话列表（对方信息回填）/ 按对方查会话（#246）/ 标记已读 / 删除会话；
                                    列表项组装 toConversationItemVO 两处共用
  application/history.go            HistoryService：消息历史
  application/cursor.go             两种列表共用的 keyset 游标（带作用域校验）
  infrastructure/message_repo_pg.go Repository 的 PG 实现
  interfaces/http/handler.go        Handler + 请求 DTO + 错误映射
  interfaces/http/routes.go         路由（全部需登录）
pkg/composition/
  wire_message.go                   构造 + 路由注册
  message_bridges.go                user → message 桥接（接收方状态、对方简要信息、私信权限设置 #240）；follow → message 桥接（关注关系，#239）
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
| #240 私信权限设置 | **已实现**，见第十二节 |
| #241 未读总数与实时推送 | `conversation_member.unread_count`（按成员、按会话），未读总数 = `SUM(unread_count) WHERE user_id = ?`（可复用 `idx_conversation_member_list` 的前缀）；推送可在 `Send` 事务提交后发事件 |
| #242 频率限制与内容安全 | 发送入口只有 `MessageService.Send`；内容校验集中在 `normalizeContent` |
| #31 群聊 | `conversation.type=2`、`user_low/high` 为 NULL；`conversation_member.peer_id` 为 NULL、`deleted` 表示退出群聊；成员行的已读 / 未读 / 隐藏机制直接复用 |

## 八、范围外 / 未做

- 未读总数与实时推送、频率限制与内容安全（#241、#242）。
- 图片 / 文件消息、撤回、引用回复、消息搜索、拉黑、群聊（#31）。
- 进入会话时自动创建空会话（会话仍只在首次发送时创建，#246 范围外）。
- 「能否给对方发私信」（`can_send` / `cannot_send_reason`）由 #240 加到 #246 的接口里（第十二节）；进入会话前预先展示「还能发几条」
  （剩余条数）仍不返回，只在发送响应的 `remaining_count` 里。
- 拉黑 / 屏蔽某个用户、陌生人消息折叠（「消息请求」收件箱）：#240 范围外。

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

## 十二、私信权限设置（#240）

> 用户设置「谁可以给我发私信」。维护者 2026-11-11 确认：四个选项、存 `user_setting` 新列默认所有人；已有会话同样受约束、对方回复过不豁免；
> 「我能否给对方发私信」放在 `GET /message/conversations/with/:user_id` 的响应里；先看权限再看 #239 的 5 条限制。
> （第十一节留给 #241 未读总数与实时推送。）

### 12.1 选项与存储

`domains.user_setting.dm_permission smallint NOT NULL DEFAULT 0`（迁移 `migrations/20261111194151_add_user_setting_dm_permission.up.sql`，
只加带常量默认值的列，兼容旧版本应用；没有设置行 = 0）。经 `GET / PUT /user/settings` 的 `dm_permission` 读写，非法值 400。

| 值 | 含义（从接收方视角） | 发送方 S 能给接收方 R 发的条件 |
|---|---|---|
| `0` | 所有人（默认） | 总是可以；之后照常走 #239 的 5 条限制 |
| `1` | 仅我关注的人 | R 关注了 S（`IsFollowing(R, S)`） |
| `2` | 仅互相关注 | R 关注了 S **且** S 关注了 R |
| `3` | 关闭 | 不可以 |

未知取值（应用层只写 0–3，理论上不会出现，例如新版本加了选项后回滚）按**关闭**处理。

### 12.2 判断位置与顺序

`application.Send`：自己 / 正文校验 → **接收方状态**（`checkRecipient`：404 / 机器人 403）→ **私信权限**（`checkDMPermission`）→
**5 条限制**（`sendLimitFor` + 仓储事务内 `checkSendLimit`）→ 写入。权限拒绝时直接返回，不再查 5 条限制（「关闭」时连关注关系都不查）。

| 拒绝 | 哨兵错误 | HTTP |
|---|---|---|
| 仅我关注的人，R 没关注 S | `domain.ErrRecipientFollowingOnly` | 403 `This user only accepts private messages from people they follow` |
| 仅互相关注，不是互关 | `domain.ErrRecipientMutualOnly` | 403 `This user only accepts private messages from mutual follows` |
| 关闭 | `domain.ErrRecipientDMClosed` | 403 `This user has turned off private messages` |

- **已有会话同样受约束，对方回复过也不豁免**：权限判断不看会话状态；设置只约束发给设置者的消息，设置者自己仍能给别人发（包括回复已有会话）。
- 选项 1 / 2 放行的发送方一定被 R 关注，`sendLimitFor` 返回 0，5 条限制自然不生效。
- 权限与关注关系都在事务外实时读（不缓存）：与改设置 / 关注 / 取关并发时，按任一先后顺序的结果都合理，不需要加锁（同 #239）。

**跨域**：message 域声明端口 `application.DMPermissionReader`（返回 message 域自己的 `DMPermission` 枚举），由
`pkg/composition/message_bridges.go` 的 `messageDMPermissionReader` 用 user 域 `SettingService.GetSettings` 桥接并映射取值；
`wire_message.go` 的 `wireMessageDMPermission` 在 `server.go` 里用 setter 注入（未注入时不校验，等同所有人）。关注关系复用 #239 的 `FollowChecker`。

### 12.3 can_send

`GET /message/conversations/with/:user_id` 的响应在 `conversation` 之外平铺 `can_send`（bool）与 `cannot_send_reason`（string | null），
**有没有会话都返回**。由 `sendPermission` 按与 `Send` 相同的顺序判断，原因与发送错误一一对应：

| `cannot_send_reason` | 对应发送错误 |
|---|---|
| `recipient_not_found` | 404 `ErrRecipientNotFound` |
| `recipient_unavailable` | 403 `ErrRecipientUnavailable`（机器人） |
| `following_only` | 403 `ErrRecipientFollowingOnly` |
| `mutual_only` | 403 `ErrRecipientMutualOnly` |
| `dm_closed` | 403 `ErrRecipientDMClosed` |
| `dm_limit_reached` | 403 `ErrSendLimitReached`（#239） |

- **计入 5 条限制**（与发送接口同一口径）：所有人模式下对方没关注我、没回复过、我已发满 5 条时 `can_send=false`。
  只读查询 `Repository.GetSendLimitState`（按 `uk_conversation_pair` 定位会话，左连双方成员行，不加锁；口径同事务内的 `sqlSendLimitState`）。
- 接口原先不校验对方状态（#246）；现在为了 `can_send` 会读一次对方状态（`UserService.GetByID`，缓存优先），但仍**不因对方状态报错**，
  `conversation` 的返回规则不变（对方被禁用 / 删除时 `peer=null`、`can_send=false`）。
- `can_send` 是查询时刻的快照，发送仍以 `POST /message/send` 的结果为准。

### 12.4 测试

- 单测：`application/permission_test.go`（四个选项 × 四种关注关系的发送结果、判断顺序、can_send 各原因与 JSON）、user 域设置校验 / handler。
- 集成：`pkg/composition/message_dm_permission_integration_test.go`（真实设置 + 真实关注：四个选项 × 关注关系组合下发送结果与 `can_send` 一致、
  拒绝不建会话；已有会话且对方回复过不豁免；先权限后 5 条）、`infrastructure/dm_permission_integration_test.go`（`GetSendLimitState`）、
  `user/infrastructure/user_setting_dm_permission_integration_test.go`（新列按列 upsert）。
