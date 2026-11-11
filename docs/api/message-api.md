# 私信 API 对接文档（前端）

> **功能**：一对一文本私信：发送、会话列表、消息历史、标记已读、删除会话。
> **对应实现**：`pkg/domains/message/`（`application/send.go`、`conversation.go`、`history.go`，`interfaces/http/handler.go`），issue #238；按对方查会话 #246。
> 设计见 [message-design.md](../message-design.md)。
> **状态**：已交付私信核心（#238）、按对方用户 ID 查会话（#246）。未关注发送限制（#239）、私信权限设置（#240）、未读总数与实时推送（#241）、频率限制（#242）后续提供。

---

## 0. TL;DR — 前端要做的事

1. **发私信**：`POST /message/send`，带 `receiver_id` 和 `content`。第一次给某人发消息时自动建会话，响应里有 `conversation_id`。
2. **私信列表页**：`GET /message/conversations`，游标分页，按最后一条消息时间倒序；每项带对方信息、最后一条消息摘要、我的未读数。
3. **从用户主页进私信**：`GET /message/conversations/with/:user_id` 拿 `conversation_id`；`conversation` 为 `null` 表示还没聊过，
   聊天页显示空白，用户发出第一条消息后从 `POST /message/send` 的响应里拿 `conversation_id`。
4. **聊天页**：`GET /message/conversations/:conversation_id/messages`，第一页是最新的消息，`next_cursor` 往更旧的消息翻（上滑加载）。
   **返回顺序是新 → 旧**，渲染时请反转。
5. **进入聊天页 / 收到新消息时**：调 `POST /message/conversations/:conversation_id/read` 清零未读。
6. **删除会话**：`DELETE /message/conversations/:conversation_id`，只对自己隐藏。对方再发消息时会话会重新出现，只能看到删除之后的消息。
7. AI 机器人账号不能收私信（403），机器人主页不要显示「私信」按钮。
8. 当前没有实时推送：新消息需要前端轮询会话列表或进入页面时刷新（#241 会提供）。

---

## 1. 统一约定

- 全部接口需要登录（未登录 401）。
- 响应信封 `{code, message, data}`，`code` 与 HTTP 状态码的对应同其他接口：`200` 成功、`201` 参数错误（HTTP 400）、`202` 未登录（401）、`203` 无权限（403）、`204` 不存在（404）、`500` 服务器错误。下文错误表写的是 HTTP 状态码。
- `message` 是英文，前端请按 `code` + 自身文案渲染。
- 列表接口都是游标分页：query 参数 `cursor`（上一页的 `next_cursor`，原样回传；不传表示第一页）、`size`（不传或 ≤0 取 20，超过 50 按 50）。
  响应里 `has_more=false` 时 `next_cursor` 为空串。游标是不透明字符串，不要解析或拼接；非法游标返回 400 `Invalid cursor`。
- 时间字段是 RFC 3339 字符串。

### 消息对象 `MessageVO`

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | 消息 ID（UUIDv7，越大越新） |
| `conversation_id` | string | 会话 ID |
| `sender_id` | string | 发送者用户 ID；等于当前用户即「我发的」 |
| `msg_type` | int | `1` 文本（当前只有文本） |
| `content` | string | 正文（原样，含换行） |
| `create_time` | string | 发送时间 |

---

## 2. 接口

### 2.1 POST /message/send — 发送私信

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `receiver_id` | string | 是 | 接收方用户 ID |
| `content` | string | 是 | 正文：1–2000 个字符（按字符计，中文一个字算 1），不能只有空白 |

```json
{ "receiver_id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10", "content": "你好" }
```

**响应 `data`**：`MessageVO`（见 §1）。

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "id": "019a7b10-2c3d-7e4f-8a9b-0c1d2e3f4a5b",
    "conversation_id": "019a7b10-2c3a-7d11-9e22-3f4a5b6c7d8e",
    "sender_id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b11",
    "msg_type": 1,
    "content": "你好",
    "create_time": "2026-11-11T14:30:00.123456+08:00"
  }
}
```

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid request body` / `Invalid receiver_id` | 请求体不是 JSON / `receiver_id` 不是合法 UUID |
| 400 | `Cannot send message to yourself` | 发给自己 |
| 400 | `Message content is empty` | 正文为空或只有空白 |
| 400 | `message content is too long: at most 2000 characters` | 正文超过上限（数字为当前上限） |
| 401 | `Token not found` | 未登录 |
| 403 | `This user cannot receive private messages` | 接收方是 AI 机器人账号 |
| 404 | `No such user` | 接收方不存在、已禁用或已删除 |

### 2.2 GET /message/conversations — 会话列表

按最后一条消息时间倒序。删除过、且之后没有新消息的会话不在列表里。

**query**：`cursor`、`size`（见 §1）。

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `items` | array | 会话列表项，见下表 |
| `has_more` | bool | 是否还有下一页 |
| `next_cursor` | string | 下一页游标；`has_more=false` 时为空串 |

列表项：

| 字段 | 类型 | 说明 |
|---|---|---|
| `conversation_id` | string | 会话 ID |
| `type` | int | `1` 单聊（群聊以后再加） |
| `peer_id` | string | 对方用户 ID |
| `peer` | object \| null | 对方 `{id, username, avatar_url}`；对方已被禁用 / 删除时为 `null`（会话与历史仍保留，但不能再发消息） |
| `last_message` | object | 最后一条消息：`{id, sender_id, msg_type, content, create_time}`，`content` 只有前 100 个字符 |
| `unread_count` | int | 我在该会话的未读数 |

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "items": [
      {
        "conversation_id": "019a7b10-2c3a-7d11-9e22-3f4a5b6c7d8e",
        "type": 1,
        "peer_id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10",
        "peer": { "id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10", "username": "alice", "avatar_url": "https://..." },
        "last_message": {
          "id": "019a7b10-2c3d-7e4f-8a9b-0c1d2e3f4a5b",
          "sender_id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10",
          "msg_type": 1,
          "content": "你好",
          "create_time": "2026-11-11T14:30:00.123456+08:00"
        },
        "unread_count": 1
      }
    ],
    "has_more": false,
    "next_cursor": ""
  }
}
```

翻页过程中某个会话来了新消息，它会被顶到第一页，本轮往后翻不会再出现；下拉刷新即可看到。

**错误**：400 `Invalid cursor`、401。

### 2.2.1 GET /message/conversations/with/:user_id — 按对方用户 ID 查会话

从用户主页点「私信」时用：返回我与该用户的单聊会话。**只读**，不会创建会话，也不会清未读或取消删除。

**路径参数**：`user_id` 对方用户 ID。

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `conversation` | object \| null | 与 §2.2 会话列表项**字段完全相同**；两人还没有会话时为 `null` |

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "conversation": {
      "conversation_id": "019a7b10-2c3a-7d11-9e22-3f4a5b6c7d8e",
      "type": 1,
      "peer_id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10",
      "peer": { "id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10", "username": "alice", "avatar_url": "https://..." },
      "last_message": {
        "id": "019a7b10-2c3d-7e4f-8a9b-0c1d2e3f4a5b",
        "sender_id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10",
        "msg_type": 1,
        "content": "你好",
        "create_time": "2026-11-11T14:30:00.123456+08:00"
      },
      "unread_count": 1
    }
  }
}
```

没有会话：

```json
{ "code": 200, "message": "Success", "data": { "conversation": null } }
```

几种情况：

| 情况 | 返回 |
|---|---|
| 还没有会话 | `conversation: null`。进入空白聊天页，发第一条消息后用 `POST /message/send` 响应里的 `conversation_id` |
| 我删除过这个会话，之后没有新消息 | 返回会话，`conversation_id` 可用，但 **`last_message` 为 `null`**、`unread_count` 为 `0`；用它拉历史只能看到删除之后的消息（此时为空）。这个会话在会话列表里仍然看不到，发消息后会重新出现 |
| 对方已被禁用 / 删除，但以前聊过 | 返回会话，`peer` 为 `null`（同会话列表）；不能再发消息 |
| 对方不存在 / 是 AI 机器人，且没有会话 | `conversation: null`（本接口不校验对方状态；能否发送以 `POST /message/send` 的结果为准） |

注意：会话列表（§2.2）里的 `last_message` 一定不为 `null`；只有本接口在「删除后没有新消息」时为 `null`。

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid user id` | `:user_id` 不是合法 UUID |
| 400 | `Cannot have a conversation with yourself` | `user_id` 是自己 |
| 401 | `Token not found` | 未登录 |

### 2.3 GET /message/conversations/:conversation_id/messages — 消息历史

只有会话双方可读。第一页（不传 `cursor`）是最新的 `size` 条，**按新 → 旧返回**；用 `next_cursor` 继续取更旧的消息。
删除过会话的一方只能看到删除之后的消息。

**query**：`cursor`、`size`（见 §1）。

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `items` | array | `MessageVO` 列表，新 → 旧 |
| `has_more` | bool | 是否还有更旧的消息 |
| `next_cursor` | string | 更旧一页的游标；`has_more=false` 时为空串 |
| `last_read_message_id` | string | 我的最后已读位置（空串 = 从未读过）。可以用它画「以下为新消息」分隔线：ID 大于它的是新消息（在标记已读之前读取） |

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid conversation id` | `:conversation_id` 不是合法 UUID |
| 400 | `Invalid cursor` | 游标非法，或是别的会话的游标 |
| 401 | `Token not found` | 未登录 |
| 404 | `No such conversation` | 会话不存在，或我不是会话成员 |

### 2.4 POST /message/conversations/:conversation_id/read — 标记已读

把我在该会话的未读清零（已读到当前最新一条）。重复调用无副作用。无请求体，响应 `data` 为 `null`。

**错误**：400 `Invalid conversation id`、401、404 `No such conversation`。

### 2.5 DELETE /message/conversations/:conversation_id — 删除会话

只对自己隐藏：会话从我的列表消失，未读清零，对方不受影响。之后对方（或我自己）再发消息，会话重新出现，**只显示删除之后的消息**。
重复调用无副作用。响应 `data` 为 `null`。

**错误**：400 `Invalid conversation id`、401、404 `No such conversation`。
