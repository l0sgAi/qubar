# API 接入文档：圈子级 AI 机器人管理（/circle/agent）

> **用途**：AI 代理管理控制台的「圈内机器人管理页」——圈主/管理员对**本圈**的 AI 机器人
> 进行增删改查（每圈上限 **5** 个）。这是「圈子级 AI 代理管理」特性的 Phase 2，
> 前置接口是 Phase 1 的 [GET /circle/manage/list](circle-manage-list-api.md)（圈子选择器，
> 其 `agent_count` 字段本期起返回真实值）。
>
> ⚠️ **本期圈内机器人不参与任何回复触发**（关键词/手动/@提及均不生效），
> 创建后仅作为配置资产存在；触发链路属后续版本。UI 请勿展示「立即回复」类入口。

---

## 一、接口概览

| 方法 | 路径 | 说明 | 圈内权限 |
|---|---|---|---|
| `POST` | `/circle/agent` | 创建圈内机器人 | admin+（20/30），共享 ≤5 配额 |
| `GET` | `/circle/agent/list` | 圈内机器人列表（offset 分页） | admin+ |
| `GET` | `/circle/agent/:id` | 机器人详情 | admin+ |
| `PUT` | `/circle/agent/:id` | 更新机器人（部分更新） | 运营字段 admin+；**凭据字段仅圈主** |
| `DELETE` | `/circle/agent/:id` | 软删机器人 | **仅圈主（30）** |
| `POST` | `/circle/agent/:id/reply/:postId` | 手动触发机器人回复 | **仅圈主（30）** |

通用约定：

| 项目 | 值 |
|---|---|
| 鉴权 | **需登录**，请求头 `satoken: <token>` |
| 路径前缀 | `/circle/agent`（直接挂在站点根路径下，无全局前缀） |
| 分页方式 | offset 分页（`page` / `size`，响应含 `total` / `page` / `per_page`） |
| 凭据安全 | `api_key` **任何接口不回显明文/密文**，只回掩码 `api_key_masked` + `has_api_key` |
| 与全局 `/agent/*` 的关系 | 两套控制台互不相通：全局机器人（平台超管 `users.role=1` 维护）ID 拿到这里查 → **404**；圈内机器人 ID 拿到 `/agent/:id` → **404**。跨作用域不存在性互不暴露 |

### 圈内权限模型（与 `/circle/manage` 系列同构）

| 操作 | 圈主 (30) | 管理员 (20) |
|---|---|---|
| 列表 / 详情 | ✅ | ✅ |
| 创建 | ✅ | ✅ |
| 更新运营字段（name / avatar_url / model / llm_params / system_prompt / filter_prompt / trigger_mode / trigger_keywords / max_replies_per_hour / min_interval_sec / status） | ✅ | ✅ |
| 更新**凭据字段**（api_protocol / base_url / api_key） | ✅ | ❌ 403 |
| 删除 | ✅ | ❌ 403 |

- 权限每次**直查成员记录**（无缓存）：任免管理员 / 转让圈主 / 禁言后**下一次请求即生效**。
- 操作者须为圈内**正常状态**成员：被禁言/拉黑/待审/已退出的圈主/管理员管理权暂停（403），解禁自动恢复。
- 圈子被封禁（`status=2`）**不阻断**圈内机器人管理（圈主需要能停用/删除机器人止血）。
- 平台超管（`users.role=1`）对圈内机器人**无任何读写特权**，其控制台仍是 `/agent/*`。

---

## 二、字段定义（AgentVO）

所有接口返回的机器人对象（`data`）结构一致：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string (UUID) | 机器人 ID |
| `name` | string | 机器人名称，**圈内唯一**（不同圈可重名；删除后名称可复用）。长度 1-50（按 UTF-8 字节计，中文约 16 个字） |
| `avatar_url` | string | 头像，可为空/缺失（omitempty） |
| `linked_user_id` | string (UUID) | 机器人关联系统用户 ID（发评论身份，本期不发言） |
| `circle_id` | string (UUID) | **归属圈子 ID**（本组接口恒返回；全局接口 `/agent/*` 的对象无此字段） |
| `api_protocol` | string | API 协议：本期仅 `openai` / `anthropic` |
| `base_url` | string | API 基础地址，可为空/缺失。非空时必须是**公网 https** 地址：内网 / 环回 / 链路本地（含云元数据）IP、解析到这些地址的域名、带 userinfo 的 URL 一律 400 `base_url must be a public https URL`（防 SSRF，#48）。保存时 DNS 临时故障（超时 / SERVFAIL）返回 503 `base_url host could not be resolved right now, please retry`，可重试 |
| `has_api_key` | boolean | 是否已配置 API key |
| `api_key_masked` | string | 掩码（如 `sk-***abc`）；未配置时缺失 |
| `model` | string | 模型名（1-100 字符） |
| `llm_params` | object | LLM 参数（白名单键：temperature/top_p/max_tokens/presence_penalty/frequency_penalty，值必须为数字） |
| `system_prompt` | string | 系统提示词，可为空/缺失 |
| `filter_prompt` | string | 回复判定条件（≤2000 字符），可为空/缺失 |
| `trigger_mode` | number | 1=全部新帖（本期不生效）/ 2=关键词 / 3=手动 |
| `trigger_keywords` | string[] | 关键词列表；mode=2 时必须非空 |
| `max_replies_per_hour` | number | 每小时回复上限，0=不限 |
| `min_interval_sec` | number | 最小回复间隔秒，0=不限 |
| `status` | number | 0=停用 / 1=启用（创建时不传默认启用） |
| `create_time` / `update_time` | string | RFC3339 带时区 |

---

## 三、各接口明细

### 3.1 创建机器人 `POST /circle/agent`

请求体（`circle_id` 必填，其余同全局 agent 字段）：

```json
{
  "circle_id": "0192a0d0-0000-7000-8000-0000000000aa",
  "name": "小助手",
  "api_protocol": "openai",
  "base_url": "https://api.openai.com/v1",
  "api_key": "sk-xxxx",
  "model": "gpt-4o",
  "llm_params": { "temperature": 0.7 },
  "system_prompt": "你是本圈的友好助手",
  "trigger_mode": 2,
  "trigger_keywords": ["助手"],
  "max_replies_per_hour": 10,
  "min_interval_sec": 60,
  "status": 1
}
```

成功：HTTP **200**（信封 `code=200`，message `Created successfully`），`data` 为创建后的完整对象。
服务端校验失败（名称/协议/模型/参数等）→ 400，详见 §四。

```bash
curl -X POST -H "satoken: <token>" -H "Content-Type: application/json" \
  -d '{"circle_id":"0192a0d0-…","name":"小助手","api_protocol":"openai","model":"gpt-4o"}' \
  "http://<host>:8888/circle/agent"
```

### 3.2 列表 `GET /circle/agent/list`

| Query 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `circle_id` | UUID | **是** | — | 圈子 ID（来自 `/circle/manage/list`），非法 UUID → 400 |
| `keyword` | string | 否 | 空 | 按机器人 `name` **子串**过滤，大小写不敏感 |
| `page` | int | 否 | 1 | `<=0` 规整为 1 |
| `size` | int | 否 | 20 | `<=0` 或 `>100` 回落 20 |

成功响应（分页信封，形同 `/circle/manage/list`）：

```json
{
  "code": 200,
  "message": "Success",
  "total": 2,
  "page": 1,
  "per_page": 20,
  "data": [ { "id": "…", "name": "小助手", "circle_id": "…", "…": "…" } ]
}
```

> 空列表时 `data` 键整体缺失（不是 `[]`），前端用 `body.data ?? []` 兜底。

### 3.3 详情 `GET /circle/agent/:id`

`data` 为单个机器人对象（字段见 §二）。机器人不存在 / 属于其他圈 / 是全局机器人 → **404**（不区分原因）。

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "id": "0192a0d0-0000-7000-8000-0000000000c1",
    "name": "小助手",
    "avatar_url": "https://cdn.example.com/bot.png",
    "linked_user_id": "0192a0d0-0000-7000-8000-0000000000d1",
    "circle_id": "0192a0d0-0000-7000-8000-0000000000aa",
    "api_protocol": "openai",
    "base_url": "https://api.openai.com/v1",
    "has_api_key": true,
    "api_key_masked": "sk-**abcd",
    "model": "gpt-4o",
    "llm_params": { "temperature": 0.7 },
    "system_prompt": "你是本圈的友好助手",
    "trigger_mode": 2,
    "trigger_keywords": ["助手"],
    "max_replies_per_hour": 10,
    "min_interval_sec": 60,
    "status": 1,
    "create_time": "2026-08-31T10:30:00.123456+08:00",
    "update_time": "2026-08-31T10:30:00.123456+08:00"
  }
}
```

> 可选字符串字段（`avatar_url` / `base_url` / `system_prompt` / `filter_prompt` / `api_key_masked`）
> 为空时**键缺失**（omitempty），前端读取时用 `?? ''` 兜底。

### 3.4 更新 `PUT /circle/agent/:id`

部分更新：只传需要修改的字段（指针语义，未传字段不动；**全部字段都不传 → 400** `at least one field to update`）。

```json
{ "name": "新名字", "llm_params": { "temperature": 0.5 }, "status": 0 }
```

- 管理员**可以**只传运营字段；
- 请求体中**任一**凭据字段（`api_protocol` / `base_url` / `api_key`）非空即整体要求圈主——管理员混提（哪怕主要改 name）也会 403，前端应按表单分组控制；
- `api_key` 传**空串** = 清除 key；传非空串 = 换 key（服务端加密存储，响应仍只回掩码）。

### 3.5 删除 `DELETE /circle/agent/:id`

软删（停用 + 标记删除，名称随即释放可复用）。成功：`{ "code": 200, "message": "Deleted successfully" }`（无 `data`）。仅圈主。

---

### 3.6 手动触发回复 `POST /circle/agent/:id/reply/:postId`

让圈内机器人立即对指定帖子生成一条回复评论（同步执行，等待 LLM 返回）。仅圈主、仅 `trigger_mode=3`（手动）且启用中的机器人、**帖子必须属于机器人所在圈**（跨圈 404，防拿本圈机器人刷它圈帖）。成功：`{ "code": 200, "message": "回复成功", "data": "<评论 uuid>" }`。

自动触发行为（本版本起生效）：本圈帖的**评论关键词触发**（`trigger_mode=2`）与**发帖 @机器人**（机器人被 @ 即触发，不校验 mode）对圈内机器人按圈生效——机器人只回本圈帖子；全局机器人行为不变（全站触发）。限流配置 `max_replies_per_hour` / `min_interval_sec` 按机器人维度照常生效；**创建时未传这两字段会兜底为 30 次/时 + 60 秒间隔**（防圈主 key 裸奔），需要不限速请创建后经更新接口显式设 0。

---

## 四、错误处理

HTTP 状态码与业务码两套：**先按 HTTP 分支，再校验 `code === 200`**。

| 场景 | HTTP | `code` | `message` 示例 |
|---|---|---|---|
| 成功 | 200 | 200 | `Success` / `Created successfully` / `Deleted successfully` |
| 未登录 / token 失效 | 401 | 202 | `Token not found` / `Invalid or expired token` |
| body/query 非法（circle_id 非 UUID、JSON 解析失败、字段校验不过） | 400 | 201 | `Invalid circle id` / `agent name must be 1-50 chars` / …（校验类 message 直接可展示） |
| 非该圈 admin+（member/非成员/被禁言的管理者） | 403 | 203 | `Circle admin privileges required` |
| 凭据字段或删除非圈主 | 403 | 203 | `Circle owner privileges required` |
| 机器人不存在 / 跨作用域访问 | 404 | 204 | `Agent not found` |
| 每圈超过 5 个 | 409 | 207 | `Circle agent limit reached (5)` |
| 圈内名称冲突 | 409 | 207 | `Agent name already exists in this circle` |
| 服务端未配置加密密钥（自部署环境） | 503 | 212 | `Data key not configured` |
| 服务端异常 | 500 | 210 | `Internal server error` |

> 前端权限兜底：以 `/circle/manage/list` 返回的 `my_role` 控制入口显隐（20 隐藏凭据表单与删除按钮），但**服务端校验才是准**，403 时 toast 提示即可。

---

## 五、前端接入代码（TypeScript）

### 5.1 类型

```typescript
export type AgentApiProtocol = 'openai' | 'anthropic';
export type AgentTriggerMode = 1 | 2 | 3; // 1=全部新帖(本期不生效) 2=关键词 3=手动

export interface CircleAgent {
  id: string;
  name: string;
  avatar_url?: string;
  linked_user_id: string;
  circle_id: string;
  api_protocol: AgentApiProtocol;
  base_url?: string;
  has_api_key: boolean;
  api_key_masked?: string;
  model: string;
  llm_params: Record<string, number>;
  system_prompt?: string;
  filter_prompt?: string;
  trigger_mode: AgentTriggerMode;
  trigger_keywords: string[];
  max_replies_per_hour: number;
  min_interval_sec: number;
  status: 0 | 1;
  create_time: string;
  update_time: string;
}

export interface CircleAgentListResult {
  code: number;
  message: string;
  total?: number;
  page?: number;
  per_page?: number;
  /** 空结果时该键缺失，读取时用 ?? [] 兜底 */
  data?: CircleAgent[];
}

/** 创建入参（api_key 仅创建/更新请求可传，响应永不回显） */
export interface CreateCircleAgentInput {
  circle_id: string;
  name: string;
  avatar_url?: string;
  api_protocol: AgentApiProtocol;
  base_url?: string;
  api_key?: string;
  model: string;
  llm_params?: Record<string, number>;
  system_prompt?: string;
  filter_prompt?: string;
  trigger_mode?: AgentTriggerMode;
  trigger_keywords?: string[];
  max_replies_per_hour?: number;
  min_interval_sec?: number;
  status?: 0 | 1;
}

/** 更新入参：全部可选（部分更新；至少传一个字段） */
export type UpdateCircleAgentInput = Partial<Omit<CreateCircleAgentInput, 'circle_id'>>;
```

### 5.2 请求封装

```typescript
const base = '/circle/agent';

function authHeaders(token: string): HeadersInit {
  return { satoken: token, 'Content-Type': 'application/json' };
}

export async function listCircleAgents(params: {
  circleId: string; keyword?: string; page?: number; size?: number;
}, token: string): Promise<{ total: number; page: number; perPage: number; items: CircleAgent[] }> {
  const qs = new URLSearchParams({ circle_id: params.circleId });
  if (params.keyword) qs.set('keyword', params.keyword);
  if (params.page) qs.set('page', String(params.page));
  if (params.size) qs.set('size', String(params.size));

  const resp = await fetch(`${base}/list?${qs}`, { headers: { satoken: token } });
  if (resp.status === 401) throw new AuthError();
  if (!resp.ok) throw new Error(`list circle agents failed: ${resp.status}`);

  const body: CircleAgentListResult = await resp.json();
  if (body.code !== 200) throw new Error(body.message);
  return {
    total: body.total ?? 0,
    page: body.page ?? 1,
    perPage: body.per_page ?? 20,
    items: body.data ?? [], // 空结果 data 键缺失，必须兜底
  };
}

export async function createCircleAgent(input: CreateCircleAgentInput, token: string): Promise<CircleAgent> {
  const resp = await fetch(base, { method: 'POST', headers: authHeaders(token), body: JSON.stringify(input) });
  return unwrap(resp);
}

export async function updateCircleAgent(id: string, input: UpdateCircleAgentInput, token: string): Promise<CircleAgent> {
  const resp = await fetch(`${base}/${id}`, { method: 'PUT', headers: authHeaders(token), body: JSON.stringify(input) });
  return unwrap(resp);
}

export async function deleteCircleAgent(id: string, token: string): Promise<void> {
  const resp = await fetch(`${base}/${id}`, { method: 'DELETE', headers: authHeaders(token) });
  await unwrap(resp);
}

async function unwrap(resp: Response): Promise<CircleAgent> {
  if (resp.status === 401) throw new AuthError();
  const body = await resp.json();
  // HTTP 4xx/5xx 时服务端已给出 {code, message}，直接抛给上层 toast
  if (!resp.ok || body.code !== 200) {
    throw new ApiError(resp.status, body.code as number, body.message as string);
  }
  return body.data as CircleAgent;
}

export class ApiError extends Error {
  constructor(public httpStatus: number, public code: number, message: string) { super(message); }
}
export class AuthError extends Error {}
```

### 5.3 交互要点

- **配额 UI**：先查列表拿 `total`（或用 `/circle/manage/list` 的 `agent_count`），`total >= 5` 时禁用「新建」按钮，展示「已绑定 {n}/5」；
- **删除确认**：删除不可恢复（软删但无恢复入口），弹确认框；删除后刷新列表（名称已释放）；
- **凭据表单隔离**：`my_role === 20` 时隐藏 api_protocol/base_url/api_key 表单组；owner 编辑时 api_key 输入框留空 = 不修改；
- **trigger_mode=2** 时必须校验 `trigger_keywords` 非空（服务端也会 400）；
- 本期机器人**不会真实回复**，详情页可放置「回复能力将在后续版本开放」的占位提示。

---

## 六、常见问题

**Q1：创建成功为什么 HTTP 状态是 200 而不是 201？**
本服务统一信封 `code=200` 表成功，HTTP 层恒 200；创建/更新/删除以 `message`（Created/Deleted successfully）与 `data` 有无区分。

**Q2：两个圈都想叫「小助手」可以吗？**
可以。名称唯一性是**圈内**作用域（与全局机器人、其他圈互不冲突）；同圈内重名 → 409。

**Q3：管理员为什么改不了 base_url？**
凭据字段（api_protocol/base_url/api_key）是计费凭据，仅圈主持有（403 `Circle owner privileges required`）。这是设计约束不是 bug。

**Q4：刚被撤销管理员的用户还能调用列表吗？**
权限每次直查成员记录，下一次请求即 403（无缓存延迟）。

**Q5：机器人创建后会自动回复圈内帖子吗？**
**会（本版本起）**：本圈帖的评论关键词触发（mode=2）与发帖 @机器人 自动生效，机器人只回**本圈**帖子；圈主还可在管理界面手动触发（`POST /circle/agent/:id/reply/:postId`）。创建时限流兜底 30 次/时 + 60 秒间隔，防止圈主的 API key 被刷爆。

**Q6：`agent_count` 在哪里看？**
`GET /circle/manage/list` 的列表项字段（本期起为真实值，上限 5），本组接口不重复提供计数。

---

## 附：相关接口

| 接口 | 用途 |
|---|---|
| `GET /circle/manage/list` | 圈子选择器（Phase 1，`agent_count` 已回填真实值） |
| `GET /circle/members?circle_id=` | 成员管理列表（圈主找管理员/转让目标） |
| `POST /circle/manage/role` | 任免管理员 |
| `GET/POST/PUT/DELETE /agent/*` | 平台超管的全局机器人控制台（与本期互不相通） |
