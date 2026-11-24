# 交接文档 — 移动端 Web 门户适配

> 更新：2026-10-02 · 分支：`feature/mobile-compatibility-20261002`
> 方案与范围：[docs/mobile-adaptation-plan.md](docs/mobile-adaptation-plan.md)（v2.1）。本文件只记录**进度、验证结果、和计划的偏差、坑、下一步**。

## 一、当前状态

- **P0 基础设施**：已提交（`97c8448`）。
- **P1 门户上线**：功能和样式已完成，**全部改动未提交**。移动端外框总开关 **`MOBILE_SHELL_ENABLED` 已改为 `true`**。
- **待做**：
  - 登录态页面的人工检查：「我的」、消息、暂不支持页（见 3.2）。
  - 真机验收：iOS Safari、Android Chrome。
- 第十二节待决策全部按推荐方案执行：
  - 断点 ≤ 768px。
  - P1 做纯文本评论。
  - 保留点赞、收藏、加入圈子。
  - 不做圈子抽屉。
  - 在手机上用桌面布局，依赖浏览器的「请求桌面网站」。
  - App 下载入口只预留配置。
  - 复盘数据先用服务端访问日志。

## 二、P1 改了什么

### 2.1 新增

| 文件 | 说明 |
|---|---|
| `src/components/layout/mobile/MobileTopBar.vue` | 两种模式：`main`（logo、搜索图标，访客另有登录按钮）和 `back`（返回、标题、`#actions` 插槽） |
| `src/components/layout/mobile/MobileTabBar.vue` | 登录态四项（首页 / 发现 / 消息 / 我的），访客三项（首页 / 发现 / 登录）。消息角标读 `useUnreadNotice`；再次点击当前项回到顶部 |
| `src/components/layout/mobile/MobileFeedSegment.vue` | 发现页和热榜页顶部的「发现 \| 热榜」分段切换 |
| `src/views/MobileUnsupported.vue` | 暂不支持页：复制链接、返回、回到首页、「请求桌面网站」提示。App 下载链接在 `APP_DOWNLOAD_URL` 有值时才显示 |
| `src/components/post/detail/PlainCommentInput.vue` | 纯文本评论和回复，对外接口与桌面编辑器一致 |
| `src/composables/useCommentSubmit.js` | 评论提交流程（调接口、补作者信息、组装乐观渲染对象、回灌 mentions）。桌面两个编辑器和移动端输入框共用 |
| `src/composables/useLogout.js` | 退出登录（确认弹窗、断开未读推送、清 token），桌面头像菜单和移动端「我的」页共用 |
| `src/utils/mobileNav.js` | 纯函数：`homePath`、`goBack`（深链直开时回落地页）、`loginLocation`（带 redirect）、`activeTab` |
| `src/utils/share.js` | `copyText`（Clipboard API，失败回落 `execCommand`）、`shareOrCopy`（系统分享面板，不支持时复制链接） |

### 2.2 修改

| 文件 | 改动 |
|---|---|
| `src/constants/breakpoints.js` | `MOBILE_SHELL_ENABLED = true` |
| `src/components/layout/AppShell.vue` | 移动端分支挂上 `MobileTopBar` 和 `MobileTabBar`，配置来自 `getMobileMeta(route)`，标题取 `titleKey`；透传 `#top-actions` 插槽 |
| `src/composables/useAppShell.js` | 移动端的 `contentStyle` 改为底部留白：有 TabBar 时让出 TabBar 高度，并计入安全区 |
| `src/App.vue` | 用 `isUnsupportedOnMobile` 把不支持的页面换成 `MobileUnsupported`，URL 不变；把外框开关同步到 `<html class="mobile-shell">` |
| `src/assets/main.css` | `html.mobile-shell` 下覆盖 `--header-height` 为 48px 加 `--safe-top`；去掉触屏点击高亮；手机上对话框和卡片弹窗宽度限制为 `100vw - 32px` |
| `src/router/mobileMeta.js` | 新增 `isUnsupportedOnMobile(route, mobileShell)` |
| `src/config/index.js` | 新增 `APP_DOWNLOAD_URL`（取 `VITE_APP_DOWNLOAD_URL`，默认空字符串） |
| `src/locales/*` | 新增 `mobile.*` 命名空间，`@` 已转义 |
| `CircleDetail.vue` | 移动端隐藏：免打扰、圈内发帖、管理入口、举报；分享走 `shareOrCopy`；**退出圈子加二次确认**（见第四节）；移动端忽略触屏模拟出的 hover |
| `PostDetail.vue`、`CommentList.vue` | 移动端评论和两处回复都换成 `PlainCommentInput` |
| `CommentEditor.vue`、`CommentReplyEditor.vue` | 提交逻辑改用 `useCommentSubmit`，行为不变（有单测守护请求体） |
| `UserProfile.vue` | 移动端：只读资料卡；设置列表（语言、协议、隐私、退出登录）；隐藏桌面右栏 |
| `MyPosts.vue` | 移动端隐藏编辑和删除菜单（原本就只是 toast 占位） |
| `MyPosts`、`MyGroups`、`MyFavorites`、`BrowseHistory` | 搜索框的 `style="width: 280px"` 改成 `.tab-search` 类，手机上占满整行 |
| `SearchResults.vue` | 移动端页内搜索框（`type=search`、字号 16px）；没有关键词时停留在本页并聚焦输入框，不再跳回首页 |
| `Discover.vue`、`Hot.vue` | 移动端顶部加分段切换 |
| `ImageCarousel.vue` | 移动端外框下用 ResizeObserver 按容器实际宽度收窄（原来写死约 675px，会被裁掉）；箭头常显 |
| `PostDetail`、`PostHeaderAndContent`、`CircleDetail`、`UserDetail` 的 CSS | 768px 以下去掉按 dvw 定宽导致的窄条问题；收小内边距；用户页头像从 260px 缩到 96px |
| 21 个文件的 `:hover` 规则（53 条） | 用脚本包进 `@media (hover: hover)`。选择器列表里混有非 hover 部分的，拆成两条规则。桌面效果不变，触屏上不再出现点一下就卡住的高亮 |
| `AppHeader.vue` | 退出登录改用 `useLogout` |
| `vitest.config.js` | 新模块加进高门槛组；全局基线提高到 55 / 55 / 90 / 66 |

### 2.3 测试

- 新增：
  - `tests/helpers/plugins.js`：测试用的 i18n 和内存路由。
  - `components/MobileTabBar`、`components/PlainCommentInput`、`composables/useCommentSubmit`、`utils/mobileNav`、`utils/share` 的 spec。
- 更新：`AppShell` 的 spec（新增移动端外框、帖子详情 meta、`#top-actions`、跨断点切换的用例）、`useAppShell` 的 spec、`mobileMeta` 的 spec（加 `isUnsupportedOnMobile`）。

## 三、验证结果

### 3.1 已验证

- **单测**：19 个文件、281 个用例全部通过（P0 之后新增 47 个）。覆盖率门槛和 `npm run build` 都通过。
- **浏览器 375×812，访客态**：Discover、Hot、Home、PostDetail、CircleDetail、UserDetail、Search（有关键词和没有关键词两种）、Login。
  - 所有页面都没有横向滚动条（`scrollWidth === 375`）。
  - 修了 4 个问题：
    - 轮播图被裁。
    - 帖子详情正文被压成约 200px 宽的窄条。
    - 圈子页帖子列被压窄。
    - 用户页内容区被压窄、头像过大。
  - 顶栏和 TabBar 的模式、高亮、标题都正确。
  - 帖子详情使用返回式顶栏、没有 TabBar，评论区是纯文本输入框（访客态显示登录引导）。
  - 圈子页的发帖、管理、免打扰入口都已隐藏。
- **跨断点实时切换**：在帖子详情页把视口从 1440 改到 375 再改回去，外框、评论组件、`--header-height`、底部留白都正确切换，页面不刷新。
- **桌面 1440 抽查**：帖子详情页外框、内联偏移样式、Markdown 编辑器都和原来一样，`html` 上没有 `mobile-shell` 类。
- **换行**：md-editor-v3 内置 markdown-it 配置是 `breaks: true`，纯文本评论里的单个换行会正常显示，不需要转换。

### 3.2 没有验证到的（需要你检查）

- **登录态页面**：「我的」页、消息页、暂不支持页（`/create-post` 等）。没有测试账号；尝试塞假 token 也会在 401 后立刻被清掉。这几页都有组件单测或替换逻辑的单测覆盖，但样式没有用眼睛看过。
- **桌面评论回归**：带图评论、@ 提及评论、回复根评论、回复某条回复。需要登录才能实测；请求体与改动前一致这一点已有单测保证。
- **真机**：iOS Safari、Android Chrome 都没测。需要看安全区、地址栏伸缩、键盘弹起时输入框是否可见、「请求桌面网站」能否切到桌面布局。
- 1920、1280、1024 三档桌面宽度。

## 四、和计划文档的偏差

1. **「加入圈子」按钮没有挪进顶栏 `#top-actions`**，仍留在圈子头部。移动端头部原本就纵向排列，按钮已经占满整行，挪走没有收益。`#top-actions` 插槽已经做好，以后需要时可以用。
2. **新增：移动端退出圈子二次确认**（`mobile.leaveConfirm`）。原逻辑靠 hover 把按钮文案换成「退出圈子」作为提示，触屏没有 hover，点一下就直接退圈。
3. **圈子分享**：移动端走系统分享面板或复制链接；桌面端仍然是「功能开发中」，没有改。
4. **`--header-height` 的覆盖挂在 `html.mobile-shell` 上**，而不是直接用媒体查询，这样和 JS 的外框开关严格同步。
5. **抽出了 `useLogout`**。计划写的是不抽 `useUserMenu`，但退出登录的逻辑原本写在 `AppHeader` 组件里，复制一份更糟。
6. **back 顶栏的标题用的是路由的通用标题**（例如「帖子详情」），没有显示圈子名或帖子名。
7. **暂不支持页挂在 `AppShell` 的 `main` 顶栏加 TabBar 下**，用户可以直接去别处，不只有一个返回按钮。
8. **没做**：
   - `LoginCard` 里 3 处 `NTooltip trigger="hover"` 没改（是第三方登录图标上的提示，点击本身就会跳转登录）。
   - 登录页 `AnimatedBackground` 没有为移动端减少粒子。
   - 这两项在 375 宽度下实测外观没有问题，暂时不处理。

## 五、坑（接手前必读）

1. **本地 Node 26 下单测会全部报 `localStorage.clear` undefined。** 原因是 Node 内置的 Web Storage 遮住了 jsdom 的实现，与本次改动无关；CI 用 Node 20/22 不受影响。本地这样跑：
   ```bash
   NODE_OPTIONS=--no-experimental-webstorage npm run check
   ```
2. **不要用 `npx vitest`**：它会临时拉取 vitest 5，和项目锁定的 3.x 不一致。缺依赖时执行 `npm ci`。
3. **浏览器预览面板隐藏时，`requestAnimationFrame` 会被挂起。** 路由 `<Transition>` 和首页信息流的 `feed-switch` 过渡都会卡住，页面停在旧路由或骨架屏，CSS 过渡也会停住。用浏览器自动化验收时，每个页面整页加载，或先注入 `*{transition:none!important}`。这不是代码 bug。
4. **紧急回退**：把 `MOBILE_SHELL_ENABLED` 改回 `false`，手机上就会恢复原桌面布局。暂不支持页、纯文本评论、`html.mobile-shell` 样式都会跟着关闭。
5. **不要把页面内容挪进 `AppShell`**（原因见计划文档 5.3）。新页面接入的写法：
   ```vue
   <AppShell v-model:offset="offset" />
   <div class="main-content" :style="contentStyle">...</div>
   ```
   ```js
   const { offset, mobileShell, contentStyle } = useAppShell()
   ```
6. **未读数**：不要在组件里直接调用 `useNoticeStream`，统一走 `useUnreadNotice`；新增登出入口时用 `useLogout`，或者先调用 `stopUnreadNotice()`。
7. **移动端专属逻辑统一用 `useMobileShell()` 或 `useAppShell().mobileShell` 判断**，不要直接用 `useBreakpoint().isMobile`，否则总开关关掉时会出现不一致。
8. **新写的 hover 样式**，凡是在移动端支持的页面里，都要包进 `@media (hover: hover)`。只在 hover 时才显示的元素，移动端要改成常显。

## 六、下一步

1. **你来检查** 3.2 里的登录态页面和桌面评论回归。
2. **真机验收**（计划文档 11.2）。
3. 提交 P1。可以按功能拆分 commit：外框和导航、暂不支持页、写入口隐藏和「我的」页、纯文本评论、样式适配和 hover、测试。
4. P2（可选，看复盘数据再定）：移动端发帖、轮播滑动手势、顶栏滚动隐藏、在 back 顶栏显示具体的圈子名或帖子名。

## 七、常用命令

```bash
NODE_OPTIONS=--no-experimental-webstorage npm run check
```

```bash
npm run dev
```
