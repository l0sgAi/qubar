# 私密圈邀请 API 对接文档（前端）

> **功能**：私密圈（`join_type=2`）是邀请制，不做入圈审核。圈主 / 管理员生成邀请链接，通过任意 IM 分享，拿到链接的登录用户确认后加入。
> **对应实现**：`pkg/domains/circle/`（`application/invite.go`、`interfaces/http/invite_handler.go`），issue #169。
> **状态**：已交付（邀请链接 + 指定 email 邀请）。

---

## 0. TL;DR — 前端要做的事

1. **生成链接**：管理端调 `POST /circle/invite/link/create`，拿到 `token` 后自己拼分享链接（如 `https://<前端域名>/invite/<token>`）。
   **`token` 只在这一次响应里出现**，之后列表接口不再返回，丢了只能重新生成。
2. **落地页**：打开分享链接时调 `GET /circle/invite/preview?token=`（访客可调）展示圈名、头像、成员数；
   返回 404 统一提示「邀请已失效」，不要区分原因。
3. **加入**：用户点「加入」时调 `POST /circle/invite/accept`（需登录，未登录先跳登录再回来）；成功后用返回的 `circle_id` 跳到圈子页。
   `already_member=true` 表示本来就是成员，同样直接跳转。
4. **email 邀请**：管理端录入邮箱（`POST /circle/invite/email/add`）；被邀请人在「我的邀请」（`GET /circle/invite/my`）里确认（`POST /circle/invite/email/respond`）。不发邮件，入口要前端自己放。
5. `POST /circle/join` 对私密圈仍然返回 403，私密圈只能走邀请。

---

## 1. 统一约定

- 响应信封 `{code, message, data}`，`code` 与 HTTP 状态码的对应同其他接口：`200` 成功、`201` 参数错误（HTTP 400）、`202` 未登录（401）、`203` 无权限 / 被拉黑（403）、`204` 不存在 / 已失效（404）。下文错误表写的是 HTTP 状态码。
- `message` 是英文，前端请按 `code` + 自身文案渲染。
- 时间字段为 RFC 3339。
- 权限：「管理员及以上」= 该圈圈主（role 30）或管理员（role 20），且成员状态正常。普通成员、非成员调用管理接口返回 403 `Circle admin privileges required`。
- token 是 32 字节随机数的 base64url 编码（无 `=` 填充，43 个字符，只含 `A-Z a-z 0-9 - _`），可以直接放进 URL path / query，无需额外转义。

---

## 2. 邀请链接

### 2.1 POST /circle/invite/link/create — 生成邀请链接

需登录；管理员及以上；**仅私密圈**。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `circle_id` | uuid | 是 | 圈子 ID |
| `expire_hours` | int | 否 | 有效期（小时）。不传或 0 = 默认 168（7 天）；范围 1–720（最长 30 天） |
| `max_uses` | int | 否 | 最多可用几次。不传 / `null` = 不限次数；范围 1–10000 |

```json
{ "circle_id": "01a1…", "expire_hours": 72, "max_uses": 10 }
```

**响应 `data`**

```json
{
  "id": "01a18aeb-bfea-72ee-8ffe-6d2d7e07ac9b",
  "token": "q3v…（43 字符）",
  "expire_time": "2026-11-06T11:47:48+08:00",
  "max_uses": 10
}
```

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invite links are only available for private circles` | 圈子不是私密圈 |
| 400 | `expire_hours must be between 1 and 720` | 有效期越界 |
| 400 | `max_uses must be between 1 and 10000` | 次数越界 |
| 403 | `Circle admin privileges required` | 不是管理员 |
| 404 | `Circle not found` | 圈子不存在 |

### 2.2 GET /circle/invite/link/list — 链接列表

需登录；管理员及以上。按创建时间倒序，包含已撤销、已过期的链接。**不返回 token。**

**Query**

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `circle_id` | uuid | 是 | | 圈子 ID |
| `page` | int | 否 | 1 | 页码，从 1 开始 |
| `size` | int | 否 | 20 | 每页条数，`<=0` 或 `>100` 回落 20 |

**响应**（分页信封，`data` 为数组）

```json
{
  "code": 200,
  "message": "Success",
  "data": [
    {
      "id": "01a18aeb-…",
      "expire_time": "2026-11-06T11:47:48+08:00",
      "max_uses": 1,
      "used_count": 1,
      "revoked": false,
      "expired": false,
      "creator_id": "01a18aea-…",
      "creator_name": "it169_owner",
      "creator_avatar": "",
      "create_time": "2026-10-30T11:47:48+08:00"
    }
  ],
  "total": 1,
  "page": 1,
  "per_page": 20
}
```

- `max_uses` 为 `null` 表示不限次数。
- 链接可用 = `!revoked && !expired && (max_uses == null || used_count < max_uses)`，前端可据此展示「有效 / 已撤销 / 已过期 / 已用完」。

### 2.3 POST /circle/invite/link/revoke — 撤销链接

需登录；管理员及以上（该链接所属圈子）。重复撤销幂等成功。

```json
{ "link_id": "01a18aeb-…" }
```

成功：`{"code":200,"message":"Invite link revoked"}`。链接不存在 404 `Invite link not found`；无权限 403。

### 2.4 GET /circle/invite/preview — 邀请预览

**访客可读**（带不带 token 都行）。

```
GET /circle/invite/preview?token=q3v…
```

**响应 `data`**（只有这三个字段）

```json
{ "name": "Golang 私密小组", "avatar_url": "https://…/a.png", "member_count": 12 }
```

token 不存在、已过期、已撤销、次数用完、圈子已改为公开或不可用：**一律 404** `Invite not found or no longer valid`，不区分原因。

### 2.5 POST /circle/invite/accept — 接受邀请

需登录。

```json
{ "token": "q3v…" }
```

**响应 `data`**

```json
{ "circle_id": "01a18aeb-…", "already_member": false }
```

| 情况 | 结果 |
|---|---|
| 链接有效，用户不是成员（含曾经退出 / 被移出） | 加入圈子，`already_member=false`，消耗一次使用次数 |
| 用户已是成员（正常 / 禁言） | 幂等成功，`already_member=true`，**不消耗次数**；链接已用完 / 过期 / 撤销时同样成功 |
| 用户被该圈拉黑 | 403 `User is banned from this circle` |
| 链接无效 / 过期 / 撤销 / 用完 / 圈子已改公开 | 404 `Invite not found or no longer valid` |
| 未登录 | 401 |

加入后的效果与公开圈直接加入一致：成员数 +1、出现在「我加入的圈子」、圈内帖子立即可见。

### 2.6 私密圈改为公开

圈主通过 `PUT /circle/update` 把 `join_type` 改成 0 后，该圈已有的邀请链接全部撤销（列表里 `revoked=true`），再改回私密也不会恢复，需要重新生成。公开圈不能生成邀请链接。

---

## 3. 指定 email 邀请

圈主 / 管理员录入邮箱名单；对应邮箱的用户登录后在「我的邀请」里看到，自己确认后加入。**不发邮件**，前端需要在消息中心或个人页放一个「圈子邀请」入口调 `GET /circle/invite/my`。

- 匹配规则：录入时 trim + 转小写；当前用户的 `users.email` 同样 trim + 小写后等值比较。
- 录入的邮箱可以还没有注册，注册后就能看到邀请。
- 邀请状态 `status`：`0` 待接受、`1` 已接受、`2` 已撤销、`3` 已拒绝。

### 3.1 POST /circle/invite/email/add — 添加邀请

需登录；管理员及以上；**仅私密圈**。

```json
{ "circle_id": "01a1…", "emails": ["Alice@Example.com", " bob@example.com ", "bad", "alice@example.com"] }
```

- `emails` 1–50 个，否则 400 `emails must contain 1 to 50 entries`。
- 已撤销 / 已拒绝的邮箱再次添加：重新变为待接受。
- 待接受 / 已接受的邮箱：不变，出现在 `ignored` 里。

**响应 `data`**

```json
{
  "added": ["alice@example.com", "bob@example.com"],
  "ignored": [
    { "email": "bad", "reason": "invalid" },
    { "email": "alice@example.com", "reason": "duplicate" }
  ]
}
```

| `reason` | 含义 |
|---|---|
| `invalid` | 格式不合法或超过 100 字符（只接受 `a@b.c` 形式的单个地址，不接受 `Name <a@b.c>`） |
| `duplicate` | 本次请求里重复（规整后相同） |
| `already_invited` | 该邮箱已有待接受或已接受的邀请 |

`added` 是规整后的邮箱，`ignored[].email` 是请求原文。

### 3.2 GET /circle/invite/email/list — 邀请列表

需登录；管理员及以上。按创建时间倒序。

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `circle_id` | uuid | 是 | | 圈子 ID |
| `status` | int | 否 | -1 | `-1` / 不传 = 全部；`0`–`3` 按状态过滤 |
| `page` / `size` | int | 否 | 1 / 20 | offset 分页，`size` `<=0` 或 `>100` 回落 20 |

**响应**（分页信封）

```json
{
  "code": 200, "message": "Success",
  "data": [
    {
      "id": "01a18b…", "email": "alice@example.com", "status": 0,
      "inviter_id": "01a18a…", "inviter_name": "圈主小王",
      "create_time": "2026-10-30T12:00:00+08:00", "update_time": "2026-10-30T12:00:00+08:00"
    }
  ],
  "total": 1, "page": 1, "per_page": 20
}
```

### 3.3 POST /circle/invite/email/revoke — 撤销邀请

需登录；管理员及以上。只有待接受的邀请能撤销；重复撤销幂等成功；已接受 / 已拒绝的返回 409 `Email invite is already accepted or rejected`。

```json
{ "invite_id": "01a18b…" }
```

### 3.4 GET /circle/invite/my — 我的邀请

需登录。当前用户 email 收到的**待接受**邀请，按创建时间倒序；已删除的圈子不展示。`page` / `size` 同上。

```json
{
  "code": 200, "message": "Success",
  "data": [
    {
      "invite_id": "01a18b…", "circle_id": "01a18a…",
      "circle_name": "Golang 私密小组", "circle_avatar": "https://…/a.png",
      "inviter_name": "圈主小王", "create_time": "2026-10-30T12:00:00+08:00"
    }
  ],
  "total": 1, "page": 1, "per_page": 20
}
```

### 3.5 POST /circle/invite/email/respond — 接受 / 拒绝

需登录。`accept` 必填（`true` 接受，`false` 拒绝），缺省返回 400。

```json
{ "invite_id": "01a18b…", "accept": true }
```

接受成功的 `data` 与邀请链接相同：`{ "circle_id": "…", "already_member": false }`；拒绝成功 `{"code":200,"message":"Invite rejected"}`。

| 情况 | 结果 |
|---|---|
| 邀请不是发给当前用户 email 的、不存在、已撤销、已拒绝，或圈子不可用 | 404 `Invite not found or no longer valid` |
| 接受，用户不是成员（含曾经退出 / 被移出） | 加入圈子，邀请变为已接受 |
| 接受，用户已是成员；或重复接受已接受的邀请 | 幂等成功，`already_member=true` |
| 接受，用户被该圈拉黑 | 403 `User is banned from this circle`，邀请保持待接受 |
| 拒绝 | 邀请变为已拒绝，不再出现在「我的邀请」；管理员可以重新添加 |

被移出圈子后想重新邀请同一个邮箱：已接受的邀请不会因重新添加而重置（`already_invited`），请改用邀请链接。
