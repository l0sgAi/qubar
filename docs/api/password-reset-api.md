# 找回密码 API 对接文档

> 对应后端领域：`auth`（登录/注册/OAuth 共用，本接口属于「找回密码」子流程）。
> 本文档供前端对接「忘记密码 / 重置密码」页面使用。

---

## 0. 需求背景

### 0.1 这是什么

当用户忘记密码无法登录时，通过**邮箱验证码**完成身份核验并重置密码。整个流程是**注册流程的镜像**：

| 流程 | 前置条件 | 邮箱状态 |
|---|---|---|
| 注册 | 邮箱**未注册** | 验证码发送 → 标记已验证 → 创建账号 |
| **找回密码** | 邮箱**已注册** | 验证码发送 → 标记已验证 → 覆盖密码 |

### 0.2 三步流程

```
① 发送验证码        ② 校验验证码         ③ 重置密码
┌──────────────┐   ┌──────────────┐   ┌──────────────────┐
│ 用户输入邮箱  │──▶│ 用户输入邮箱  │──▶│ 用户输入新密码    │
│              │   │ + 收到的验证码│   │                  │
└──────────────┘   └──────────────┘   └──────────────────┘
   ↓ POST send-code   ↓ POST verify       ↓ POST reset
   邮箱必须已注册     标记 10min 有效      改密码 + 踢下线所有设备
```

**重置成功后不会自动登录**（出于安全考虑），用户需要用新密码再走一次 `/auth/login`。

### 0.3 关键安全行为

| 行为 | 说明 |
|---|---|
| 邮箱不存在 → 404 | 与注册端点（邮箱已存在 → 409）行为对齐，前端应正确处理 |
| 频率限制 60s | 同一邮箱 60 秒内只能请求一次验证码，重复请求返回 429 |
| 验证码有效期 5min | 过期后需重新发送 |
| 已验证标记有效期 10min | 校验验证码成功后，必须在 10 分钟内完成重置，否则需重新校验 |
| 重置成功踢下线所有设备 | 旧 token 全部失效，用户需要用新密码重新登录所有设备 |
| 仅正常账号可重置 | 被禁用账号（status≠1）返回 403 |

---

## 1. 端点

三个端点**均无需登录**（公开组），挂在 `/auth/password` 前缀下：

| # | 方法 | 路径 | 说明 |
|---|---|---|---|
| ① | POST | `/auth/password/send-code` | 发送找回密码验证码到邮箱 |
| ② | POST | `/auth/password/verify` | 校验验证码，标记该邮箱「已验证」 |
| ③ | POST | `/auth/password/reset` | 校验已验证标记 → 更新密码 → 踢下线 |

> **全局前缀**：本服务**无全局 `/api` 前缀**，路径从根开始（如 `https://qubar.site/auth/password/send-code`）。

> **请求头**：`Content-Type: application/json`。找回密码三个端点均**不需要** `satoken` 头（重置后用户需重新登录，见 §6）。

---

## 2. 通用约定

### 2.1 标准响应壳（全站一致）

```jsonc
{
  "code": 200,            // 业务码：200=成功，其余为错误
  "message": "Success",
  "data": null            // 找回密码三个端点的 data 均为 null（成功时不返回业务数据）
}
```

> 成功响应的 `data` 一律为 `null`——三个端点都是「触发动作」型，无返回数据。
> 重置成功时 `message` 为 `"Password reset successful"`，其余成功响应为 `"Success"`。

### 2.2 错误响应

```jsonc
{
  "code": 404,            // 见下方错误码表
  "message": "Account not found"
  // data 字段错误时省略
}
```

### 2.3 业务码与 HTTP 状态码映射

| 业务码 `code` | HTTP 状态 | 含义 | 本流程触发场景 |
|---|---|---|---|
| `200` | 200 | 成功 | 操作成功 |
| `400` | 400 | 请求参数错误 | 邮箱格式不合法 / 验证码错误 / 验证码已过期 / 新密码过短 / 缺少必填字段 |
| `403` | 403 | 禁止 | 账号已被禁用（status≠1） |
| `404` | 404 | 资源不存在 | 邮箱未注册（send-code 与 reset 都可能返回） |
| `429` | 429 | 请求过多 | 60 秒内重复请求发送验证码 |
| `500` | 500 | 服务器错误 | 邮件服务不可用 / 其他未知错误 |

> ⚠️ 前端**不应**用 HTTP status 判断业务结果，应统一读响应体的 `code` 字段。HTTP status 仅作为传输层兜底。

---

## 3. 端点 ①：发送验证码

### 请求

```
POST /auth/password/send-code
Content-Type: application/json
```

```jsonc
{
  "email": "user@example.com",   // 必填，合法邮箱地址
  "lang":  "zh"                  // 可选，"zh" | "en"，默认按 "en" 处理
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `email` | string | ✅ | 合法邮箱；后端用 `net/mail.ParseAddress` 校验，不合法返回 400 |
| `lang` | string | ❌ | 邮件语言：`"zh"` 中文模板 / `"en"` 英文模板；其他值或缺省 → 英文 |

### 成功响应

```
200 OK
```
```jsonc
{ "code": 200, "message": "Success", "data": null }
```

此时邮件已发送到用户邮箱。邮件内容是一封 6 位数字验证码（样式见 `docs/email_password_reset_template.html`）。

### 错误响应

| `code` | `message` | 触发条件 | 前端建议处理 |
|---|---|---|---|
| `400` | `Invalid email format` | 邮箱格式不合法 | 输入框标红提示「邮箱格式不正确」 |
| `404` | `Account not found` | 邮箱未注册 | 提示「该邮箱未注册，请检查或先注册」 |
| `429` | `Rate limit exceeded` | 60s 内重复请求 | 提示「验证码已发送，请 X 秒后重试」并启动倒计时（建议 60s） |
| `500` | `Internal error` | 邮件服务不可用等 | 提示「服务暂时不可用，请稍后重试」 |

### 前端实现建议

- **发送按钮倒计时**：点击后进入 60 秒倒计时禁用态（即使后端没返回 429，也主动限制，提升体验）
- **404 的特殊处理**：是否提示「该邮箱未注册」是一个产品决策（暴露账号存在性）。当前后端选择返回 404（与注册端点返回 409 的行为对齐，攻击者用注册端点就能枚举邮箱，找回端点不再增加攻击面）
- **`lang` 来源**：取当前 UI 语言。中文站传 `"zh"`，英文站传 `"en"`

---

## 4. 端点 ②：校验验证码

### 请求

```
POST /auth/password/verify
Content-Type: application/json
```

```jsonc
{
  "email": "user@example.com",   // 必填
  "code":  "123456"              // 必填，6 位数字验证码
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `email` | string | ✅ | 与发送验证码时一致的邮箱 |
| `code` | string | ✅ | 用户从邮件中收到的 6 位数字验证码（字符串） |

### 成功响应

```
200 OK
```
```jsonc
{ "code": 200, "message": "Success", "data": null }
```

校验成功后，后端在 Redis 写入一条 `pwd_reset:verified:{email}` 标记，**有效期 10 分钟**。前端应立即引导用户进入「设置新密码」步骤。

### 错误响应

| `code` | `message` | 触发条件 | 前端建议处理 |
|---|---|---|---|
| `400` | `Verification code expired` | 验证码不存在（从未发送 / 已过期 5min / 已被使用过） | 提示「验证码已过期，请重新获取」并引导回 ① |
| `400` | `Invalid verification code` | 验证码不匹配 | 提示「验证码错误」并清空输入框 |
| `400` | `Missing required parameter` | 缺 `email` 或 `code` 字段 | 表单校验兜底 |

> 注意：Redis 读验证码失败（如 Redis 故障）也会归为 `Verification code expired`——这是设计上的降级，前端无法区分「过期」与「系统异常」，统一按过期提示即可。

### 前端实现建议

- **验证码输入**：6 位独立输入框组件（OTP input），输入完最后一位自动提交
- **校验通过后**：前端无需保存任何状态（后端已用 Redis 标记），直接进入第 ③ 步

---

## 5. 端点 ③：重置密码

### 请求

```
POST /auth/password/reset
Content-Type: application/json
```

```jsonc
{
  "email":        "user@example.com",   // 必填
  "new_password": "newSecret123"        // 必填，最少 6 位
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `email` | string | ✅ | 与 ①② 一致的邮箱 |
| `new_password` | string | ✅ | 新密码，**最少 6 位**（`password.MinLength`） |

> 字段名是 `new_password`（snake_case），不是 `password`。注意与注册接口 `/auth/register/complete` 的字段名区分。

### 成功响应

```
200 OK
```
```jsonc
{ "code": 200, "message": "Password reset successful", "data": null }
```

成功后后端做了三件事：
1. **更新密码哈希**（Argon2id）到数据库
2. **注销该用户全部终端**——所有旧 token 立即失效（包括多端同时在线前按 `web` / `mobile` 签发的旧 token，见 `docs/multi-device-frontend-api.md`）
3. **清除已验证标记**（`pwd_reset:verified`）

> **不会返回 token**，前端需引导用户重新登录。

### 错误响应

| `code` | `message` | 触发条件 | 前端建议处理 |
|---|---|---|---|
| `400` | `Password must be at least 6 characters` | `new_password` 少于 6 位 | 输入框下方实时校验长度 |
| `400` | `Verification code expired` | 跳过 ② 直接到 ③，或 ② 后超过 10 分钟 | 引导用户回 ① 重新走一遍流程 |
| `403` | `Account has been disabled` | 账号被禁用（status≠1） | 提示「账号已被禁用，无法重置密码」 |
| `404` | `Account not found` | 邮箱在流程进行期间被注销（罕见） | 引导回 ① |
| `400` | `Missing required parameter` | 缺 `email` 或 `new_password` | 表单校验兜底 |

### 前端实现建议

- **密码强度校验**：后端只校验长度 ≥6，前端可叠加自己的强度提示（但不阻断提交，只要 ≥6 位即可）
- **成功后跳转登录页**：清空本地缓存的 token（如果有），跳转到 `/login`，并预填邮箱
- **「确认密码」输入框**：建议前端加一个「再次输入新密码」的确认框，本地校验两次一致后再提交

---

## 6. 完整对接示例

### 6.1 推荐的前端流程编排

```
[忘记密码页]
  │
  │ 用户输入邮箱 → 点击「发送验证码」
  ▼
POST /auth/password/send-code  { email, lang }
  │
  ├─ 200 → 进入「输入验证码」步骤，按钮 60s 倒计时
  ├─ 404 → 提示「该邮箱未注册」
  ├─ 429 → 提示「请稍后重试」+ 倒计时
  └─ 其他 → 通用错误提示
  │
  ▼
[输入验证码页]
  │ 用户输入 6 位验证码 → 自动提交
  ▼
POST /auth/password/verify  { email, code }
  │
  ├─ 200 → 进入「设置新密码」步骤
  ├─ 400 (expired)  → 提示过期，引导重新发送
  └─ 400 (invalid)  → 提示验证码错误，清空重输
  │
  ▼
[设置新密码页]
  │ 用户输入新密码 + 确认密码 → 点击「重置密码」
  ▼
POST /auth/password/reset  { email, new_password }
  │
  ├─ 200 → 清除本地 token → 跳转 /login（预填 email）
  ├─ 400 (expired)  → 提示「验证已过期」→ 回到 ①
  ├─ 403 → 提示「账号已禁用」
  └─ 其他 → 通用错误提示
```

### 6.2 TypeScript 示例

```typescript
const API_BASE = 'https://qubar.site'  // 无 /api 前缀

interface ApiResponse<T> {
  code: number
  message: string
  data: T | null
}

async function callApi<T>(path: string, body: object): Promise<ApiResponse<T>> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return res.json()
}

// ① 发送验证码
async function sendResetCode(email: string, lang: 'zh' | 'en' = 'zh') {
  return callApi('/auth/password/send-code', { email, lang })
}

// ② 校验验证码
async function verifyResetCode(email: string, code: string) {
  return callApi('/auth/password/verify', { email, code })
}

// ③ 重置密码
async function resetPassword(email: string, newPassword: string) {
  return callApi('/auth/password/reset', {
    email,
    new_password: newPassword,   // ⚠️ snake_case
  })
}

// —— 使用示例 ——
const r1 = await sendResetCode('user@example.com', 'zh')
if (r1.code !== 200) {
  console.error(r1.message)  // "Account not found" / "Rate limit exceeded" / ...
  return
}

const r2 = await verifyResetCode('user@example.com', '123456')
if (r2.code !== 200) {
  console.error(r2.message)  // "Verification code expired" / "Invalid verification code"
  return
}

const r3 = await resetPassword('user@example.com', 'newSecret123')
if (r3.code === 200) {
  // ✅ 重置成功，旧 token 已失效，跳转登录页
  localStorage.removeItem('satoken')   // 清除本地 token
  router.push({ path: '/login', query: { email: 'user@example.com' } })
}
```

### 6.3 cURL 示例

```bash
# ① 发送验证码
curl -X POST https://qubar.site/auth/password/send-code \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","lang":"zh"}'

# ② 校验验证码
curl -X POST https://qubar.site/auth/password/verify \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","code":"123456"}'

# ③ 重置密码
curl -X POST https://qubar.site/auth/password/reset \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","new_password":"newSecret123"}'
```

---

## 7. 重置成功后的登录

重置成功后，用户需用新密码调用登录接口：

```
POST /auth/login
Content-Type: application/json
```
```jsonc
{
  "email":    "user@example.com",
  "password": "newSecret123",
  "device":   "web",           // 可选，"web" | "mobile"，默认 "web"
  "device_id": "0192..."       // 可选，终端 UUID，见 docs/multi-device-frontend-api.md
}
```

成功响应（与注册完成接口一致）：

```jsonc
{
  "code": 200,
  "message": "Login successful",
  "data": {
    "user": {
      "id":           "0195a3f2-...",       // UUIDv7
      "username":     "用户昵称",
      "email":        "user@example.com",
      "phone":        "",
      "google_id":    "",
      "x_id":         "",
      "github_id":    "",
      "microsoft_id": "",
      "avatar_url":   "https://...",
      "gender":       0
    },
    "token": "xxxx-xxxx-xxxx",             // 后续请求作为 satoken 头
    "device_id": "0192..."                 // 本终端 UUID，前端持久化保存
  }
}
```

> 登录成功后，后续所有需要鉴权的请求都要在请求头携带 `satoken: <token>`（sa-token，见下方说明）。

---

## 8. 请求头说明

| 场景 | 需要的头 |
|---|---|
| 找回密码三个端点（send-code / verify / reset） | 仅 `Content-Type: application/json` |
| 重置成功后的 `/auth/login` | 仅 `Content-Type: application/json` |
| 登录后的其他业务请求 | `satoken: <token>` + `Content-Type` |

> **鉴权 header 名是 `satoken`**（小写，来自 `config.yaml` 的 `sa_token.token_name`），不是 `Authorization`。
> CORS 已预放行 `satoken` 头，前端跨域请求无需额外处理（见 `pkg/composition/middleware/cors.go:48`）。

---

## 9. 与注册流程的对比（避免混淆）

两套流程结构完全相同，仅语义和字段名有细微差异：

| 步骤 | 注册 | 找回密码 |
|---|---|---|
| ① 发送验证码 | `POST /auth/register/send-code` | `POST /auth/password/send-code` |
| ② 校验验证码 | `POST /auth/register/verify` | `POST /auth/password/verify` |
| ③ 完成 | `POST /auth/register/complete` | `POST /auth/password/reset` |
| 邮箱必须 | **未注册**（已注册 → 409） | **已注册**（未注册 → 404） |
| 第③步请求体 | `{ email, username, password, device }` | `{ email, new_password }` |
| 第③步密码字段名 | `password` | `new_password` ⚠️ |
| 第③步成功后 | 自动登录，返回 token | **不自动登录**，需手动走 `/auth/login` |
| 第③步副作用 | 创建用户 + 写会话 | 更新密码 + 踢下线所有设备 |
| Redis key 前缀 | `register:code:` / `register:verified:` / `register:rate:` | `pwd_reset:code:` / `pwd_reset:verified:` / `pwd_reset:rate:` |

> 两套流程的验证码 / 频率限制 / 已验证标记**互不干扰**（独立 Redis key 前缀），同一邮箱可以同时进行注册和找回密码（虽然实际场景罕见）。

---

## 10. 邮件模板变量

后端通过 Mailtrap 模板发送找回密码邮件，模板变量为：

| 变量 | 值 | 说明 |
|---|---|---|
| `{{.Email}}` | 收件人邮箱 | 用于邮件正文中展示 |
| `{{.Code}}` | 6 位数字验证码 | 用户需输入到前端验证码输入框 |

模板 HTML 参考文件（供 Mailtrap 平台录入参考）：
- 中文：`docs/email_password_reset_template.html`
- 英文：`docs/email_password_reset_template_en.html`

> ⚠️ 模板的 UUID 需要在 Mailtrap 平台创建模板后，填入 `configs/config.yaml` 的 `mailtrap.templates.password_reset.{zh,en}`。未配置时，发送验证码端点会返回 500。
