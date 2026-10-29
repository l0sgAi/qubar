# 热点页面 API 对接文档

> 对应后端领域：`trending`（跨域编排器，聚合圈子 / 帖子 / 用户三类热点榜单）。
> 设计背景见 [trending-design.md](trending-design.md)。
> 本文档供前端对接「热点」页面使用。

---

## 0. 需求背景

### 0.1 这是什么

「热点」页面聚合 qubar 系统中**近期热门**的圈子、帖子和用户，让用户一眼看到当下社区里正在升温的内容。
对应一个独立的「热点」Tab / 页面（区别于首页信息流的个性化推荐）。

### 0.2 三类榜单

| 榜单 | 单位 | 排序信号（窗口内） |
|---|---|---|
| 热门帖子 | 单篇帖子 | 该帖自身的 **`hot`**（互动事件加权累积热度） |
| 热门圈子 | 圈子 | 窗口内该圈子所发帖子的 **`Σ hot`**（热度之和） |
| 热门用户 | 用户 | 窗口内该用户所发帖子的 **`Σ hot`**（热度之和） |

> 三类榜单都以 `hot` 为统一热度尺度，差异只在「帖子本身的热」vs「聚合到圈/人的热」。

### 0.3 两个时间窗

- **`24h`**：近 24 小时（日榜，主打"当下最热"）
- **`7d`**：近 7 天（周榜，主打"这段时间持续热"）

前端通常用 Tab/切换器在 24h 与 7d 之间切换。

### 0.4 数据新鲜度（重要）

榜单**不是实时**的，由后台定时任务（默认 **5 分钟**刷新一次）预计算后写入缓存。
因此：

- 响应里有 `refreshed_at` 字段（Unix 秒），表示榜单**最近一次刷新时间**。
- 前端可据此显示「X 分钟前更新」。
- 后台任务对 ES 聚合，ES 数据本身又有分钟级 CDC 延迟 + 热度累积延迟，**端到端最迟约十几分钟**生效属于正常。
- **降级**：若 ES 临时不可用，后台任务本轮跳过、保留上一轮榜单；读路径仍正常返回旧榜单 + 旧的 `refreshed_at`。前端看到 `refreshed_at` 较久未更新即代表榜单可能滞后，无需特殊处理。

---

## 1. 端点

```
GET /trending
```

**鉴权**：需要登录。请求头携带 `satoken: <token>`（sa-token）。未登录或 token 失效 → `401`。

---

## 2. Query 参数

| 参数 | 类型 | 必填 | 默认 | 取值 / 上限 | 说明 |
|---|---|---|---|---|---|
| `window` | string | 否 | `24h` | `24h` \| `7d` | 时间窗；非法值回落 `24h` |
| `section` | string | 否 | `all` | `all` \| `posts` \| `circles` \| `users` | 板块；`all`=三类同时返回 |
| `size` | int | 否 | `20` | `1` ~ `50`（超出回落 `20`） | 每个板块返回的条数 |
| `offset` | int | 否 | `0` | `>= 0` | 单板块翻页偏移；`section=all` 时**忽略** |

### 2.1 `section` 用法

- **`all`（默认，首屏聚合）**：一次请求同时拿到 `posts` + `circles` + `users` 三个板块，各 `size` 条。**此时 `offset` 被忽略**（首屏不分页）。用于热点页第一次进入。
- **`posts` / `circles` / `users`（单板块，翻页/查看完整榜单）**：只返回对应板块；配合 `offset` 翻页。

---

## 3. 响应结构

### 3.1 标准响应壳（与全站一致）

```jsonc
{
  "code": 200,            // 业务码：200=成功
  "message": "Success",
  "data": {                // TrendingBoard
    "window": "24h",
    "posts":   [ /* TrendingPostItem[] */ ],
    "circles": [ /* TrendingCircleItem[] */ ],
    "users":   [ /* TrendingUserItem[]   */ ],
    "refreshed_at": 1735660800,
    "truncated":   false,
    "offset":      0,
    "size":        20
  }
}
```

### 3.2 TrendingBoard 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `window` | string | 实际生效的时间窗（`"24h"` / `"7d"`） |
| `posts` | `TrendingPostItem[]` | 热门帖子；按热度降序。`section=all` 或 `posts` 时填充，否则不返回该字段（`omitempty`） |
| `circles` | `TrendingCircleItem[]` | 热门圈子；按热度降序。同上 |
| `users` | `TrendingUserItem[]` | 热门用户；按热度降序。同上 |
| `refreshed_at` | int64 | 榜单最近刷新时间（Unix 秒）；`0` = 从未刷新（冷启动无数据） |
| `truncated` | bool | 是否触达榜单容量上限（默认保留 Top-100）；`true` 时提示"已显示全部" |
| `offset` | int | 回显的偏移（仅单板块 `section` 时有意义；`all` 时为 `0`） |
| `size` | int | 本次每板块实际返回条数上限 |

> 单个板块若为空，对应数组为 `[]`（不会是 `null`）。
> `posts`/`circles`/`users` 字段在不相关的 `section` 下会**省略**（`omitempty`）——前端取值前判空。

### 3.3 TrendingPostItem（热门帖子项）

镜像首页信息流帖子项 + `hot_score`。**字段与首页信息流的帖子项完全一致**（便于复用同一个帖子卡片组件）。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string(uuid) | 帖子 ID |
| `circle_id` | string(uuid) | 所属圈子 ID |
| `user_id` | string(uuid) | 作者 ID |
| `type` | int | 帖子类型（1 图文 / 2 视频 / 3 投票） |
| `title` | string | 标题 |
| `summary` | string | 摘要 |
| `content` | string | 正文 |
| `view_count` | int | 浏览数 |
| `comment_count` | int | 评论数 |
| `like_count` | int | 点赞数 |
| `collect_count` | int | 收藏数 |
| `is_pinned` | int | 置顶（0/1） |
| `is_essence` | int | 精华（0/1） |
| `is_lock` | int | 锁定（0/1） |
| `status` | int | 状态（1 已发布） |
| `create_time` | string(ISO8601) | 发帖时间 |
| `author_name` | string | 作者昵称 |
| `author_avatar` | string | 作者头像 |
| `circle_name` | string | 圈子名 |
| `circle_avatar` | string | 圈子头像 |
| `images` | string[] | 帖子图片 URL 列表 |
| `is_liked` | bool | **当前用户**是否已点赞 |
| `is_collected` | bool | **当前用户**是否已收藏 |
| `hot_score` | float | ★ 窗口内热度分（帖子榜 = 该帖 `hot`）；用于前端展示热度或排序依据 |

### 3.4 TrendingCircleItem（热门圈子项）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string(uuid) | 圈子 ID |
| `name` | string | 圈子名 |
| `avatar_url` | string | 圈子头像（可空，省略） |
| `description` | string | 简介（可空，省略） |
| `category_id` | string(uuid) | 分类 ID（可空，省略） |
| `member_count` | int | 成员数 |
| `post_count` | int | 累积帖子数 |
| `hot` | int | 累积热度（终身，参考） |
| `join_type` | int | 加入方式（0 公开 / 2 私密；1 审核已下线，#168） |
| `create_time` | string | 建圈时间（`"2006-01-02 15:04:05"`） |
| `hot_score` | float | ★ 窗口内 `Σ hot`（真正的趋势信号，建议用它做热度展示） |

### 3.5 TrendingUserItem（热门用户项）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string(uuid) | 用户 ID |
| `username` | string | 昵称 |
| `avatar_url` | string | 头像（可空，省略） |
| `hot_score` | float | ★ 窗口内 `Σ hot` |

---

## 4. 翻页流程

### 4.1 首屏（`section=all`）

```http
GET /trending?window=24h&section=all&size=20
```

一次返回三类各 20 条。**首屏不分页**（`offset` 被忽略）。

### 4.2 查看某板块完整榜单（单板块 + offset 翻页）

```http
# 帖子榜第 1 页
GET /trending?window=24h&section=posts&size=20&offset=0

# 帖子榜第 2 页
GET /trending?window=24h&section=posts&size=20&offset=20
```

**判断是否还有更多**：当本页返回的数组长度 `< size` 时，无更多。
当 `truncated: true` 出现时，表示已到榜单容量上限（默认 Top-100），不应继续翻页。

### 4.3 ⚠️ 翻页排名漂移（趋势榜通性）

榜单是基于热度的**实时排名**，后台每隔几分钟重算。因此用户翻到第 2 页时，第 1 页与第 2 页之间可能有条目重复或跳过（因为排名变化了）。

- 建议**前端默认只展示前 1~2 页**（热度榜用户的实际浏览深度本就很浅）。
- 若需要"查看更多"，告知用户榜单会动态变化，属正常现象。

---

## 5. 完整示例

### 5.1 首屏聚合（`all`）

```http
GET /trending?window=24h&section=all&size=3
```

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "window": "24h",
    "posts": [
      {
        "id": "019058b0-aaaa-7000-8000-000000000001",
        "circle_id": "019058b0-cccc-7000-8000-000000000010",
        "user_id": "019058b0-bbbb-7000-8000-000000000020",
        "type": 1,
        "title": "今天发现一个好玩的圈子",
        "summary": "...",
        "content": "...",
        "view_count": 1234, "comment_count": 56, "like_count": 78, "collect_count": 12,
        "is_pinned": 0, "is_essence": 1, "is_lock": 0, "status": 1,
        "create_time": "2026-06-30T10:00:00Z",
        "author_name": "alice", "author_avatar": "https://.../a.png",
        "circle_name": "趣玩", "circle_avatar": "https://.../c.png",
        "images": ["https://.../1.jpg"],
        "is_liked": true, "is_collected": false,
        "hot_score": 820.0
      }
    ],
    "circles": [
      {
        "id": "019058b0-cccc-7000-8000-000000000010",
        "name": "趣玩",
        "avatar_url": "https://.../c.png",
        "description": "分享有趣的事物",
        "category_id": "019058b0-dddd-7000-8000-000000000030",
        "member_count": 5210, "post_count": 980, "hot": 23300,
        "join_type": 0,
        "create_time": "2026-06-01 09:00:00",
        "hot_score": 15200.0
      }
    ],
    "users": [
      {
        "id": "019058b0-bbbb-7000-8000-000000000020",
        "username": "alice",
        "avatar_url": "https://.../a.png",
        "hot_score": 4300.0
      }
    ],
    "refreshed_at": 1735660800,
    "truncated": false,
    "offset": 0,
    "size": 3
  }
}
```

### 5.2 单板块翻页（`posts`）

```http
GET /trending?window=7d&section=posts&size=20&offset=20
```

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "window": "7d",
    "posts": [ /* TrendingPostItem[] 第 2 页，最多 20 条 */ ],
    "refreshed_at": 1735660500,
    "offset": 20,
    "size": 20
  }
}
```

> 注意：单板块请求时，未请求的板块（`circles`/`users`）字段**不出现**（`omitempty`）。

### 5.3 冷启动（无数据）

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "window": "24h",
    "posts": [],
    "circles": [],
    "users": [],
    "refreshed_at": 0,        // 0 = 从未刷新
    "offset": 0,
    "size": 20
  }
}
```

### 5.4 未登录

```jsonc
{
  "code": 201,              // CodeUnauthorized
  "message": "Token not found"
}
// HTTP 401
```

---

## 6. 错误码

| 业务码 `code` | HTTP | 含义 | 触发场景 |
|---|---|---|---|
| `200` | 200 | 成功 | 正常返回 |
| `201` | 401 | 未认证 | 缺少 / 无效 `satoken`，或 userID 解析失败 |
| `500`（内部错误码） | 500 | 服务内部错误 | Redis 异常等罕见情况（读路径已尽量降级，一般不会触发） |

> 业务码与 HTTP 状态的关系：业务码从 `200` 起，`201` 是**业务码**（对应 HTTP 401），不要与 HTTP 201 混淆。
> 前端统一按响应壳的 `code` 字段判断业务结果即可。

---

## 7. 边界与注意事项

### 7.1 榜单可能"断层"
榜单中的实体（帖子/圈子/用户）如果已被删除或禁用，会**从结果中跳过**。因此实际返回条数可能**少于** `size`，且排名序号不连续——这属于正常，不是 bug。

### 7.2 `hot_score` 单位与展示
- `hot_score` 是后端热度系统的内部累积分，**没有固定单位**（不要硬编码"X 赞=1 分"之类换算）。
- 前端建议：要么只用来**排序**（不展示数值），要么做相对展示（如"🔥 热度 820"），不要让用户以为是点赞数。
- 同一榜单内 `hot_score` 越大越靠前。

### 7.3 时间窗切换无额外开销
`24h` 与 `7d` 是两套独立预计算的榜单，切换只是不同查询参数，**不触发后端重算**，响应很快（直接读 Redis ZSET）。

### 7.4 不要频繁轮询
榜单每 5 分钟才更新一次。前端无需短轮询；下拉刷新即可。`refreshed_at` 可用于"上次更新时间"展示，避免给用户"数据是实时的"错觉。

### 7.5 `is_liked` / `is_collected` 仅帖子项有
圈子项和用户项不涉及当前用户的交互态。帖子项的两个布尔值反映**当前登录用户**对该帖的点赞/收藏状态，可直接复用首页帖卡的同款交互逻辑。

### 7.6 与现有接口的关系
- 帖子卡片可直接复用**首页信息流**（`GET /post/home`）的帖子组件——字段完全一致，只是多了 `hot_score`。
- 圈子项可复用**「近期活跃圈子」**（`GET /circle/active`）的圈子卡片，字段高度一致（多了 `hot_score`，活跃圈子里的 `recent_post_count` 在此为 `hot` 累积值）。
- 点击跳转：帖子 → 帖子详情；圈子 → 圈子详情；用户 → 用户主页（走各自已有的 detail 接口）。

---

## 8. 字段速查（复制即用）

**TrendingBoard**：`window` `posts[]` `circles[]` `users[]` `refreshed_at` `truncated` `offset` `size`

**TrendingPostItem**：`id` `circle_id` `user_id` `type` `title` `summary` `content` `view_count` `comment_count` `like_count` `collect_count` `is_pinned` `is_essence` `is_lock` `status` `create_time` `author_name` `author_avatar` `circle_name` `circle_avatar` `images[]` `is_liked` `is_collected` `hot_score`

**TrendingCircleItem**：`id` `name` `avatar_url` `description` `category_id` `member_count` `post_count` `hot` `join_type` `create_time` `hot_score`

**TrendingUserItem**：`id` `username` `avatar_url` `hot_score`
