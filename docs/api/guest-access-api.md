# 访客模式 API 对接文档（前端迁移指南）

> **背景**：后端已开放 15 个接口给未登录访客（anonymous）。本文档说明前端需要做的对接调整。
> **对应实现**：分支 `feature/guest-auth-refactor-20260718`，详见 [guest-access-analysis.md](guest-access-analysis.md)。

---

## 0. TL;DR —— 前端三件事

1. **新增「访客可读」接口清单**：以下 15 个接口**未登录也能正常调用**，不再返回 401。
2. **统一鉴权头处理**：所有接口都通过 `satoken: <token>` 请求头传 token；访客请求**不带该头**即可。
3. **理解「登录态降级字段」**：部分接口在匿名访问时，个性化字段（`is_liked`/`is_collected`/`is_joined`）会返回 `false`，浏览历史不会记录——这是**预期行为**，不是 bug。

---

## 1. 鉴权机制变更说明

### 1.1 三类接口

后端接口按鉴权要求分三类：

| 类别 | 鉴权要求 | token 处理 | 未带 token 行为 |
|---|---|---|---|
| **登录必需**（写操作/个人数据） | 强制登录 | 必须带有效 `satoken` | **返回 401** |
| **可选登录**（本次新增） | 访客可读 | 建议带（提升体验） | **静默放行**，按匿名处理 |
| **完全公开**（登录注册流） | 无鉴权 | 不需要 | 直接放行 |

### 1.2 「可选登录」的语义（重要）

对**可选登录**接口，后端的处理逻辑：

```
请求进来
  ├─ 有 token 且有效   → 当作登录用户，回填个性化字段（is_liked/is_collected/is_joined 等）
  ├─ 无 token          → 当作访客，个性化字段返回 false，不记录浏览历史
  └─ 有 token 但无效   → 静默当作访客（不返回 401，避免 token 过期打乱访客浏览体验）
```

**前端含义**：
- 同一个接口，登录/未登录用户都能调，**响应结构完全一致**，只是个别字段值不同。
- 不需要为访客单独写一套 API 调用代码。
- token 过期时，**可选登录接口不会触发 401 拦截器**——前端可继续展示访客内容，或按业务逻辑决定是否跳登录。

### 1.3 请求头

所有需鉴权的接口统一用 sa-token：

```
satoken: <登录成功后返回的 token 字符串>
```

访客请求**不要带**这个头（带了无效 token 也 OK，会被静默当访客）。

---

## 2. 访客可读接口清单（本次新增 15 个）

> 以下接口在改造前都返回 401，现在访客可正常调用。

### 2.1 纯只读（B 级，handler 不读用户身份）

这些接口对访客和登录用户**完全无差别**——响应内容不依赖登录态。

| 接口 | 方法 | 说明 | 访客与登录差异 |
|---|---|---|---|
| `/category/get` | GET | 分类列表 | 无差异 |
| `/user/search` | GET | 搜索用户 | 无差异 |
| `/user/detail/:id` | GET | 用户公开主页 | 无差异 |
| `/circle/list` | GET | 搜索圈子 | 无差异 |
| `/circle/active` | GET | 近期活跃圈子榜 | 无差异 |
| `/circle/user` | GET | 指定用户加入的圈子 | 无差异 |
| `/circle/posts` | GET | 圈内帖子列表 | 无差异 |
| `/post/list` | GET | 搜索帖子 | 无差异 |

**前端无需特殊处理**——直接去掉这些接口调用前的登录拦截即可。

### 2.2 带个性化字段的只读（C 级，登录态降级）

这些接口访客可读，但**个性化字段在匿名时降级为 `false`**：

| 接口 | 方法 | 匿名时降级的字段 | 登录时的额外副作用 |
|---|---|---|---|
| `/circle/detail/:id` | GET | `is_joined`、`member_role`、`member_status` 等成员字段 → `false`/零值 | — |
| `/post/detail/:id` | GET | `is_liked`、`is_collected` → `false` | 登录用户会记录浏览历史、增加浏览量 |
| `/comment/list` | GET | 每条评论的 `is_liked` → `false` | — |
| `/comment/replies` | GET | 每条回复的 `is_liked` → `false` | — |
| `/comment/detail/:id` | GET | `is_liked` → `false` | — |
| `/trending` | GET | 帖子的 `is_liked`、`is_collected` → `false` | — |
| `/post/home?tab=hot` | GET | 帖子的 `is_liked`、`is_collected` → `false` | — |
| `/post/home?tab=latest` | GET | 同上 | — |

**前端建议**：
- 访客态展示时，点赞/收藏按钮要么隐藏、要么点击后引导登录（详见 §4）。
- 浏览历史对访客不记录——这是**后端有意为之**，前端无需处理。

### 2.3 `/post/home` 的特殊 tab 行为

`/post/home` 共 4 个 tab，访客策略不同：

| tab | 访客访问 | 说明 |
|---|---|---|
| `hot` | ✅ 允许 | 全局热度流 |
| `latest` | ✅ 允许 | 全局最新流 |
| `recommend` | ❌ 返回 401 | 依赖用户行为池（点赞/收藏/浏览历史），访客无数据源 |
| `following` | ❌ 返回 401 | 依赖「已加入圈子」列表，访客无此数据 |

**401 响应示例**（仅 `recommend`/`following` tab 访客访问时）：

```json
{
  "code": 401,
  "message": "This feed tab requires login"
}
```

**前端建议**：
- 访客态首页默认展示 `hot` 或 `latest` tab。
- `recommend`/`following` tab 若要展示，建议置灰 + 「登录后查看」提示；点击触发登录。
- 登录后自动切换到 `recommend` tab（原行为）。

---

## 3. 仍需登录的接口（保持 401 行为）

以下接口**未对访客开放**，前端原拦截逻辑保留：

| 接口 | 原因 |
|---|---|
| `GET /user/get`、`PUT /user/update` | 个人资料读写 |
| `POST /post/create`、`GET /post/my` | 发帖（状态由服务端决定，暂不支持草稿）、个人帖子列表 |
| `POST /circle/create`、`/join`、`/leave`、`GET /circle/my` | 圈子写操作、我的圈子 |
| `POST /comment/create` | 发评论 |
| `POST /like/toggle`、`POST /collect/toggle`、`GET /collect/posts` | 点赞/收藏写操作、我的收藏 |
| `GET /history/posts` | 个人浏览历史 |
| `POST /upload/*`（全部） | 文件上传（防滥用） |
| `POST /auth/logout` | 登出 |

这些接口的 401 响应不变，前端全局 401 拦截器继续生效。

---

## 4. 前端改造建议

### 4.1 全局请求拦截器调整

**问题**：原有前端通常有「无 token → 拦截请求 + 跳登录页」的逻辑。现在部分接口允许无 token。

**方案**：维护一份「访客可读接口白名单」（或反过来，「需登录接口黑名单」），在请求拦截器里：

```js
// 伪代码
const GUEST_ACCESSIBLE = [
  'GET /category/get',
  'GET /user/search',
  'GET /user/detail',  // 注意是 /user/detail/:id，按需匹配
  'GET /circle/list',
  'GET /circle/active',
  'GET /circle/user',
  'GET /circle/posts',
  'GET /circle/detail',  // /circle/detail/:id
  'GET /post/list',
  'GET /post/user',      // /post/user/:user_id
  'GET /post/detail',    // /post/detail/:id
  'GET /post/home',      // 仅 tab=hot|latest 真正可读；recommend/following 会 401
  'GET /comment/list',
  'GET /comment/replies',
  'GET /comment/detail', // /comment/detail/:id
  'GET /trending',
  'GET /discover',
]

function beforeRequest(config) {
  const key = `${config.method.toUpperCase()} ${config.url}`
  if (!token && !isGuestAccessible(key)) {
    // 需登录但未登录 → 跳登录
    redirectToLogin()
    return
  }
  if (token) config.headers['satoken'] = token
  // 访客可读接口不带 token 也 OK
}
```

### 4.2 全局 401 响应拦截器调整

**关键**：`/post/home?tab=recommend|following` 在访客访问时也会返回 401。需要区分两种 401：

- **登录态过期**（调用需登录接口返回 401）→ 清 token + 跳登录
- **tab 不支持访客**（调用 `/post/home` 返回 401，message = `"This feed tab requires login"`）→ 不清 token，按业务降级（如切到 hot tab）

```js
function onResponseError(error) {
  if (error.response?.status === 401) {
    const msg = error.response.data?.message
    if (msg === 'This feed tab requires login') {
      // /post/home 的 tab 限制，不清 token，交给调用方处理
      return Promise.reject(error)
    }
    // 真正的鉴权失败 → 清 token + 跳登录
    clearToken()
    redirectToLogin()
  }
  return Promise.reject(error)
}
```

### 4.3 个性化字段的访客态 UI

对于 §2.2 的接口，访客看到的内容里 `is_liked`/`is_collected`/`is_joined` 都是 `false`。前端 UI 建议：

| 元素 | 访客态处理 |
|---|---|
| 帖子/评论的点赞按钮 | 显示未点赞状态；点击 → 弹登录引导（不要直接调 `/like/toggle`，会 401） |
| 帖子的收藏按钮 | 同上 |
| 帖子详情的「加入圈子」按钮 | 显示「加入圈子」；点击 → 弹登录引导 |
| 评论输入框 | 隐藏或置灰，提示「登录后评论」 |

**判断访客**：前端用「本地是否有 token」判断，**不要**依赖响应里的 `is_liked === false`（因为登录用户也可能确实没点赞）。

### 4.4 路由守卫调整

建议把以下页面从「需登录路由」改为「访客可访问路由」：

- 首页（`/`）→ 默认 `hot` tab
- 帖子详情页（`/post/:id`）
- 圈子详情页（`/circle/:id`）
- 圈子列表/搜索页
- 用户公开主页（`/user/:id`）
- 热点页（`/trending`）
- 发现页（`/discover`，本来就是）

**保留为「需登录路由」**：
- 发帖/发评论页
- 个人资料/设置页
- 我的帖子/圈子/收藏/历史
- 关注流（`/post/home?tab=following`）

---

## 5. 典型场景示例

### 5.1 访客浏览帖子详情

```http
GET /post/detail/019632a4-xxxx-xxxx-xxxx-xxxxxxxxxxxx
# 不带 satoken 头
```

响应（与登录用户结构完全一致，只是 `is_liked`/`is_collected` 为 false）：

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "id": "019632a4-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "title": "...",
    "content": "...",
    "view_count": 1024,
    "like_count": 56,
    "collect_count": 12,
    "comment_count": 8,
    "is_liked": false,
    "is_collected": false,
    "author_id": "...",
    "author_name": "...",
    "author_avatar": "...",
    "circle_id": "...",
    "create_time": "2026-07-15T10:30:00Z",
    "..."
  }
}
```

注意：**访客访问不会增加 `view_count`**（后端有意为之，避免无意义计数与历史污染）。

### 5.2 访客访问 `/post/home?tab=recommend`

```http
GET /post/home?tab=recommend
# 不带 satoken 头
```

响应（401）：

```json
{
  "code": 401,
  "message": "This feed tab requires login"
}
```

前端应捕获该 401，自动切到 `tab=hot` 或弹登录引导。

### 5.3 登录用户带 token 访问访客可读接口

```http
GET /post/list?size=20
satoken: eyJhbGciOi...
```

响应与访客一致，但如果该用户对某些帖子点过赞，未来扩展字段会带 `is_liked: true`（当前 `/post/list` 不回填该字段，仅 `/post/detail` 回填——这是原有行为，未变更）。

---

## 6. 字段对照速查（C 级接口）

| 接口 | 字段 | 登录用户 | 访客 | 备注 |
|---|---|---|---|---|
| `/post/detail/:id` | `is_liked` | 真实状态 | `false` | |
| `/post/detail/:id` | `is_collected` | 真实状态 | `false` | |
| `/post/detail/:id` | view_count 计数 | 会 +1 | 不变 | 后端跳过异步计数 |
| `/post/detail/:id` | 浏览历史 | 会记录 | 不记录 | |
| `/circle/detail/:id` | `is_joined` | 真实状态 | `false` | |
| `/circle/detail/:id` | `member_role` | 真实角色 | `0` | |
| `/circle/detail/:id` | `member_status` | 真实状态 | `0` | |
| `/comment/*` | `is_liked`（每条） | 真实状态 | `false` | |
| `/trending` | `is_liked`/`is_collected`（帖子） | 真实状态 | `false` | |
| `/post/home?tab=hot\|latest` | `is_liked`/`is_collected` | 真实状态 | `false` | |

---

## 7. 错误码参考

标准响应信封：

```json
{ "code": <int>, "message": <string>, "data": <any|null> }
```

| HTTP 状态 | code | message | 触发场景（访客相关） |
|---|---|---|---|
| 200 | 200 | `"Success"` | 访客正常访问可读接口 |
| 401 | 401 | `"This feed tab requires login"` | **仅** `/post/home?tab=recommend\|following` 访客访问 |
| 401 | 401 | `"Token not found"` / `"Invalid or expired token"` | 需登录接口未带/带失效 token（原有行为，未变） |

> 其他错误码（400/404/500 等）与登录态无关，未在本轮变更。

---

## 8. 测试 checklist（前端自测）

- [ ] 不带 token 访问 `/category/get`、`/user/search`、`/post/list` → 200
- [ ] 不带 token 访问 `/post/detail/:id` → 200，`is_liked=false`、`is_collected=false`
- [ ] 不带 token 访问 `/post/home?tab=hot` → 200
- [ ] 不带 token 访问 `/post/home?tab=recommend` → 401 `This feed tab requires login`
- [ ] 不带 token 访问 `/post/home?tab=following` → 401 `This feed tab requires login`
- [ ] 不带 token 访问 `/trending` → 200
- [ ] 不带 token 访问 `/comment/list` → 200，评论的 `is_liked=false`
- [ ] 带过期 token 访问 `/post/list` → 200（静默当访客，不 401）
- [ ] 带过期 token 访问 `/post/create` → 401（需登录接口原行为）
- [ ] 登录后访问 `/post/detail/:id` 两次 → 第二次 `view_count` +1（验证计数未因访客路径回归）

---

## 9. 相关文档

- [guest-access-analysis.md](guest-access-analysis.md) —— 完整可行性分析报告（B/C/D 分级、风险评估）
- [discover-api.md](discover-api.md) —— 发现页 API（原本就是访客可读，是本次改造的范式蓝本）
- [home-feed-api.md](home-feed-api.md) —— 首页信息流 API（`/post/home` 完整说明）
- [trending-api.md](trending-api.md) —— 热点看板 API
