# 未读通知数 SSE 实时推送 前端对接文档

> 面向前端同学。后端设计见 `docs/design/sse-notification-design.md`，轮询接口见 `docs/notice-frontend-api.md`。
> 一句话：新增 `GET /notice/stream` 长连接，未读数一变服务端主动推；**保留原 30s 轮询作为降级**。

## 一、能力总览

| 项 | 内容 |
|---|---|
| 端点 | `GET /notice/stream`（SSE / EventSource） |
| 推送内容 | 未读通知数**全量**（不是增量），与 `GET /notice/unread-count` 同源同值 |
| 触发时机 | 新通知产生（点赞/收藏/评论/回复/@）、标记已读、全部已读 |
| 延迟 | 推送段 ≤1s；新通知链路叠加后端批量落库窗口，端到端 ≤6s |
| 降级 | 连接建立失败 / 被断开 → 回落 30s 轮询 `GET /notice/unread-count` |

## 二、连接建立

```js
// EventSource 不能自定义 header，token 走 query 参数（与 header 同名 satoken）
const es = new EventSource(`/notice/stream?satoken=${encodeURIComponent(token)}`)
```

- 鉴权：header `satoken` 优先，query `?satoken=` 兜底，二者其一即可。
- **无 token / token 无效** → 响应 `401 {"code":202,"message":"..."}`，**不是** SSE 流。不要重试，引导重新登录。
- **连接数超限**（同一账号 >5 个连接，多标签页场景）→ 新连接**照常建立**，该账号**最旧**的连接收到 `conn-evicted` 事件后被服务端断开（见 §3.3）。
  （旧版超限返回 429，已废弃；服务关停中会返回 503，按建连失败处理。）
- 浏览器 EventSource 自动重连：服务端首事件已下发 `retry: 5000`，断线后浏览器按 5s 间隔自动重连，无需自己写重连逻辑。

## 三、事件格式

### 3.1 `unread-count` — 未读数更新（唯一数据事件）

```text
event: unread-count
id: 1756713600123-7
retry: 5000
data: {"unread_count": 3}
```

```js
es.addEventListener('unread-count', (e) => {
  const { unread_count } = JSON.parse(e.data)
  updateBadge(unread_count) // 直接全量替换角标，不做加减
})
```

要点：

- **连接建立立即推一条全量**（首事件），不用先调轮询接口拿初值。
- 数据是**全量**，收到直接替换本地值。批量已读 100 条也只是一条事件（值为最终数）。
- 全部已读后推 `{"unread_count": 0}`。
- `id` 单调递增，仅用于服务端重连协议；前端**不需要**消费它，也不要持久化 `Last-Event-ID`——重连后服务端直接推当前全量，不补历史（本就不存在"漏消息"概念）。
- 值未变化不推送；收到推送后可顺手刷新通知列表页（若用户正在列表页）。

### 3.2 `auth-expired` — 登录态失效（服务端主动断连）

```text
event: auth-expired
```

登出 / token 过期 / 被踢下线后 ≤25s 内收到，随后服务端关闭连接。

```js
es.addEventListener('auth-expired', () => {
  es.close()            // 必须主动 close，否则浏览器会自动重连 → 401 死循环
  handleLogout()        // 清 token，跳登录页
})
```

### 3.3 `conn-evicted` — 连接被同账号新连接挤掉（服务端主动断连）

```text
event: conn-evicted
```

同一账号连接数达到上限（5）时，新连接建立会挤掉最旧的那条；被挤的标签页收到本事件，随后服务端关闭连接。

```js
es.addEventListener('conn-evicted', () => {
  es.close()     // 必须主动 close，否则浏览器自动重连又会挤掉别的标签页（互踢）
  startPolling() // 本标签页回落 30s 轮询（见 §四）
})
```

### 3.4 心跳注释行 `: ping`

25s 一帧，EventSource **不会触发任何事件**，仅保活，无需处理。5 分钟无数据属正常。

## 四、降级策略（必须做）

| 场景 | 行为 | 前端动作 |
|---|---|---|
| 建连 401 | token 失效 | 走登出流程 |
| 收到 `conn-evicted` | 本连接被同账号新连接挤掉 | `es.close()`，本标签页回落 30s 轮询 |
| SSE 断线且 EventSource 重连持续失败 | 网络/服务端不可用 | `es.onerror` 计数兜底，回落 30s 轮询 |
| 页面隐藏（`visibilitychange`） | 省电可选 | 可 `es.close()`，回前台重连（建连即拿全量，无补偿成本） |

```js
let pollingTimer = null

function startPolling() {
  if (pollingTimer) return
  pollingTimer = setInterval(refreshUnreadByApi, 30000)
  refreshUnreadByApi()
}

function stopPolling() {
  clearInterval(pollingTimer)
  pollingTimer = null
}

es.onerror = () => {
  // EventSource 自己重连；readyState === CLOSED 说明彻底失败
  if (es.readyState === EventSource.CLOSED) startPolling()
}
es.onopen = () => stopPolling()
```

## 五、参考实现（完整最小版）

```js
let es = null

export function connectNoticeStream(token, onUnread) {
  es?.close()
  es = new EventSource(`/notice/stream?satoken=${encodeURIComponent(token)}`)

  es.addEventListener('unread-count', (e) => {
    onUnread(JSON.parse(e.data).unread_count)
  })
  es.addEventListener('auth-expired', () => {
    es.close()
    es = null
    handleLogout()
  })
  es.addEventListener('conn-evicted', () => {
    es.close()
    es = null
    startPolling() // 见 §四
  })
  es.onerror = () => {
    if (es?.readyState === EventSource.CLOSED) {
      es = null
      startPolling() // 见 §四
    }
  }
}

export function disconnectNoticeStream() {
  es?.close()
  es = null
}
```

## 六、注意事项

- 与轮询接口的关系：**互备不互斥**。SSE 建立后停轮询；SSE 挂了就开轮询。两条链路的值永远一致（同源）。
- 不要在 `unread-count` 回调里立刻再调 `/notice/unread-count`——推送的就是全量值，再调是浪费。
- 新通知到达只推未读数，**不推通知内容**；用户进入消息中心仍走 `GET /notice/list` 拉列表。
- 原生 EventSource 不支持自定义 header；若用 fetch/`@microsoft/fetch-event-source` 实现，可直接 header 带 `satoken`，query 不传。
