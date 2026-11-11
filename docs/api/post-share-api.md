# 帖子分享 API 对接文档

> 对应后端领域：`share`（#232，总览 #231）。热度设计见 [hot-sync-design.md](../design/hot-sync-design.md) P4。
> 只做**外部分享**：前端拼链接 / 卡片、唤起系统分享面板或复制链接，后端只记录分享、累计分享数与热度；不生成站内转发帖。

---

## 1. 分享帖子

```
POST /post/share
```

**鉴权**：需要登录（请求头 `satoken: <token>`）。未登录 → `401`。

**时机**：用户点「分享」后、唤起系统分享面板 / 复制链接时调用一次。接口失败不影响用户继续分享（链接照常可用），只是这次不计数。

### 1.1 请求体

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `post_id` | string(UUID) | 是 | 帖子 ID |

```json
{ "post_id": "0192f7c1-…" }
```

### 1.2 响应

```json
{ "code": 200, "message": "…", "data": { "share_count": 8, "first_share": true } }
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `share_count` | int | 分享后的分享数（去重后的分享人数） |
| `first_share` | bool | 本次是否为当前用户对该帖的**首次**分享。重复分享返回 `false`，计数、热度不变 |

### 1.3 规则

- **一人一帖只记一次**：同一用户重复分享（含并发多次请求）都返回 `200`，只有一次 `first_share=true`。
- **不可撤销**：没有取消分享接口。
- **可分享范围**：帖子须存在、已发布、未删除，且对当前用户可见（与帖子详情同一判断）。私密圈帖子只有圈成员能分享，
  分享出去的链接也只有圈成员能打开；非成员调用返回 `404`。
- **落地页**：复用访客可读的 `GET /post/detail/:id`，不需要新接口。
- 分享数在帖子列表 / 详情里的展示与 `is_shared` 由 #233 提供。

### 1.4 错误码

| HTTP | `code` | 场景 | 前端处理 |
|---|---|---|---|
| 400 | 201 | 参数错误：缺少 / 非法 `post_id` | 修正请求 |
| 401 | 202 | 未登录 / token 失效 | 引导登录（或直接让用户复制链接，不计数） |
| 404 | 204 | 帖子不存在 / 未发布 / 已删除 / 私密圈非成员 | 提示内容不可分享 |
| 503 | 212 | 分享事件投递失败，**服务端已回滚，本次未计数** | 可直接重试（重试是安全的，只会计一次） |
| 500 | 210 | 其它服务端错误 | 不影响用户分享，可忽略或稍后重试 |

---

## 2. 后端数据流（供后续 issue 参考）

```
POST /post/share
  → 可分享校验（post 桥接：GetPostBrief 已发布 + CanViewPost）            不可分享 → 404
  → INSERT domains.post_share ON CONFLICT (user_id, post_id) DO NOTHING   0 行 → 重复分享，返回当前分享数
  → 同步发布分享事件（topic post_share_events）                          失败 → 删除刚插入的行，返回 503
  → best-effort：post_statistics 发 post_share_count 标记；热度 ApplyHotDelta(post_share, +1) + post_hot
  → Redis post:sharecnt:{post_id} +1（冷 key 按 post_share 行数 SET NX 回种）
帖子统计消费者（post_flush_interval 批量窗口）
  → share_count = count(post_share WHERE post_id)（幂等重算，值不变不写行）→ Redis 只升不降校准
```

| 存储 | 说明 |
|---|---|
| `domains.post_share` | 去重真值，`(user_id, post_id)` 唯一，只插入不更新；`-- cdc: none` |
| `domains.post.share_count` | 批量重算落库；在 CDC 链路里但 `es_sinks` 白名单不含它，**不进 ES** |
| Redis `post:sharecnt:{post_id}` | 读路径真值（String，TTL 与 `post:stats` 相同 43min） |

## 3. 分享事件（供声望 #234 / #235 消费）

- **topic**：`redpanda.share_event_topic`（默认 `post_share_events`，配置为空时代码兜底；Redpanda 自动建 topic）
- **key**：`post_id`
- **发布时机**：只在**首次**分享（`post_share` 新插入一行）且事件同步 ack 成功后才算分享成功；重复分享不发；没有负向事件。
- **投递语义**：at-least-once，消费方按 `share_id` 去重。
- **消费者**：本期没有，由声望 issue 新增 consumer group。

```json
{
  "type": "post_share",
  "share_id": "0192f7c1-…",
  "user_id": "0192f7c1-…",
  "post_id": "0192f7c1-…",
  "author_id": "0192f7c1-…",
  "circle_id": "0192f7c1-…",
  "ts": 1762848000000
}
```

| 字段 | 说明 |
|---|---|
| `type` | 固定 `post_share` |
| `share_id` | `domains.post_share.id`（UUIDv7），消费方幂等键 |
| `user_id` | 分享人（「每日首次分享 +3」的对象） |
| `post_id` | 被分享的帖子 |
| `author_id` | 帖子作者 `post.user_id`（「被分享 +5」的对象）。**可能等于 `user_id`**（作者分享自己的帖子），是否计声望由声望 issue 决定 |
| `circle_id` | 帖子所属圈子 `post.circle_id` |
| `ts` | 分享时间，Unix 毫秒（应用侧生成）。「每日」按哪个时区切由消费方决定 |
