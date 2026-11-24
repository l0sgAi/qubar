# 未读通知数 SSE 实时推送 — 后端需求文档

> 版本：2026-09-01 · 前端仓库：qubar-frontend · 关联代码：`src/api/notice.js`、`src/components/layout/AppHeader.vue`、`src/views/Notifications.vue`

## 1. 背景与现状

首页顶栏通知红点的未读数当前由前端**轮询**实现，无任何服务端推送：

- `AppHeader.vue` 挂载时拉一次 `GET /notice/unread-count`，之后 `setInterval` 每 **30s** 轮询一次；
- 通知页标记已读后，前端通过 `window` 事件（`notice-read` / `notice-read-all`）做本地校正；
- 对接文档已说明「通知落库有约 5s 延迟，软实时即可」，因此 30s 轮询的时效性勉强可用，但存在三个问题：
  1. **时效性差**：新通知最长 30s+ 后才在红点体现；
  2. **无效请求多**：后台标签页也在持续轮询，未读数长期不变时空转；
  3. **多标签页不一致**：A 标签页已读，B 标签页最长 30s 后才同步。

目标：新增 SSE（Server-Sent Events）通道，未读数变化时由服务端**主动推送**，前端保留轮询作为降级。选择 SSE 而非 WebSocket 的原因：未读数是典型的**服务端→客户端单向推送**，SSE 基于普通 HTTP，自带重连与事件 ID，无协议升级与双向心跳负担。

## 2. 前端消费方式（供后端理解）

- 前端将使用 **`@microsoft/fetch-event-source`**（基于 fetch 的 SSE 实现）建立连接，原因是现有认证依赖自定义请求头 **`satoken`**（`src/utils/request.js` 拦截器统一注入），原生 `EventSource` 不支持自定义 header；
- 连接流程：登录态下建立 SSE → 收到事件即更新红点 → 连接失败/断开时**自动回落到现有 30s 轮询**，可用性不依赖 SSE 是否可用；
- 已读校正仍走现有 `notice-read` / `notice-read-all` 本地事件，SSE 推送与本地校正**取最新值覆盖**，不做增量累加（见 3.3 事件语义）。

## 3. 接口规格

### 3.1 连接端点

```
GET /notice/stream
```

| 项 | 要求 |
|---|---|
| 认证 | 与现有 REST 一致：请求头 `satoken: <token>`；**建议同时支持** `?satoken=<token>` query 参数（兼容原生 EventSource 及调试工具，sa-token 开启对应读取配置即可） |
| 未认证 | 返回 `401`，响应体沿用现有 `{ code, message, data }` JSON 结构（连接建立前直接拒绝，不要建立后再断） |
| 响应头 | `Content-Type: text/event-stream; charset=utf-8`、`Cache-Control: no-cache, no-transform`、`Connection: keep-alive`、`X-Accel-Buffering: no` |
| CORS | 允许前端源；`Access-Control-Allow-Headers` 需包含 `satoken`；无需 `credentials`（token 走 header 不走 cookie） |

### 3.2 事件格式

```text
event: unread-count
id: 1024
retry: 5000
data: {"unread_count": 3}

: ping

: ping
```

- **`unread-count`（业务事件）**：`data` 为 JSON，含**全量**未读数 `unread_count`（非增量），与 `GET /notice/unread-count` 的返回值语义完全一致；
- **`id`**：单调递增事件 ID（可用通知表自增序列或时间戳+序号），配合 `Last-Event-ID` 用于重连续传（见 3.4）；
- **`retry: 5000`**：建议重连间隔，前端 fetch-event-source 会以服务端值为准；
- **心跳**：每 **25s** 发送一次注释行 `: ping`（SSE 规范中的注释，不触发客户端事件），防止 Nginx/网关默认 60s 空闲超时断连。

### 3.3 推送触发时机

| 时机 | 推送内容 |
|---|---|
| 连接建立成功 | **立即推送一次**当前未读数（前端以此替代首次轮询） |
| 新通知落库（事务提交后） | 推送该用户最新未读数 |
| 标记已读（`POST /notice/read`） | 推送最新未读数 |
| 全部已读（`POST /notice/read-all`） | 推送 `{"unread_count": 0}` |
| 通知删除（如有） | 推送最新未读数 |

约定：

- **推全量数字**，前端直接覆盖本地值。多标签页、本地已读校正与推送并发时不会出现累加错乱；
- 推送发生在**事务提交之后**，避免客户端收到推送立刻去拉详情时读不到数据；
- 高频触发（如批量已读 100 条）允许**合并推送**：同一用户 1s 内多次变化可只推最终值。

### 3.4 断线与重连

- 连接因网络断开时，前端按 `retry` 值（默认 5s）重连，并携带 `Last-Event-ID`；
- 后端收到 `Last-Event-ID` 后**无需补发历史事件**，只需推送一次当前最新未读数即可（未读数是状态量而非事件流，重放历史无意义）；
- token 过期：连接建立时校验失败返回 401；连接存续期间 token 过期，可主动断开并在断开前推送一个 `event: auth-expired`（无 data），前端收到后清 token 跳登录页（与 REST 401 行为对齐）。

### 3.5 连接管理

- **每用户连接数上限**：建议 ≤ 5（覆盖多标签页场景），超限拒绝新连接或踢掉最旧连接，防止异常客户端打满连接表；
- **登出失效**：`POST /auth/logout` 后该用户的 SSE 连接应主动断开；
- **无消息不推**：未读数不变时不重复推送（心跳除外）。

## 4. 基础设施要求

### 4.1 反向代理（Nginx）

SSE 长连接需要显式配置，否则推送会被缓冲成「攒一批才到」：

```nginx
location /notice/stream {
    proxy_pass http://backend;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_buffering off;          # 关键：禁用响应缓冲
    proxy_cache off;
    proxy_read_timeout 3600s;     # 长连接读超时（大于心跳间隔即可，建议 1h）
}
```

### 4.2 多实例部署

若后端多实例部署，用户连接落在实例 A 而通知落库在实例 B，需要**跨实例广播**：建议用 Redis pub/sub（channel 如 `notice:unread:{user_id}`），收到广播的实例向本实例上该用户的连接推送。单实例阶段可先不做，但连接注册表请按 `user_id → connections` 的结构设计，便于后续接入广播层。

### 4.3 HTTP 版本

SSE 在 HTTP/1.1 下每连接独占一个 TCP 连接，浏览器对单域名有 6 连接上限（本项目 API 独立域名，SSE 只占 1 个，无实际影响）。若网关支持 HTTP/2，开启后多路复用更省资源，非必须。

## 5. 兼容与降级

- `GET /notice/unread-count` **保留不动**，作为 SSE 不可用时的降级通道（前端断线自动回落 30s 轮询）；
- SSE 端点上线不影响任何现有接口与前端版本，老前端继续轮询即可正常工作；
- 灰度建议：可先仅对测试环境/内部账号开放，验证心跳与代理配置后全量。

## 6. 验收清单

- [ ] 登录用户连接 `GET /notice/stream` 成功，且**立即收到一次**当前未读数；
- [ ] 未携带/携带无效 `satoken` 时返回 401 JSON 错误体，不建立 SSE 连接；
- [ ] 他端触发新通知（点赞/评论/回复/提及/收藏）后，已连接客户端 **1s 内**收到 `unread-count` 事件，数值与 `GET /notice/unread-count` 一致；
- [ ] 调用 `POST /notice/read`、`POST /notice/read-all` 后收到更新推送（read-all 推 0）；
- [ ] 心跳 `: ping` 每 ≤30s 到达一次，连接挂 5 分钟无业务事件不断开；
- [ ] 断网恢复后携带 `Last-Event-ID` 重连，收到最新全量未读数；
- [ ] token 过期 / 登出后连接被断开，前端能区分「认证失效」与「网络抖动」；
- [ ] 同用户 6+ 标签页连接被限制，不拖垮连接表；
- [ ] Nginx 层确认无缓冲：单条事件即时到达，不攒批。

## 7. 备注（可选的后续增强）

- **推送完整通知对象**（P1）：除未读数外，可在新通知落库时推送 `event: new-notice`、`data` 为完整通知对象（同 `GET /notice/list` 元素结构），前端可实现在线 toast 提醒与通知页列表实时插入。本期不做，事件流结构上已预留（不同 event 名互不干扰）；
- **Web Push**（站外提醒）：解决关标签页后的通知触达，与 SSE 互补，需 Service Worker + 订阅管理，如需另立项；
- 若未来出现 IM/聊天等双向通信需求，再评估 WebSocket；当前 SSE 已覆盖全部「服务端主动告知」场景。
