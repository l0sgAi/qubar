# 发现页面 API 对接文档

> 对应后端领域：`discover`（跨域编排器，随机推送圈子 / 帖子两个分区）。
> 设计背景见 [discover-design.md](discover-design.md)。
> 本文档供前端对接「发现」页面使用。

---

## 0. 需求背景

### 0.1 这是什么

「发现」页面帮用户**探索兴趣边界**：随机推送一些**发散性**的圈子、帖子内容，让用户跳出已有的信息气泡，
看到自己平时不会接触到的新内容。区别于：

- **首页信息流**（`GET /post/home`）：个性化推荐，推你爱看的（**收敛**）
- **热点页**（`GET /trending`）：全局热门榜，推大家都在看的（**收敛**）
- **发现页**（`GET /discover`）：随机推送，推你**没看过**的（**发散**）

### 0.2 核心机制：反气泡随机池

发现页的内容来自一个**预计算的随机候选池**（不是实时查询）：

1. 后台定时任务（默认 **10 分钟**刷新一次）对帖子 / 圈子做 **`random_score` 随机采样**
2. **登录用户**：排除「已加入的圈子」+「已点赞 / 收藏 / 浏览过的帖子」（反气泡），只推气泡外的陌生内容
3. **匿名用户**：纯随机（无排除），常用于新用户落地页场景
4. 采样结果写入 Redis 候选池，读路径分页读取

因此发现页的内容：

- **不是实时随机的**，每次刷新周期（约 10 分钟）内，同一用户翻到的是同一批「随机结果」
- 后台任务每 10 分钟重建一次候选池，相当于**「换一批」**——用户感觉内容在更新
- 登录态下看到的内容**刻意排除**了你已加入的圈子，鼓励探索新圈子

### 0.3 两个分区

发现页返回 **圈子** + **帖子** 两个独立分区（不包含用户榜，用户榜在热点页）：

| 分区 | 内容 | 随机来源 |
|---|---|---|
| `circles` | 随机圈子 | 对圈子 ES 索引随机采样（排除已加入圈子） |
| `posts` | 随机帖子 | 对帖子 ES 索引随机采样（排除已加入圈子的帖 + 已交互帖） |

前端通常用两个区域布局：上方圈子卡片流，下方帖子信息流（或 Tab 切换）。

### 0.4 与推荐 / 热点的区别（重要）

| 维度 | 首页推荐（`/post/home`） | 热点榜（`/trending`） | **发现页（`/discover`）** |
|---|---|---|---|
| 目标 | 推你爱看 | 推大家都在看 | **推你没看过** |
| 信号 | 热度 + CF 相似（收敛） | 热度（收敛） | **纯随机 + 反气泡排除（发散）** |
| 个性化 | 兴趣召回 | 无 | 反气泡排除（只排除，不偏向） |
| 数据形态 | 帖子（单一） | 圈子 + 帖子 + 用户 | **圈子 + 帖子**（两分区） |
| 匿名 | ❌ 禁（401） | ❌ 禁（401） | **✅ 允许（纯随机退化）** |
| 刷新感 | 召回池 30min | 榜单 5min | 候选池 10min「换一批」 |

---

## 1. 端点

```
GET /discover
```

**鉴权**：**可选登录**（与推荐/热点不同）。
- 登录用户：请求头携带 `satoken: <token>`（sa-token）→ 反气泡个性化（推气泡外内容）
- 匿名用户：不带 token 或 token 失效 → **不返回 401**，而是返回纯随机内容（适合新用户落地页）
- 坏 token 也会被静默当作匿名处理，不会 401

---

## 2. Query 参数

| 参数 | 类型 | 必填 | 默认 | 取值 / 上限 | 说明 |
|---|---|---|---|---|---|
| `section` | string | 否 | `all` | `all` \| `posts` \| `circles` | 分区；`all`=两分区同时返回 |
| `size` | int | 否 | `20` | `1` ~ `50`（超出回落 `20`） | 每个分区返回的条数 |
| `offset` | int | 否 | `0` | `>= 0` | 单分区翻页偏移；`section=all` 时**忽略**（首屏不分页） |
| `pool_token` | string | 否 | — | 上一页响应返回的 token | 候选池版本 token；不匹配→池已重建→回 `offset=0`（见 §4.2） |

### 2.1 `section` 用法

- **`all`（默认，首屏聚合）**：一次请求同时拿到 `circles` + `posts` 两个分区，各 `size` 条。**此时 `offset` 被忽略**（首屏不分页）。用于发现页第一次进入。
- **`circles` / `posts`（单分区，翻页/查看更多）**：只返回对应分区；配合 `offset` 翻页。

### 2.2 `pool_token` 是什么

候选池每 10 分钟被后台任务重建一次。`pool_token` 是池的「版本号」：

- 首次请求**不传** `pool_token` → 后端返回当前池的 token，前端保存
- 翻页（下一页）时**回传**上一次的 `pool_token`
- 如果在翻页期间后台恰好重建了池，token 会**不匹配** → 后端返回新的 `pool_token` + `pool_refreshed: true` + `offset: 0`，提示前端「池已换新，从头开始」

---

## 3. 响应结构

### 3.1 标准响应壳（与全站一致）

```jsonc
{
  "code": 200,            // 业务码：200=成功
  "message": "Success",
  "data": {                // DiscoverBoard
    "circles":     [ /* DiscoverCircleItem[] */ ],
    "posts":       [ /* DiscoverPostItem[]   */ ],
    "pool_token":  "a1b2c3d4-...",
    "has_more":    true,
    "pool_refreshed": false,
    "offset":      0,
    "size":        20
  }
}
```

### 3.2 DiscoverBoard 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `circles` | `DiscoverCircleItem[]` | 随机圈子；按随机序。`section=all` 或 `circles` 时填充，否则省略（`omitempty`） |
| `posts` | `DiscoverPostItem[]` | 随机帖子；按随机序。同上 |
| `pool_token` | string | 当前候选池版本 token；翻页时回传此值。池为空或重建失败时可能省略 |
| `has_more` | bool | 是否还有更多（基于池总长度与当前 offset+size 比较） |
| `pool_refreshed` | bool | `true` 表示本次请求时池已重建（token 不匹配或池过期），本次 `offset` 已重置为 `0` |
| `offset` | int | 回显的偏移（仅单分区 `section` 时有意义；`all` 时为 `0`） |
| `size` | int | 本次每分区实际返回条数上限 |

> 单个分区若为空，对应数组为 `[]`（不会是 `null`）。
> `circles`/`posts` 字段在不相关的 `section` 下会**省略**（`omitempty`）——前端取值前判空。

### 3.3 DiscoverPostItem（随机帖子项）

**字段与首页信息流帖子项完全一致**（便于复用同一个帖子卡片组件）。不含热度分（发现页是随机序，无热度概念）。

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
| `is_liked` | bool | **当前用户**是否已点赞（匿名恒为 `false`） |
| `is_collected` | bool | **当前用户**是否已收藏（匿名恒为 `false`） |

### 3.4 DiscoverCircleItem（随机圈子项）

字段与热点圈子项一致，但**不含 `hot_score`**（发现页是随机序，无热度排序）。

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
| `join_type` | int | 加入方式（0 公开 / 2 私密；发现页已过滤私圈，故不会出现 2；1 审核已下线，#168） |
| `create_time` | string | 建圈时间（`"2006-01-02 15:04:05"`） |

---

## 4. 翻页流程

### 4.1 首屏（`section=all`）

```http
GET /discover?section=all&size=20
```

一次返回圈子 + 帖子两分区各 20 条。**首屏不分页**（`offset` 被忽略）。响应里的 `pool_token` 保存下来供后续翻页用。

### 4.2 单分区翻页（`circles` 或 `posts` + offset + pool_token）

```http
# 帖子分区第 1 页（首屏）
GET /discover?section=posts&size=20&offset=0
# 响应 data.pool_token = "abc-123"

# 帖子分区第 2 页（回传上一次的 pool_token）
GET /discover?section=posts&size=20&offset=20&pool_token=abc-123
```

**判断是否还有更多**：看响应 `has_more` 字段（`true`=还有下一页）。

**池已重建的处理**（`pool_refreshed: true`）：
如果翻页时后台恰好重建了候选池（每 10 分钟一次），后端会：
- 返回**新池**的内容（一批新的随机结果）
- `pool_refreshed: true`
- `offset: 0`（从头开始）
- 新的 `pool_token`

前端处理建议：当 `pool_refreshed: true` 时，**重置分页状态**（清空已加载列表，从第 1 页重新展示），并给用户一个轻提示（如「已为你换一批新内容」），或者静默更新。这是发现页的**正常行为**，不是错误。

### 4.3 列表为空

若池为空（极端情况，如系统刚启动 + ES 故障），响应：

```jsonc
{"code":200,"message":"Success","data":{"circles":[],"posts":[],"pool_token":"","has_more":false,"offset":0,"size":20}}
```

前端展示空态即可（如「暂无发现内容，下拉刷新试试」）。

---

## 5. 完整示例

### 5.1 登录用户首屏聚合（`all` + 反气泡）

```http
GET /discover?section=all&size=3
Header: satoken: <已登录用户的token>
```

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "circles": [
      {
        "id": "019058b0-cccc-7000-8000-000000000010",
        "name": "户外探险",
        "avatar_url": "https://.../c.png",
        "description": "徒步、登山、露营爱好者的聚集地",
        "category_id": "019058b0-dddd-7000-8000-000000000030",
        "member_count": 3210,
        "post_count": 540,
        "hot": 8800,
        "join_type": 0,
        "create_time": "2026-05-01 09:00:00"
      }
    ],
    "posts": [
      {
        "id": "019058b0-aaaa-7000-8000-000000000001",
        "circle_id": "019058b0-eeee-7000-8000-000000000040",
        "user_id": "019058b0-bbbb-7000-8000-000000000020",
        "type": 1,
        "title": "分享一个冷门但超棒的露营地",
        "summary": "...",
        "content": "...",
        "view_count": 234, "comment_count": 12, "like_count": 45, "collect_count": 8,
        "is_pinned": 0, "is_essence": 0, "is_lock": 0, "status": 1,
        "create_time": "2026-06-28T08:30:00Z",
        "author_name": "bob", "author_avatar": "https://.../b.png",
        "circle_name": "户外探险", "circle_avatar": "https://.../c.png",
        "images": ["https://.../camp.jpg"],
        "is_liked": false, "is_collected": false
      }
    ],
    "pool_token": "f4e5d6c7-1111-2222-3333-444455556666",
    "has_more": true,
    "pool_refreshed": false,
    "offset": 0,
    "size": 3
  }
}
```

> 注意：登录用户看到的圈子/帖子都是**未加入圈子的内容**（反气泡）。

### 5.2 匿名用户首屏（纯随机）

```http
GET /discover?section=all&size=20
（不带 satoken 头）
```

返回结构与上面一致。区别：
- 所有匿名用户**共享同一个全局随机池**（`discover:anon:*`），看到的是同一批内容
- `is_liked` / `is_collected` **恒为 `false`**（无用户态）
- 内容**无排除**（纯随机，可能包含任意圈子/帖子）

### 5.3 单分区翻页（`posts`，回传 pool_token）

```http
GET /discover?section=posts&size=20&offset=20&pool_token=f4e5d6c7-1111-2222-3333-444455556666
```

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "posts": [ /* DiscoverPostItem[] 第 2 页，最多 20 条 */ ],
    "pool_token": "f4e5d6c7-1111-2222-3333-444455556666",
    "has_more": true,
    "offset": 20,
    "size": 20
  }
}
```

> 单分区请求时，未请求的分区（`circles`）字段**不出现**（`omitempty`）。

### 5.4 池已重建（`pool_refreshed: true`）

```http
GET /discover?section=posts&size=20&offset=40&pool_token=f4e5d6c7-1111-2222-3333-444455556666
```

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "posts": [ /* 全新一批随机帖子（新池内容） */ ],
    "pool_token": "a9b8c7d6-9999-8888-7777-666655554444",  // ★ 新 token
    "has_more": true,
    "pool_refreshed": true,   // ★ 提示前端：池已换新
    "offset": 0,              // ★ 已重置为 0
    "size": 20
  }
}
```

### 5.5 空态

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "circles": [],
    "posts": [],
    "pool_token": "",
    "has_more": false,
    "offset": 0,
    "size": 20
  }
}
```

---

## 6. 错误码

| 业务码 `code` | HTTP | 含义 | 触发场景 |
|---|---|---|---|
| `200` | 200 | 成功 | 正常返回（含空态） |
| `204` | 400 | 参数错误 | query 参数格式错误 / `section` 不支持（极罕见，`section` 非法值会回落 `all` 而非报错） |
| `500`（内部错误码） | 500 | 服务内部错误 | Redis/ES 异常等罕见情况（读路径已尽量降级，一般返回空态而非 500） |

> **注意：发现页不会返回 401**。即使无 token 或 token 失效，也按匿名处理返回纯随机内容。
> 这与首页推荐（`/post/home`）和热点页（`/trending`）不同——它们强制登录，401 即未登录。

> 业务码与 HTTP 状态的关系：业务码从 `200` 起。前端统一按响应壳的 `code` 字段判断业务结果即可。

---

## 7. 边界与注意事项

### 7.1 内容可能"断层"
候选池中的实体（帖子/圈子）如果已被删除或禁用，会在回填时**从结果中跳过**。因此实际返回条数可能**少于** `size`，且不连续——这属于正常，不是 bug。

### 7.2 随机性说明
- 发现页是**随机推送**，没有排序依据（不像热点榜按热度降序）。
- 同一候选池周期（约 10 分钟）内，同一用户翻页是稳定的（同一批随机结果按 offset 切片）。
- 后台每 10 分钟重建候选池 = 「换一批」，用户感知内容更新。
- 不要把发现页当成实时随机——每次请求都重新随机会导致翻页重复/漏内容，故采用预计算池。

### 7.3 反气泡行为（登录态）
- 登录用户看到的帖子/圈子**排除了已加入的圈子**——这是设计意图（鼓励探索新圈子）。
- 帖子还**排除了已点赞/收藏/浏览过的**——避免推已看过的内容。
- 如果系统内容较少，反气泡排除后候选不足，后端会**兜底**：不排除再采一次，保证非空。所以即使你加入了很多圈子，发现页也不会是空的。
- 匿名用户无排除，纯随机。

### 7.4 候选池重建时机
两种情况会触发候选池重建（同步，约几百毫秒）：
1. **读路径 miss**：用户首次访问、或池已过期（30 分钟 TTL）→ 后端同步重建后返回。首次进入可能略慢（< 500ms）。
2. **后台定时刷新**：每 10 分钟后台任务重建活跃用户的池，让内容保鲜。

前端无需关心重建时机——只需正确处理 `pool_refreshed: true`（重置分页）即可。

### 7.5 不要频繁轮询
候选池每 10 分钟才更新一次。前端无需短轮询；**下拉刷新**触发重新请求即可（若池未更新，会返回相同 token 的相同池）。给用户的「换一批」体验主要靠后台周期刷新。

### 7.6 `is_liked` / `is_collected` 仅帖子项有
圈子项不涉及当前用户的交互态。帖子项的两个布尔值反映**当前登录用户**对该帖的点赞/收藏状态：
- 登录用户：真实交互态（可直接复用首页帖卡的交互逻辑）
- 匿名用户：恒为 `false`

### 7.7 私圈过滤
发现页的圈子已**过滤掉私密圈子**（`join_type=2`），前端不会拿到私圈卡片。返回的圈子 `join_type` 只会是 `0`（公开，直接加入）；`1`（需审核）已下线（#168），存量已转为私密。

### 7.8 与现有接口的关系
- **帖子卡片**可直接复用**首页信息流**（`GET /post/home`）的帖子组件——字段完全一致（发现页帖子无 `hot_score`，首页信息流也没有，天然兼容）。
- **圈子卡片**可复用**热点页**（`GET /trending`）的圈子卡片——字段高度一致（发现页圈子无 `hot_score`，前端取值前判空即可，或用一个不展示 `hot_score` 的通用圈子卡片）。
- **点击跳转**：帖子 → 帖子详情；圈子 → 圈子详情（走各自已有的 detail 接口）。

---

## 8. 字段速查（复制即用）

**DiscoverBoard**：`circles[]` `posts[]` `pool_token` `has_more` `pool_refreshed` `offset` `size`

**DiscoverPostItem**：`id` `circle_id` `user_id` `type` `title` `summary` `content` `view_count` `comment_count` `like_count` `collect_count` `is_pinned` `is_essence` `is_lock` `status` `create_time` `author_name` `author_avatar` `circle_name` `circle_avatar` `images[]` `is_liked` `is_collected`

**DiscoverCircleItem**：`id` `name` `avatar_url` `description` `category_id` `member_count` `post_count` `hot` `join_type` `create_time`

---

## 附录：前端集成 Checklist

- [ ] 首屏请求 `GET /discover?section=all&size=20`，保存响应的 `pool_token`
- [ ] 帖子卡片复用首页信息流组件；圈子卡片复用热点页组件（忽略 `hot_score`）
- [ ] 翻页：`section=posts`/`circles` + `offset` + 回传 `pool_token`
- [ ] 检查 `has_more` 决定是否显示「加载更多」
- [ ] **必须**处理 `pool_refreshed: true`：重置分页状态 + 更新 `pool_token`（可加「已换新内容」轻提示）
- [ ] 匿名场景：不带 `satoken` 头，`is_liked`/`is_collected` 恒 `false`
- [ ] 空态：`circles`/`posts` 为 `[]` 时展示「暂无发现内容，下拉刷新」
- [ ] 不要短轮询；用下拉刷新触发重新请求
