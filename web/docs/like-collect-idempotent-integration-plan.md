# 点赞 / 收藏幂等接口 — 前端对接实施规划

> 版本：2026-09-28 · 分支：`claude/gallant-lovelace-jchxuz`
> 对应后端：[l0sgAi/qubar#47](https://github.com/l0sgAi/qubar/pull/47)（修复 l0sgAi/qubar#46，head `e22a9ba`，基线 `develop@84b3a80`）
> 后端对接文档：`docs/api/like-collect-toggle-api.md`（后端仓库）
> 状态：**已实施**（后端 l0sgAi/qubar#47 已合并部署，`develop@d7be892`）。实施记录见第八节。

---

## 一、后端变更摘要（与前端相关的部分）

l0sgAi/qubar#47 共 48 个文件，大部分是服务端内部修复（Redpanda 消费者 offset、落库幂等、唯一索引 SQL、计数对账脚本）。**对前端可见的只有 3 点：**

| # | 变更 | 接口 | 细节 |
|---|---|---|---|
| 1 | 请求体新增**可选** `action` 字段 | `POST /like/toggle`：`action ∈ {like, unlike}`<br>`POST /collect/toggle`：`action ∈ {collect, uncollect}` | 传了 = 设置为该状态（幂等，重复请求只生效一次）；不传 = 仍按「服务端真实状态取反」切换（兼容旧客户端）；非法值 → 400 |
| 2 | 新增 **503** 响应 | 两个 toggle 接口 | 事件投递 MQ 失败，**服务端已回滚，本次未生效**。body `code = 212`（`CodeServiceUnavailable`） |
| 3 | 信息流 `is_liked` / `is_collected` 更准确 | 推荐 / 热点 / 发现 | Redis ZSET miss 时回源 DB，不再把已赞帖显示为未赞。**前端无需改动** |

响应结构**不变**：`data.is_liked` / `data.is_collected` 仍是操作后的最终状态；已处于期望状态时（如重复 `action=like`）返回 200 + 当前状态，计数不变。

错误码对照（后端 `ResponseCode = 200 + iota`）：

| HTTP | body `code` | 场景 | 前端处理 |
|---|---|---|---|
| 400 | 201 | `type` / `action` 非法、缺字段 | 回滚 UI（属前端 bug，不应出现） |
| 401 | 202 | 未登录 | 走现有拦截器（清 token 跳首页）；页面已有访客前置拦截 |
| 404 | 204 | 帖子 / 评论不存在或已删除 | 回滚 UI + 提示内容已不存在 |
| 503 | 212 | MQ 投递失败，已回滚 | **带 action 自动重试 1 次**，仍失败则回滚 UI + 提示稍后重试 |
| 500 | 210 | 其它（含无法确认当前状态） | 回滚 UI + 提示稍后重试（不自动重试） |

---

## 二、依赖判断

**结论：有依赖（语义依赖，非接口破坏性依赖）→ 建议先合并并部署后端，再执行前端更新。**

分析：

1. **接口层兼容**：旧后端 `ToggleLikeRequest` / `ToggleCollectRequest` 没有 `action` 字段，hertz `BindJSON` 默认忽略未知字段，所以新前端发 `action` 给旧后端**不会报错**，只是被当作「切换」处理。
2. **语义不兼容 — 这才是依赖所在**：本次前端改造的核心收益是「带 action 的请求可安全重试 / 连点不翻转」。若前端先上线而后端未部署：
   - 超时 / 503 自动重试 → 旧后端会**再切一次**，把用户的赞翻回去（比现状更糟）；
   - 「最终状态」语义失效，只能靠响应 `is_liked` 兜底对齐。
3. 503 错误码、`code=212` 是新后端才会返回的，旧后端下相关分支只是不会触发，无害。
4. 后端 #47 还带运维步骤（`docs/ops/like-pipeline-rollout/`：唯一索引修复 SQL、计数对账、Redis 统计失效），与前端无关，但前端应在这套 rollout **完成后**再发布。

当前状态：已核实 `e22a9ba` **尚未进入** `develop`（`develop` 仍为 `84b3a80`），因此本次只输出规划，等你合并后端后再动代码。

> 如果之后希望前端可以不依赖部署顺序，可把「自动重试」做成开关（默认关，后端上线后打开）。本规划默认不做开关，严格按「先后端后前端」执行，保持代码简单。

---

## 三、前端现状盘点

### 3.1 调用点

| 文件 | 目标 | 现有逻辑 |
|---|---|---|
| `src/views/post/PostDetail.vue:143-205` | 帖子点赞 + 收藏 | 乐观翻转 → 600ms debounce → 盲切换请求 → 与响应对齐 / 失败再翻转 |
| `src/components/post/detail/CommentList.vue:441-473` | 评论 / 回复点赞 | 同上，**所有评论共用一个 debounce** |
| `src/components/post/PostCard.vue:222-251` | 信息流卡片 | `handleLike` / `handleCollect` **模板中未绑定，是死代码**；卡片只展示计数 |
| `src/api/like.js`、`src/api/collect.js` | API | 透传 data，JSDoc 仍写「幂等切换」 |
| `src/utils/request.js` | 拦截器 | 错误对象只带 `code` / `data`，**不带 HTTP status**，也不区分超时 |

### 3.2 现有逻辑的问题（均由「盲切换 + debounce」引起，本次一并修复）

1. **双击反而切换**：已赞状态下快速点两次（取消→再赞），UI 回到「已赞」，但 debounce 只发出 1 个盲切换请求 → 服务端变成「未赞」→ 响应回来 UI 被改成未赞。用户本意是 no-op。
2. **评论点赞丢失**：`CommentList` 全局共用一个 debounce，600ms 内先赞评论 A 再赞评论 B，A 的请求被覆盖，A 的 UI 显示已赞但服务端没记录，刷新后消失。
3. **失败回滚方向不可靠**：catch 里做 `liked = !liked`，若请求期间用户又点了一次，翻转会得到错误的状态。应回滚到「最后一次服务端确认的状态」。
4. **请求乱序**：请求在途时再次点击，会并发发出第二个请求，两个响应的到达顺序不确定，后到的旧响应会覆盖新状态。
5. **路由切换串帖**：`PostDetail` 的 debounce 在触发时才读 `post.value.id`；在 600ms 内从帖子 A 跳到帖子 B（组件复用），点赞会打到 B 上。
6. **超时不可重试**：盲切换下重试会翻回去，所以目前网络抖动只能失败；有了 `action` 后可以安全重试。

---

## 四、实施方案

### 4.1 设计原则

- **UI 状态 = 用户期望状态**；请求永远携带 `action = UI 当前状态`，不再盲切换。
- **按目标 key 串行**（`post:<id>` / `comment:<id>`）：同一目标同时最多 1 个请求在途；在途期间的点击只改 UI，请求返回后若 UI ≠ 服务端确认值，再补发一次。天然避免乱序，无需序号比对。
- **debounce 仍保留（600ms）**，但按 key 独立；触发时若 UI 已等于服务端确认值（例如双击），**直接不发请求**。
- **失败回滚到 confirmed**（最后一次服务端确认值），而非盲目取反。
- **只对可重试错误自动重试 1 次**：网络超时 / 无响应、HTTP 503。

### 4.2 改动清单

#### ① `src/utils/request.js`（小改）

错误对象补充 HTTP 状态，供调用方区分 503 / 404 / 超时：

```js
// 有响应的错误分支
errorObj.status = error.response.status
// 业务 code 非 200 的分支（HTTP 200 但 code 异常）不变
```

无响应（超时 / 断网）分支保持原样 reject axios 原生错误（有 `error.code === 'ECONNABORTED'` / `'ERR_NETWORK'`，无 `response`），调用方以「无 `status`」判定为网络错误。

#### ② `src/api/like.js`、`src/api/collect.js`（文档 + 参数）

- 更新 JSDoc：`action` 可选字段、返回值为最终状态、错误码表。
- 函数签名保持 `toggleLike(data)` / `toggleCollect(data)` 不变（调用方传入 `action`），不做额外封装，避免改动面扩大。

#### ③ 新增 `src/composables/useInteractionToggle.js`（核心）

一个与具体业务无关的「按 key 串行、幂等设值」小状态机，PostDetail / CommentList 共用。

```js
/**
 * @param {Object} opts
 * @param {(key, desired:boolean) => Promise<boolean>} opts.request  发请求，返回服务端最终状态
 * @param {number} [opts.delay=600]  debounce 毫秒
 * @param {number} [opts.retryDelay=800]  可重试错误的重试间隔
 * @returns {{ schedule(key, ctx), dispose() }}
 */
export function useInteractionToggle(opts)
```

每个 key 维护：`{ confirmed, timer, inflight, ctx }`，`ctx` 由调用方提供：

```js
ctx = {
  get: () => boolean,          // 读 UI 当前状态（即期望状态）
  apply: (state:boolean) => void, // 把 UI 设为 state（调用方负责联动计数）
  onError: (err) => void       // 最终失败时的提示
}
```

流程：

```
schedule(key, ctx)            // 调用方已先做乐观翻转
 ├─ 首次见到该 key：confirmed = 翻转前的值（= !ctx.get()）
 └─ 重置该 key 的 debounce timer

timer 触发 → flush(key)
 ├─ inflight → 直接返回（请求结束后会再检查）
 ├─ desired = ctx.get()
 ├─ desired === confirmed → 结束（双击 no-op，不发请求）
 └─ inflight = true; send(desired, attempt=0)

send 成功(server)
 ├─ confirmed = server
 ├─ inflight = false
 ├─ ctx.get() === desired（期间没再点）→ 若 server ≠ desired 则 apply(server) 对齐
 └─ 否则（期间又点了）→ 立即 flush(key) 补发新的期望状态

send 失败(err)
 ├─ 可重试（无 status / status===503）且 attempt===0 → retryDelay 后 send(desired, 1)
 └─ 否则：inflight = false; apply(confirmed); ctx.onError(err)
```

说明：

- key 与 `ctx` 在**点击时**捕获，修复问题 5（路由切换串帖）。
- 组件卸载时**不取消**待发请求（与现状一致，保证离开页面前的点赞不丢）；`dispose()` 仅用于清理引用，暂不在卸载时调用 flush 之外的逻辑。
- 不引入新依赖；`src/utils/throttle.js` 的 `useDebounceFn` 不再用于这两处，但保留（其它地方仍在用）。

#### ④ `src/views/post/PostDetail.vue`

- 点赞：删除 `debouncedPostLike`，改为
  ```js
  const likeToggle = useInteractionToggle({
    request: async (id, desired) => {
      const res = await toggleLike({ type: 'post', target_id: id, action: desired ? 'like' : 'unlike' })
      return res.data.is_liked
    }
  })
  ```
  `handleLike` 保留访客拦截 + 乐观更新，然后 `likeToggle.schedule(post.value.id, ctx)`；`ctx.apply` 联动 `like_count ±1`（仅在状态真正变化时）。
- 收藏：同理，`action: desired ? 'collect' : 'uncollect'`；**不本地 ±1 `collect_count`**（保持现有约定）。
- `ctx` 捕获的是点击时的 `post.value` 对象引用，而非每次读 `post.value`。

#### ⑤ `src/components/post/detail/CommentList.vue`

- 删除全局共享的 `debouncedLikeRequest`，改为一个 `useInteractionToggle` 实例，key = `comment.id`（评论与回复同属 `type: 'comment'`，id 全局唯一）。
- `ctx` 捕获 `target` 对象（响应式代理），`apply` 联动 `target.liked` / `target.like_count`。
- 修复问题 2（跨评论点赞丢失）。

#### ⑥ 错误提示（i18n，`src/locales/zh-CN.js` / `en-US.js`）

新增 `messages` 下 2 个 key（实施时按 `frontend-constraints` skill 的 i18n 约束编写）：

| key | zh-CN | en-US | 触发 |
|---|---|---|---|
| `interactionRetryLater` | 操作未生效，请稍后重试 | Action didn't go through, please try again later | 503 重试后仍失败 / 500 / 网络错误 |
| `contentUnavailable` | 内容已不存在 | This content is no longer available | 404 |

其它错误沿用现有 `messages.operationFailed`。评论点赞目前失败时静默，改为同样给提示（帖子页与评论区行为一致）。

#### ⑦ `src/components/post/PostCard.vue`（清理，可选）

删除未绑定到模板的 `handleLike` / `handleCollect` / `debouncedPostCardLike` / `isLiked` / `isCollected` 以及 `toggleLike`、`useDebounceFn` 导入。信息流卡片目前只展示计数、无点赞按钮，这些是死代码且保留着旧的盲切换写法，容易被误用。
**如果你计划后续给卡片加点赞按钮，可以保留，改为到时直接复用 `useInteractionToggle`。请 review 时确认。**

### 4.3 不改动的部分

- `Home.vue` / `Discover.vue` / `CircleDetail.vue` / `SearchResults.vue` 的 `is_liked` / `is_collected` 映射：后端修复后数据更准，前端无需改。
- 「我的收藏」列表 `GET /collect/posts`：接口未变。
- `request.js` 401 处理逻辑：不变。

---

## 五、验证计划

仓库没有单测框架（`package.json` 只有 `dev` / `build` / `preview`），验证方式：

1. `npm run build` 通过。
2. 在 scratchpad 用 Node 写一个临时脚本，mock `request` 函数，覆盖 `useInteractionToggle` 的状态机分支（不提交到仓库）：
   - 单击 → 发 1 次 `action=like`；
   - 双击（600ms 内）→ 不发请求；
   - 在途期间再点 → 返回后补发 1 次，最终状态 = 最后一次点击；
   - 超时 / 503 → 重试 1 次成功；重试仍失败 → 回滚到 confirmed；
   - 404 / 500 → 不重试，回滚 + 提示；
   - 两个不同 key 快速连点 → 各自发请求，互不覆盖。
3. 后端合并部署后，本地连后端手测（浏览器 DevTools 观察 Network）：
   - 帖子详情：点赞 / 取消 / 双击 / 连点 5 次；收藏同上；刷新页面状态一致；
   - 评论区：600ms 内连赞 3 条不同评论 → 刷新后 3 条都是已赞；
   - DevTools 限速 / 离线模拟超时 → 自动重试且不翻转；
   - 请求体中确实带 `action`。

---

## 六、执行顺序与交付

1. ~~创建分支 `claude/gallant-lovelace-jchxuz`，提交本规划文档~~（本次完成）
2. **等待**：你 review 本文档 + 合并 / 部署后端 l0sgAi/qubar#47
3. 按 4.2 ①→⑦ 顺序实施，每步独立 commit：
   - `feat(request): expose HTTP status on rejected errors`
   - `feat(api): document optional action on like/collect toggle`
   - `feat(composables): add useInteractionToggle for idempotent like/collect`
   - `refactor(post-detail): use explicit action for like/collect`
   - `fix(comments): per-comment like requests with explicit action`
   - `feat(i18n): add interaction retry/unavailable messages`
   - `chore(post-card): remove unused like/collect handlers`（视 review 结论）
4. 本地验证（第五节）后 push 到本分支；是否开 PR 由你决定。

## 七、需要你确认的点

1. 依赖结论与发布顺序：**先后端（含 rollout 运维步骤）→ 后前端**，前端不加兼容开关。是否同意？
2. 自动重试策略：仅超时 / 503、最多 1 次、间隔 800ms。是否需要调整（如 500 也重试）？
3. `PostCard.vue` 死代码：删除，还是保留待后续加卡片点赞按钮？
4. 评论点赞失败由「静默回滚」改为「回滚 + toast 提示」，是否接受？

## 八、实施记录（2026-09-28）

按 4.2 实施，第七节 4 个待确认点均按本文默认方案执行：先后端后前端、不加兼容开关；仅超时 / 503 自动重试 1 次（800ms）；删除 `PostCard.vue` 死代码；评论点赞失败改为回滚 + toast。

与规划的差异：

- `useInteractionToggle` 未提供 `dispose()`：一轮操作结算后条目即从 Map 删除，无需额外清理。
- 新增 `interactionErrorMessage(err, t)`（同文件导出），PostDetail / CommentList 共用：404 → `contentUnavailable`；5xx / 网络错误 → `interactionRetryLater`；其它 → `operationFailed`。
- `PostDetail.vue` / `CommentList.vue` 顺带移除了本就未使用的 `useThrottleFn` 导入。

验证：

- `vite build` 通过。
- Node 临时脚本（未提交）覆盖状态机 15 个场景全部通过：单击、双击 no-op、三击、在途补发、在途双击不补发、超时重试成功、503 重试成功、重试仍失败回滚、404 / 500 / 业务码错误不重试、多 key 互不覆盖、结算后第二轮、重试等待期间再点以最新状态重试、服务端结果与期望不一致时对齐 UI。
- 连真实后端的浏览器手测（第五节第 3 条）尚未执行，需在部署环境完成。
