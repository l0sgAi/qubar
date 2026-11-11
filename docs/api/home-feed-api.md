# 首页信息流 API 对接文档

首页 4 个 tab 共用一个端点，通过 `tab` 参数切换。所有 tab 返回统一的帖子项结构（含 `is_liked` / `is_collected`），但**翻页机制不同**（推荐走候选池 offset，其余走 search_after 游标）。

---

## 1. 端点

```
GET /post/home
```

**鉴权**：需要登录。请求头携带 `satoken: <token>`（sa-token）。未登录或 token 失效 → `401`。

---

## 2. Query 参数

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `tab` | string | 是 | — | `recommend` \| `hot` \| `latest` \| `following` |
| `size` | int | 否 | `20` | 每页条数，范围 1~100 |
| `offset` | int | 否 | `0` | **仅 `recommend`**：候选池偏移 |
| `pool_token` | string | 否 | — | **仅 `recommend`**：上次返回的池版本 token（翻页回传） |
| `search_after` | string | 否 | — | **仅 `hot`/`latest`/`following`**：上次返回的游标（URL 编码的 JSON 数组） |

> `offset` / `pool_token` 仅对 `recommend` 生效；`search_after` 仅对其余 3 tab 生效。混用忽略。

---

## 3. 4 个 tab

| tab | 内容 | 排序 | 翻页 | 个性化 |
|---|---|---|---|---|
| `recommend` | 个性化推荐（5 路召回 + CF） | 多路交错合并 | 候选池 `offset` + `pool_token` | 强（已加圈子 + 行为 + CF） |
| `hot` | 全局热门 | 热度时间衰减 `hot/(age_h+2)^0.8` | `search_after` | 否（全局） |
| `latest` | 全局最新 | 发帖时间倒序 | `search_after` | 否（全局） |
| `following` | 关注流（已加入圈子的帖子 + 关注的用户发的帖子） | 发帖时间倒序 | `search_after` | 已加入的圈子 ∪ 关注的用户 |

> `following` 是**合并流**（#220）：帖子所在圈子是我已加入的，**或**帖子作者是我关注的用户（含 AI 机器人账号），满足任一即返回，同一帖子只出现一次。
>
> - 只加了圈子 / 只关注了人时，按有的那一种返回；**两者都没有**时返回空列表（前端可引导加圈或关注）。
> - 我看不到的私密圈里的帖子不返回，即使作者是我关注的人；只返回已发布帖子（与 `latest` 一致）。
> - 关注 / 取关、加圈 / 退圈后，重新从第一页请求即按新关系返回；已经在翻的序列从下一页起按新关系过滤，游标仍然可用。
> - 关注列表读取失败时服务端降级为只按已加入的圈子返回，不报错。

---

## 4. 响应结构

标准响应壳：

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": { /* FeedPage，见下 */ }
}
```

`data`（FeedPage）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `posts` | PostItem[] | 本页帖子列表 |
| `pool_token` | string | **recommend**：候选池版本 token，翻页原样回传（其余 tab 无此字段） |
| `search_after` | string | **hot/latest/following**：下一页游标，翻页原样回传（recommend 无此字段） |
| `has_more` | bool | 是否还有更多 |
| `pool_refreshed` | bool | **recommend**：`true` 表示池已重建，本次回的是 `offset=0`（前端可提示"推荐已刷新"） |

**PostItem** 字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string(uuid) | 帖子 ID |
| `circle_id` | string(uuid) | 所属圈子 ID |
| `user_id` | string(uuid) | 作者 ID |
| `type` | int | 帖子类型：1=图文 2=视频 3=投票 |
| `title` | string | 标题 |
| `summary` | string | 摘要（列表预览用） |
| `content` | string | 正文 |
| `view_count` | int | 浏览量（实时） |
| `comment_count` | int | 评论数（实时） |
| `like_count` | int | 点赞数（实时） |
| `collect_count` | int | 收藏数（实时） |
| `share_count` | int | 分享数（实时，去重后的分享人数；口径同帖子详情，见 [post-share-api.md](post-share-api.md) 第 4 节） |
| `is_pinned` | int | 是否置顶：0/1 |
| `is_essence` | int | 是否加精：0/1 |
| `is_lock` | int | 是否锁定（禁评）：0/1 |
| `status` | int | 帖子状态（1=已发布） |
| `create_time` | string(RFC3339) | 发帖时间 |
| `author_name` | string | 作者昵称 |
| `author_avatar` | string | 作者头像 URL |
| `circle_name` | string | 圈子名 |
| `circle_avatar` | string | 圈子头像 URL |
| `images` | string[] | 帖子图片 URL 列表 |
| `is_liked` | bool | **当前用户是否已点赞** |
| `is_collected` | bool | **当前用户是否已收藏** |
| `is_shared` | bool | **当前用户是否分享过**（访客恒为 `false`） |

---

## 5. 翻页流程

### 5.1 `recommend`（候选池 offset）

候选池由服务端构建并缓存（TTL 30min），含已去重、已剔除已交互的候选。客户端用 `offset` 翻页，`pool_token` 保证翻页期间池未变。

```
# 第 1 页
GET /post/home?tab=recommend&size=20
→ { posts:[...], pool_token:"abc123", has_more:true }

# 第 2 页（回传 pool_token + offset=20）
GET /post/home?tab=recommend&size=20&offset=20&pool_token=abc123
→ { posts:[...], pool_token:"abc123", has_more:true }
```

**`pool_refreshed=true` 处理**：若客户端用的 `pool_token` 已过期（池被重建），服务端返回新的 `offset=0` 第 1 页 + `pool_refreshed:true`。前端收到后应**重置本地列表到第 1 页**（避免重复/跳过），可给用户"推荐已刷新"提示。

**`has_more=false`**：候选池耗尽。前端可停止翻页，或稍后（池 TTL 过期重建后）再拉。

### 5.2 `hot` / `latest` / `following`（search_after 游标）

ES `search_after` 稳定游标翻页。把上次响应的 `search_after` 原样作为下次的 `search_after` 传入（需 URL 编码）。

```
# 第 1 页
GET /post/home?tab=hot&size=20
→ { posts:[...], search_after:"[1234567890,\"019058b0-...\"]", has_more:true }

# 第 2 页（回传 search_after）
GET /post/home?tab=hot&size=20&search_after=%5B1234567890%2C%22019058b0-...%22%5D
→ { posts:[...], search_after:"...", has_more:true }

# has_more=false → 无更多
```

> `search_after` 是不透明游标，**不要在前端解析或构造**，原样回传即可。`has_more=false` 时不返回 `search_after`。

---

## 6. 完整示例

### recommend 首屏
```http
GET /post/home?tab=recommend&size=20
satoken: <token>
```
```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "posts": [
      {
        "id": "019058b0-5b40-7000-8000-000000000abc",
        "circle_id": "019058b0-5b40-7000-8000-000000000001",
        "user_id": "01905712-3c01-7000-8000-000000000009",
        "type": 1,
        "title": "聊聊 Go 1.24 的泛型改进",
        "summary": "新版本对类型约束做了...",
        "content": "...",
        "view_count": 1234,
        "comment_count": 56,
        "like_count": 210,
        "collect_count": 33,
        "share_count": 4,
        "is_pinned": 0,
        "is_essence": 1,
        "is_lock": 0,
        "status": 1,
        "create_time": "2026-06-25T08:30:00Z",
        "author_name": "代码民工",
        "author_avatar": "https://cdn/.../u9.jpg",
        "circle_name": "Go 语言",
        "circle_avatar": "https://cdn/.../c1.jpg",
        "images": ["https://cdn/.../img1.jpg"],
        "is_liked": true,
        "is_collected": false,
        "is_shared": false
      }
    ],
    "pool_token": "9f3a1c2b-...",
    "has_more": true
  }
}
```

### following（空：没有加入圈子，也没有关注任何人）
```jsonc
{ "code": 200, "message": "Success", "data": { "posts": [], "has_more": false } }
```

---

## 7. 错误码

| HTTP | code | 触发 | 说明 |
|---|---|---|---|
| 401 | 401 | 未带 token / token 失效 | 推荐流强制登录 |
| 400 | 400 | `tab` 非法 / `search_after` 非 JSON | 参数错误 |
| 500 | 500 | ES/Redis 故障降级失败 | 极端情况，`data.posts` 可能为空数组（前端显"暂无内容"） |

> **降级保证**：单路召回失败不报错（只影响内容丰富度）。`recommend` 池为空时返回空数组 + `has_more:false`（不报 500）。

---

## 8. 边界与注意

- **冷启动（新用户）**：`recommend` 无点赞/收藏/圈子数据时，靠全局热门 + 最新兜底，仍非空。
- **`is_liked` / `is_collected`**：服务端按当前登录用户批量回填，列表项直接渲染高亮态，无需额外请求。
- **统计数据实时性**：`view/like/comment/collect_count` 来自实时缓存，与详情页一致。
- **`recommend` 候选池不含已交互帖**：已点赞/收藏/浏览过的不会出现在 `recommend`（避免重复推）。`hot`/`latest`/`following` **不过滤**已交互（可能看到已点赞的帖）。
- **同一端点切换 tab**：前端切 tab 时**清空本地列表 + 游标**，用对应 tab 的首页参数重新拉（不要把 `recommend` 的 `pool_token` 带给 `hot`）。
- **`search_after` URL 编码**：含 `[` `"` `,` 等特殊字符，必须 URL 编码后拼到 query。

---

## 9. 字段速查：帖子类型 / 状态

- `type`：1=图文，2=纯视频，3=投票/链接
- `status`：列表只返回 `1`（已发布），无需处理其它态
- `is_lock=1`：帖子已锁定，前端隐藏评论入口
