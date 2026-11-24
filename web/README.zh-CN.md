# 趣吧前端

[English](README.md) | **简体中文**

[趣吧（Qubar）](https://github.com/l0sgAi/qubar) 的 Web 前端。趣吧是一个类似百度贴吧 / Reddit 的兴趣社区。
本项目使用 Vue 3（JavaScript）+ Naive UI 开发，Vite 构建，界面支持中英文。

线上地址：<https://qubar.site>

## ✨ 功能特性

- **账号** – 邮箱密码注册（验证码）、找回密码，以及 Google / GitHub / Azure AD 第三方登录（一次性 code 换取 token）。
  同一账号可以在多个设备上同时登录。
- **游客模式** – 未登录也能浏览信息流、帖子、圈子、用户主页和搜索结果；点赞、评论等操作会引导登录。
- **信息流与发现** – 首页信息流、热门榜、发现页，以及帖子 / 圈子 / 用户的全文搜索。
- **帖子与评论** – Markdown 编辑器与图片上传，点赞、收藏，两级评论与回复。
  待审核的帖子会向作者提示审核状态，直到圈子管理者审核通过。
- **@ 提及** – 发帖和评论时可以选择 @ 的用户；提及按用户 ID 绑定，渲染时高亮显示。
- **兴趣圈子** – 创建、加入、浏览圈子。圈主和管理员有成员管理（角色、禁言、封禁、入圈审核）、
  圈子资料编辑和待审帖子队列等管理页面。
- **AI 机器人** – 圈主和管理员可以管理本圈机器人（每个圈子最多 5 个）；平台管理员管理全站机器人。
- **消息中心** – 通知列表，支持标记已读 / 全部已读。未读数通过 SSE 实时推送，SSE 不可用时降级为 30 秒轮询。
- **个人中心** – 用户公开主页，个人资料编辑（头像裁剪上传），我的帖子、我的圈子、收藏和浏览历史。
- **移动端网页** – 宽度 ≤ 768px 的屏幕使用专门的移动端布局（顶栏 + 底部 TabBar）。
  管理类页面在移动端显示「暂不支持」页（见[移动端](#-移动端)）。
- **国际化** – 简体中文和英文，默认跟随浏览器语言，切换后保存在本地。

## 🛠 技术栈

| 库 | 用途 |
|----|------|
| [Vue 3](https://vuejs.org/) | UI 框架（`<script setup>` 单文件组件） |
| [Vite](https://vite.dev/) | 开发服务器与构建 |
| [Vue Router](https://router.vuejs.org/) | 路由、登录守卫、路由级移动端配置 |
| [Naive UI](https://www.naiveui.com/) | 组件库（深色主题） |
| [vue-i18n](https://vue-i18n.intlify.dev/) | 中英文文案 |
| [axios](https://axios-http.com/) | HTTP 请求，统一封装 |
| [md-editor-v3](https://github.com/imzbf/md-editor-v3) | Markdown 编辑器 |
| [cropperjs](https://github.com/fengyuanchen/cropperjs) | 图片裁剪 |
| [Vitest](https://vitest.dev/) + [Vue Test Utils](https://test-utils.vuejs.org/) | 单元测试（jsdom） |

## 🚀 快速开始

### 环境要求

- **Node.js 20**（与 CI 一致）和 npm
- 一个可访问的[趣吧后端](https://github.com/l0sgAi/qubar)（本地或远程均可）

### 安装与启动

```bash
git clone https://github.com/l0sgAi/qubar-frontend.git
cd qubar-frontend
npm install
npm run dev
```

开发服务器地址为 <http://localhost:5173>，启动后会自动打开浏览器。

| 命令 | 作用 |
|------|------|
| `npm run dev` | 启动 Vite 开发服务器（读取 `.env.development`） |
| `npm run build` | 生产构建，输出到 `dist/`（读取 `.env.production`） |
| `npm run preview` | 本地预览生产构建 |
| `npm test` | 运行一次全部单元测试 |
| `npm run test:watch` | 监听模式运行测试 |
| `npm run test:coverage` | 带覆盖率运行测试，低于门槛会失败 |
| `npm run check` | 提交前自检：覆盖率测试 + 生产构建 |

### 环境变量

后端地址**没有**写死在代码里。Vite 从仓库根目录的 `.env.*` 读取，再由
[src/config/index.js](src/config/index.js) 以 `API_BASE` / `APP_DOWNLOAD_URL` 提供给应用其他部分使用。

| 变量 | 必填 | 用途 |
|------|------|------|
| `VITE_API_BASE` | 是 | 趣吧后端 API 的根地址，所有后端请求都用它（axios 封装、SSE 通知流、第三方登录） |
| `VITE_APP_DOWNLOAD_URL` | 否 | 移动端「暂不支持」页上的 App 下载链接；为空时不显示 |

- `npm run dev` 读取 `.env.development`；`npm run build` 读取 `.env.production` 并写入构建产物。
- 想让本地开发服务器连别的后端、又不想改动已提交的文件，可以新建 `.env.development.local`
  （`*.local` 已被 git 忽略），在里面设置 `VITE_API_BASE`。
- 只有 `VITE_` 开头的变量会进入浏览器端的代码，所以不要在这些文件里放任何密钥。

## 📱 移动端

- 断点和总开关在 [src/constants/breakpoints.js](src/constants/breakpoints.js)：`BP_MOBILE_MAX = 768` 和
  `MOBILE_SHELL_ENABLED`。开关打开时，≤ 768px 的屏幕渲染移动端外框（`src/components/layout/mobile/` 下的
  `MobileTopBar` / `MobileTabBar`）；关掉则回退到桌面布局。CSS 媒体查询读不到 JS 常量，`@media` 里需要手写同样的 `768px`。
- 每个路由可以配置 `meta.mobile`（默认值和工具函数见 [src/router/mobileMeta.js](src/router/mobileMeta.js)）：
  - `supported` – 设为 `false` 时，移动端改为渲染 `MobileUnsupported.vue`，URL 不变
  - `topBar` – `main`（logo + 搜索）或 `back`（返回 + 标题）
  - `tabBar` – 是否显示底部 TabBar
- 目前移动端不支持的页面：发帖、圈子成员 / 资料编辑 / 机器人 / 内容审核，以及机器人管理。
- 方案与范围：[docs/mobile-adaptation-plan.md](docs/mobile-adaptation-plan.md)。

## 📁 项目结构

```
qubar-frontend/
├── src/
│   ├── api/            # 按后端领域划分的接口封装（auth、post、comment、circle、notice、agent、moderation 等）
│   ├── assets/         # main.css：全局样式与设计 token
│   ├── components/     # 可复用组件，按业务区域分组
│   │   ├── layout/     #   应用外框、顶栏、侧边导航、右侧栏；mobile/ 下是移动端顶栏和 TabBar
│   │   ├── common/     #   通用组件（AppDropdown、SmartLink、图片裁剪等）
│   │   └── ...         #   auth/、feed/、post/、circle/、user/、agent/
│   ├── composables/    # 组合式函数（SSE 未读数、断点、@ 提及、上传、点赞收藏切换等）
│   ├── config/         # 运行时配置，来自 VITE_* 环境变量
│   ├── constants/      # 公共常量（断点、圈子角色、审核枚举）
│   ├── locales/        # zh-CN / en-US 文案
│   ├── router/         # 路由、登录守卫、meta.mobile 工具函数
│   ├── utils/          # 请求封装、登录态存储、游客模式、内容净化、@ 提及解析等
│   ├── views/          # 路由页面：auth/、feed/、post/、circle/、user/、admin/，以及消息中心和兜底页
│   ├── App.vue
│   └── main.js
├── tests/              # Vitest 用例，目录结构与 src/ 对应
├── public/             # 原样复制：CNAME、favicon、独立宣传页（/landing）
├── docs/               # 方案与对接文档
├── scripts/            # 构建诊断脚本
├── .github/workflows/  # CI、部署、Claude review
├── .env.development    # 开发环境变量
├── .env.production     # 生产环境变量
├── vite.config.js
└── vitest.config.js    # 复用 vite.config.js；覆盖率门槛
```

## 🧪 测试与 CI

```bash
npm test        # 全部单元测试
npm run check   # 覆盖率测试 + 生产构建，与 CI 一致
```

- 用例放在 `tests/` 下，目录结构与 `src/` 对应，文件名 `*.spec.js`。覆盖率统计范围是
  `src/utils`、`src/composables` 和 `src/router`，门槛配置在 [vitest.config.js](vitest.config.js)。
- 修改 `src/locales/` 后要跑 `npm test`：[tests/i18n.spec.js](tests/i18n.spec.js) 会用生产环境同款编译器编译全部文案，
  并校验中英文的 key 和插值占位符一致。
- 用例写法见 [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md#测试)。

[.github/workflows](.github/workflows) 下的工作流：

| 工作流 | 触发条件 | 内容 |
|--------|----------|------|
| `ci.yml` | 指向 `main` 的 PR（也被 `deploy.yml` 调用） | 两个检查：`unit`（单测 + 覆盖率门槛）和 `build`（生产构建） |
| `deploy.yml` | push `main` | 先跑同一套 CI，通过后构建并部署到 GitHub Pages |
| `claude-review.yml` | 本仓库分支发起的、指向 `main` 的非草稿 PR | Claude 自动代码审查（行内评论 + 一条总结评论） |
| `claude.yml` | issue / PR 评论或 review 中出现 `@claude` | 按需运行 Claude；只有对仓库有写权限的用户能触发 |

## 🌐 部署

每次 push `main`，`deploy.yml` 会把站点部署到 GitHub Pages。自定义域名配置在 `public/CNAME`；
构建时会把 `index.html` 复制为 `404.html`，让 SPA 的深层链接可以直接打开。
Pages 设置、分支保护和常见问题见 [DEPLOY.md](DEPLOY.md)。

## 📚 文档索引

| 文档 | 内容 |
|------|------|
| [DEPLOY.md](DEPLOY.md) | GitHub Pages 部署、CI 检查与分支保护 |
| [OAUTH_CALLBACK.md](OAUTH_CALLBACK.md) | 第三方登录回调与一次性 code 换 token |
| [HANDOFF.md](HANDOFF.md) | 移动端门户开发时的阶段性交接记录（2026 年 10 月），之后不再更新 |
| [docs/mobile-adaptation-plan.md](docs/mobile-adaptation-plan.md) | 移动端 Web 门户方案：范围、断点、`meta.mobile`、逐页清单 |
| [docs/mobile-circle-card-plan.md](docs/mobile-circle-card-plan.md) | 移动端「已加入的圈子」卡片紧凑化 |
| [docs/testing-ci-implementation-plan.md](docs/testing-ci-implementation-plan.md) | 测试体系、覆盖率门槛与 CI 门禁 |
| [docs/like-collect-idempotent-integration-plan.md](docs/like-collect-idempotent-integration-plan.md) | 点赞 / 收藏幂等接口对接 |
| [docs/mention-backend-requirements.md](docs/mention-backend-requirements.md) | @ 提及精确绑定的后端需求 |
| [docs/mention-user-ids-integration.md](docs/mention-user-ids-integration.md) | `mention_user_ids` 的持久化与回传 |
| [docs/notice-sse-backend-requirements.md](docs/notice-sse-backend-requirements.md) | 未读通知数 SSE 推送的后端需求 |
| [docs/comment-locate-backend-requirements.md](docs/comment-locate-backend-requirements.md) | 点击通知定位到评论的后端需求 |

后端：

- [l0sgAi/qubar](https://github.com/l0sgAi/qubar) – Go 后端
- [后端 `develop` 分支的 docs/](https://github.com/l0sgAi/qubar/tree/develop/docs) – 接口以这里为准。
  前端对接文档是其中的 `*-frontend-api.md` 文件和
  [`docs/api/`](https://github.com/l0sgAi/qubar/tree/develop/docs/api) 目录。

## 🤝 参与贡献

欢迎提 issue 和 PR。本地开发、代码约定、测试和 PR 流程见 [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md)。

## 📄 许可证

[MIT License](LICENSE)
