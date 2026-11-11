# 用户关注 API 对接文档（前端）

> **功能**：用户间关注 / 取关，用户主页展示关注数、粉丝数和「我与对方」的关系状态；关注 / 粉丝 / 互关列表、批量关系查询、列表公开 / 私密开关；被关注通知。
> **对应实现**：`pkg/domains/follow/`（`application/relation.go`、`stats.go`、`list.go`、`notify.go`，`interfaces/http/handler.go`、`list_handler.go`）与
> `pkg/domains/user/`（`application/setting.go`、`interfaces/http/setting_handler.go`），issue #217 / #218 / #219。设计见 [follow-design.md](../follow-design.md)。
> **状态**：已交付关注 / 取关 / 统计（#217），列表 / 批量关系 / 隐私开关（#218），被关注通知（#219，前端对接见 [notice-frontend-api.md](../notice-frontend-api.md)）。首页关注 tab（#220）见 [home-feed-api.md](home-feed-api.md)。

---

## 0. TL;DR — 前端要做的事

1. **用户主页**：进页面调 `GET /follow/stats/:user_id`（访客也能调），拿 `following_count`、`follower_count` 和 `relation`，按 `relation` 渲染关注按钮。
2. **关注 / 取关**：`POST /follow/:user_id`、`DELETE /follow/:user_id`（需登录）。两个接口都幂等，重复点击不会报错；
   响应里的 `relation` 是操作后的最新关系，直接用它更新按钮。计数需要刷新时重新调 stats。
3. **上限**：每人最多关注 2000 人（服务端可配置）。达到上限后再关注返回 400，`message` 里带上限值；取关后可以再关注。
4. 机器人账号可以被关注，接口无差别。
5. **列表页**：`GET /follow/following/:user_id`、`GET /follow/followers/:user_id`（访客也能调），游标分页；每项带 `relation`（**我**与这个人的关系），直接渲染关注按钮。
   进列表前看 stats 的 `list_visible`：为 `false` 说明对方把列表设为私密，数字照常显示但不可点（硬调接口会得到 403）。
6. **个人中心开关**：`GET /user/settings` 读、`PUT /user/settings` 改 `follow_list_visibility`（`0` 公开 / `1` 私密）。一个开关同时控制关注列表和粉丝列表。
   同一接口还有私信权限 `dm_permission`（#240，`0` 所有人 / `1` 仅我关注的人 / `2` 仅互相关注 / `3` 关闭）。
7. **我的互关**：`GET /follow/mutual`（需登录，只能看自己的）。
8. **别处的关注按钮**（搜索结果、评论区等）：拿到一批用户 ID 后调 `POST /follow/relations` 一次取回所有关系，不要逐个调 stats。

---

## 1. 统一约定

- 响应信封 `{code, message, data}`，`code` 与 HTTP 状态码的对应同其他接口：`200` 成功、`201` 参数错误（HTTP 400）、`202` 未登录（401）、`203` 无权限（403）、`204` 不存在（404）、`500` 服务器错误。下文错误表写的是 HTTP 状态码。
- `message` 是英文，前端请按 `code` + 自身文案渲染。
- `:user_id` 是目标用户的 ID（UUID）。

### relation 取值

`relation` 表示**当前登录用户**与目标用户的关系：

| 值 | 含义 | 按钮建议 |
|---|---|---|
| `0` | 未关注（访客、查看自己时也是 `0`） | 关注 |
| `1` | 我关注了对方 | 已关注 |
| `2` | 对方关注了我，我没关注对方 | 回关 |
| `3` | 互相关注 | 互相关注 |

---

## 2. 接口

### 2.1 POST /follow/:user_id — 关注

需登录。幂等：已经关注时同样返回 200，不产生重复数据，计数不变。

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `relation` | int | 操作后的关系：`1`（我关注了对方）或 `3`（互相关注） |

```json
{ "code": 200, "message": "Success", "data": { "relation": 1 } }
```

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid user id` | `:user_id` 不是合法 UUID |
| 400 | `Cannot follow yourself` | 关注自己 |
| 400 | `following limit reached: you can follow at most 2000 users` | 关注数已达上限（数字为当前上限） |
| 401 | `Token not found` | 未登录 |
| 404 | `No such user` | 目标用户不存在、已禁用或已删除 |

### 2.2 DELETE /follow/:user_id — 取关

需登录。幂等：本来没关注时同样返回 200，计数不变。对方已被禁用 / 删除时也可以取关（不返回 404）。

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `relation` | int | 操作后的关系：`0`（未关注）或 `2`（对方仍关注我） |

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid user id` | `:user_id` 不是合法 UUID |
| 400 | `Cannot follow yourself` | 目标是自己 |
| 401 | `Token not found` | 未登录 |

### 2.3 GET /follow/stats/:user_id — 关注数 / 粉丝数 / 关系 / 列表是否可见

访客可读。带登录态时回填 `relation`；访客或查看自己时 `relation = 0`。对方把列表设为私密不影响两个计数和 `relation`。

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `user_id` | string | 目标用户 ID |
| `following_count` | int | 关注数（该用户关注了多少人） |
| `follower_count` | int | 粉丝数（多少人关注了该用户） |
| `relation` | int | 当前登录用户与该用户的关系，取值见 §1 |
| `list_visible` | bool | 该用户的关注 / 粉丝列表对**当前查看者**是否可见。看自己恒为 `true`；对方设为私密时其他人（含访客）为 `false`，此时两个数字不要做成可点击 |

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "user_id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10",
    "following_count": 12,
    "follower_count": 340,
    "relation": 3,
    "list_visible": true
  }
}
```

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid user id` | `:user_id` 不是合法 UUID |
| 404 | `No such user` | 目标用户不存在、已禁用或已删除 |

### 2.4 GET /follow/following/:user_id — 关注列表

`:user_id` 关注的人，按关注时间倒序（最近关注的在前）。访客可读；`:user_id` 把列表设为私密时只有本人能看。

**Query**

| 参数 | 类型 | 说明 |
|---|---|---|
| `cursor` | string | 上一页响应里的 `next_cursor`，**原样回传**（不透明字符串，不要解析或拼接）；不传表示第一页 |
| `size` | int | 页大小，默认 20，最大 50（超过按 50，≤ 0 按 20） |

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `items` | array | 本页列表项，可能为空数组 |
| `items[].user` | object | `id` / `username` / `avatar_url` |
| `items[].follow_time` | string | 关注时间（RFC 3339）。关注列表里是 `:user_id` 关注 TA 的时间 |
| `items[].relation` | int | **当前登录用户**与 TA 的关系（不是 `:user_id` 与 TA 的），取值见 §1；访客恒为 `0`，TA 就是我自己时也是 `0` |
| `has_more` | bool | 是否还有下一页，**以它判断列表是否结束** |
| `next_cursor` | string | 下一页游标；`has_more = false` 时为空串 |

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "items": [
      {
        "user": { "id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b11", "username": "alice", "avatar_url": "https://…/a.png" },
        "follow_time": "2026-11-10T09:12:45.123456+08:00",
        "relation": 3
      }
    ],
    "has_more": true,
    "next_cursor": "eyJ2IjoxLCJsaXN0IjoiZm9sbG93aW5nIiwi…"
  }
}
```

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid user id` | `:user_id` 不是合法 UUID |
| 400 | `Invalid request parameters` | `size` 不是整数 |
| 400 | `Invalid cursor` | 游标损坏，或不是这个用户、这个列表的游标（比如把关注列表的游标用在粉丝列表上） |
| 403 | `This user's following and follower lists are private` | 对方把列表设为私密，且请求者不是本人（含访客） |
| 404 | `No such user` | `:user_id` 不存在、已禁用或已删除 |

### 2.5 GET /follow/followers/:user_id — 粉丝列表

关注 `:user_id` 的人，按对方关注 `:user_id` 的时间倒序。参数、响应结构、错误与 §2.4 完全相同；`follow_time` 是 TA 关注 `:user_id` 的时间。
两个列表各有自己的游标，不能混用。

### 2.6 GET /follow/mutual — 我的互关列表

需登录。当前登录用户的互关列表（我关注了 TA 且 TA 也关注了我），按**我关注 TA** 的时间倒序。没有 `:user_id`：只能看自己的，不受列表隐私开关影响。

Query 与响应结构同 §2.4（`cursor` / `size`；`items` / `has_more` / `next_cursor`）；列表项的 `relation` 都是 `3`，`follow_time` 是我关注 TA 的时间。

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid cursor` / `Invalid request parameters` | 同 §2.4 |
| 401 | `Token not found` | 未登录 |

### 2.7 POST /follow/relations — 批量查询关系

需登录。一次返回当前登录用户与一组用户的关系，用于搜索结果、评论区等位置批量渲染关注按钮。

**请求体**

```json
{ "user_ids": ["019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b11", "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b12"] }
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `user_ids` | string[] | 用户 ID 列表，一次最多 100 个；重复的 ID 会合并。不传或空数组返回空结果 |

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `relations` | object | key 是请求里的每个用户 ID，value 是 `relation`（取值见 §1）。没有关系、用户不存在、或 ID 是我自己，都是 `0` |

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "relations": {
      "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b11": 1,
      "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b12": 0
    }
  }
}
```

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid request parameters` | 请求体不是合法 JSON，或 `user_ids` 不是字符串数组 |
| 400 | `Invalid user id in user_ids` | 数组里有不是 UUID 的值 |
| 400 | `too many user ids: at most 100 per request` | 超过 100 个 |
| 401 | `Token not found` | 未登录 |

### 2.8 GET /user/settings — 读取我的设置

需登录，只返回当前登录用户自己的设置。从未修改过的设置项返回默认值。

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `follow_list_visibility` | int | 关注列表 / 粉丝列表可见性：`0` 公开（默认），`1` 私密 |
| `dm_permission` | int | 谁可以给我发私信（#240）：`0` 所有人（默认）/ `1` 仅我关注的人 / `2` 仅互相关注 / `3` 关闭。规则见 [message-api.md](message-api.md) §2.1 |

```json
{ "code": 200, "message": "Success", "data": { "follow_list_visibility": 0, "dm_permission": 0 } }
```

### 2.9 PUT /user/settings — 修改我的设置

需登录，只能改自己的。部分更新：只改请求体里出现的设置项，响应是修改后的全部设置（结构同 §2.8）。

**请求体**

```json
{ "follow_list_visibility": 1 }
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `follow_list_visibility` | int | `0` 公开 / `1` 私密。一个开关同时控制关注列表和粉丝列表 |
| `dm_permission` | int | 谁可以给我发私信（#240）：`0` 所有人 / `1` 仅我关注的人 / `2` 仅互相关注 / `3` 关闭 |

两项可以一起传；任一项非法时整个请求 400，什么都不改。

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `follow_list_visibility must be 0 (public) or 1 (private)` | 取值不是 0 / 1 |
| 400 | `dm_permission must be 0 (everyone), 1 (people I follow), 2 (mutual follows) or 3 (no one)` | 取值不是 0–3 |
| 400 | `At least one setting must be provided` | 请求体里没有任何设置项（或值为 `null`） |
| 400 | `Invalid request parameters` | 请求体不是合法 JSON，或字段类型不对 |
| 401 | `Token not found` | 未登录 |

---

## 3. 说明

- 两个计数对所有人公开；本期没有互关数。
- 取关后再关注，关注时间以最近一次为准，会重新排到列表最前。
- 关注数包含之后被禁用 / 删除的账号（仍占名额，可以取关）。
- 关注接口目前没有频率限制。
- **列表与计数可能有少量差异**：列表不返回已禁用 / 已删除的用户，但计数不因此调整，所以列表总人数可能略少于关注数 / 粉丝数。
- **一页可能不足 `size` 条**（被过滤掉的用户所在的页，极端情况下可能是空页）：是否还有下一页只看 `has_more`，不要用「本页条数 < size」判断结束。
- 列表隐私开关修改后立即生效。设为私密只隐藏两个列表的名单：关注数、粉丝数、`relation`、批量关系查询都不受影响；本人始终能看自己的列表。
- 翻页过程中列表发生变化（有人取关 / 新关注）不会出现重复项；新关注的人排在最前，需要刷新（不带 `cursor` 重新请求）才能看到。
- 关注后，对方（含 AI 机器人账号）发的帖子会出现在首页 `GET /post/home?tab=following`，与已加入圈子的帖子合并显示（#220，见 [home-feed-api.md](home-feed-api.md) 第 3 节）；取关后重新从第一页请求即不再出现。
- 关注成功（关系从「未关注」变为「已关注」）后，对方会在消息中心收到「XX 关注了你」（`notice_type = 12`，约 5 秒延迟，#219）；
  重复关注不会再通知，取关不通知也不撤回，取关后再关注复用同一条并重新变成未读。前端对接见 [notice-frontend-api.md](../notice-frontend-api.md)。
