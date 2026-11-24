# 私信：`conversation` / `conversation_member` / `message`

> [返回目录](README.md)。权威 DDL 见 `migrations/20261111134129_add_private_message.up.sql`、`migrations/20261111193813_add_message_client_msg_id.up.sql`（#248）；设计见 [message-design.md](../message-design.md)（#238）。
> 三张表都不进 CDC（`-- cdc: none`），只在 PG 内读写。

## 会话表

```sql
CREATE TABLE domains.conversation (
    id UUID PRIMARY KEY DEFAULT uuidv7(),       -- 主键 (UUIDv7,应用层生成)
    type SMALLINT NOT NULL DEFAULT 1,           -- 会话类型 (1=单聊, 2=群聊 #31 预留)
    user_low  UUID,                             -- 单聊双方中 UUID 字节序较小的一方;群聊 NULL
    user_high UUID,                             -- 单聊双方中 UUID 字节序较大的一方;群聊 NULL
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted SMALLINT NOT NULL DEFAULT 0         -- 预留,当前不使用
);

-- ⚠️ 应用依赖：首次发送走 INSERT … ON CONFLICT (user_low, user_high)，勿删 / 勿改为部分索引
CREATE UNIQUE INDEX uk_conversation_pair ON domains.conversation (user_low, user_high);
```

| 字段 | 说明 |
|---|---|
| `type` | `1` 单聊；`2` 群聊（#31 预留，当前不产生） |
| `user_low` / `user_high` | 单聊双方，按 UUID 字节序排好（`domain.OrderPair`），保证每对用户只有一行。群聊为 NULL，NULL 互不相等，不受唯一索引约束 |
| `update_time` | 每次发送消息时刷新（发送事务用它对会话行加锁） |
| `deleted` | 预留。用户删除会话只改自己的成员行 |

## 会话成员状态表

```sql
CREATE TABLE domains.conversation_member (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    conversation_id UUID NOT NULL,              -- conversation.id
    user_id UUID NOT NULL,                      -- 成员 (users.id)
    peer_id UUID,                               -- 单聊对方 (users.id);群聊 NULL
    last_message_id UUID,                       -- 会话最新一条消息 (会话列表排序键)
    last_read_message_id UUID,                  -- 最后已读位置
    unread_count INTEGER NOT NULL DEFAULT 0,    -- 未读数 (#241)
    cleared_message_id UUID,                    -- 隐藏截止位置 (删除会话)
    sent_count INTEGER NOT NULL DEFAULT 0,      -- 本成员累计发送条数 (#239)
    first_sent_time TIMESTAMPTZ,                -- 本成员首次发送时间 (#239)
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted SMALLINT NOT NULL DEFAULT 0         -- 群聊退出 (#31 预留)
);

-- ⚠️ 应用依赖：发送时走 INSERT … ON CONFLICT (conversation_id, user_id)，勿删 / 勿改为部分索引
CREATE UNIQUE INDEX uk_conversation_member ON domains.conversation_member (conversation_id, user_id);

-- 会话列表：按最新消息倒序 keyset 翻页
CREATE INDEX idx_conversation_member_list ON domains.conversation_member (user_id, last_message_id DESC) WHERE deleted = 0;
```

| 字段 | 说明 |
|---|---|
| `last_message_id` | 会话最新一条消息的 ID（双方的值相同）。消息 ID 是 UUIDv7，比较即先后，会话列表按它倒序、单列 keyset |
| `last_read_message_id` | 标记已读 / 删除会话时置为 `last_message_id`。NULL = 从未读过 |
| `unread_count` | 对方每发一条 +1，标记已读 / 删除会话清零。自己发消息不改自己的未读 |
| `cleared_message_id` | 删除会话时置为当时的 `last_message_id`。本成员只能看到 ID 大于它的消息；`last_message_id` 不大于它时会话不在列表里，对方再发消息即重现 |
| `sent_count` / `first_sent_time` | 本成员在该会话的发送统计。「对方首条消息时间」= 对方成员行的 `first_sent_time`。#239 未关注发送限制在发送事务内锁会话行后读取：对方 `first_sent_time` 非 NULL（回复过）永久解除，否则 `sent_count ≥ 5` 拒绝（见 [message-design.md](../message-design.md) 第十节） |
| `deleted` | 单聊恒为 0；删除会话用 `cleared_message_id`，不改本列 |

## 消息表

```sql
CREATE TABLE domains.message (
    id UUID PRIMARY KEY DEFAULT uuidv7(),       -- UUIDv7,在会话行锁内生成,同一会话内单调递增
    conversation_id UUID NOT NULL,              -- conversation.id
    sender_id UUID NOT NULL,                    -- 发送者 (users.id)
    msg_type SMALLINT NOT NULL DEFAULT 1,       -- 1=文本
    content VARCHAR(2048) NOT NULL,             -- 正文 (应用层校验 1-2000 字符)
    client_msg_id VARCHAR(64),                  -- 客户端消息ID (发送幂等键,#248;NULL=未提供)
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted SMALLINT NOT NULL DEFAULT 0         -- 撤回等后续功能预留
);

-- 消息历史：按会话向上翻旧消息
CREATE INDEX idx_message_conversation ON domains.message (conversation_id, id DESC) WHERE deleted = 0;

-- 发送幂等（#248）：同一发送人同一 client_msg_id 只落一条
CREATE UNIQUE INDEX uk_message_sender_client_msg ON domains.message (sender_id, client_msg_id) WHERE client_msg_id IS NOT NULL;
```

- `content` 列宽取 2048（2 的幂档位，覆盖 2000 字符上限）；上限由应用层 `MaxContentChars` 控制，调整上限在 2048 以内不需要改表。
- `client_msg_id`（#248）：客户端生成的发送幂等键，1–64 位 `[A-Za-z0-9_-]`（应用层 `MaxClientMsgIDLen` 校验，列宽 64 即上限）。
  `uk_message_sender_client_msg` 是部分唯一索引，只约束非 NULL：不带键的消息（含旧版本应用写入的）不受影响。
  发送事务锁会话行后按它查重放，插入时作为 `ON CONFLICT … DO NOTHING` 的仲裁索引；设计见 [message-design.md](../message-design.md) 第十二节。
- 审计列只保留 `create_time` / `update_time` / `deleted`：操作人恒为 `sender_id` / 成员本人，不设 `create_by` / `update_by`。
