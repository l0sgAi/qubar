# 移动端 Web 门户适配 — 实施计划（v2.1）

> 版本：2026-10-02 · 分支：`feature/mobile-compatibility-20261002`（基于 `main@fbd82b6`）
> 状态：**P0 已提交；P1 已实施**（2026-10-02，未提交，进度和与本文的偏差见根目录 `HANDOFF.md`）。第十二节「待决策」全部按推荐方案执行。
>
> **v2 变更**：v1 的定位是"移动端完整功能"，v2 收缩为**轻量 Web 门户**：只做浏览和一键互动，所有创作、编辑、管理功能留给未来的 App。
> 删掉的设计包括：`AppSheet` 弹窗体系、全屏搜索层、评论编辑抽屉、发帖页适配、管理页适配、键盘避让、顶栏滚动隐藏、轮播手势。
> 页面迁移数从 14 降到 9。新增「功能分级」（第二节）和「暂不支持页」机制（第 6.3 节）。
>
> **v2.1 变更**：P1 加入**纯文本评论和回复**（第 6.6 节）：只用 textarea，不带 Markdown 工具栏、图片和 @ 提及。
> 同时补充方案前提与复盘条件（第 1.1 节）。第十二节第 2 项已定。

---

## 一、定位与范围

**移动端 Web = 门户**，承担三件事：

1. **内容分发入口**：分享链接、搜索引擎、通知跳转打开后能好好阅读。
2. **轻量参与**：登录、点赞、收藏、加入圈子、看消息，都是一次点击就能完成的操作；另外支持**纯文本评论和回复**。
3. **引流**：遇到创作或管理类操作时，引导用户去桌面端，将来改为引导去 App。

**设计取舍**

- 写入口在移动端**直接隐藏**，而不是点了以后报错。偶尔通过深链进入不支持的页面，由统一的「暂不支持页」兜底，URL 保持不变。
- 不再为移动端重写任何复杂组件（Markdown 编辑器、@ 提及、图片上传、裁剪、表格）。唯一的写操作是纯文本评论，用原生 textarea 实现。
- 桌面端（≥ 769px）**零视觉变化**。

### 1.1 方案前提与复盘条件

「轻量门户 + App 完整功能」成立，依赖下面三个前提。任何一条不成立，都要回头复盘范围：

| 前提 | 不成立时 |
|---|---|
| **App 采用与 Web 不同的技术栈**（Flutter、原生、React Native） | 如果 App 用 Capacitor、uni-app 之类把现有 Vue 代码套壳，移动 Web 本身就是 App。此时应回到 v1 完整方案，现在简化等于以后重做 |
| **App 在可预期的时间内上线**（目标半年内） | 如果超过一年或时间不确定，手机用户在空窗期无法发帖。应把移动端发帖提前到 P2 |
| **移动端流量以阅读为主**（分享链接、搜索、通知跳转） | 如果统计显示手机端是主要的创作来源，应提前开放写功能 |

**范围不会白做**：v2.1 是 v1 的严格子集。`AppShell`、`useBreakpoint`、`meta.mobile.supported` 这套机制可以扩展，以后开放某个功能，只需把 `supported` 改成 `true` 再补适配。

**复盘依据（建议）**：统计手机端访问占比，以及「暂不支持页」的访问次数和来源路由，用这些数据决定 P2 先开放什么。项目目前没有埋点通道，方案待定（第十二节第 7 项）。

---

## 二、功能分级评估

判断标准：

- 一次点击能完成，或者是纯阅读 → **保留**。
- 桌面形态在手机上不可用，但有简单替代方案 → **简化**。
- 依赖富文本输入、多步表单、文件处理或批量管理 → **移除**，由 App 承接。

### 2.1 保留（移动端完整可用）

| 功能 | 涉及页面 / 组件 | 移动端处理 |
|---|---|---|
| 登录 / 注册 / 第三方登录 / 忘记密码 / 访客浏览 | Login、`LoginCard`、`ForgotPasswordModal` | `LoginCard` 已有 560px 断点，复查即可；忘记密码弹窗只修宽度 |
| 首页信息流（推荐 / 热门 / 关注） | Home | 单列，tab 吸顶 |
| 热榜 | Hot | 单列 |
| 发现 | Discover | 网格改 2 列 |
| 帖子阅读（正文、图片、徽章） | PostDetail、`PostHeaderAndContent`、`MentionPreview` | 排版适配；Markdown 溢出处理 |
| 评论阅读（排序、展开回复、通知跳转定位） | `CommentList` | 定位时考虑顶栏偏移；查看回复改为底部抽屉（`CommentReplies` sheet 变体，开合跟随 `?replies=`，返回手势关闭，见 issue #22） |
| 点赞 / 收藏 | `useInteractionToggle` | 原样复用，只放大触控区域 |
| 圈子主页阅读 + 加入 / 退出 | CircleDetail、`CircleInfoCard` | 「加入」放进顶栏右侧 |
| 用户主页阅读 | UserDetail | 信息纵向排列 |
| 消息通知（分类 tab、全部已读、未读角标） | Notifications、`useNoticeStream` | tab 横向滑动；未读数上提（5.3） |
| 搜索帖子 / 圈子 / 用户 | SearchResults | 见 2.2 |
| 语言切换、退出登录、用户协议、隐私政策 | `LanguageSwitcher`、Terms、Privacy | 收进「我的」页 |

### 2.2 简化

| 功能 | 桌面形态 | 移动端简化方案 |
|---|---|---|
| 搜索 | 顶栏输入框 + 建议下拉（帖子 / 圈子 / 用户）+ 圈子内搜索标签 | 顶栏搜索图标直接跳 `/search`，页面顶部放一个输入框。**去掉建议下拉和圈子内搜索标签** |
| 个人中心 | 头像下拉菜单 + `/profile` 资料卡（可编辑）+ 帖子 / 收藏 / 圈子 / 历史 tab | 「我的」页：只读资料卡 + 四个只读列表 tab + 设置列表（语言、协议、退出）。**去掉编辑资料和头像上传** |
| 圈子导航 | SideNav（我的 / 活跃 / 随机圈子 + 创建圈子） | **不做抽屉**（推荐，见第十二节）。我的圈子从「我的 → 圈子」tab 进入，活跃和随机圈子在发现页可以看到 |
| 最近浏览 | RightSidebar 常驻 | 不显示，从「我的 → 历史」tab 进入 |
| 圈子主页操作 | 分享、举报、消息免打扰、管理入口、发帖 | 只保留**加入 / 退出**和**分享**（`navigator.share`，不可用时复制链接）。举报本来就是 `featureInDevelopment`，隐藏 |
| 图片轮播 | 箭头 + 圆点 | 原样保留，不加滑动手势 |
| 删除确认等 `useDialog` | 居中对话框 | 只在 `main.css` 修宽度 |
| 发表评论 / 回复 | `MdEditor`：Markdown 工具栏、图片上传、@ 提及选择器 | **纯文本 textarea**（6.6）：保留发评论、回复评论、回复某条回复；去掉 Markdown 工具栏、图片和 @ 提及 |

### 2.3 移除（移动端不提供，留给 App 或桌面端）

| 功能 | 涉及代码 | 移除原因 | 移动端处理 |
|---|---|---|---|
| 发帖 | CreatePost、`MdEditor`、`UploadImageWall`、圈子选择 | 富文本加图片加多字段表单，移动端要重做编辑体验 | 入口隐藏；路由走「暂不支持页」 |
| 富文本评论（Markdown 工具栏、评论配图、@ 提及） | `CommentEditor`、`CommentReplyEditor` 中的 `MdEditor`、`UploadImageWall`、`MentionPicker`、`MentionTrigger` | `MdEditor` 在手机上难用，提及浮层依赖光标坐标 | 移动端换成纯文本输入框（6.6），输入框下方小字提示「图片和 @ 提及请在电脑端使用」 |
| 建圈 | `CreateCircleModal` | 多步表单加头像上传 | 入口不渲染（属于 SideNav，移动端本来就不渲染） |
| 编辑资料 / 头像裁剪 | UserProfile 编辑弹窗、`ImageCropperModal` | 文件选择加裁剪交互 | 入口隐藏 |
| 圈子管理（成员 / 编辑 / Agents） | CircleMembers、CircleEdit、CircleAgents、`CircleAdminHeader`、`ManagedCircleList` | 表格、批量操作、复杂表单 | 入口隐藏；路由走「暂不支持页」 |
| 平台 Agent 管理 | AdminAgents、`AgentFormModal` | 管理员后台 | 同上 |
| 圈子消息免打扰 | `CircleInfoCard`、CircleDetail | 低频设置项 | 隐藏 |
| 圈子内发帖按钮 | CircleDetail | 依赖发帖功能 | 隐藏 |

### 2.4 App 端承接清单（备忘）

将来做 App 时，2.3 的全部功能，加上 2.2 中被简化掉的部分，需要按原生体验重新设计：

- 发帖、富文本评论：原生编辑器、@ 提及、多图上传。
- 编辑资料、头像裁剪。
- 建圈、圈子管理、Agent 管理。
- 搜索建议、圈子内搜索。
- 消息推送：替代 SSE 的未读通知。
- 图片手势浏览、下拉刷新。

移动 Web 的「暂不支持页」预留了 App 下载入口（6.3），App 上线后只需改配置。

---

## 三、现状（main@fbd82b6）

| 项目 | 现状 | 对移动端的影响 |
|---|---|---|
| 页面布局 | 14 个页面各自手写 `AppHeader` + `SideNav` + `content-wrapper`（内联 `margin-left: offset`），Home / Discover / Hot / SearchResults 另挂 `RightSidebar` | 没有公共布局壳 → 先抽壳（5.3）。v2 只迁移 9 个支持移动端的页面 |
| 断点 | 散落 560 / 640 / 768 / 1024 / 1200 / 1400 / 1600 | 统一以 768 为移动端分界 |
| viewport | 没有 `viewport-fit=cover` | 刘海屏、Home Indicator 安全区无法处理 |
| 高度 | `SideNav` / `RightSidebar` 用 `100vh` | 移动端不渲染这两个组件，影响小；新组件一律用 `dvh` |
| 未读数 | `useNoticeStream` 由 `AppHeader` 实例持有（`AppHeader.vue:180`），`AppHeader.vue:344-347` 监听两个 window 事件 | 移动端不渲染 `AppHeader` → 未读数断掉，**必须上提** |
| 页面过渡 | `pageFade` 动画只作用于 `.main-content`（`main.css:702`） | 抽壳后页面内容根节点保留 `.main-content` |
| 写入口 | CircleDetail 发帖按钮（`:87`）、管理入口（`:102`）、免打扰（`:286`）、举报（`:344`）；PostDetail `CommentEditor`（`:24`）；CommentList `CommentReplyEditor`（`:60`、`:155`）；UserProfile 编辑弹窗（`:151`） | 移动端逐个加 `v-if="!isMobile"` |
| 死代码 | `FloatingCreateButton.vue` 没有任何引用 | 不处理 |

---

## 四、设计原则

1. **JS 管结构，CSS 管样式。** 骨架切换（顶栏 ↔ TabBar、写入口隐藏、暂不支持页）用 `isMobile` + `v-if`；间距、字号、列数用 `@media`。
2. **断点单一事实源。** JS 常量与 CSS 媒体查询数值一致（5.1）。
3. **桌面零回归。** P0 抽壳完成后先做桌面截图对比，再开始移动端开发。
4. **隐藏优于拦截。** 移动端不支持的操作，入口直接不渲染；深链打开不支持的页面时显示「暂不支持页」，不重定向，URL 保持不变。
5. **遵守 `frontend-constraints`。** 复用 token 和主题绿；teleport 组件的样式写在 `main.css`；新 i18n 文案中的 `@`、`{`、`}` 要转义。
6. **跨断点不丢状态。** 窗口缩放或平板转屏跨越断点时，页面组件不 remount（5.3）。

---

## 五、基础设施（P0，桌面零视觉变化）

### 5.1 断点常量

新增 `src/constants/breakpoints.js`：

```js
// 断点单一事实源。CSS 媒体查询无法引用变量，
// main.css 与各组件 @media 必须手写同样的数值（见 main.css 顶部注释）。
export const BP_MOBILE_MAX = 768   // ≤ 768：移动端门户
export const MOBILE_QUERY = `(max-width: ${BP_MOBILE_MAX}px)`
```

- 取 **768（含）**，与现有 30+ 处 `@media (max-width: 768px)` 一致，旧样式不用批量改。
- v2 不再收敛其他断点（560 / 640 / 1024 / 1400 / 1600），平板和桌面维持现状。

### 5.2 `useBreakpoint` composable

新增 `src/composables/useBreakpoint.js`，模块级单例，全应用共享一个 `matchMedia` 监听器：

```js
import { ref, readonly } from 'vue'
import { MOBILE_QUERY } from '@/constants/breakpoints'

const isMobile = ref(false)
let mql = null

const onChange = (e) => { isMobile.value = e.matches }

function ensureListener() {
  if (mql || typeof window === 'undefined' || !window.matchMedia) return
  mql = window.matchMedia(MOBILE_QUERY)
  isMobile.value = mql.matches
  mql.addEventListener('change', onChange)
}

export function useBreakpoint() {
  ensureListener()
  return { isMobile: readonly(isMobile) }
}

// 仅供单测：重置单例，使下一次调用重新读取 matchMedia
export function __resetBreakpoint() {
  mql?.removeEventListener('change', onChange)
  mql = null
  isMobile.value = false
}
```

首屏同步读取 `matches`，不会出现先渲染桌面布局再闪回移动端的情况。

### 5.3 `AppShell.vue` 外框 + `useAppShell`（已实施，与初稿不同）

> 初稿让 `AppShell` 用插槽包住页面内容，实施时改掉了。原因：9 个页面有两种结构：
>
> - Home、Hot、Discover、Notifications：`content-wrapper`（带 offset）包住 `main-content` 和 `RightSidebar`。
> - PostDetail、UserProfile、UserDetail、CircleDetail、SearchResults：`main-content` 自带 offset。
>
> 两种结构的样式都写在各页的 scoped CSS 里。如果 wrapper 改由 `AppShell` 渲染，页面的 scoped 样式就命中不了它，桌面会走样。

**最终方案：`AppShell` 只渲染外框，页面内容是它的兄弟节点**

```vue
<!-- 页面：外框一行；内容容器、RightSidebar 保留在页面自己的模板里 -->
<AppShell v-model:offset="offset" />
<div class="content-wrapper" :style="contentStyle">
  <div class="main-content">...</div>
  <RightSidebar v-if="isLoggedIn && !mobileShell" />
</div>

<script setup>
const { offset, mobileShell, contentStyle } = useAppShell()
</script>
```

- **`AppShell.vue`**：多根节点，不额外包任何 DOM。
  - 桌面渲染 `AppHeader` + `SideNav`，并通过 `defineModel('offset')` 回写侧栏宽度。
  - 移动端分支在 P1 放 `MobileTopBar` / `MobileTabBar`。
  - 它与内容容器同属页面根节点的直接子节点，**桌面 DOM 与迁移前逐节点一致**。
- **`useAppShell()`**（`src/composables/useAppShell.js`）返回三项：
  - `offset`：侧栏宽度，默认 260，收起时 64。
  - `mobileShell`：是否启用移动端外框。
  - `contentStyle`：桌面为 `{ 'margin-left', width }`，与原内联样式逐字一致；移动端为 `{}`。
- **`useMobileShell()`**：等于 `MOBILE_SHELL_ENABLED && isMobile`。`AppShell` 和页面读的是同一个断点单例，所以结果始终一致。
- **总开关 `MOBILE_SHELL_ENABLED`**（`src/constants/breakpoints.js`）：P0 为 `false`，手机上仍渲染原桌面布局，不暴露半成品。P1 完成后改为 `true`。
- **跨断点不 remount**：页面内容不在 `AppShell` 内部，外框切换不会影响它，不依赖插槽位置这种约定。
- **RightSidebar**：Home、Discover 为 `v-if="isLoggedIn && !mobileShell"`，Hot 为 `v-if="!mobileShell"`。SearchResults 原本就把 RightSidebar 注释掉了，保持原样。
- **迁移范围**：只迁移 9 个支持移动端的页面：Home、Hot、Discover、PostDetail、Notifications、UserProfile、UserDetail、CircleDetail、SearchResults。
  - CreatePost、CircleMembers、CircleEdit、CircleAgents、AdminAgents 在移动端由「暂不支持页」接管，**这次不迁移**，保持原有模板。
- **路由结构不动**：`routes` 导出、`tests/router/guard.spec.js`、`App.vue` 的 `pageFade` 逻辑都不受影响。各页仍保留 `.main-content`。

**未读数上提（`src/composables/useUnreadNotice.js`，已实施）**

- 模块级单例，持有 `useNoticeStream` 的连接和 `unreadCount`，以及原先 `AppHeader` 里 `notice-read` / `notice-read-all` 两个 window 事件的监听。
- **消费方自行接入**：`AppHeader`（P1 起再加 `MobileTabBar`）调用 `useUnreadNotice({ onAuthExpired })`，挂载时 acquire，卸载时 release，按引用计数管理。`AppShell` 不参与。
- 全部消费方卸载后，**延迟 3 秒**再停止连接：页面切换时旧页卸载、新页挂载，这个间隙不断连，SSE 不再随每次跳转重建。离开到没有消费方的页面（例如登录页）后，最终会停止。
- 登出时，`AppHeader.handleLogout` 先调用 `stopUnreadNotice()` 立即断开，避免服务端随后推送 `auth-expired`，再触发一次提示和跳转。
- `auth-expired` 只回调最近挂载的消费方。
- 5 个未迁移的页面同样直接渲染 `AppHeader`，走的是同一个单例。

### 5.4 路由 meta：`meta.mobile`

```js
// 默认值（不写即为此）：{ supported: true, topBar: 'main', tabBar: true }
meta: { titleKey: 'title.postDetail', pageFade: true, mobile: { topBar: 'back', tabBar: false } }
meta: { requiresAuth: true, pageFade: true, titleKey: 'title.createPost', mobile: { supported: false } }
```

- `supported: false`：移动端渲染「暂不支持页」（6.3）。
- `topBar`：
  - `'main'`：左侧 logo，右侧搜索图标；访客态再加一个「登录」按钮。
  - `'back'`：左侧返回按钮，中间标题（复用现有 `titleKey`），右侧放 `#top-actions`。
- 返回逻辑：`window.history.state?.back` 存在时调用 `router.back()`；直接打开的深链调用 `router.replace('/home')`，避免点返回退出站点。

### 5.5 全局样式基础

- **`index.html`**：viewport 改为 `width=device-width, initial-scale=1.0, viewport-fit=cover`；另加 `<meta name="theme-color" content="#0a0e27">`。
- **`main.css` 新增**：

  ```css
  :root {
    --safe-top: env(safe-area-inset-top, 0px);
    --safe-bottom: env(safe-area-inset-bottom, 0px);
    --tabbar-height: 56px;
    --mobile-topbar-height: 48px;
  }
  ```

  P0 已加入上面四个 token。
- **推迟到 P1**：在手机断点下用 `@media (max-width: 768px) { :root { --header-height: var(--mobile-topbar-height); } }` 覆盖顶栏高度，并且只在移动端外框启用后生效。提前覆盖的话，P0 阶段手机上仍在用的桌面顶栏会被压扁。届时现有的 `calc(... var(--header-height) ...)` 都可以直接复用。
- **`dvh`（P0 已改）**：只改了"高度必须贴合可视区"的 6 处，写法是保留 `100vh` 作为回退，下一行再写 `100dvh`：
  - `SideNav` 高度
  - `RightSidebar` 两处 `max-height`
  - `PostDetail` 和 `CircleDetail` 侧栏的 `max-height`

  桌面上 `dvh` 等于 `vh`，没有差异。各页的 `min-height: 100vh` 不影响布局，暂不改。
- **移动端内容区（P1）**：`contentStyle` 在移动端返回 `{}`。P1 改为返回 `padding-bottom: calc(var(--tabbar-height) + var(--safe-bottom) + 16px)`；顶部间距靠被覆盖后的 `--header-height` 加 `--safe-top`。

---

## 六、门户骨架（P1）

### 6.1 `MobileTopBar.vue`

- 高度 48px，加 `padding-top: var(--safe-top)`，`position: fixed`。背景用 `--glass-bg` 加 `backdrop-filter: blur(12px)`。
- `main` 模式：左侧 logo 加 `common.appName`，链接到 `homeTarget`；右侧搜索图标，链接到 `/search`；访客态再加小号「登录」按钮。
- `back` 模式：左侧返回箭头，中间单行省略的标题，右侧 `#actions` 插槽。
- 始终固定显示，不做滚动隐藏。

### 6.2 `MobileTabBar.vue`

- 高度 56px，加 `padding-bottom: var(--safe-bottom)`，`position: fixed; bottom: 0`。每一项都用 `RouterLink` 渲染真实的 `<a>`。
- **登录态四项**（没有发帖按钮）：

  | 项 | 路由 | 高亮 |
  |---|---|---|
  | 首页 | `/home` | `/home` |
  | 发现 | `/discover` | `/discover`、`/hot`、`/search` |
  | 消息 | `/notifications` | `/notifications`；角标读 `useUnreadNotice().unreadCount`，`max=99` |
  | 我的 | `/profile` | `/profile` |

- **访客态三项**：首页、发现、登录。登录复用 `guest-access` 的跳转逻辑，带 redirect 参数。
- 热榜入口：发现页和热榜页顶部加分段控件「发现 | 热榜」，两页互相跳转。首页的 `hot` tab 是热门信息流，和热榜不是一回事，保持不变。

### 6.3 暂不支持页 `MobileUnsupported.vue`

**挂载点**：在 `App.vue` 的 `router-view` 插槽内按条件替换组件。URL 不变，路由守卫不动：

```vue
<component :is="resolveView(Component, route)" :key="route.name" />
```

```js
const { isMobile } = useBreakpoint()
const resolveView = (Component, route) =>
  isMobile.value && route.meta.mobile?.supported === false ? MobileUnsupported : Component
```

- 用户把窗口拉宽到桌面断点后，页面自动换回真实组件，不需要刷新。
- `requiresAuth` 守卫照常生效：访客先被要求登录，登录后再看到这个页面。

**页面内容**

- 图标，加文案「该功能暂未支持手机网页版」。
- 按钮：「复制链接，在电脑上打开」（`navigator.clipboard`）、「返回」、「回到首页」。
- 提示：也可以使用浏览器的「请求桌面网站」功能。iOS Safari 和 Android Chrome 开启后，视口宽度约 980px，会自动走桌面布局。**这一点要在真机上验证**（11.2）。
- App 下载入口：在 `src/config/index.js` 加 `appDownloadUrl`。值为空时不显示；App 上线后只需改配置。

**接入的路由**：`/create-post`、`/circle/:id/members`、`/circle/:id/edit`、`/circle/:id/agents`、`/admin/agents`。

### 6.4 写入口隐藏清单

各入口处 `import { useBreakpoint }`，加 `v-if="!isMobile"`：

| 位置 | 入口 | 移动端 |
|---|---|---|
| `CircleDetail.vue:87` | 圈内发帖按钮 | 隐藏 |
| `CircleDetail.vue:102` | 圈子管理入口 | 隐藏 |
| `CircleDetail.vue:286` / `CircleInfoCard` | 消息免打扰开关 | 隐藏 |
| `CircleDetail.vue:340-345` | 更多菜单的「举报」 | 从 options 中过滤掉；「分享」改用 `navigator.share`，不可用时复制链接 |
| `PostDetail.vue:24` | `CommentEditor` | 换成底部「说点什么…」输入条 + 输入抽屉（6.6） |
| `CommentList.vue:60/155` | `CommentReplyEditor` | 回复按钮打开输入抽屉（6.6） |
| `UserProfile.vue:151` | 编辑资料（含头像上传） | 隐藏入口 |
| `MyGroups` / `ManagedCircleList` | 「管理」按钮或链接 | 隐藏，只保留进入圈子 |
| `AppHeader` / `SideNav` 中的发帖、建圈 | — | 移动端本来就不渲染这两个组件 |

**新增 i18n 命名空间 `mobile.*`**（zh-CN、en-US 都要加）：

- `mobile.unsupported.title`、`mobile.unsupported.desc`
- `mobile.unsupported.copyLink`、`mobile.unsupported.desktopHint`
- `mobile.comment.richHint`：「图片和 {'@'} 提及请在电脑端使用」。`@` 必须用 `{'@'}` 转义，见 `frontend-constraints`。占位符、提交按钮、成功和失败提示复用现有的 `comment.editor.*` 和 `comment.reply.*`
- `mobile.tab.home`、`mobile.tab.discover`、`mobile.tab.notice`、`mobile.tab.me`
- `mobile.me.settings`

### 6.5 「我的」页（`/profile` 移动端视图）

- 顶部：只读个人卡片（头像、用户名、简介），**没有编辑按钮**。
- 中部：沿用现有 tab：我的帖子、收藏、圈子、浏览历史。
  - 去掉写死的 `width: 280px`。
  - 移动端隐藏列表项上的管理和删除操作，至少要保证这些操作不依赖 hover 才能看到。
- 底部：设置列表，依次为语言切换（内嵌 `LanguageSwitcher`）、用户协议、隐私政策、退出登录（`useDialog` 二次确认）。
- 不抽 `useUserMenu`：移动端只需要退出登录一个动作，直接调用现有逻辑即可。

### 6.6 纯文本评论与回复

**范围**

- 支持：发表顶层评论、回复评论、回复某条回复（`reply_to_id`）。
- 不支持：Markdown 工具栏、评论配图、@ 提及选择器。
- 后端接口与桌面端完全相同（`createComment`），不需要后端改动。

**抽出提交逻辑：`useCommentSubmit`**

`CommentEditor.vue:182-235` 和 `CommentReplyEditor.vue:172-225` 的提交流程几乎一样，依次是：

1. 调 `createComment`。
2. `getUserInfo` 补作者信息。
3. 组装乐观渲染用的对象（`author_*`、`like_count`、`create_time` 等）。
4. `seedContentMentions`。

这段逻辑抽到 `src/composables/useCommentSubmit.js`，三个输入组件共用：

```js
// 返回 { submitting, submit }
// submit({ postId, content, rootId?, replyToId?, replyToName?, extraData?, mentionIds? }) → 乐观渲染用的评论对象
export function useCommentSubmit() { ... }
```

- 桌面的 `CommentEditor`、`CommentReplyEditor` 改为调用它，行为不变。这是本节唯一的桌面端改动，需要回归验证（11.2）。
- 移动端调用时不传 `extraData` 和 `mentionIds`。

> **#35 变更**：输入从「内联在评论区 / 被回复的评论下方」改为**底部输入抽屉**。内联输入框在长帖里要先滚回正文末尾才能评论，弹出键盘时还常被挡住、把页面顶来顶去。下面是现行方案。

**组件**

- `src/components/post/detail/PlainCommentInput.vue`：抽屉里的输入区。
  - Props：`postId`、`rootId`、`replyToId`、`replyToName`，内容走 `v-model:value`（草稿由抽屉持有）。传 `rootId` 即回复模式。
  - UI：`NInput type="textarea"`，`autosize: { minRows: 1, maxRows: 6 }`，`maxlength` 2000，字号 16px；右侧发送按钮，内容为空时禁用、提交中 loading；下方小字提示 `mobile.comment.richHint`；字数到 1800 才显示 `n/2000`。
  - 回车换行，不发送：移动端软键盘的回车很容易误触。
  - 成功：提示、清空、emit `submit`。失败：提示错误，保留内容。
- `src/components/post/detail/MobileCommentComposer.vue`：输入抽屉（`NDrawer placement="bottom" height="auto"`，高度随内容），挂在 `CommentList` 里，只在移动端外框渲染。
  - `open({ rootId, replyToId, replyToName })` 打开：顶层评论 / 回复根评论 / 回复某条回复。头部显示「发表评论」或「回复 @{name}」（`mobile.comment.replyTitle`）。
  - 访客：`requireLogin('comment')`，不打开抽屉。
  - 发送成功：收起抽屉和键盘，emit `submit(created, target)`；`CommentList` 沿用 `addComment` / `handleReplySubmit` 乐观插入，高亮新评论，抽屉离场动画结束（emit `closed`）后滚动过去。
- `src/components/post/detail/MobileComposeBar.vue`：「说点什么…」伪输入条，点击打开输入抽屉。
  - 帖子详情页底部常驻（`fixed`）：占位 `mobile.comment.barPlaceholder`，右侧显示评论数，点它滚到评论区；底部让出 `var(--safe-bottom)`，内容区底部加等高留白。帖子未发布（`status !== 1`，评论区不渲染）时不显示。
  - 回复抽屉（`reply-sheet`）的 footer 里也放一条，占位「回复 @{根评论作者}」，打开回复根评论的输入抽屉。

**交互细节**

- **打开即弹键盘**：iOS Safari 只在用户手势的同步调用里 `focus()` 才弹键盘，而抽屉内容要等路由更新后才渲染。点击时先同步聚焦一个常驻的代理输入框（`.composer-focus-proxy`，透明、1px、`position: fixed`），键盘弹出；抽屉渲染后再把焦点移到真正的 textarea，键盘保持弹出。所以 `open()` 必须在点击处理里同步调用。两个抽屉都关掉了 `trap-focus`，否则回复抽屉的焦点陷阱会把代理输入框的焦点抢回去。
- **贴着键盘**：`src/composables/useKeyboardInset.js` 监听 `visualViewport` 的 `resize` / `scroll`，遮挡高度 = `innerHeight − visualViewport.height − visualViewport.offsetTop`，作为抽屉的内联 `bottom`。键盘弹出时去掉底部安全区留白。iOS Safari 和 Android Chrome（默认 `interactive-widget=resizes-visual`）走同一条路径，`index.html` 不加 `interactive-widget=resizes-content`，避免任何输入框聚焦都让整页重新布局。
- **不滚动穿透**：`NDrawer` 默认锁住 `html` 滚动；遮罩加 `touch-action: none`，抽屉加 `overscroll-behavior: contain`。
- **草稿**：按目标（顶层 `post` / 根评论 id / 被回复的回复 id）保存在内存里，关闭再打开同一目标时恢复，发送成功后清空。草稿随评论区卸载，离开帖子不保留。
- **叠加与返回手势**：开合跟随路由 query `compose=<目标 key>`，与回复抽屉的 `replies` 用同一套做法：打开时 push，关闭时上一条历史正是「同页无该 query」才 `router.back()`，否则 `replace`，并用 `closing` 标记防止重复关闭多退一步。从回复抽屉打开时后打开的 `NDrawer` z-index 更高，叠在上方。返回顺序：先关输入抽屉，再关回复抽屉，最后离开帖子。刷新或后退回来时残留的 `replies` / `compose` 由 `CommentList` 挂载时一并去掉；跨到桌面断点时也一样。
- 桌面端不变：`CommentEditor` / `CommentReplyEditor` 内联 Markdown 编辑器。

**内容格式**

- 评论内容按 Markdown 存储和渲染（`MentionPreview` → `MdPreview`）。纯文本里偶尔出现的 `*`、`#`、`_` 会被当作 Markdown 解析，**不做转义**。这与桌面用户手打这些字符时的行为一致，而且一旦转义，桌面编辑时会看到反斜杠。
- **换行（已确认）**：md-editor-v3 内置的 markdown-it 配置为 `breaks: true`，单个换行会渲染成 `<br>`，**不需要做转换**。
- 手打的 `@用户名` 不传 `mention_user_ids`，所以**不会**通知对方。`mentionDom` 会按缓存解析显示，可能渲染成链接，也可能保持纯文本，两种都可以接受。

---

## 七、逐页适配清单

| 页面 | `meta.mobile` | 迁移 AppShell | 主要改动 |
|---|---|---|---|
| Home `/home` | 默认 | ✅ | tab 吸顶；单列；去掉 RightSidebar |
| Hot `/hot` | 默认 | ✅ | 顶部分段控件「发现 \| 热榜」；`TrendingCard` 单列 |
| Discover `/discover` | 默认 | ✅ | 分段控件；帖子网格 2 列；圈子卡片改横向滚动带 |
| PostDetail `/post/:id` | `back`，无 TabBar | ✅ | 标题显示圈子名；点赞、收藏放大触控区；评论和回复改为底部输入条 + 输入抽屉（6.6）；Markdown 溢出处理；评论定位加 `scroll-margin-top` |
| Notifications `/notifications` | 默认 | ✅ | 分类 tab 横向滑动；列表项整行可点 |
| UserProfile `/profile` | 默认 | ✅ | 6.5 |
| UserDetail `/user/:id` | `back` | ✅ | 信息纵向排列 |
| CircleDetail `/circle/:id` | `back` | ✅ | 头部压缩；「加入」进 `#top-actions`；隐藏 6.4 所列入口；`CircleRuleCard` 默认折叠 |
| SearchResults `/search` | `back` | ✅ | 顶部输入框（`type="search"`、`enterkeyhint="search"`、字号 16px）；结果 tab 横向滑动 |
| Login `/` | 不使用壳 | — | `LoginCard` 的 560 断点复查；3 处 `NTooltip trigger="hover"` 改为 `click`；`AnimatedBackground` 移动端减少粒子 |
| Terms / Privacy / Success / NotFound | 不使用壳 | — | 已有 768 样式，复查 |
| CreatePost、CircleMembers / Edit / Agents、AdminAgents | `supported: false` | ❌ | 显示暂不支持页 |

---

## 八、移动端样式规范

只对第七节中 ✅ 的页面和 Login 适用：

| 项 | 规范 |
|---|---|
| 边距 | 页面左右 `12px`，卡片之间 `8px` |
| 触控目标 | ≥ `44×44px`，图标按钮用 padding 撑大点击区域 |
| 字号 | 正文 `15px`，次要文字 `13px`；input ≥ `16px`，防止 iOS 聚焦时缩放页面 |
| 宽度 | 不写死超过 280px 的宽度，改成 `width: 100%; max-width: Npx` |
| 横向溢出 | 360px 宽度下 `scrollWidth === innerWidth`，不准用全局 `overflow-x: hidden` 掩盖 |
| hover | 本页的 `:hover` 规则包进 `@media (hover: hover)`；按压反馈用 `:active`；只在 hover 时出现的按钮，移动端改为常显 |
| 点击 | 全局加 `-webkit-tap-highlight-color: transparent`；交互元素加 `touch-action: manipulation` |
| 毛玻璃 | blur ≤ 12px；嵌套的内层毛玻璃改用纯色 `--bg-secondary` |
| Markdown | 代码块 `overflow-x: auto`；图片 `max-width: 100%`；表格外层包横向滚动容器 |
| 颜色 | 只用已有 token 和主题绿 |

---

## 九、分期

| 阶段 | 内容 | 合入标准 |
|---|---|---|
| **P0** 基础设施 ✅ | 第五节全部：断点、`useBreakpoint`、`useUnreadNotice`、`AppShell` / `useAppShell`、9 页迁移、`meta.mobile`、viewport、token、`dvh` | 桌面零差异，单测通过，CI 绿。**已提交**（`97c8448`） |
| **P1** 门户上线 ✅ | 第六、七、八节：TopBar、TabBar、暂不支持页、写入口隐藏、「我的」页、纯文本评论（`useCommentSubmit` 加 `PlainCommentInput`）、9 个页面加 Login 的样式适配、i18n | 手机上能完成：登录 → 刷信息流 → 看帖子和评论 → 点赞收藏 → **发评论、回复** → 进圈子并加入 → 看消息 → 搜索；除评论外的写入口都不可见；深链打开发帖页显示暂不支持页；桌面端评论和回复回归无差异 |
| **P2**（可选） | 根据第 1.1 节的复盘数据决定：移动端发帖（纯文本加图片）、轮播滑动手势、顶栏滚动隐藏 | 单独评审 |
| **App** | 2.4 清单 | — |

- 从 `feature/mobile-compatibility-20261002` 切 P0、P1 两个子分支，各提一个 PR。
- P0 内部按「基础设施 → 每 3 个页面迁移一次」拆分 commit。
- 和 v1 相比：新组件从约 10 个降到 5 个（`AppShell`、`MobileTopBar`、`MobileTabBar`、`MobileUnsupported`、`PlainCommentInput`）；新 composable 从 5 个降到 3 个（`useBreakpoint`、`useUnreadNotice`、`useCommentSubmit`）。
- P1 内部把纯文本评论单独作为一个 commit 组：先抽 `useCommentSubmit` 并完成桌面回归，再加 `PlainCommentInput`。

---

## 十、风险与对策

| 风险 | 对策 |
|---|---|
| 抽壳影响桌面 | 纯机械迁移，按页截图对比，按页提交方便回滚 |
| `pageFade` 动画失效 | 页面内容根节点保留 `.main-content`。PostDetail 在迁移范围内，P0 验收时检查它的进出动画 |
| 迁移和未迁移两套布局写法并存 | 5 个未迁移页面在文档和代码注释中标明「待统一」；`useUnreadNotice` 单例保证两种写法的未读数一致 |
| SSE 生命周期变化 | `useUnreadNotice` 在登录态下全局常驻。验证退出登录和 token 过期时能正确 `stop`，不重复建连 |
| 隐藏写入口有遗漏 | P1 验收时逐页过一遍 6.4 的清单；用 `grep` 搜 `CommentEditor`、`CreateCircleModal`、`/create-post`、`/edit` 等关键词交叉检查 |
| 用户需要在手机上发帖 | 暂不支持页提供复制链接和「请求桌面网站」两条路径；根据第 1.1 节的复盘数据决定是否在 P2 做移动端发帖 |
| 抽 `useCommentSubmit` 影响桌面评论 | 纯提取、不改逻辑；补单测；桌面回归覆盖带图评论、@ 提及评论、回复根评论、回复某条回复四种场景 |
| 纯文本评论被 Markdown 误解析，或换行丢失 | 不转义特殊字符（理由见 6.6）；换行在 P1 实测，必要时转成硬换行 |
| 「请求桌面网站」时桌面布局在小屏上过挤 | 这只是兜底方案，不做额外适配；真机验证能用即可 |

---

## 十一、测试与验收

### 11.1 单测（Vitest，纳入现有 CI）

- **`tests/setup.js`**：加 `matchMedia` stub。默认 `matches: false`（桌面）；暴露 `__setMatches(bool)`，调用时触发 change 监听。
- **`useBreakpoint.spec.js`**：
  - 初始值取自 `matches`。
  - change 后 `isMobile` 跟着更新。
  - 多次调用只注册一个监听器。
  - 没有 `matchMedia` 时不抛错。
- **`useUnreadNotice.spec.js`**：
  - 两处调用拿到同一个 ref。
  - window 事件能减少或清零未读数，不会出现负数。
  - `stop` 后不再更新。
- **`tests/components/AppShell.spec.js`**（P0 已实施，子组件 stub 掉）：
  - 桌面渲染 `AppHeader` 和 `SideNav`，内容区让出侧栏宽度。
  - 不额外包 DOM：外框与内容是页面根节点的直接子节点。
  - SideNav 收起和展开时回写 offset。
  - 移动端外框下不渲染桌面外框和右栏，内容不偏移。
  - 总开关关闭时，手机断点仍渲染桌面外框。
  - 跨断点切换后，内容组件 `onMounted` 计数仍为 1。
  - P1 补充：移动端渲染 `MobileTopBar`；TabBar 是否出现跟随 `meta.mobile.tabBar`。
- **`tests/composables/useAppShell.spec.js`**（P0 已实施）：覆盖 `contentStyle` 和 `mobileShell` 在桌面、移动、收起、跨断点、总开关关闭各情况下的取值。
- **`useCommentSubmit.spec.js`**（用 `axios-mock-adapter` 模拟接口）：
  - 顶层评论和回复的请求体正确：带 `root_id` 和 `reply_to_id`；不传 `extraData` 和 `mentionIds` 时请求体里也没有这两个字段。
  - 乐观渲染对象字段补全。
  - `getUserInfo` 失败时仍然返回对象。
  - 并发提交时 `submitting` 能防止重复提交。
  - 接口失败时抛错，`submitting` 复位。
- **`PlainCommentInput.spec.js`**：
  - 内容为空 / 只有空白时发送按钮禁用。
  - 受控 `v-model:value`；提交成功后 emit `submit` 并清空内容；失败时保留内容。
  - 回复模式的请求参数和占位符；接近上限才显示字数。
- **`MobileCommentComposer.spec.js`**（#35）：访客不打开；打开时同步聚焦代理输入框、push `?compose=`、渲染后焦点移到 textarea；按目标保留草稿；关闭不多退一步；叠在回复抽屉上时返回只关输入抽屉；发送成功 / 失败；键盘遮挡时抽屉 `bottom` 抬高。
- **`useKeyboardInset.spec.js`**（#35）：用假的 `visualViewport` 覆盖遮挡高度计算、只在打开期间监听。
- **`CommentList.spec.js`**（#35）：主列表 / 回复抽屉里点回复打开输入抽屉；回复抽屉底部输入条；返回手势关闭顺序；残留 query 清理；跨断点。
- **暂不支持页的替换逻辑**：把 `resolveView` 抽成纯函数，测四种组合：桌面 × supported、桌面 × unsupported、移动 × supported、移动 × unsupported。
- **`tests/router/`**：断言所有 `meta.mobile` 取值合法；第 6.3 节列出的 5 个路由都带有 `supported: false`。
- `vitest.config.js` 的 coverage `include` 已经覆盖 `src/composables/**`；给 `useBreakpoint` 和 `useUnreadNotice` 设高门槛。

### 11.2 手动验收

- **DevTools 设备模拟**：360×800、375×812、390×844、430×932，外加 768 和 769 两个边界宽度。
- **真机**：iOS Safari（刘海机型）、Android Chrome。
  - 检查安全区、地址栏伸缩、登录输入不缩放。
  - 检查「请求桌面网站」能切到桌面布局。
- **每页检查项**：
  - 无横向滚动。
  - 顶栏和 TabBar 不遮挡内容。
  - 写入口全部不可见。
  - 返回按钮在深链直开时回到首页。
  - 跨断点缩放后状态不丢。
- **桌面回归**：在 1920、1440、1280、1024 四档宽度下，对 9 个迁移页面截图对比。
- **评论回归**（抽 `useCommentSubmit` 之后）：桌面端带图评论、@ 提及评论、回复根评论、回复某条回复，四种场景都能正常提交、乐观渲染，被 @ 的人能收到通知。
- **移动端评论**：iOS 和 Android 上键盘弹起后输入框可见；多行内容换行显示正确；发出的评论在桌面端显示一致。

---

## 十二、待决策

| # | 问题 | 推荐 | 备选 |
|---|---|---|---|
| 1 | 移动端断点 | **≤ 768px** | ≤ 767px（iPad mini 竖屏走桌面布局，但要改 30+ 处旧样式） |
| 2 | 移动端能否写评论 | ✅ **已定（v2.1）**：P1 做纯文本评论和回复（6.6） | — |
| 3 | 点赞、收藏、加入圈子 | **保留**，都是一次点击，组件现成 | 移动端完全只读（但登录和消息仍需要保留，省下的工作量很少） |
| 4 | 圈子导航抽屉 | **不做**，从「我的 → 圈子」和发现页进入 | 做一个左侧抽屉，只放「我的圈子」 |
| 5 | 移动端用桌面布局的方式 | **依赖浏览器的「请求桌面网站」**，零开发量 | 站内加「切换桌面版」开关（localStorage 标记，加动态改 viewport 为 `width=1280`，再加「切回移动版」悬浮按钮） |
| 6 | App 下载入口 | **预留配置**，值为空时不显示 | 现在就放「App 即将上线」占位 |
| 7 | 复盘数据怎么采集（1.1） | **先用服务端访问日志**：按 UA 统计手机端占比，按路径统计暂不支持页的 5 个路由被手机访问的次数，前端零改动 | 前端上报：暂不支持页 `onMounted` 时上报来源路由，需要后端提供埋点接口或接入第三方统计 |
