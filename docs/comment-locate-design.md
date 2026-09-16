# 评论定位（通知点击直达评论）设计

> 版本：2026-09-03 · 需求来源：前端《通知点击定位评论 — 后端需求文档》
> 状态：**已实现**（2026-09-03，Q1~Q3 已拍板，见 §11）
> 目标：新增 `GET /comment/locate`，一次请求返回定位目标评论所需的全部游标信息，
> 支撑「点击通知 → 直达评论 → 滚动居中 + 高亮」。

## 1. 可行性评估

**结论：可行，改动小（纯只读接口，comment 域内闭环），无 DDL、无 CDC/ES/通知改动。**

现状已具备全部基础：

- 顶层列表 `GET /comment/list`：keyset 游标，页大小**服务端固定 20**
  （[service.go](../pkg/domains/comment/application/service.go) `GetRootComments` 硬编码 `20`）；
  sort=0 → `like_count DESC, id DESC`，sort=1 → `id DESC`（UUIDv7 字典序=时间序）。
- 回复列表 `GET /comment/replies`：同一套游标函数，`limit` 由客户端传（1~50，默认 10）。
- 游标格式：base64(JSON)，sort=0 含 `{like_count, id}`，sort=1 含 `{id}`；
  条件为严格小于（`id < ?` / `like_count < ? OR (= AND id < ?)`）。
- `GetByID` 过滤 `deleted=0`，已删除评论天然返回 `ErrCommentNotFound`。
- 现有索引（[docs/pgsql-ddl/comment.md](pgsql-ddl/comment.md)）：
  `idx_comment_post_root_like (post_id, root_id, like_count DESC)`、
  `idx_comment_post_root_time (post_id, root_id, create_time DESC)`、
  `idx_comment_root_id (root_id) WHERE root_id IS NOT NULL`。
  rank 的 `COUNT` 与 `LIMIT 1 OFFSET k` 均可走前缀等值 + 索引扫描，
  热点帖数千评论量级下 P99 < 200ms 无压力，**无需新索引**（备选见 §6）。

## 2. 现状勘查发现的问题（影响需求落地，必须处理）

### P1. 回复页大小不固定 —— `reply_page`/`reply_cursor` 无唯一定义 ⚠️ 需求漏洞

`/comment/replies` 的 `limit` 是客户端参数（1~50，默认 10）。同一回复在不同 limit
下所在页码与页起始游标都不同。前端需求文档未约定 locate 计算用哪个页大小。
**决策 D3（已拍板）：前端拉回复不传 limit（确认 2026-09-03），故 locate 不设
`reply_limit` 参数，回复页大小固定取服务端默认值 10，与 `GetReplies` 缺省值
共用同一常量 `defaultReplyPageSize`。前端若未来改传 limit，须同步给 locate 加参数。**

### P2. 回复排序语义文档与实现不一致 ⚠️ 既有注释错位

- handler/service 注释声称 replies `sort: 0=时间倒序, 1=点赞倒序`；
- 实际 repo 实现（`applyOrderBy`/`applyCursorCondition`/`buildNextCursor`）对两个列表
  是同一套映射：**sort=0 → 点赞倒序，sort=1 → 时间倒序**，回复并未反转。
- 前端实际认知「0最热 1最新」与 repo 实际行为**一致**（确认 2026-09-03）——
  错的只是后端 Go 注释，线上行为无需改动。

**决策 D4（已拍板）：不改行为，修正注释与文档；locate 设可选参数 `reply_sort`
（缺省=1 时间倒序，对齐前端回复列表当前「最新」用法），前端拉回复用哪个 sort
就传哪个。**

### P3. 业务错误码 40401 与现有响应码体系不兼容

现有 `httputil.ResponseCode` 是 200+iota 连续码并映射 HTTP 状态码，全站无
5 位业务码先例。`/comment/detail` 对不存在评论返回 **HTTP 404 + code=204(CodeNotFound)**。
**决策 D2（已拍板）：不引入 40401，沿用 HTTP 404 + `CodeNotFound`，
message="评论不存在或已删除"，与 `/comment/detail` 行为完全一致。
前端按 HTTP 404（或 code=204）识别。**

### P4. status（审核中/隐藏）不过滤 —— 与列表语义保持一致

现有 `list`/`replies` 均只过滤 `deleted=0`，**不过滤 status**（2=审核中、3=隐藏
仍会出现在列表里）。locate 若额外按 status 判定"不可见"会与列表自相矛盾
（locate 说不存在，列表却能刷到）。
**决策 D6：locate 与列表同谓词（仅 `deleted=0`）。前端文档"审核/隐藏不可见"
场景在当前列表语义下不存在；未来列表加 status 过滤时 locate 同步加。**

### P5. 并发漂移（固有限制，接受）

locate 与后续 list 请求之间若有新评论/点赞，rank 会移位，目标可能落到相邻页
（sort=0 点赞序尤其敏感）。低 QPS 场景概率低。
**决策：接受。前端兜底：定位页不含目标时可向后再拉一页找，找不到 toast 降级。
不做双向游标、不做快照一致性。**

## 3. 需求合理性结论

- 独立 locate 端点方案**合理**，同意；备选方案（list 加 comment_id 参数）确实无法
  表达"目标是回复 + 回复页码"，拒绝。
- 响应字段设计合理，仅按 D2/D3/D4 调整，并追加 D5。
- **决策 D5：响应增加 `post_id` 回显**。前端可校验通知 payload 的 post_id 与评论
  实际归属一致，防串帖（通知数据异常时提前 toast，而不是跳错帖子）。

## 4. 接口规格（调整后）

```
GET /comment/locate
```

挂在 `/comment` 访客可读组（optionalCheck），与 list/replies 一致，无需登录。

**请求参数：**

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `comment_id` | string(UUID) | 是 | 目标评论 ID，顶层或回复均可 |
| `sort` | int | 否 | 顶层列表排序，同 `/comment/list`：0=点赞倒序（默认），1=时间倒序 |
| `reply_sort` | int | 否 | 回复列表排序，同 `/comment/replies`：0=点赞倒序（最热），1=时间倒序（最新，**缺省值**，对齐前端当前用法）。**须与前端拉回复时的 sort 一致** |

回复页大小固定为服务端默认 10（前端拉回复不传 limit，见 D3）。

**响应 `data`：**

```json
{
  "comment_id": "018f...",
  "post_id":    "018e...",
  "root_id":    "018e...",
  "is_root": 0,
  "list_cursor": "eyJ...",
  "reply_cursor": "eyJ...",
  "reply_page": 3
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `comment_id` | string | 回显目标评论 ID |
| `post_id` | string | 目标所属帖子 ID（D5 新增） |
| `root_id` | string | 所属顶层评论 ID；目标本身是顶层时等于 `comment_id` |
| `is_root` | int | 1=顶层评论，0=回复 |
| `list_cursor` | string\|null | 传给 `/comment/list?sort=sort` 的 cursor，返回页**含根评论**；`null`=根评论在首页 |
| `reply_cursor` | string\|null | 仅 `is_root=0` 有意义。传给 `/comment/replies?root_id&sort=reply_sort`（不传 limit）的 cursor，返回页**含目标回复**；`null`=在回复首页。`is_root=1` 固定 `null` |
| `reply_page` | int | 仅 `is_root=0` 有意义。目标回复所在页码（从 1，按页大小 10 计）。`is_root=1` 固定 `0` |

**错误：**

| 场景 | 响应 |
|---|---|
| `comment_id` 不存在 / 已删除 | HTTP 404，code=204(CodeNotFound)，message="评论不存在或已删除" |
| 目标是回复但根评论已删除 | 同上（其回复在列表中本就不可达） |
| `comment_id` 格式非法 / 参数非法 | HTTP 400，code=201(CodeBadRequest) |
| 未登录 | 正常服务（访客可读） |

## 5. 定位算法

记号：目标评论 T；若 T 是回复，根评论 R = GetByID(T.RootID)。
页大小：顶层 SIZE=20（常量 `rootCommentPageSize`，与 `GetRootComments` 共用）；
回复 SIZE=10（常量 `defaultReplyPageSize`，与 `GetReplies` 缺省 limit 共用）。

### 5.1 rank 计算（COUNT，走索引）

"严格排在 X 之前"的条数 `rank - 1`：

```sql
-- 顶层，sort=1（时间倒序，key=id）：
SELECT COUNT(*) FROM domains.comment
WHERE post_id = $1 AND root_id IS NULL AND deleted = 0
  AND id > $2;

-- 顶层，sort=0（点赞倒序，key=(like_count, id)）：
SELECT COUNT(*) FROM domains.comment
WHERE post_id = $1 AND root_id IS NULL AND deleted = 0
  AND (like_count > $2 OR (like_count = $2 AND id > $3));

-- 回复：把 post_id = $1 AND root_id IS NULL 换成 root_id = $1，key 选择按 reply_sort 同上。
```

### 5.2 页码与页起始游标（off-by-one 关键）

```
rank      = COUNT_BEFORE + 1            -- 目标在排序中的位次（从 1）
page      = floor((rank - 1) / SIZE) + 1
page == 1 → cursor = null
page > 1  → 取排序后第 (page-1)*SIZE 条（上一页末条）的 keyset 编码为 cursor
```

页起始游标 = **上一页末条**的 keyset（不是目标本身——游标条件是严格小于，
用目标自身会把目标排除在页外）。

取第 k = (page-1)*SIZE 条（1-based）：

```sql
SELECT id, like_count FROM domains.comment
WHERE post_id = $1 AND root_id IS NULL AND deleted = 0
ORDER BY like_count DESC, id DESC   -- sort=0；sort=1 改 ORDER BY id DESC
LIMIT 1 OFFSET k - 1;
```

cursor 编码复用既有 `buildNextCursor(item, sort)`，保证与列表接口游标格式逐字节一致。

### 5.3 边界自测表（验收 #6）

以 SIZE=20 为例：

| rank | page | cursor |
|---|---|---|
| 1 | 1 | null |
| 20（首页末条） | 1 | null |
| 21（次页首条） | 2 | rank=20 那条的 keyset |
| 40（次页末条） | 2 | rank=20 那条的 keyset |
| 41 | 3 | rank=40 那条的 keyset |

回复同理（SIZE=10）：rank=10 → page1 + `reply_cursor=null` + `reply_page=1`；
rank=11 → page2 + rank=10 那条 keyset。

### 5.4 完整流程

```
T = repo.GetByID(comment_id)                -- 不存在/已删 → ErrCommentNotFound → 404
is_root = (T.RootID == nil)
R       = is_root ? T : repo.GetByID(*T.RootID)   -- 根已删 → 404
list_cursor = LocateRootCursor(R.PostID, sort, R, 20)     -- "" → null
if is_root:
    return {..., root_id: T.ID, is_root: 1, reply_cursor: null, reply_page: 0}
else:
    reply_cursor, reply_page = LocateReplyCursor(R.ID, reply_sort, T, reply_limit)
    return {..., root_id: R.ID, is_root: 0, reply_cursor, reply_page}
```

DB 往返：顶层 2~3 次（GetByID + COUNT + 必要时 OFFSET 查），回复 3~5 次，全部走索引。

## 6. 性能与索引

- 现有索引足够（§1）。热点帖数千评论时：
  COUNT 为索引前缀等值 + 范围扫描数千行，亚毫秒~个位数毫秒；
  `LIMIT 1 OFFSET k` 索引顺序扫跳过 k 行，k ≤ 评论总数。
- **备选（仅当 EXPLAIN 实测不达标再做）**：为 sort=1 的 rank 查询补
  `(post_id, root_id, id DESC)` 部分索引（`WHERE root_id IS NULL AND deleted = 0`），
  为回复补 `(root_id, id DESC)`。本迭代不做，避免无谓 DDL。

## 7. 代码改动清单

| 层 | 文件 | 改动 |
|---|---|---|
| domain | [repository.go](../pkg/domains/comment/domain/repository.go) | 接口 +2 方法：`LocateRootCursor(ctx, postID, sort, target *Comment, size) (string, error)`（`""`=首页）；`LocateReplyCursor(ctx, rootID, sort, target *Comment, size) (string, int, error)`（cursor + page 从 1） |
| infra | [comment_repo_pg.go](../pkg/domains/comment/infrastructure/comment_repo_pg.go) | 实现两方法；抽 `countCommentsBefore(scope, sort, target)` 与 `keysetItemAt(scope, sort, k)` 两 helper；游标编码复用 `buildNextCursor` |
| application | [service.go](../pkg/domains/comment/application/service.go) | `LocateComment(ctx, commentID, sort, replySort, replyLimit) (*CommentLocateResult, error)`；提取常量 `rootCommentPageSize = 20`（替换 GetRootComments 硬编码）、`defaultReplyLimit = 10`（替换 GetReplies 硬编码），locate 与列表共用同一常量，防页大小漂移 |
| application | service.go | `CommentLocateResult` 结构体：`ListCursor`/`ReplyCursor` 用 `*string`（nil → JSON `null`） |
| http | [handler.go](../pkg/domains/comment/interfaces/http/handler.go) | `LocateCommentRequest`（query 绑定，`comment_id` 必填 uuid 校验，sort `oneof=0 1`）+ handler；`reply_sort` 手动解析（缺省=1）避开指针 query 绑定不确定性；`ErrCommentNotFound` → `httputil.NotFound(c, "评论不存在或已删除")` |
| http | [routes.go](../pkg/domains/comment/interfaces/http/routes.go) | `pub.GET("/locate", h.LocateComment)`（optionalCheck 组） |
| http | handler.go 注释修正 | P2：GetRepliesRequest / GetReplies 的 sort 注释改为与实际一致（0=点赞倒序，1=时间倒序）；service/repo 接口注释同步修正 |

无 composition 层改动（复用既有 CommentService 装配）。无 DDL。无通知/ES/CDC 改动。

## 8. 测试计划

- **infra 单测**（扩展现有 [cursor_test.go](../pkg/domains/comment/infrastructure/cursor_test.go)
  或新增 locate_test.go）：page/cursor 计算覆盖 §5.3 边界表，两种 sort 各一遍；
  回复侧 rank=10/11 边界。
- **service 单测**（参照 mention_test.go 的 fake repo 模式）：
  顶层目标、回复目标、目标已删 → ErrCommentNotFound、根已删 → ErrCommentNotFound、
  is_root=1 时 reply_cursor=nil/reply_page=0。
- **handler 层**：参数校验（缺 comment_id、非法 uuid、sort=2、reply_limit=51 → 400）。
- **手工联调自测**：造 45 条顶层 + 某根下 25 条回复的帖子，两 sort 各验证：
  locate → list?cursor= 含根；replies?cursor= 含目标；首页目标 cursor=null。

## 9. 实施步骤（已完成，2026-09-03）

1. ✅ domain 接口 + infra 实现 + infra 单测（[locate_test.go](../pkg/domains/comment/infrastructure/locate_test.go)，边界表全覆盖）。
2. ✅ application `LocateComment` + 常量提取 + service 单测（[locate_test.go](../pkg/domains/comment/application/locate_test.go)，fake repo）。
3. ✅ http handler/routes + P2 注释修正（handler/service/repo 三处）。
4. ⬜ 手工联调自测（§8 手工项：造 45 顶层 + 25 回复帖子，两 sort 验证）——待有库环境执行。
5. ✅ 前端对接文档 [comment-locate-frontend-api.md](comment-locate-frontend-api.md)。

## 10. 验收标准（对齐前端文档 §6）

1. 顶层目标：`list?cursor=list_cursor` 返回页含该评论；首页时 `list_cursor=null`。✔ §5.2
2. 回复目标：`root_id`/`reply_page` 正确；`list?cursor=` 含根；`replies?cursor=` 含目标。✔ §5.4
3. sort=0/1 下游标各自按对应排序键计算。✔ §5.1
4. 已删除评论 → HTTP 404 + "评论不存在或已删除"（按 D2 调整）。✔ §4
5. 回复在首页时 `reply_cursor=null` 且 `reply_page=1`。✔ §5.3
6. 页首/页末边界 off-by-one 按 §5.3 表自测。✔

## 11. 待确认问题（全部已拍板，2026-09-03）

- **Q1（D2 错误码）**：✅ 拍板 HTTP 404 + code=204(CodeNotFound)，不引入 40401。
- **Q2（P2 回复排序）**：✅ 前端 replies 实际传 `root_id` + `sort=1`（0最热 1最新），
  与 repo 实际行为一致（仅后端注释错位，已修）。`reply_sort` 缺省=1。
- **Q3（P1 回复页大小）**：✅ 前端不传 limit → 砍掉 `reply_limit` 参数，
  回复页大小固定服务端默认 10（`defaultReplyPageSize` 常量共用）。
