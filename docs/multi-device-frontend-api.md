# 多端同时在线：前端对接文档

> 面向前端同学，对应 issue #106。
> 同一账号可以在多个终端同时在线：两台电脑、电脑 + 手机浏览器、Web + App 互不踢下线；同一终端重新登录时，只替换这个终端自己的旧 token。
> 前端只需要做一件事：**生成并持久化一个 `device_id`，登录相关接口都带上它**。

## 一、`device_id` 是什么

| 项 | 说明 |
|---|---|
| 含义 | 一个"终端实例"的稳定标识：同一个浏览器 / 同一个 App 安装始终用同一个值 |
| 格式 | 标准 UUID 字符串：`8-4-4-4-12` 共 36 位，例如 `3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31`。大小写都可以，后端会统一转成小写 |
| 生成 | 客户端第一次启动时生成（浏览器用 `crypto.randomUUID()`，App 用系统 UUID API），任意版本的 UUID 都可以 |
| 存储 | Web：`localStorage`（**不要**放 `sessionStorage`，否则每开一个标签页就是一个新终端）；App：本地持久化存储 |
| 生命周期 | 退出登录、重置密码、换账号登录时**都不要清除**；只有清除站点数据 / 卸载 App 才会变 |
| 非法值 | 不是标准 UUID 的值（包括 `urn:uuid:...`、`{...}`、32 位无连字符、全零 UUID）会被后端当作没传 |

`device`（终端类型）字段保持不变：`"web"` 或 `"mobile"`，缺省或非法值按 `"web"`。

推荐写法（Web）：

```js
const DEVICE_ID_KEY = 'qubar_device_id'

export function getDeviceId() {
  let id = localStorage.getItem(DEVICE_ID_KEY)
  if (!id) {
    id = crypto.randomUUID()
    localStorage.setItem(DEVICE_ID_KEY, id)
  }
  return id
}

// 登录成功后：以服务端返回的为准（没传 / 传错时服务端会生成新的）
export function saveDeviceId(id) {
  if (id) localStorage.setItem(DEVICE_ID_KEY, id)
}
```

## 二、行为说明

| 场景 | 结果 |
|---|---|
| 同一账号在 `device_id` 不同的两个终端登录（两个都是 `web` 也一样） | 两个 token 都有效 |
| 同一 `device_id` 重新登录 | 只有这个终端之前的 token 失效，其他终端不受影响 |
| 不传 `device_id` 或传了非法值 | 后端生成一个新的，并在登录响应 `data.device_id` 返回。这次登录算作一个新终端，不会踢掉任何人 |
| 同时在线的终端超过上限（默认 10，后端配置 `sa_token.max_login_count`） | **最早登录**的终端被注销；它再请求时返回 401 |
| `POST /auth/logout` | 只注销当前 token，同账号的其他终端不受影响 |
| 重置密码（`POST /auth/password/reset`）成功 | 该账号**所有终端**的 token 都失效，包括多端上线前签发的旧 token |

兼容性：上线前已经签发的 token 照常使用，直到过期，前端不需要做任何迁移。老版本客户端不传 `device_id` 也能登录，只是每次登录都算一个新终端（靠上限兜底）。

## 三、接口变更

统一响应信封仍是 `{code, message, data}`，下文只列新增字段。

### 1. 邮箱登录 `POST /auth/login`

请求体新增 `device_id`：

```jsonc
{
  "email":     "user@example.com",
  "password":  "secret123",
  "device":    "web",                                   // 可选，"web" | "mobile"，默认 "web"
  "device_id": "3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31"   // 可选，见第一节
}
```

响应 `data` 新增 `device_id`：

```jsonc
{
  "user":      { "id": "0195a3f2-...", "username": "...", "email": "..." },
  "token":     "xxxx-xxxx-xxxx",
  "device_id": "3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31"   // 本次登录使用的终端 ID，前端保存（saveDeviceId）
}
```

### 2. 注册完成 `POST /auth/register/complete`

请求体新增 `device_id`，响应 `data` 新增 `device_id`，含义同上：

```jsonc
{
  "email":     "user@example.com",
  "username":  "昵称",
  "password":  "secret123",
  "device":    "web",
  "device_id": "3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31"
}
```

### 3. OAuth 登录（Google / GitHub / Azure）

三步都可以带 `device_id`。前端最简单的做法是：**第 1 步在 query 带上；第 3 步 POST 时直接从 localStorage 取**（不依赖第 2 步回跳 URL 里的值）。

1. **跳转登录** `GET /auth/<provider>/login?device=web&device_id=<uuid>`
   后端把 `device` 与 `device_id` 编码进 OAuth state。
2. **回跳前端** `GET /auth/<provider>/callback`（provider 回调，后端 302 到前端 success 页）
   回跳 URL 的 query 在 `code` / `device` / `provider` 之外新增 `device_id`（第 1 步带了合法值时才有）：

   ```
   https://<frontend>/success?code=<一次性code>&device=web&device_id=<uuid>&provider=google
   ```
3. **换 token** `POST /auth/<provider>/callback`

   ```jsonc
   // 请求体
   {
     "code":      "<回跳 URL 里的 code>",
     "device":    "web",
     "device_id": "3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31"   // 优先用 localStorage 里的；没有就用回跳 URL 里的
   }
   ```

   ```jsonc
   // 响应 data：新增 device_id
   {
     "user":      { "id": "...", "email": "..." },
     "token":     "xxxx-xxxx-xxxx",
     "expire":    259199,
     "email":     "user@example.com",
     "device_id": "3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31"
   }
   ```

### 4. 退出登录 `POST /auth/logout`

接口不变（请求头带 `satoken`）。语义明确为"只退出当前终端"。退出后**保留** `device_id`，下次登录继续使用。

## 四、401 处理

被上限挤掉的终端、重置密码后的旧终端，再请求时都会返回 401（`Invalid or expired token`）。前端照常清掉本地 token、跳转登录页即可。目前 401 不区分"被踢下线"和"过期"（另开 issue 处理）。

## 五、注意事项

- SSE 通知流有每用户连接上限（`notice_stream.max_conns_per_user`，默认 5）。多终端加多标签页时可能触顶，上线后再观察是否需要调大。
- 本期**不提供**"查看我的在线终端 / 踢掉指定终端 / 退出其他终端"接口，后端的会话索引已经为它们做好准备，后续另开 issue。
