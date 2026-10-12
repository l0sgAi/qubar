# 点赞 / 收藏 API 对接文档

> 对应后端领域：`like`（帖子 / 评论点赞）、`collect`（帖子收藏）。
> 设计背景见 [like-pipeline-fix-design.md](../design/like-pipeline-fix-design.md)（#46）。
> 本文档供前端对接点赞 / 收藏按钮使用。

---

## 0. 变更摘要（#46）

| 变更 | 影响 | 前端动作 |
|---|---|---|
| 请求体新增**可选** `action` 字段 | 显式期望状态，重试 / 双击只生效一次 | **推荐**：按钮按当前 UI 状态传 `action`；旧客户端不传仍按切换处理 |
| 新增 `503` 响应 | 事件投递失败时服务端已回滚，本次操作**未生效** | 提示稍后重试；带 `action` 重试是安全的 |
| 信息流 `is_liked` / `is_collected` 更准确 | 推荐 / 热点 / 发现页不再把已赞帖显示为未赞 | 无 |

---

## 1. 点赞 / 取消点赞

```
POST /like/toggle
```

**鉴权**：需要登录（请求头 `satoken: <token>`）。未登录 → `401`。

### 1.1 请求体

| 字段 | 类型 | 必填 | 取值 | 说明 |
|---|---|---|---|---|
| `type` | string | 是 | `post` \| `comment` | 点赞目标类型 |
| `target_id` | string(UUID) | 是 | — | 帖子 ID 或评论 ID |
| `action` | string | 否 | `like` \| `unlike` | 期望状态。**缺省 = 切换**（以服务端真实状态取反） |

```json
{ "type": "post", "target_id": "0192f7c1-…", "action": "like" }
```

### 1.2 响应

```json
{
  "code": 200,
  "message": "…",
  "data": { "is_liked": true, "type": "post", "target_id": "0192f7c1-…" }
}
```

`is_liked` 是操作后的**最终状态**，前端以它为准刷新按钮。已处于期望状态时（如重复 `action=like`）同样返回 `200` 与当前状态，计数不变。

---

## 2. 收藏 / 取消收藏

```
POST /collect/toggle
```

**鉴权**：同上。

### 2.1 请求体

| 字段 | 类型 | 必填 | 取值 | 说明 |
|---|---|---|---|---|
| `post_id` | string(UUID) | 是 | — | 帖子 ID |
| `action` | string | 否 | `collect` \| `uncollect` | 期望状态。**缺省 = 切换** |

### 2.2 响应

```json
{ "code": 200, "message": "…", "data": { "is_collected": true, "post_id": "0192f7c1-…" } }
```

---

## 3. 错误码

| HTTP | `code` | 场景 | 前端处理 |
|---|---|---|---|
| 400 | 201 | 参数错误：`type` 非法 / `action` 非法 / 缺字段 | 修正请求 |
| 401 | 202 | 未登录 / token 失效 | 引导登录 |
| 404 | 204 | 帖子 / 评论不存在或已删除 | 提示内容已不存在 |
| 503 | 212 | 事件投递失败，**服务端已回滚，本次未生效** | 恢复按钮原状态，提示稍后重试 |
| 500 | 210 | 其它服务端错误（含无法确认当前状态） | 恢复按钮原状态，提示稍后重试 |

---

## 4. 推荐用法

```js
// 按钮点击：以当前 UI 状态决定期望动作，而不是盲切换
const action = isLiked ? 'unlike' : 'like';
setIsLiked(!isLiked);                      // 乐观更新
try {
  const { data } = await post('/like/toggle', { type: 'post', target_id: id, action });
  setIsLiked(data.is_liked);               // 以服务端最终状态为准
} catch (e) {
  setIsLiked(isLiked);                     // 400/500/503：回滚 UI
}
```

- 带 `action` 时，网络超时后**直接重试是安全的**：同一动作只生效一次。
- 不带 `action`（切换）时，超时重试可能把状态翻回去，**不要自动重试**。
- 快速连点：以最后一次请求的 `action` 为准即可，无需前端去重。
