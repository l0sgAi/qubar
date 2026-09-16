# 评论定位（通知点击直达评论）前端对接文档

> 面向前端同学。后端设计与实现见 `docs/comment-locate-design.md`。
> 本文只讲前端需要对接的部分：**1 个定位接口** + 复用现有 `list`/`replies`。
> 关联代码：`src/views/Notifications.vue`、`src/views/post/PostDetail.vue`、
> `src/components/post/detail/CommentList.vue`、`src/api/comment.js`。

## 一、总览

| 能力 | 接口 | 说明 |
|---|---|---|
| 评论定位 | `GET /comment/locate` | 一次请求拿到定位所需全部游标（新增） |
| 顶层评论列表 | `GET /comment/list` | 复用，签名不变，传 locate 返回的 cursor |
| 回复列表 | `GET /comment/replies` | 复用，签名不变，传 locate 返回的 cursor |

- `locate` **无需登录**（与 list/replies 同组，游客可用）。
- 统一响应信封：`{code, message, data}`，下文只描述 `data` 部分。
- 通知列表接口（`/notice/list`）**无任何改动**，继续用 payload 里的 `post_id` + `comment_id`。

## 二、定位流程（前端实现步骤）

通知点击跳 `/post/{post_id}?comment_id={comment_id}`，帖子页按以下步骤：

1. URL 含 `comment_id` → 调 `GET /comment/locate?comment_id=...&sort=...`；
   - 建议先校验返回的 `post_id` 与 URL 的 post_id 一致，不一致按异常 toast 处理（防串帖）。
2. 用返回的 `list_cursor` 调 `GET /comment/list?post_id=...&sort=...&cursor=...`
   取含根评论的页，作为评论流第 0 页正常渲染（`list_cursor=null` 则不传 cursor 拉首页）；
3. 若 `is_root=0`（目标是回复）：自动展开 `root_id` 那条评论的回复区，用
   `reply_cursor` 调 `GET /comment/replies?root_id=...&sort=1&cursor=...`
   取含目标回复的页（`reply_cursor=null` 则不传 cursor 拉回复首页）；
4. 滚动到目标评论 DOM，高亮 2 秒。

**翻页约定**：定位命中的页作为分页缓存第 0 页，**只支持继续向下翻**
（与现有无限滚动方向一致）。用户无法翻到定位点之前的评论——已接受的限制，
不做双向游标。

## 三、接口详情

### 3.1 评论定位 `GET /comment/locate`

Query 参数：

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `comment_id` | string(UUID) | 是 | 目标评论 ID，顶层评论或回复均可（通知 payload 里的 `comment_id` 原样传） |
| `sort` | int | 否 | 顶层列表排序，与 `/comment/list` 一致：0=点赞倒序（默认），1=时间倒序。**返回的 `list_cursor` 仅在同 sort 下有效** |
| `reply_sort` | int | 否 | 回复列表排序：0=最热，1=最新。**缺省=1**（对齐你们当前回复列表用法）。前端拉回复传什么 sort，这里就传什么 |

**注意：回复页大小固定为服务端默认 10**（你们拉回复不传 limit，两边天然一致）。
若未来回复列表改传 `limit`，需同步告知后端给 locate 加参数，否则 `reply_page`/
`reply_cursor` 会错位。

请求示例：

```
GET /comment/locate?comment_id=0192f8c1-...&sort=0
GET /comment/locate?comment_id=0192f8c1-...&sort=1&reply_sort=1
```

响应 `data`：

```json
{
  "comment_id": "0192f8c1-...",
  "post_id":    "0192a0d0-...",
  "root_id":    "0192b1e2-...",
  "is_root": 0,
  "list_cursor": "eyJsaWtlX2NvdW50Ijo0MiwiaWQiOiIwMTkyLi4uIn0=",
  "reply_cursor": "eyJpZCI6IjAxOTIuLi4ifQ==",
  "reply_page": 3
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `comment_id` | string | 回显目标评论 ID |
| `post_id` | string | 目标所属帖子 ID（防串帖校验用） |
| `root_id` | string | 所属顶层评论 ID；`is_root=1` 时**等于 `comment_id`** |
| `is_root` | int | 1=目标是顶层评论，0=目标是回复 |
| `list_cursor` | string\|null | 传给 `/comment/list` 的 `cursor`，返回页**包含根评论**；`null`=根评论在首页（不传 cursor 拉首页即可） |
| `reply_cursor` | string\|null | 仅 `is_root=0` 有意义。传给 `/comment/replies` 的 `cursor`，返回页**包含目标回复**；`null`=在回复首页。`is_root=1` 固定 `null` |
| `reply_page` | int | 仅 `is_root=0` 有意义。目标回复所在页码（**从 1 开始**，按页大小 10 计），回复分页器显示用；`is_root=1` 固定 `0` |

**游标语义**：`list_cursor`/`reply_cursor` 均为「目标所在页的**起始**游标」，
原样传给列表接口，当页结果集里必含目标（正常时序下，例外见 §四-漂移）。

三种典型形态：

| 场景 | `list_cursor` | `reply_cursor` | `reply_page` |
|---|---|---|---|
| 目标是顶层评论，在首页 | `null` | `null` | `0` |
| 目标是顶层评论，在第 3 页 | `"eyJ..."` | `null` | `0` |
| 目标是回复，根在第 2 页，回复在第 3 页 | `"eyJ..."` | `"eyJ..."` | `3` |
| 目标是回复，根在首页，回复在首页 | `null` | `null` | `1` |

### 3.2 错误处理

| 场景 | HTTP | code | 前端表现 |
|---|---|---|---|
| 评论不存在 / 已删除 | **404** | 204 | toast「评论不存在或已删除」，停留帖子页顶部 |
| 目标是回复但根评论已删除 | 404 | 204 | 同上（根删则回复不可达） |
| `comment_id` 格式非法 / 参数非法 | 400 | 201 | 同上兜底 |
| 未登录 | 正常服务 | — | 游客可用，无需鉴权 |

注意：**不是**原需求文档建议的「200 + 40401」，已拍板改为 HTTP 404 + code=204，
与 `/comment/detail` 行为一致。按 HTTP 状态码判断即可。

## 四、边界与已知限制

1. **并发漂移（小概率）**：locate 与后续 list 请求之间若有新评论/点赞，目标可能
   移到相邻页。建议兜底：定位页渲染后检查目标是否在页内，不在则向后再拉一页找；
   仍找不到 toast 降级（如「评论定位失败」），停留在帖子页。
2. **sort 一致性**：`list_cursor` 只在同 `sort` 下有效；`reply_cursor` 只在同
   `reply_sort` 下有效。切排序后游标作废，需重新 locate 或回到列表头部。
3. **单向翻页**：定位页之前的评论不可达（§二约定）。
4. **回复排序说明**：回复列表 `sort` 语义为 0=最热（点赞倒序）、1=最新（时间倒序），
   与你们现有认知一致（后端旧注释曾写反，仅为注释错误，线上行为从未变过）。

## 五、联调自测清单

后端已覆盖单测；联调建议按此表验证（重点 off-by-one）：

- [ ] 顶层目标在首页 → `list_cursor=null`，不传 cursor 拉首页即含目标
- [ ] 顶层目标在第 2/3 页 → `list?cursor=list_cursor` 返回页含目标
- [ ] 顶层目标恰为某页首条/末条 → cursor 指向正确页（目标在当页，不在上一页/下一页）
- [ ] 回复目标 → `root_id` 正确，`replies?root_id=...&cursor=reply_cursor` 含目标回复
- [ ] 回复在回复首页 → `reply_cursor=null` 且 `reply_page=1`
- [ ] 两种 `sort`（0/1）各验一遍（同一评论位置不同，cursor 必须按请求 sort 算）
- [ ] 已删除评论 → 404 toast
- [ ] `post_id` 与 URL 不一致 → 异常 toast（正常不会发生）
