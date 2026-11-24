# OAuth 登录回调配置说明

## 登录流程

### 1. 用户发起登录
用户点击登录按钮，跳转到后端 OAuth 登录接口，query 带上终端类型与终端 ID（多端同时在线，
见后端 `docs/multi-device-frontend-api.md`；`device_id` 存在 localStorage 的 `qubar_device_id`）：

```
https://your-backend.com/auth/google/login?device=web&device_id=3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31
```

后端把 `device` 与 `device_id` 编码进 OAuth state，回跳前端时原样带回。

### 2. Google 认证
用户在 Google 页面完成认证。

### 3. 后端处理
后端收到 Google 回调后，需要：
1. 处理 OAuth 认证
2. 生成 sa-token
3. **重定向到前端成功页面，并传递一次性 code（不要直接传递 token）**

## 后端回调配置（重要！）

### 推荐方式：后端重定向携带一次性 code

后端收到 OAuth provider 回调后**不消耗 code、不在 URL 中下发 token**，只把 provider 的原始授权码
302 透传到前端，URL 中携带一次性 code、provider、device 与 device_id（第 1 步带了合法值时才有；
token、email 等敏感信息不出现在 URL 中，避免被浏览器历史、代理日志和分析工具留存）：

```
HTTP 302 Redirect
Location: https://qubar.site/success?code=4/0ATX87lPy3pP...&device=web&device_id=3f1c9a52-...&provider=google
```

> **为什么不在后端直接换 token？** 授权码是一次性的：后端 GET 回调若先 Exchange，
> 前端 POST 再换必然 `invalid_grant`。所以 code 必须原样透传，由前端 POST 回
> **签发它的 provider 端点**完成交换。

**示例代码（Go）：**

```go
// OAuth 回调处理
func GoogleCallback(c *gin.Context) {
    // 透传 Google 下发的授权码，附带 provider（前端 POST 交换时要路由回本端点）与 device
    code := c.Query("code")

    redirectURL := fmt.Sprintf(
        "https://qubar.site/success?code=%s&provider=google&device=web",
        url.QueryEscape(code),
    )
    c.Redirect(http.StatusFound, redirectURL)
}
```

**前端接收：**
[Success.vue:43-100](src/views/auth/Success.vue#L43-L100) 会自动：
1. 从 URL 参数提取 code、provider（白名单校验，缺省 google）、device（缺省 web）；
   device_id 优先取 localStorage，没有才用 URL 里的
2. POST 到后端 `/auth/<provider>/callback` 交换 token
3. 保存 token 到 localStorage，并以响应里的 device_id 覆盖本地值
4. 显示成功消息
5. 1.5秒后自动跳转到 `/home`

### 前端 code 交换 token 流程

前端检测到 URL 中的 `code` 参数后，会自动：

1. POST 到后端 `/auth/<provider>/callback` 交换 token（provider 取自 URL 参数）
2. 收到 JSON 响应后保存 token
3. 跳转到主页

**前端会发送的请求：**

```javascript
POST https://api.qubar.site/auth/google/callback   // google 登录；github/azure 走各自端点
Content-Type: application/json

{
  "code": "4/0ATX87lPy3pP...",
  "device": "web",
  "device_id": "3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31"
}
```

**后端应返回：**

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "expire": "259200",
    "token": "465f4e12-e538-40a4-98bd-6a50bb337a65",
    "email": "user@example.com",
    "device_id": "3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31"
  }
}
```

## 前端路由配置

前端已配置路由：

| 路由 | 用途 |
|------|------|
| `/` | 登录页 |
| `/success` | OAuth 回调成功页 |
| `/home` | 主页（需要登录） |

## Google OAuth 应用配置

在 Google Cloud Console 配置授权重定向 URI：

```
https://qubar.site/success
```

**注意：** 由于使用了 History 模式路由，不需要 `#/`。

## 测试登录流程

1. 访问：`https://qubar.site/`
2. 点击"使用 Google 账号继续"
3. 完成 Google 认证
4. 自动跳转到：`https://qubar.site/success`
5. 显示欢迎信息
6. 1.5秒后自动跳转到：`https://qubar.site/home`

## Token 使用

登录成功后，所有 API 请求会自动在请求头中携带 token：

```
satoken: 465f4e12-e538-40a4-98bd-6a50bb337a65
```

详见：[src/utils/request.js:21-24](src/utils/request.js#L21-L24)

## 故障排查

### 问题：回调后没有跳转到主页

**检查：**
1. 浏览器控制台是否有错误
2. URL 参数是否包含 code
3. Network 标签查看请求是否成功

### 问题：token 未保存

**检查：**
1. 打开浏览器开发者工具 > Application > Local Storage
2. 查找 `qubar_token` 和 `qubar_token_expire`
3. 如果不存在，检查回调 URL 格式

### 问题：跳转到 /home 后又被重定向回登录页

**原因：** Token 验证失败或未保存成功

**解决：**
1. 检查 localStorage 中是否有 token
2. 检查 token 是否过期
3. 查看路由守卫逻辑

## 开发环境 vs 生产环境

### 开发环境
- URL: `http://localhost:5173/success?code=xxx`
- 后端可以使用 ngrok URL 进行测试

### 生产环境
- URL: `https://qubar.site/success?code=xxx`
- 需要配置正确的 GitHub Pages 域名

## 安全建议

1. **HTTPS 生产环境必须使用 HTTPS**
2. **URL 不携带敏感信息** 重定向 URL 只传一次性 code，token/email 由前端用 code 向后端交换（已实现）
3. **Token 过期时间** 建议设置为 3-7 天
4. **Token 存储** 使用 localStorage（已实现）
5. **Token 刷新** 可考虑实现 refresh token 机制
