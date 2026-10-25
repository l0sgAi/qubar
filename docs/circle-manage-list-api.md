# API 接入文档：我可管理的圈子列表（GET /circle/manage/list）

> **用途**：AI 代理管理控制台的「圈子选择器」数据源——列出**当前登录用户作为圈主/管理员**的圈子，
> 供其进入某个圈子的代理管理页（查看/绑定 ≤5 个 AI 代理）。
> 这是「圈子级 AI 代理管理」特性的 Phase 1；Phase 2（圈内代理 CRUD，见
> [circle-agent-manage-api.md](circle-agent-manage-api.md)）已交付，列表项的 `agent_count`
> 返回真实值（统计失败降级为 0），本接口契约不变。

---

## 一、接口概览

| 项目 | 值 |
|---|---|
| 方法 | `GET` |
| 路径 | `/circle/manage/list`（无全局前缀，直接挂在站点根路径下） |
| 鉴权 | **需登录**。请求头携带 token：`satoken: <token>` |
| 分页方式 | offset 分页（`page` / `size`，响应含 `total`） |
| 数据实时性 | 直查 PostgreSQL，**不走缓存/ES**：角色变更（任免管理员、转让圈主）后**立即可见** |

> **与 `GET /circle/my` 的区别**：`/circle/my` 是「我加入的所有圈子」（任意角色），
> 服务于个人中心展示；本接口是「我能管理代理的圈子」（仅 role 20/30），服务于管理控制台。
> **不要**用 `/circle/my` 的数据做代理管理入口过滤——它虽返回 `role`（见 [加入圈子列表文档](api/circle-joined-list-api.md)），
> 但只含正常状态的圈子（ES 过滤掉待审 / 封禁圈子），圈子资料也依赖 ES 有秒级延迟。

---

## 二、请求

### 2.1 Header

| Header | 必填 | 说明 |
|---|---|---|
| `satoken` | 是 | 登录 token。缺失/失效返回 401。CORS 白名单已包含此头，跨域可直接携带 |

### 2.2 Query 参数

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `keyword` | string | 否 | 空（不过滤） | 按圈子 `name` 或 `description` **子串**过滤，大小写不敏感。详见 §5 |
| `page` | int | 否 | `1` | 页码，从 1 开始。`<=0` 或非整数由服务端规整（见下方行为） |
| `size` | int | 否 | `20` | 每页条数，上限 100。`<=0` 或 `>100` 服务端回落为 20 |

**服务端规整行为（前端无需预处理，但应理解）**：

- `page=0`、`page=-1` → 按 `1` 处理；`page=abc`（非整数）→ **400 错误**（绑定失败，见 §6）。
- `size=0`、`size=500` → 按 `20` 处理。
- **响应中回显的是规整后的值**（`page` / `per_page` 字段）。翻页状态请以响应为准，不要假设请求参数原样返回。

### 2.3 请求示例

```bash
curl -H "satoken: <your-token>" \
  "http://<host>:8888/circle/manage/list?keyword=%E6%B8%B8%E6%88%8F&page=1&size=20"
```

---

## 三、响应

### 3.1 成功响应信封（HTTP 200）

```json
{
  "code": 200,
  "message": "Success",
  "total": 2,
  "page": 1,
  "per_page": 20,
  "data": [
    {
      "id": "0192a0d0-0000-7000-8000-0000000000aa",
      "name": "游戏讨论圈",
      "slug": "gaming",
      "avatar_url": "https://cdn.example.com/avatar/xx.png",
      "description": "聊聊最近玩的游戏",
      "member_count": 128,
      "post_count": 456,
      "join_type": 0,
      "status": 1,
      "my_role": 30,
      "agent_count": 0,
      "create_time": "2026-08-01T10:30:00.123456+08:00"
    },
    {
      "id": "0192a0d0-0000-7000-8000-0000000000bb",
      "name": "被封禁的圈",
      "description": "",
      "member_count": 5,
      "post_count": 0,
      "join_type": 1,
      "status": 2,
      "my_role": 20,
      "agent_count": 0,
      "create_time": "2026-07-15T09:00:00+08:00"
    }
  ]
}
```

> ⚠️ **空结果时 `data` 键整个缺失**（信封层 `omitempty`），不是 `"data": []`：
> ```json
> { "code": 200, "message": "Success", "total": 0, "page": 1, "per_page": 20 }
> ```
> **前端必须用 `body.data ?? []` 兜底**，否则 `.map()` 会报错。

### 3.2 信封字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `code` | number | 业务码，成功恒为 `200`（注意与 HTTP 状态码是两套，见 §6） |
| `message` | string | `"Success"` |
| `total` | number | 符合条件（含 keyword 过滤）的**总条数**，用于算总页数 |
| `page` | number | 当前页码（规整后的值） |
| `per_page` | number | 每页条数（规整后的值）。**注意字段名是 `per_page`，不是 `size`** |
| `data` | array | 列表项，**可能整体缺失**（空结果），见上方警告 |

### 3.3 列表项字段（ManagedCircleItem）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string (UUID) | 圈子 ID，后续代理绑定/管理接口均用它标识圈子 |
| `name` | string | 圈子名称 |
| `slug` | string | 圈子短链标识；**可为空串或缺失**（omitempty） |
| `avatar_url` | string | 圈子头像；**可为空串或缺失**，UI 需有占位图 |
| `description` | string | 圈子简介，恒存在（可能空串） |
| `member_count` | number | 成员数（DB 快照，可能与详情页的实时计数有秒级~分钟级偏差，控制台展示够用） |
| `post_count` | number | 帖子数（同上，快照值） |
| `join_type` | number | 加入方式：`0` 直接加入 / `1` 需审核 / `2` 私密邀请制 |
| `status` | number | **圈子状态**：`0` 审核中 / `1` 正常 / `2` 被封禁。见 §4.2 |
| `my_role` | number | **调用者在此圈的角色**：`20` 管理员 / `30` 圈主。见 §4.1 |
| `agent_count` | number | 该圈已绑定 AI 代理数（实时统计，上限 5，用于「3/5」这类配额 UI；统计失败降级为 0）。圈内代理管理接口见 [circle-agent-manage-api.md](circle-agent-manage-api.md) |
| `create_time` | string | 建圈时间，RFC3339 带时区（如 `2026-08-01T10:30:00.123456+08:00`），`new Date()` 可直接解析 |

### 3.4 排序规则（固定的，前端无需排序）

1. **圈主在前**：`my_role = 30` 的圈子排在 `my_role = 20` 之前；
2. 同角色内按**建圈时间新 → 旧**。

---

## 四、业务语义（UI 该怎么用）

### 4.1 `my_role` 驱动能力差异

| `my_role` | 含义 | UI 建议 |
|---|---|---|
| `30` | 圈主 | 完整管理能力（Phase 3 起：增删改代理 + 圈子设置入口） |
| `20` | 管理员 | 圈内代理的日常管理（Phase 3 起的权限校验同样以服务端为准） |

Phase 3 的代理管理接口会以服务端校验为准，前端按 `my_role` 做入口展示即可，**不要**仅靠前端隐藏来兜权限。

### 4.2 `status` 与置灰

列表**包含非正常状态的圈子**（`status=0` 审核中 / `status=2` 被封禁）——因为这些圈的存量代理仍需管理/停用：

- `status === 1`：正常，可点击进入代理管理；
- `status !== 1`：整项**置灰**，禁止进入代理管理页（但保留展示，tooltip 可提示原因）。

注意区分两种「状态」：

- `status` 是**圈子本身**的状态；
- 用户**作为成员**的状态不在本接口体现：只有成员状态为「正常」的圈主/管理员才会出现在查询结果里
  （被禁言/被拉黑期间管理者暂时从列表消失，与「管理权暂停」语义一致，解禁后自动恢复）。

### 4.3 `agent_count`

该圈已绑定的 AI 代理数（实时 COUNT，上限 5）。选择器/列表可直接渲染 `已绑定 {agent_count}/5`。
点击进入代理管理页后，用 `GET /circle/agent/list?circle_id=` 拉取机器人列表
（接口契约见 [circle-agent-manage-api.md](circle-agent-manage-api.md)）。

### 4.4 何时调用

- 进入「AI 代理管理」页时加载第一页；
- 圈子选择器搜索框输入关键词（建议 300ms 防抖后请求）；
- 在其他页面执行了**任免管理员 / 转让圈主 / 圈子被封禁**等操作后返回本页时**重新拉取**（本接口无缓存，刷新即最新）。

---

## 五、关键词搜索语义（与全站搜索框不同）

| 特性 | 行为 |
|---|---|
| 匹配范围 | 圈子 `name` **或** `description` |
| 匹配方式 | **子串包含**，大小写不敏感（`ILIKE '%kw%'`） |
| 通配符 | `%`、`_`、`\` 按**字面字符**匹配（服务端已转义），用户搜 `100%` 就是搜字面 `100%` |
| 拼写容错 | **无**。控制台场景故意不做模糊容错；搜不到提示用户检查关键词即可 |
| 预处理 | 服务端自动 trim、去非法字符；超 50 字符**静默截断**到 50 字 |
| 翻页 | keyword 模式下 `total` 是过滤后的总数，翻页请求须**携带同一 keyword** |

---

## 六、错误处理

业务码与 HTTP 状态码是两套：**HTTP 层面先按状态码分支，业务层再看 `code === 200`**。

| 场景 | HTTP | `code` | `message` | 前端处理 |
|---|---|---|---|---|
| 成功 | 200 | 200 | `Success` | 渲染 |
| 未携带 token | 401 | 202 | `Token not found` | 跳登录页 |
| token 失效/过期 | 401 | 202 | `Invalid or expired token` | 清本地态 → 跳登录 |
| query 参数非法（如 `page=abc`） | 400 | 201 | `Invalid query parameters` | 属于前端 bug，开发期排查；线上可 toast 后兜底回第一页 |
| 服务端错误（DB 异常等） | 500 | 210 | `Failed to list managed circles` | toast 重试入口；**不要**静默清空列表 |

错误响应体形如：

```json
{ "code": 202, "message": "Invalid or expired token" }
```

> 本接口**没有** 403：权限过滤在查询里完成——不是管理者的用户调用只会得到空列表，不会报错。

---

## 七、前端接入代码

### 7.1 TypeScript 类型

```typescript
/** 圈子状态 */
export enum CircleStatus {
  Pending = 0,
  Normal = 1,
  Banned = 2,
}

/** 我在此圈的角色 */
export enum MyManageRole {
  Admin = 20,
  Owner = 30,
}

export interface ManagedCircleItem {
  id: string;
  name: string;
  slug?: string;
  avatar_url?: string;
  description: string;
  member_count: number;
  post_count: number;
  join_type: 0 | 1 | 2;
  status: CircleStatus;
  my_role: MyManageRole;
  /** 该圈已绑定代理数（实时统计，上限 5；统计失败降级 0） */
  agent_count: number;
  create_time: string; // RFC3339
}

export interface ManagedCircleListResult {
  code: number;
  message: string;
  total: number;
  page: number;
  per_page: number;
  /** 空结果时该键缺失，读取时用 ?? [] 兜底 */
  data?: ManagedCircleItem[];
}
```

### 7.2 请求封装（fetch 示例）

```typescript
export async function fetchManagedCircles(params: {
  keyword?: string;
  page?: number;
  size?: number;
}, token: string): Promise<Required<Pick<ManagedCircleListResult, 'total' | 'page' | 'per_page'>> & { data: ManagedCircleItem[] }> {
  const qs = new URLSearchParams();
  if (params.keyword) qs.set('keyword', params.keyword);
  if (params.page) qs.set('page', String(params.page));
  if (params.size) qs.set('size', String(params.size));

  const resp = await fetch(`/circle/manage/list?${qs.toString()}`, {
    headers: { satoken: token },
  });

  // HTTP 层分支
  if (resp.status === 401) {
    throw new AuthError(); // 跳登录
  }
  if (!resp.ok) {
    throw new Error(`managed circles request failed: ${resp.status}`);
  }

  const body: ManagedCircleListResult = await resp.json();
  if (body.code !== 200) {
    throw new Error(body.message);
  }
  // data 键可能缺失（空结果），必须兜底
  return { total: body.total, page: body.page, per_page: body.per_page, data: body.data ?? [] };
}
```

### 7.3 翻页与列表渲染要点

```typescript
const totalPages = Math.max(1, Math.ceil(total / perPage));

// 请求页码越界（如搜索后停留在旧的高页码）不会报错：
// 返回空 data + 正确 total。检测到 data 为空且 page > 1 时，建议回跳第一页重拉。
useEffect(() => {
  if (page > 1 && items.length === 0 && total > 0) {
    setPage(1); // 触发重拉
  }
}, [page, items.length, total]);
```

渲染每一项时：

- `status !== 1` → 整项置灰、禁用点击；
- 右上角角色徽标：`my_role === 30 ? '圈主' : '管理员'`；
- 代理配额位：`已绑定 {agent_count}/5`；
- `avatar_url` 为空用默认占位图。

---

## 八、常见问题

**Q1：为什么我刚把某个用户设为管理员，他的列表里马上就有这个圈了？**
本接口直查数据库、无任何缓存，角色变更即时可见（这正是它不同于 `/circle/my` 的设计点）。

**Q2：平台超管（users.role=1）能在这里看到全站圈子吗？**
不能。本接口严格返回「我是圈主/管理员」的圈子；超管的全站代理控制台走 `GET /agent/list`，两者互不相干。

**Q3：为什么搜索 `%` 或 `_` 搜出来一大堆/搜不到？**
它们按字面匹配（已转义）。搜不到就是确实没有含该字符的名称/简介，属预期行为。

**Q4：`member_count` 和圈子详情页对不上？**
列表用的是 DB 快照计数，详情页走 Redis 实时计数，可能有短暂偏差。控制台展示可接受；需要精确值时用 `GET /circle/detail/:id`。

**Q5：圈子级代理管理（Phase 2）上线后这个接口变了吗？**
字段契约保持兼容：`agent_count` 从恒 0 变为真实值；其余字段不变。前端现有接入无需改动。

---

## 附：相关接口

| 接口 | 用途 |
|---|---|
| `GET /circle/my` | 我加入的所有圈子（个人中心，非本场景） |
| `GET /circle/detail/:id` | 圈子详情（含实时统计 + 我的成员信息） |
| `GET /circle/members?circle_id=` | 圈子成员管理列表（admin+） |
| `GET /agent/list` | 平台超管的全局 AI 代理列表 |
