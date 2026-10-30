# 私密圈邀请 API 对接文档（前端）

> **功能**：私密圈（`join_type=2`）是邀请制，不做入圈审核。圈主 / 管理员生成邀请链接，通过任意 IM 分享，拿到链接的登录用户确认后加入。
> **对应实现**：`pkg/domains/circle/`（`application/invite.go`、`interfaces/http/invite_handler.go`），issue #169。
> **状态**：邀请链接已交付；指定 email 邀请见第 3 节（后续 PR）。

---

## 0. TL;DR — 前端要做的事

1. **生成链接**：管理端调 `POST /circle/invite/link/create`，拿到 `token` 后自己拼分享链接（如 `https://<前端域名>/invite/<token>`）。
   **`token` 只在这一次响应里出现**，之后列表接口不再返回，丢了只能重新生成。
2. **落地页**：打开分享链接时调 `GET /circle/invite/preview?token=`（访客可调）展示圈名、头像、成员数；
   返回 404 统一提示「邀请已失效」，不要区分原因。
3. **加入**：用户点「加入」时调 `POST /circle/invite/accept`（需登录，未登录先跳登录再回来）；成功后用返回的 `circle_id` 跳到圈子页。
   `already_member=true` 表示本来就是成员，同样直接跳转。
4. `POST /circle/join` 对私密圈仍然返回 403，私密圈只能走邀请。

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

后续 PR 交付（`POST /circle/invite/email/add`、`GET /circle/invite/email/list`、`POST /circle/invite/email/revoke`、
`GET /circle/invite/my`、`POST /circle/invite/email/respond`）。
