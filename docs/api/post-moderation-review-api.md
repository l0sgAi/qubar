# 帖子人工审核（圈主 / 管理员）前端对接文档

> #45 帖子 AI 审核流第 4 步（#118）。开启 `moderation.enabled` 后，新帖先进入**审核中**（`status=2`，除作者外不可见），
> 由 AI 审核 worker（#117）分流：放行 → 已发布；明显违规 → 驳回；**拿不准的转人工**——留在审核中，等本圈圈主 / 管理员在这里处理。
> 审核是圈子的职责：每个圈子的审核人**只能看到、只能处理本圈**的待审帖子。平台超管跨圈审核不在本期范围。

## 一、总览

| 能力 | 接口 | 谁可用 |
|---|---|---|
| 待审帖子列表（带 AI 结论） | `GET /circle/moderation/posts` | 本圈圈主（30）+ 管理员（20），成员状态正常 |
| 通过 / 驳回 | `POST /circle/moderation/review` | 同上 |

- 两个接口都**需要登录**（未登录 `code=202`）。
- 普通成员、非成员、禁言 / 拉黑中的管理员、其他圈子的管理员：一律 `403`（不区分原因，不暴露成员身份）。
- 前端入口建议：`GET /circle/detail/:id` 返回的 `member_role >= 20` 时在圈子管理页展示「待审帖子」。
- 统一响应信封 `{code, message, data}`；`message` 为英文，前端按 `code` + 自身文案渲染。

## 二、待审帖子列表 `GET /circle/moderation/posts`

Query 参数：

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `circle_id` | string(uuid) | 是 | 圈子 ID |
| `size` | int | 否 | 每页条数，≤0 或 >100 按 20 处理 |
| `cursor` | string | 否 | 上一页返回的游标，首页不传 |

只返回本圈 `status=2`（审核中）且未删除的帖子，**按发帖时间倒序**（最新的在前）。

响应 `data`：

```json
{
  "posts": [
    {
      "id": "0192f1c0-...",
      "circle_id": "0192a0d0-...",
      "user_id": "0192b3e1-...",
      "type": 1,
      "title": "帖子标题",
      "summary": "摘要",
      "content": "完整正文（审核需要看全文）",
      "media_extra": ["https://img.example/1.png"],
      "status": 2,
      "create_time": "2026-10-23T12:00:00.123456+08:00",
      "author_name": "作者用户名",
      "author_avatar": "https://...",
      "ai_verdict": {
        "decision": "review",
        "hit_hazards": ["self_harm", "spam_ad"],
        "hazards": { "sexual": 0.01, "violence_terror": 0.02, "harassment": 0.03, "illegal": 0.01,
                     "spam_ad": 0.41, "privacy": 0.01, "self_harm": 0.91, "manipulation": 0.02 },
        "severity": 2.1,
        "severity_confidence": 0.86,
        "model": "jev-1.13.0",
        "error_msg": null,
        "create_time": "2026-10-23T12:00:03.5+08:00"
      }
    }
  ],
  "size": 20,
  "cursor": "eyJ0IjoxNzYxMTkyMDAwMTIzNDU2LCJpIjoiMDE5..."
}
```

| 字段 | 说明 |
|---|---|
| `cursor` | **空字符串 = 没有更多**；非空原样带入下一页，不要解析 / 拼接。非法游标返回 400 |
| `ai_verdict` | 该帖最近一条 **AI** 审核结论。**可能为 `null`**：AI 还没处理到（审核事件排队中），此时也可以直接人工处理 |
| `ai_verdict.decision` | 一般是 `review`（转人工）。AI 服务不可用且 `fail_open=false` 时也会是 `review`，此时 `error_msg` 非空、`hazards` 为空对象、`severity` 为 `null`——提示「AI 未能给出结论」 |
| `ai_verdict.hit_hazards` | 命中的风险（概率 ≥ 转人工阈值，默认 0.35），按概率从高到低。用于标签展示 |
| `ai_verdict.hazards` | 全部风险概率（0–1）。key 见下表 |
| `ai_verdict.severity` | 危害程度期望分（0–3：无 / 轻微 / 严重 / 极端） |
| `ai_verdict.severity_confidence` | `severity` 的置信度（0–1），低置信度通常是转人工的原因之一 |

风险 key：

| key | 含义 |
|---|---|
| `sexual` | 色情低俗 / 招嫖 |
| `violence_terror` | 现实暴力 / 恐怖主义（虚构创作不算） |
| `harassment` | 辱骂、骚扰、攻击他人 |
| `illegal` | 赌博、毒品、诈骗、非法交易 |
| `spam_ad` | 广告、站外引流、刷单 |
| `privacy` | 泄露他人隐私 |
| `self_harm` | 自伤 / 轻生倾向（只转人工、不自动驳回，需要人工关怀） |
| `manipulation` | 内容里写给审核系统的指令，试图影响审核结果（只转人工） |

分页：

```text
第一页: GET /circle/moderation/posts?circle_id=<id>&size=20
后续页: GET /circle/moderation/posts?circle_id=<id>&size=20&cursor=<上一页响应的 cursor>
```

## 三、通过 / 驳回 `POST /circle/moderation/review`

```json
{ "circle_id": "0192a0d0-...", "post_id": "0192f1c0-...", "action": "approve", "reason": "人工复核无问题" }
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `circle_id` | string(uuid) | 是 | 帖子所在圈子（必须与帖子实际所属圈子一致） |
| `post_id` | string(uuid) | 是 | 待审帖子 ID |
| `action` | string | 是 | `approve` 通过 / `reject` 驳回。**必须显式传**，其他值（含空）400 |
| `reason` | string | 否 | 备注 / 驳回理由，≤200 字符（超出截断）。只记入审核日志，**不展示给作者** |

成功 `data`：

```json
{ "post_id": "0192f1c0-...", "status": 1 }
```

- `approve`：帖子变为已发布（`status=1`），此时才执行发布副作用（圈子帖子数 +1、@提及通知、AI 机器人回复，只执行一次）；
  进入搜索 / 圈子列表依赖 CDC 同步，可能有数秒延迟。
- `reject`：帖子变为驳回（`status=3`），只有作者在「我的帖子」里能看到。
- 两种结果都会给作者发站内通知（`notice_type` 7 = 审核通过、8 = 审核驳回，见 `docs/notice-frontend-api.md`；不展示审核人）。
- 并发：多个管理员同时处理同一帖子（或 AI worker 恰好同时处理完）时**只有一次生效**，其余请求返回 404。
  前端收到 404 时提示「该帖子已被处理」并刷新列表即可。

## 四、错误码

| 场景 | HTTP / code | message |
|---|---|---|
| 未登录 | 401 / 202 | `Token not found` |
| `circle_id` 缺失或非法、`action` 非法、请求体非法、游标非法 | 400 | `Invalid circle_id` / `action must be approve or reject` / `Invalid request parameters` / `Invalid cursor` |
| 不是本圈圈主 / 管理员（含禁言、拉黑中的管理员） | 403 | `Only circle owner or admin can review posts` |
| 帖子不存在 / 已删除 / 不在该圈 / 不是审核中（含已被他人处理） | 404 | `Post not found or not under review` |

## 五、后端实现备注

- 代码：`pkg/domains/post/application/moderation_review.go`（鉴权 + 列表 + 处理）、`PostRepository.ResolveReview`（条件更新与审计行同事务）。
- 鉴权直查 `circle_member`（无缓存，任免 / 禁言即时生效），规则与 `/circle/agent/*` 一致：`role >= 20 且 status = 1`。
- 每次人工处理写一行 `domains.post_moderation_log`（`source=2`、`operator_id`、`reason`），字段说明见 `docs/pgsql-ddl/post.md`。
