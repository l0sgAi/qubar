# 参与贡献趣吧前端

[English](CONTRIBUTING.md) | **简体中文**

感谢你关注趣吧！bug 反馈、想法、文档修正和代码都欢迎。
这份指南介绍从克隆仓库到 PR 合并需要了解的内容。

## 开始之前

- **小改动**（错别字、文档、明显的 bug）：直接提 PR。
- **较大的改动**（新页面、新接口对接、布局或移动端改动）：先开 issue，把方案对齐后再写代码。
  想认领已有的 issue，在下面留言即可。
- issue 标题用 Conventional Commit 风格，例如 `feat(notice): show review results` 或
  `docs: add a contributing guide`。正文建议包含：
  - **背景** – 现在哪里有问题或缺了什么，为什么要做
  - **后端接口** – 涉及哪些接口，附上后端文档链接（见[接口以后端文档为准](#接口以后端文档为准)）
  - **验收标准** – 用清单写明怎样算完成
  - **范围外** – 这次明确不做的内容
  - **涉及文件** – 预计要改的文件
- 现有 issue 大多用中文写，用英文写也完全可以。
- 发现安全问题？请不要公开提 issue，先私下联系维护者。

## 本地开发

需要 **Node.js 20**（与 CI 一致）和 npm。

```bash
git clone https://github.com/<your-fork>/qubar-frontend.git
cd qubar-frontend
npm install
npm run dev     # http://localhost:5173
```

Node 25 及以上版本自带的 `localStorage` 会覆盖 jsdom 的实现，导致所有用例报
`Cannot read properties of undefined (reading 'clear')`。请使用 Node 20，或者用
`NODE_OPTIONS=--no-webstorage` 运行测试。

前端需要连后端。`npm run dev` 连哪个后端，由 `.env.development` 里的 `VITE_API_BASE` 决定：

- **本地后端** – 自己运行 [l0sgAi/qubar](https://github.com/l0sgAi/qubar)（它的 README 里有 Docker 开发环境），
  把 `VITE_API_BASE` 指过去。
- **线上后端** – 使用与 `.env.production` 相同的值。浏览没问题，但操作的是真实数据，写操作请谨慎。

个人配置写在 `.env.development.local`（已被 git 忽略），不要改已提交的文件。
全部变量见 [README](README.zh-CN.md#环境变量)。

## 项目结构与代码约定

目录总览见 [README](README.zh-CN.md#-项目结构)。日常需要遵守的约定：

- **组件** – Vue 3 单文件组件，使用 `<script setup>`，纯 JavaScript（不用 TypeScript）。
  UI 基础组件用 Naive UI；自己写之前先看 `src/components/common/` 里有没有现成的：下拉菜单用 `AppDropdown`，
  站内跳转用 `SmartLink`（渲染真实的 `<a href>`，同时走 SPA 路由）等。
- **样式** – 改样式、覆盖 Naive UI、写动画或改文案之前，先读 frontend-constraints skill：
  [.claude/skills/frontend-constraints/SKILL.md](.claude/skills/frontend-constraints/SKILL.md)（`.agents/skills/`
  下有同一份）。要点：复用 `src/assets/main.css` 里的设计 token，不要写死颜色；Naive UI 中 teleport 出去的部分
  （下拉、弹窗、气泡等）要写全局样式，`scoped` 样式管不到它们。
- **国际化** – 所有面向用户的文字都走 vue-i18n。新增 key 时 `src/locales/zh-CN.js` 和 `src/locales/en-US.js`
  **两边都要加**。`@`、`{`、`}` 在文案里是保留字符，字面量 `@` 要写成 `{'@'}`。
- **接口封装** – 每个接口都封装到 `src/api/` 下对应的模块（按后端领域划分），使用 `src/utils/request.js`
  导出的 `request`，并写 JSDoc 说明参数和返回值。不要在组件里直接调 axios，也不要写死后端地址——
  它来自 `src/config/index.js` 的 `API_BASE`。
- **移动端** – ≤ 768px 的屏幕使用移动端外框。新增路由如果移动端用不了，必须设置
  `meta: { mobile: { supported: false } }`；否则按需选择 `topBar` / `tabBar`（见
  [src/router/mobileMeta.js](src/router/mobileMeta.js)）。逻辑判断用 `useBreakpoint()`，样式用 `768px` 媒体查询，
  并在手机宽度下检查页面。完整规则见 [docs/mobile-adaptation-plan.md](docs/mobile-adaptation-plan.md)。
- **注释与文档** – 代码注释和内部文档用中文，与现有代码保持一致。issue 和 PR 讨论用英文也可以。
- 不要提交任何密钥。`VITE_*` 变量里的内容都会出现在公开的构建产物里。

### 接口以后端文档为准

后端行为以后端仓库 `develop` 分支的文档为准：
[l0sgAi/qubar/docs](https://github.com/l0sgAi/qubar/tree/develop/docs)（其中的 `*-frontend-api.md` 文件和
[`docs/api/`](https://github.com/l0sgAi/qubar/tree/develop/docs/api)）。如果文档和后端实际返回不一致，
请到后端仓库提出，不要在前端悄悄绕过。

## 测试

```bash
npm test               # 全部单元测试
npm run test:watch     # 监听模式，改代码自动重跑
npm run test:coverage  # 带覆盖率报告（coverage/index.html）
npm run check          # 覆盖率测试 + 生产构建，与 CI 一致
```

- 提 PR 之前 `npm run check` 必须通过。
- 用例放在 `tests/` 下，目录结构与 `src/` 对应（例如 `src/utils/share.js` → `tests/utils/share.spec.js`），
  文件名 `*.spec.js`。导入源码用 `@/` 别名。
- 测试运行在 jsdom 中。[tests/setup.js](tests/setup.js) 会在每个用例结束后清空 `localStorage` / `sessionStorage`、
  恢复真实定时器，并还原 `vi.spyOn` / `vi.stubGlobal`。
- 涉及时间的逻辑（防抖、重试、轮询）一律用 `vi.useFakeTimers()`，不要依赖真实时间。
- HTTP：测试 `src/utils/request.js` 本身时，把 `axios-mock-adapter` 挂在真实实例上；测试其他模块时直接
  `vi.mock('@/api/xxx')`。
- 改了 `src/locales/` 一定要跑 `npm test`。[tests/i18n.spec.js](tests/i18n.spec.js) 会用生产环境同款编译器编译全部文案，
  并校验中英文的 key 和插值占位符一致。vue-i18n 在 dev 模式下会吞掉编译错误，只有这个用例能提前发现。
- 覆盖率门槛配置在 [vitest.config.js](vitest.config.js)，**只升不降**。新用例让覆盖率提高后，请同步调高门槛；
  不要为了让 CI 通过而调低。

## 分支、提交与 PR

- 从 **`main`** 拉分支，PR 也指向 **`main`**。每次合并到 `main` 都会部署到线上。
- 分支命名：`<类型>/<主题>-<YYYYMMDD>`，例如 `feature/multi-device-login-20261022`、
  `fix/sse-conn-evicted-20261022`、`ci/claude-review-20261022`。
- 提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/)，用英文写，例如
  `feat(circle): add pending-post moderation queue page` 或 `fix(notice): close SSE on conn-evicted`。
- 一个 PR 解决一个 issue（描述里写 `Closes #N`）。说明改了什么、怎么验证的（`npm run check`、点过哪些页面、
  桌面和手机宽度），以及哪些没能验证。

### Review 与合并

- 每个指向 `main` 的 PR 都会运行 `ci.yml` 里的两个必需检查：`unit`（单测 + 覆盖率门槛）和 `build`（生产构建）。
- 本仓库分支发起的非草稿 PR 还会由 Claude 自动 review（`claude-review.yml`），发行内评论和一条总结。
  fork 发起的 PR 拿不到仓库 secrets，会跳过这一步。
- 对仓库有写权限的人可以在 issue 或 PR 里评论 `@claude ...`，按需让 Claude 处理（`claude.yml`）。
- 由维护者 review 并合并。如果 `main` 比你的分支新，把 `origin/main` 合并进来（或在 GitHub 上点「Update branch」）。

## 关于 AI agent

这个仓库的很多开发工作由 AI 编程 agent 完成。它们的改动走完全相同的流程：issue、PR、`unit` / `build` 检查，
合并前经过人工 review。

仓库里和 agent 相关的配置：

- [.claude/skills/frontend-constraints](.claude/skills/frontend-constraints) 和
  [.agents/skills/frontend-constraints](.agents/skills/frontend-constraints) – 同一套前端约束，
  分别打包给 Claude Code 和其他 agent 使用
- `.claude/launch.json` – agent 预览时如何启动开发服务器
- [.github/workflows/claude-review.yml](.github/workflows/claude-review.yml) – PR 自动 review 使用的提示词

作为人类贡献者，你不需要用到这些；不过 skill 里的样式和 i18n 规则值得一读。

## 许可证

参与贡献即表示你同意你的贡献按 [MIT License](LICENSE) 授权。
