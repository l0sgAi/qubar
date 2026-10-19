# 访客（匿名）接口开放可行性分析报告

> **阶段**：caveman mode — 仅评估与建议，**不含代码改动**。待审阅通过后再分阶段实施。
> **基线分支**：`feature/guest-auth-refactor-20260718`
> **日期**：2026-07-18

## 一、目标

评估当前后端全部 HTTP 接口对**未登录访客（anonymous / guest）开放**的可行性，给出分级建议与
分阶段实施路线。核心收益：让未注册用户能浏览内容（帖子/圈子/评论/用户主页/趋势），降低注册转化漏斗前段的流失。

## 二、现状盘点

### 2.1 鉴权机制（两层）

来源：`pkg/composition/auth.go`、各域 `interfaces/http/handler.go`。

| 中间件 | 位置 | 行为 |
|---|---|---|
| `RequireLogin`（`auth.go:25-55`） | 路由组级，全局默认 | 读 header token → `stputil.IsLogin` → 失败写 401；成功只 `c.SetLoginID(loginID)` |
| `OptionalLoginFn`（`auth.go:72-89`） | 路由组级，**仅 discover 使用** | 同上，但 token 空/失效均**静默放行**（视为访客） |
| `requireUserID` | handler 级（每域各拷一份） | 读 `c.LoginID()` → `uuid.Parse` → 失败 401/400 |
| `requireUserIDAllowAnon` | handler 级（仅 comment / discover 各一份） | 解析失败返回 `(uuid.Nil, true)` 不报错 |

**关键事实**：

- `RequireLogin` 只写 `SetLoginID`，从不 `SetUserID`；`appctx.UserID()` 实际未在请求路径使用（`context.go:68-82`）。
- 目前**全代码库唯一对匿名开放的端点是 `GET /discover/`**：路由组挂 `OptionalLoginFn`（`server.go:257-259` 硬编码丢弃传入 `authCheck`）+ handler 用 `requireUserIDAllowAnon`（`discover/interfaces/http/handler.go:62-72`）。
- discover 是**现成的范式蓝本**：service 层用 `userID *uuid.UUID`（可空指针）穿透，匿名走全局共享池 `discover:anon:*`，登录走个性化池（`discover-design.md:178-202,445,477`）。
- comment 域的读端点（list/replies/detail）handler **已迁移到 `requireUserIDAllowAnon`**（`comment/interfaces/http/handler.go:189-199`），但路由组仍挂 `RequireLogin`——属于"半迁移"状态。

### 2.2 端点全量盘点（13 域，约 55 个端点）

下表标注每端点的：路由组鉴权、handler 内是否实际读取用户身份、身份用途。

#### auth 域（`/auth` 组根级 OPEN，仅 `/logout` 在 LOCKED 子组）
| 端点 | 组 | handler 用身份？ | 用途 |
|---|---|---|---|
| `GET /auth/{google\|github\|azure}/login` | OPEN | 否 | OAuth 跳转 |
| `GET /auth/{...}/callback` | OPEN | 否 | OAuth 回调 |
| `POST /auth/register/send-code\|verify\|complete` | OPEN | 否 | 注册流 |
| `POST /auth/login` | OPEN | 否 | 登录 |
| `POST /auth/password/{send-code,verify,reset}` | OPEN | 否 | 找回密码 |
| `POST /auth/logout` | LOCKED | token | sa-token 登出 |

#### user 域（`/user` 组 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `GET /user/get` | 是（直接读 `c.LoginID()`） | 取**自己**资料 |
| `PUT /user/update` | 是（`requireUserID`） | 改**自己**资料 |
| `GET /user/search` | **否** | 纯 ES 关键词搜索 |
| `GET /user/detail/:id` | **否** | 路径 `:id` 指定用户 |

#### category 域（`/category` 组 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `GET /category/get` | **否** | 全局分类列表 |

#### circle 域（`/circle` 组 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `POST /circle/create` | 是 | 建圈 owner |
| `GET /circle/list` | **否** | ES 搜索 |
| `GET /circle/active` | **否** | 全局活跃榜 |
| `GET /circle/detail/:id` | 是 | **is_joined / is_member** 个性化标记 |
| `GET /circle/my` | 是 | 我的圈子 |
| `GET /circle/user` | **否**（query `user_id`） | 指定用户圈子 |
| `POST /circle/join` / `POST /circle/leave` | 是 | 写成员关系 |
| `GET /circle/posts` | **否** | 圈内帖子 ES |

#### post 域（`/post` 组 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `POST /post/create` | 是 | 作者 + 触发历史 |
| `GET /post/list` | **否** | ES 搜索 |
| `GET /post/my` | 是 | 我的帖子（含草稿） |
| `GET /post/user/:user_id` | **否** | 指定用户帖子 |
| `GET /post/detail/:id` | 是 | **is_liked/is_collected** + 异步浏览历史 |

#### comment 域（`/comment` 组 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `POST /comment/create` | 是 | 作者 |
| `GET /comment/list` | 软（已用 `requireUserIDAllowAnon`） | **is_liked** 标记 |
| `GET /comment/replies` | 软（同上） | **is_liked** 标记 |
| `GET /comment/detail/:id` | 软（同上） | **is_liked** 标记 |

#### like / collect / history 域（均 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `POST /like/toggle` | 是 | 写点赞状态 |
| `POST /collect/toggle` | 是 | 写收藏状态 |
| `GET /collect/posts` | 是 | 我的收藏 |
| `GET /history/posts` | 是 | 我的浏览历史 |

#### recommend 域（`/post/home`，复用 `/post` 组 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `GET /post/home?tab=hot\|latest` | 是（强制） | 全局流，但 handler 注释"推荐流强制登录"（`handler.go:66`） |
| `GET /post/home?tab=recommend` | 是 | 个性化 CF + 防气泡（依赖用户行为池） |
| `GET /post/home?tab=following` | 是 | 已加圈子最新流 |

#### storage 域（`/upload` 组 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `POST /upload/image` / `post-images` | 是（返回 string loginID） | S3 key 前缀 / owner |
| `POST /upload/video` | **否**（handler 不校验） | 仅按 key 上传 |
| `DELETE /upload/delete` | **否** | 按 key 删 |
| `GET /upload/presign` | **否** | 按 key 预签名 |

#### discover 域（`/discover` 组 OPTIONAL）— **已开放**
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `GET /discover/` | 软（`requireUserIDAllowAnon`） | 登录=个性化；匿名=全局共享随机池 |

#### trending 域（`/trending` 组 LOCKED）
| 端点 | handler 用身份？ | 用途 |
|---|---|---|
| `GET /trending/` | 是（`requireUserID`，注释"热点页强制登录"） | 回填返回帖子的 **is_liked/is_collected** |

## 三、可行性分级

按"开放给访客的难度 + 风险"分四级。

### 🟢 A 级：已开放 / 公开（无需改动）
- 全部 `/auth/*`（除 logout）—— 登录注册流本身就是公开入口。

### 🟢 B 级：可立即开放（纯只读，handler 已不读身份）
这些端点 handler 内**根本不调用 `requireUserID`**，仅因路由组挂 `RequireLogin` 而被锁。
改 `routes.go` 把组级中间件换成 `OptionalLoginFn`（甚至无需中间件）即可，**service 层零改动**。

| 端点 | 备注 |
|---|---|
| `GET /category/get` | 全局分类，无任何用户语义 |
| `GET /user/search` | ES 用户搜索 |
| `GET /user/detail/:id` | 用户公开主页 |
| `GET /circle/list` / `active` / `user` / `posts` | 圈子搜索/活跃榜/指定用户圈子/圈内帖 |
| `GET /post/list` / `user/:user_id` | 帖子搜索/指定用户帖子 |

**风险评估**：低。均为只读 ES 查询。唯一注意点：`/user/search` 若返回手机号/邮箱等敏感字段需在 VO 层确认已脱敏（user 域 brief 仅含 id/username/avatar，安全）。

### 🟡 C 级：可开放，需 service/handler 小幅改造（走 discover 范式）
handler 需要 userID 但仅用于**个性化标记**（is_liked / is_collected / is_joined），匿名时这些标记可安全退化为 `false`。
改造模式 = `OptionalLoginFn` 组级 + `requireUserIDAllowAnon` handler 级 + service 接收 `userID *uuid.UUID`（nil 时跳过个性化回填）。

| 端点 | 改造点 | 难度 |
|---|---|---|
| `GET /comment/list` / `replies` / `detail` | **handler 已迁移完**，只差路由组换 `OptionalLoginFn` | 极低 |
| `GET /circle/detail/:id` | handler 换 `requireUserIDAllowAnon`；service is_joined 在 nil 时返回 false | 低 |
| `GET /post/detail/:id` | 同上 + **必须守护异步浏览历史**：`if userID != uuid.Nil { go recordView() }`（`post/application/service.go:447` 的 fire-and-forget 需加 nil 守卫） | 低-中 |
| `GET /trending/` | handler 注释"强制登录"需改；service 的 `trendingInteractionChecker` 回填在 nil 时跳过 | 低 |
| `GET /post/home?tab=hot\|latest` | 见 D 级说明——混合 tab 需拆分 | 中 |

**关键改造点（`/post/detail`）**：当前 `GetPostDetail` 既回填 is_liked/is_collected，又异步触发浏览历史记录。匿名开放后，**历史记录必须仅在登录时触发**（匿名无历史可记），is_liked/is_collected 在匿名时返回 false。这是整个改造里唯一需要谨慎的 service 层改动。

### 🔴 D 级：不应开放（硬身份依赖 / 写操作 / 个人数据）
| 端点 | 原因 |
|---|---|
| `GET /user/get` / `PUT /user/update` | 个人资料读写 |
| `POST /circle/create` / `join` / `leave` / `GET /circle/my` | 写成员关系 / 个人列表 |
| `POST /post/create` / `GET /post/my` | 发帖（状态由服务端决定，暂不支持草稿）/ 个人列表 |
| `POST /comment/create` | 写评论 |
| `POST /like/toggle` / `POST /collect/toggle` / `GET /collect/posts` | 写交互状态 / 个人收藏 |
| `GET /history/posts` | 个人浏览历史 |
| `GET /post/home?tab=recommend` | 依赖用户行为池（like/collect/view ZSET + CF），匿名无数据源 |
| `GET /post/home?tab=following` | 依赖已加圈子列表 |
| `POST /upload/*`（全部） | **强烈建议保持锁定**——见下方"存储滥用风险" |

### ⚠️ `/post/home` 混合 tab 的特殊处理
该端点跨 4 个 tab，访问控制需求不同：
- `tab=hot` / `tab=latest`：全局流 → 可开放（C 级）
- `tab=recommend` / `tab=following`：硬依赖 userID → D 级

**两种方案**：
1. **方案 1（推荐）**：组级换 `OptionalLoginFn`，handler 用 `requireUserIDAllowAnon`，在 service 内按 tab 分支——`recommend`/`following` 在 `userID==nil` 时返回 401（或重定向到 hot）。
2. **方案 2**：维持组级 `RequireLogin`，仅 hot/latest 拆出独立端点（如 `GET /post/hot`）。改动更大、不优雅。

推荐方案 1，与 discover 的"单端点 + service 分支"范式一致。

### ⚠️ 存储滥用风险（`/upload/*`）
虽然 `/upload/video`、`/upload/delete`、`/upload/presign` 的 handler 当前不校验身份，但**开放给匿名 = 公开 S3 写入**，会带来：
- 存储成本滥用（任意人可上传任意大小视频）
- 恶意内容/版权素材托管
- 预签名 URL 滥发

**建议**：所有 `/upload/*` 保持 `RequireLogin`，不纳入本次访客开放范围。即便未来要开放，也必须先上**配额 + 内容审核**。

## 四、横向风险与前置条件

| 风险 | 影响 | 对策 |
|---|---|---|
| **匿名无限流防护** | sa-token 现有频控按用户维度；匿名无用户 key，易被刷 | 新增**基于 IP 的速率限制**中间件，仅作用于匿名路径（`X-Forwarded-For` 需信任前置代理） |
| **爬虫/数据抽取** | 帖子/圈子/用户列表对匿名开放后，全站内容可被批量抓取 | 列表类端点对匿名收紧 `size` 上限（如匿名最大 10，登录 20）；search_after/cursor 不变 |
| **热点缓存击穿** | 匿名流量可能远大于登录流量，打爆 Redis/ES | 匿名路径加 CDN 或边缘缓存层（后续 P2） |
| **`/post/detail` 异步历史** | 若漏加 nil 守卫，匿名请求会写 `user:view:posts:{00000000-…}` 污染 | 改造时**强制 code review** 该点；加单测 |
| **`SetUserID` 未用** | `appctx.UserID()` 在请求路径从未被设置（`RequireLogin` 只 SetLoginID） | 本次不清理，保持现状；但新增匿名逻辑统一走 `LoginID()` 解析 |
| **`requireUserIDAllowAnon` 重复拷贝** | 每域各一份，逻辑完全相同 | 建议本次顺手抽到 `pkg/shared/appctx` 或新建 `pkg/shared/authutil` 共享（可选 P1） |

## 五、分阶段交付建议

### P0 — 立即可做（低风险、零 service 改动）
把 B 级端点的路由组从 `RequireLogin` 换为 `OptionalLoginFn`（或无中间件）：
- `/category/get`
- `/user/search`、`/user/detail/:id`
- `/circle/list`、`/circle/active`、`/circle/user`、`/circle/posts`
- `/post/list`、`/post/user/:user_id`

同时补一个 **IP 维度速率限制**中间件挂到这些匿名可读路径（前置条件）。

### P1 — 走 discover 范式（C 级，需 service 小改）
按依赖顺序：
1. `/comment/list|replies|detail` — handler 已就绪，仅换路由组（最快出成果）
2. `/circle/detail/:id` — 加 is_joined 匿名降级
3. `/post/detail/:id` — 加 is_liked/is_collected 匿名降级 + **异步历史 nil 守卫**（重点测试）
4. `/trending/` — 去掉"强制登录"，interaction checker 匿名跳过
5. `/post/home` 混合 tab — service 内按 tab + userID 分支

可选：抽公共 `requireUserIDAllowAnon` 到 shared 包，消除各域拷贝。

### P2 — 后续增强（非阻塞）
- 匿名路径边缘缓存 / CDN
- 匿名 size 上限收紧
- `appctx.UserID()` 清理（与本主题弱相关，单独立项）

### 不做（D 级，明确排除）
所有写操作、个人列表、`/upload/*`、`recommend`/`following` tab —— 本次不改。

## 六、判定总表（一图速览）

| 级别 | 端点数 | 处置 |
|---|---|---|
| 🟢 A 已公开 | 9（auth 全部） | 无需改动 |
| 🟢 B 立即可开 | 8 | 换路由组中间件 + IP 频控 |
| 🟡 C 改造后可开 | 7（含 /post/home 多 tab） | discover 范式 + service 降级 |
| 🔴 D 不开放 | 16 | 维持 RequireLogin |
| 已开放 | 1（/discover） | 范式蓝本 |

**合计可对访客开放：A(9) + B(8) + C(7) + discover(1) = 25 个端点**，覆盖全部浏览/搜索/详情/趋势/评论读路径，
足以支撑"未注册用户完整浏览内容"的体验闭环。

## 七、参考

- 范式蓝本：`docs/discover-design.md`、`pkg/domains/discover/`
- 鉴权机制：`pkg/composition/auth.go:25-89`、`pkg/shared/appctx/context.go:68-82`
- 路由装配：`pkg/composition/server.go:47-113,257-259`
- 各域路由：`pkg/domains/<name>/interfaces/http/routes.go`
- post 异步历史（重点改造点）：`pkg/domains/post/application/service.go:447`
