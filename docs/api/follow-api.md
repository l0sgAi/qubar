# 用户关注 API 对接文档（前端）

> **功能**：用户间关注 / 取关，用户主页展示关注数、粉丝数和「我与对方」的关系状态。
> **对应实现**：`pkg/domains/follow/`（`application/relation.go`、`application/stats.go`、`interfaces/http/handler.go`），issue #217。设计见 [follow-design.md](../follow-design.md)。
> **状态**：已交付关注 / 取关 / 统计三个接口与被关注通知（#219）。关注列表、粉丝列表、首页关注 tab 在后续 issue（#218 / #220）。

---

## 0. TL;DR — 前端要做的事

1. **用户主页**：进页面调 `GET /follow/stats/:user_id`（访客也能调），拿 `following_count`、`follower_count` 和 `relation`，按 `relation` 渲染关注按钮。
2. **关注 / 取关**：`POST /follow/:user_id`、`DELETE /follow/:user_id`（需登录）。两个接口都幂等，重复点击不会报错；
   响应里的 `relation` 是操作后的最新关系，直接用它更新按钮。计数需要刷新时重新调 stats。
3. **上限**：每人最多关注 2000 人（服务端可配置）。达到上限后再关注返回 400，`message` 里带上限值；取关后可以再关注。
4. 机器人账号可以被关注，接口无差别。

---

## 1. 统一约定

- 响应信封 `{code, message, data}`，`code` 与 HTTP 状态码的对应同其他接口：`200` 成功、`201` 参数错误（HTTP 400）、`202` 未登录（401）、`204` 不存在（404）、`500` 服务器错误。下文错误表写的是 HTTP 状态码。
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

### 2.3 GET /follow/stats/:user_id — 关注数 / 粉丝数 / 关系

访客可读。带登录态时回填 `relation`；访客或查看自己时 `relation = 0`。

**响应 `data`**

| 字段 | 类型 | 说明 |
|---|---|---|
| `user_id` | string | 目标用户 ID |
| `following_count` | int | 关注数（该用户关注了多少人） |
| `follower_count` | int | 粉丝数（多少人关注了该用户） |
| `relation` | int | 当前登录用户与该用户的关系，取值见 §1 |

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "user_id": "019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10",
    "following_count": 12,
    "follower_count": 340,
    "relation": 3
  }
}
```

**错误**

| HTTP | message | 场景 |
|---|---|---|
| 400 | `Invalid user id` | `:user_id` 不是合法 UUID |
| 404 | `No such user` | 目标用户不存在、已禁用或已删除 |

---

## 3. 说明

- 两个计数对所有人公开；本期没有互关数。
- 取关后再关注，关注时间以最近一次为准（后续列表按它倒序）。
- 关注数包含之后被禁用 / 删除的账号（仍占名额，可以取关）。
- 关注接口目前没有频率限制。
- 关注成功（关系从「未关注」变为「已关注」）后，对方会在消息中心收到「XX 关注了你」（`notice_type = 12`，约 5 秒延迟）；
  重复关注不会再通知，取关不通知也不撤回，取关后再关注复用同一条并重新变成未读。前端对接见 `docs/notice-frontend-api.md`。
