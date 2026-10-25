# 加入圈子列表 API 对接文档（前端）

> **功能**：个人中心「我加入的圈子」与他人主页「TA 加入的圈子」列表，支持关键字搜索、按角色筛选。
> **对应实现**：`pkg/domains/circle/`，接口 `GET /circle/my`、`GET /circle/user`。
> **状态**：slug / 角色 / 成员状态 / 角色筛选于 2026-10-25 交付（#157）。

---

## 0. TL;DR — 前端要改的

1. 列表项新增 `slug`、`role`；`/circle/my` 额外有 `member_status`、`mute_end_time`（他人列表**不会**出现这两个字段）。
2. 新增可选参数 `role`：`30` 圈主 / `20` 管理员 / `10` 成员，不传或 `0` 为全部，其他值 400。单选。
3. 切换 `role` 或 `keyword` 时**丢弃旧的 `search_after` 从第一页开始**：不同筛选条件的游标不能混用，混用返回 400。

---

## 1. 接口

### GET /circle/my —— 我加入的圈子（需登录）

### GET /circle/user —— 指定用户加入的圈子（访客可读）

两个接口参数与返回结构相同，区别：`/circle/user` 需要 `user_id`，且列表项不含 `member_status` / `mute_end_time`（禁言属个人隐私）。

**Query 参数**

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `user_id` | string(UUID) | `/circle/user` 必填 | - | 目标用户 ID。`/circle/my` 忽略 |
| `keyword` | string | 否 | 空 | 圈子名称关键字（分词容错 + 子串包含） |
| `role` | int | 否 | 0 | 角色筛选：`30` 圈主 / `20` 管理员 / `10` 成员；`0` 或不传 = 全部。其他值（含非数字）400 |
| `size` | int | 否 | 20 | 每页数量。`<=0` 或 `>100` 时回落 20 |
| `search_after` | string | 否 | 空 | 上一页返回的 `search_after`，原样回传；首页不传 |

可以组合：`keyword` + `role` 同时生效。

**请求示例**

```
GET /circle/my?role=20&size=20
GET /circle/my?keyword=摄影&role=10&search_after=eyJrIjoicm9sZSIs...
GET /circle/user?user_id=0192a0d0-0000-7000-8000-000000000001&role=30
```

**响应**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "circles": [
      {
        "id": "0192a0d0-0000-7000-8000-0000000000aa",
        "name": "摄影爱好者",
        "slug": "photo",
        "avatar_url": "https://...",
        "member_count": 128,
        "role": 20,
        "member_status": 2,
        "mute_end_time": "2026-10-26T10:00:00+08:00"
      },
      {
        "id": "0192a0d0-0000-7000-8000-0000000000bb",
        "name": "跑步",
        "member_count": 56,
        "role": 10,
        "member_status": 1
      }
    ],
    "total": 2,
    "size": 20,
    "search_after": ""
  }
}
```

**列表项字段**

| 字段 | 类型 | 出现在 | 说明 |
|---|---|---|---|
| `id` | string | 两者 | 圈子 ID |
| `name` | string | 两者 | 圈子名称 |
| `slug` | string | 两者 | 圈子短链标识；圈子没有 slug 时字段省略（与圈子详情一致） |
| `avatar_url` | string | 两者 | 头像，无则省略 |
| `member_count` | int | 两者 | 成员数 |
| `role` | int | 两者 | 该用户在圈内的角色：`30` 圈主 / `20` 管理员 / `10` 成员 |
| `member_status` | int | 仅 `/circle/my` | 我的成员状态：`1` 正常 / `2` 禁言 |
| `mute_end_time` | string(RFC3339) | 仅 `/circle/my` | 禁言截止时间，**仅禁言生效中返回**。禁言已过期按正常返回（`member_status=1`，无此字段） |

**分页字段**

| 字段 | 说明 |
|---|---|
| `total` | 无 `keyword` 时为总数（带 `role` 时是该角色的圈子数）；有 `keyword` 时恒为 `0`（未知），按 `search_after` 是否为空判断有无下一页 |
| `search_after` | 下一页游标；空串表示没有更多 |
| `truncated` | 仅 `keyword` 模式可能出现且为 `true`（否则省略）：单次请求扫描到上限仍未凑满一页，可提示用户细化关键字；继续翻页仍可取到后续结果 |

---

## 2. 行为说明

- **列表口径**：「已加入」= 正常 + 禁言。被拉黑 / 已退出 / 申请中的圈子不出现；圈子本身被封禁、待审核或已删除时也不出现。
  因此单页条数可能小于 `size`，以 `search_after` 是否为空判断是否还有下一页。
- **排序**：按加入时间倒序（最近加入在前）分页；带 `role` 时同一页内也按加入时间倒序。
  带 `keyword` 且不带 `role` 时，同一页内按相关度排序（与改动前一致）。
- **游标不能跨条件复用**：`role` 改变（包括在「全部」与某个角色之间切换）后，必须不带 `search_after` 重新请求第一页。
  带着旧游标会返回 400 `Invalid search_after parameter`。
- **禁言状态**以截止时间为准：`mute_end_time` 一过即视为正常，与圈内发帖的禁言校验一致。
- **实时性**：角色、成员状态直查数据库，任免 / 禁言后立即可见；圈子名称、头像、slug、成员数来自搜索索引，可能有秒级延迟。

---

## 3. 错误

| HTTP / code | message | 场景 |
|---|---|---|
| 400 | `Invalid request parameters` | 参数类型错误（如 `role=abc`、`size=abc`） |
| 400 | `role must be 30 (owner), 20 (admin), 10 (member) or 0 (all)` | `role` 取值不在 0 / 10 / 20 / 30 |
| 400 | `Invalid search_after parameter` | 游标非法，或游标与当前 `role` 不匹配 |
| 400 | `Invalid user_id` | `/circle/user` 的 `user_id` 不是 UUID |
| 401 | - | `/circle/my` 未登录 |

---

## 4. 前端建议

- 角色筛选用单选 Tab：全部 / 圈主 / 管理 / 成员，对应 `role` = `0` / `30` / `20` / `10`；切 Tab 时清空列表与游标。
- 角色徽标直接用 `role`；`/circle/my` 中 `member_status=2` 时可显示「禁言至 {mute_end_time}」。
- 他人主页只展示角色，不要尝试展示禁言状态（接口不返回）。
