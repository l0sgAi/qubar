# 随机圈子列表 API 对接文档（前端）

> **功能**：侧栏随机推荐圈子，每次请求返回一批随机圈子（默认 20 个，每次结果不同）。
> **对应实现**：`pkg/domains/circle/`，接口 `GET /circle/random`。
> **状态**：已交付（2026-08-31）。

---

## 0. TL;DR — 前端三件事

1. **无需登录**：访客可读，不用带 token；带了也不影响。
2. **无分页**：每次请求就是一整批随机结果，想"换一批"就再调一次。
3. **字段与 `/circle/active` 完全一致**：可直接复用活跃圈子的卡片组件；`recent_post_count` 恒为 0，忽略即可。

---

## 1. 接口

### GET /circle/random

随机返回一批正常状态的圈子（已过滤：删除、非正常状态、私密圈子）。

**Query 参数**

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `size` | int | 否 | 20 | 返回数量。`<=0` 或 `>100` 时回落 20 |

**请求示例**

```
GET /circle/random
GET /circle/random?size=5
```

**响应**

统一信封 `{code, message, data}`，成功时 `data`：

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "circles": [
      {
        "id": "0190f1a2-…",
        "name": "Golang 交流",
        "avatar_url": "https://…/avatar.png",
        "description": "讨论 Go 语言的一切",
        "category_id": "0190e…",
        "member_count": 128,
        "post_count": 456,
        "hot": 789,
        "recent_post_count": 0,
        "join_type": 0,
        "create_time": "2026-08-01T10:00:00Z"
      }
    ],
    "total": 87,
    "size": 20,
    "offset": 0
  }
}
```

**`circles[]` 字段**

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | 圈子 ID（UUID） |
| `name` | string | 圈子名 |
| `avatar_url` | string | 头像，可能缺省（`omitempty`） |
| `description` | string | 简介，可能缺省 |
| `category_id` | string | 分类 ID，可能缺省（无分类时不返回该字段） |
| `member_count` | int | 成员数 |
| `post_count` | int | 累积帖子数 |
| `hot` | int | 累积热度 |
| `recent_post_count` | int | 恒为 0（本接口无此语义，仅为对齐 active 格式） |
| `join_type` | int | 加入方式：0=公开（直接加入）。私密圈子（2）已被过滤；1（需审核）已下线（#168） |
| `create_time` | string | 创建时间（ISO 8601） |

**外层字段**

| 字段 | 类型 | 说明 |
|---|---|---|
| `total` | int | 符合条件的圈子总数（全站口径，非本批数量） |
| `size` | int | 本次请求的 size |
| `offset` | int | 恒为 0（无分页） |

**错误**

| 场景 | HTTP | 说明 |
|---|---|---|
| 服务内部错误（如 ES 不可用） | 500 | `Failed to list random circles` |

本接口无 400/401/403 场景（参数有兜底、无需登录）。

---

## 2. 前端建议

- **展示数量**：侧栏一般展示前 5~10 条即可，但接口一次给 20，可本地截取，避免多次请求。
- **"换一批"**：重新调用本接口即可（后端随机排序，无 seed，每次结果不同）。
- **缓存**：可适当前端缓存（如切页面前不重复拉取），随机结果允许短暂过期。
- **空列表**：圈子总数为 0 时 `circles` 为空数组（不是 null），前端做空态处理。
- **加入**：列表只含公开圈（`join_type=0`），点"加入"直接成为成员（入圈审核已下线，#168）。

---

## 3. 与 /circle/active 的关系

| | `/circle/random` | `/circle/active` |
|---|---|---|
| 语义 | 随机推荐（侧栏） | 近 7 天发帖数排行 |
| 排序 | 随机，每次不同 | 近期发帖数降序 |
| 分页 | 无 | `offset` 分页 |
| `recent_post_count` | 恒 0 | 窗口内发帖数 |
| 响应结构 | **完全相同** | 完全相同 |

两接口字段结构一致，前端可共用同一 TypeScript 类型 / 组件。
