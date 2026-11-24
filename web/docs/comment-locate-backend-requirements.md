# 通知点击定位评论 — 后端需求文档

> 版本：2026-09-03 · 前端仓库：qubar-frontend · 关联代码：`src/views/Notifications.vue`、`src/views/post/PostDetail.vue`、`src/components/post/detail/CommentList.vue`、`src/api/comment.js`

## 1. 背景与现状

通知中心（点赞/评论/回复/提及）点击后跳转 `/post/{post_id}?comment_id={comment_id}`。前端已在 URL 携带 `comment_id`，但**无法定位到具体评论**，原因：

1. **顶层评论列表为游标分页**（`GET /comment/list`，sort + cursor），目标评论可能在任意一页，前端无"跳到包含某评论的页"的能力；
2. **回复列表同为游标分页**（`GET /comment/replies`，root_id + cursor），且回复默认折叠不渲染，目标是回复时需先定位其根评论再定位回复本身；
3. 通知 payload 只有 `comment_id`，前端**无法区分目标是顶层评论还是回复**，也不知道 `root_id`。

目标：新增**评论定位接口**，前端一次请求获得定位所需的全部信息，实现「点击通知 → 直达评论 → 滚动居中 + 高亮」。

## 2. 前端消费方式（供后端理解）

定位流程（前端实现，后端只需提供 3.1 一个接口）：

1. 进入帖子页，URL 含 `comment_id` → 调 `GET /comment/locate`；
2. 用返回的 `list_cursor` 调 `GET /comment/list` 取含根评论的页，正常渲染评论流（定位页之前的评论不加载，用户向上滚动体验与现状一致——现状本就只有向下无限滚动）；
3. 若目标是回复（`is_root=0`）：自动展开根评论的回复区，用 `reply_cursor` 调 `GET /comment/replies` 取含目标回复的页；
4. 滚动到目标评论 DOM 并高亮 2 秒。

约定：定位命中的页作为前端分页缓存的第 0 页，**只支持继续向后翻**（与现有无限滚动方向一致，用户无法翻到定位点之前的页——接受此限制，不做双向游标）。

## 3. 接口规格

### 3.1 评论定位

```
GET /comment/locate
```

**请求参数：**

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `comment_id` | string(UUIDv7) | 是 | 目标评论 ID，可为顶层评论或回复 |
| `sort` | int | 否 | 顶层评论列表排序，与 `/comment/list` 语义一致：0=按点赞倒序（默认），1=按时间倒序。**返回的 `list_cursor` 仅在同 sort 下有效** |

**响应：** 沿用现有 `{ code, message, data }` 结构，`data`：

```json
{
  "comment_id": "018f...",
  "root_id": "018e...",
  "is_root": 0,
  "list_cursor": "eyJ...",
  "reply_cursor": "eyJ...",
  "reply_page": 3
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `comment_id` | string | 回显目标评论 ID |
| `root_id` | string | 目标所属顶层评论 ID；目标本身是顶层评论时**等于 `comment_id`** |
| `is_root` | int | 1=目标是顶层评论，0=目标是回复 |
| `list_cursor` | string\|null | 传给 `/comment/list` 的 `cursor`，使返回页**包含 `root_id`** 那条评论；`null` 表示根评论在**首页**（前端不传 cursor 直接拉首页即可） |
| `reply_cursor` | string\|null | 仅 `is_root=0` 时有意义。传给 `/comment/replies` 的 `cursor`，使返回页**包含目标回复**；`null` 表示在回复首页。`is_root=1` 时固定为 `null` |
| `reply_page` | int | 仅 `is_root=0` 时有意义。目标回复所在页码（**从 1 开始**），前端回复分页器显示用；`is_root=1` 时固定为 `0` |

**游标语义（关键约定）：** `list_cursor` / `reply_cursor` 均为「目标所在页的**起始**游标」，即上一页的 `next_cursor`。前端把它原样传给列表接口，**当页结果集里必须包含目标**。若目标恰好在第一页，返回 `null`（而非空字符串），前端据此走无 cursor 的首页请求。

### 3.2 错误处理

| 场景 | HTTP | code（建议） | 前端表现 |
|---|---|---|---|
| `comment_id` 不存在 / 已删除 | 200 | 业务码 `40401`（"评论不存在或已删除"） | toast 提示"评论已被删除"，停留在帖子页顶部 |
| 评论存在但因审核/隐藏不可见 | 200 | 同 `40401` | 同上（不区分删除与不可见） |
| `comment_id` 格式非法 | 200 | 参数校验错误码（沿用现有规范） | 同上兜底 |
| 未登录 | — | **无需鉴权**，与 `/comment/list` 行为一致（帖子详情页游客可浏览） | — |

### 3.3 性能要求

- 定位计算应基于排序键的 rank 查询（`COUNT(*) WHERE sort_key > target.sort_key` 类）或索引，**禁止全表扫描**；热点帖子评论数可达数千；
- 接口预期 QPS 低（通知点击触发），但需保证 P99 < 200ms。

## 4. 不需要的改动

- **通知 payload 无需加字段**：`root_id`、`is_root` 均由 locate 接口返回，通知列表接口（`/notice/list`）保持不变；
- `/comment/list`、`/comment/replies` 接口签名不变，前端仅复用其现有 `cursor` 参数。

## 5. 备选方案（不推荐，仅备查）

若不新增端点，可改为 `/comment/list` 与 `/comment/replies` 各加一个可选 `comment_id` 参数：传了就直接返回含目标的页（响应外加 `located: 1` 与目标的 `root_id`）。

缺点：列表语义与定位语义耦合；`/comment/list` 无法表达"目标是回复"（需先查 root 再调 replies，仍要两次往返且前端拿不到 reply 所在页码）。**仍推荐 3.1 的独立 locate 接口。**

## 6. 验收标准

1. 顶层评论目标：`locate` → `list?cursor=list_cursor` 返回页包含该评论；目标在首页时 `list_cursor=null`；
2. 回复目标：`locate` 返回正确 `root_id`/`reply_page`；`list?cursor=list_cursor` 含根评论；`replies?root_id=...&cursor=reply_cursor` 返回页包含该回复；
3. 两种 sort（0/1）下游标各自正确（同一评论在不同排序下位置不同，cursor 必须按请求的 sort 计算）；
4. 已删除评论返回约定业务码；
5. 回复目标在回复首页时 `reply_cursor=null` 且 `reply_page=1`；
6. 目标恰为某页最后一条 / 某页第一条（边界）时，cursor 指向正确页——**重点自测 off-by-one**。
