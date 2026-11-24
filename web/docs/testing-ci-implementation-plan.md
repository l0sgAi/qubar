# 前端测试体系 + CI 门禁 — 实施文档（阶段 1 + 阶段 3）

> 版本：2026-09-28 · 分支：`claude/busy-gates-p989w4`（基于 `main@44a8ac9`）
> 对应 issue：[l0sgAi/qubar-frontend#14](https://github.com/l0sgAi/qubar-frontend/issues/14)
> 状态：**已实施**。实施记录见第九节。

---

## 一、本 PR 的范围

issue 第八节把整体工作拆成 6 个阶段。本 PR 交付「测试框架 + 部署流水线的测试环节」，即：

| issue 阶段 | 本 PR | 说明 |
|---|---|---|
| 1. 基础设施 + P0 | ✅ | Vitest + jsdom + @vue/test-utils + coverage-v8；P0 全部用例；`ci.yml`（`unit` + `build`） |
| 2. ESLint | ❌ | 会改动较多现有文件，按 issue 建议单独 PR，避免与进行中的功能分支冲突 |
| 3. 部署门禁 + 分支保护 | ✅（仓库侧部分） | `deploy.yml` 复用 CI，检查不过不部署；README 说明。**分支保护需管理员在 GitHub 设置中手动开启**（见第七节） |
| 4. P1 + 覆盖率 80% 门槛 | ⏳ 部分 | 本 PR 先设「基线门槛」（按当前实测值取整，只升不降），80% 门槛随 P1 用例一起提升 |
| 5. P2 组件测试 | ❌ | 后续 PR |
| 6. P3 E2E（Playwright） | ❌ | 后续 PR；本 PR 的 `ci.yml` 已为 `e2e` job 预留位置（`build` 上传 `dist/`） |

**不改动任何业务逻辑**。唯一的源码改动是路由文件的可测性重构（第五节），行为保持不变。

### 关于 issue 中「en-US 两条文案编译失败」

issue 第一节列出的 `notice.templates.likePost` / `collectPost` 已在 l0sgAi/qubar-frontend#15（`11f0b20`）中修复，当前 `main` 上为 `'{actor} liked your post "{snippet}"'`。
本 PR 的 i18n 校验用例会在 CI 中持续守住这类问题；实施时会**临时回退这两条文案验证用例确实失败**，再恢复（见第八节「反向验证」）。

---

## 二、现状（main@44a8ac9）

| 项目 | 现状 |
|---|---|
| npm scripts | `dev` / `build` / `preview` |
| 测试 / 测试依赖 | 无 |
| CI | 仅 `deploy.yml`：push `main`/`master` → `npm ci && npm run build` → 部署 GitHub Pages |
| PR 检查 | 无 |
| Node | CI 20；本地 / 云端开发环境 22 |
| 构建相关配置 | `vite.config.js`：`@` 别名、`@vitejs/plugin-vue`、`landing-redirect` 中间件、`generate-404` 插件（`closeBundle` 时复制 `dist/index.html`） |
| 环境变量 | `.env.development` / `.env.production` 只有 `VITE_API_BASE` |

与测试相关的模块级副作用（issue 第十节风险项）：

- `src/utils/auth.js`：导入时迁移 `quba_*` 旧 token 键 → 测试前需要 `localStorage` 可用（jsdom 提供）。
- `src/locales/index.js`：导入时读 `localStorage` / `navigator.language` 决定语言。
- `src/router/index.js`：导入时 `createRouter(createWebHistory)` 并静态导入 `Login.vue`。
- `src/utils/request.js`：导入时 `axios.create`，并注册拦截器；401 时 `window.location.href = '/'`。

---

## 三、技术选型与依赖

全部为 `devDependencies`，不影响生产包体积。

| 包 | 版本 | 用途 / 选择理由 |
|---|---|---|
| `vitest` | `^3.2` | 与 Vite 6 共用插件和别名；issue 指定 Vitest 3（Vitest 4 需要 Vite 7 生态，暂不升级） |
| `@vitest/coverage-v8` | `^3.2`（与 vitest 同版本） | 覆盖率 |
| `jsdom` | `^26.1` | DOM 环境。选 26 而非最新 30：jsdom 27+ 要求 Node ≥ 20.19、30 要求 Node ≥ 22.22，CI 用 Node 20，26 兼容面最宽 |
| `@vue/test-utils` | `^2.5` | 组件挂载（P0 路由测试用来渲染 stub；为 P2 预备） |
| `axios-mock-adapter` | `^2.1` | 在**真实** axios 实例上 mock 响应，拦截器能完整跑到 |
| `@intlify/message-compiler` | `^9.14.5` | 与 `vue-i18n@9.14.5` 同版本（已是其间接依赖，锁文件会复用同一份），用生产同款编译器校验文案 |

不引入：Playwright（阶段 6）、ESLint（阶段 2）、Prettier / TypeScript（issue 明确排除）。

---

## 四、目录与配置设计

```text
qubar-frontend/
├── vitest.config.js          # mergeConfig(viteConfig, { test: ... })
├── tests/
│   ├── setup.js              # 每个用例前后清理 localStorage / sessionStorage / fake timers / mocks
│   ├── i18n.spec.js          # 文案校验（issue 指定路径）
│   ├── composables/
│   │   └── useInteractionToggle.spec.js
│   ├── router/
│   │   └── guard.spec.js
│   └── utils/
│       ├── request.spec.js
│       ├── sanitize.spec.js
│       ├── throttle.spec.js
│       ├── guest-access.spec.js
│       ├── mention.spec.js
│       └── mentionResolve.spec.js
└── .github/workflows/
    ├── ci.yml                # 新增
    └── deploy.yml            # 改造：needs ci
```

测试放在独立的 `tests/` 目录（而非与源码并排），理由：`src/` 结构保持不变，`vite build` 不会扫描到测试文件，覆盖率的 `include` 也更好写。

### 4.1 `vitest.config.js`

```js
import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config.js'

export default mergeConfig(viteConfig, defineConfig({
  test: {
    environment: 'jsdom',
    include: ['tests/**/*.spec.js'],
    setupFiles: ['tests/setup.js'],
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: 'v8',
      include: ['src/utils/**', 'src/composables/**', 'src/router/**'],
      reporter: ['text', 'text-summary', 'json-summary', 'html'],
      thresholds: { /* 见 4.3 */ }
    }
  }
}))
```

- 复用 `vite.config.js`：`@` 别名、`plugin-vue` 自动生效。`server.open: true` 对 vitest 无影响。
- `generate-404` 插件的 `closeBundle` 在 dev / vitest 服务器关闭时**也会触发**（此时没有 `dist/`，会打印 ENOENT 报错；若 `dist/` 已存在则会被改写），因此给该插件加上 `apply: 'build'`，只在生产构建时生效。这是本 PR 对 `vite.config.js` 的唯一改动，构建产物不变。
- `import.meta.env.VITE_API_BASE`：vitest 以 `mode=test` 运行，不会读取 `.env.development`，值为 `undefined`；`request.js` 的 `baseURL` 为 `undefined` 时 axios 使用相对路径，axios-mock-adapter 按相对路径匹配即可，无需额外配置。
- `restoreMocks` / `unstubGlobals`：每个用例后自动还原 `vi.spyOn` 和 `vi.stubGlobal`，防止用例间串扰。

### 4.2 `tests/setup.js`

```js
afterEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  vi.useRealTimers()
})
```

`window.location` 的 stub 不放在全局 setup：只有 `request.spec.js` 需要，按需 `vi.stubGlobal('location', { href: '' })`，由 `unstubGlobals` 自动还原。

### 4.3 覆盖率门槛（只升不降）

- 统计范围：`src/utils/**`、`src/composables/**`、`src/router/**`（纯逻辑层，issue 第四阶段的考核范围）。
- 本 PR 的 `thresholds` 取**实施后实测值向下取整**作为基线（数值记录在第九节），任何 PR 让覆盖率跌破基线都会让 `CI / unit` 失败。
- 阶段 4 补齐 P1 用例后把 `src/utils/**`、`src/composables/**` 的 lines / branches 提到 80%。

### 4.4 npm scripts

| script | 命令 | 用途 |
|---|---|---|
| `test` | `vitest run` | 一次性跑全部单测 |
| `test:watch` | `vitest` | 本地开发监听 |
| `test:coverage` | `vitest run --coverage` | 带覆盖率 + 门槛校验（CI 用） |
| `check` | `npm run test:coverage && npm run build` | 一键本地自检（阶段 2 / 6 再串上 `lint`、`test:e2e`） |

`lint`、`lint:fix`、`test:e2e` 在对应阶段加入，本 PR 不放占位脚本（避免「脚本存在但什么都不做」的假绿）。

---

## 五、可测性重构：路由守卫

现状：守卫以匿名函数写在 `router.beforeEach(...)` 里，路由表内联在 `createRouter` 调用中，无法单独导入测试。

改动（**行为不变**）：

```js
export const routes = [ /* 原路由表，原样搬出 */ ]

// 全局路由守卫：requiresAuth 路由未登录时重定向到登录页
export function authGuard(to, _, next) {
  if (to.meta.requiresAuth) {
    if (auth.isAuthenticated()) next()
    else next('/')
  } else {
    next()
  }
}

const router = createRouter({ history: createWebHistory('/'), routes })
router.beforeEach(authGuard)
router.afterEach((to) => applyPageTitle(to))
export default router
```

- 保留 `next` 写法而不是改成返回值风格，保证 diff 只是「搬家 + 具名导出」。
- 测试用 `createMemoryHistory` + 导出的 `routes`（组件替换为 stub，保留 `path` / `name` / `meta`）+ `authGuard` 组装独立 router，不依赖浏览器 history、不加载真实页面组件。
- 同时断言导出的 `routes` 中所有「issue 期望需登录」的路由确实带 `requiresAuth`，防止以后改路由时误删 meta。

---

## 六、P0 用例设计

### 6.1 `useInteractionToggle`（迁入 #13 验证过的 15 个场景）

统一用 `vi.useFakeTimers()` + `vi.advanceTimersByTimeAsync()`，不依赖真实时间。辅助函数构造一个「UI 状态 + ctx」和一个可逐个 resolve/reject 的 `request` mock。

| # | 场景 | 断言 |
|---|---|---|
| 1 | 单击 | 600ms 后发 1 次 `request(key, true)`；UI 保持 true |
| 2 | 双击（600ms 内） | 不发请求；UI 回到 false |
| 3 | 三击 | 只发 1 次，desired = true |
| 4 | 在途期间再点一次 | 首个请求返回后按最新 UI 补发 1 次，最终两次请求 `true → false` |
| 5 | 在途期间双击（回到在途请求的期望值） | 返回后不补发 |
| 6 | 超时（axios 无 response）→ 重试成功 | 800ms 后重发 1 次；不回滚、不提示 |
| 7 | 503 → 重试成功 | 同上 |
| 8 | 重试仍失败 | 回滚到 confirmed；`onError` 调用 1 次 |
| 9 | 404 | 不重试；回滚；`onError` |
| 10 | 500 | 不重试；回滚；`onError` |
| 11 | 业务码错误（HTTP 200、`code ≠ 200`，无 status） | 不重试；回滚；`onError` |
| 12 | 多 key 互不覆盖 | 600ms 内先点 A 再点 B → A、B 各发 1 次 |
| 13 | 结算后第二轮操作 | 以第一轮服务端结果为基准，第二轮正常发请求 |
| 14 | 重试等待期间再点 | 重试使用最新 UI 状态 |
| 15 | 服务端结果与期望不一致 | UI 对齐到服务端返回值 |

另测 `interactionErrorMessage(err, t)`：404 → `messages.contentUnavailable`；503 / 500 / 无 status → `messages.interactionRetryLater`；400 → `messages.operationFailed` 且带 `{ error: err.message }`；兼容 `err.response.status`。

### 6.2 `src/utils/request.js`

用 `axios-mock-adapter` 挂在导出的实例上；`vi.stubGlobal('location', { href: '' })` 以断言跳转。

| 场景 | 断言 |
|---|---|
| 有 token | 请求头带 `satoken` |
| 无 token | 不带 `satoken` |
| JSON 请求体含 NULL 字节（嵌套） | 服务端收到的 body 已清洗 |
| `FormData` 请求体 | 原样透传（仍是同一个 FormData，文件字段不丢） |
| `code === 200` | resolve 为响应体 `res`（不是 axios response） |
| HTTP 200 + `code ≠ 200` | reject；error 带 `message` / `code` / `data`；**不带** `status` |
| HTTP 200 + `code === 401`（普通接口） | 清 token + `location.href = '/'` |
| HTTP 401（普通接口） | 清 token + 跳转；error 带 `status = 401` |
| HTTP 200/401 + `code === 401`，`/auth/login` / `/auth/register` / `/auth/password/*` | 不清 token、不跳转，error 带后端 message |
| 401 + `message = 'This feed tab requires login'` | 不清 token、不跳转 |
| HTTP 503 / 404 | error 带 `status`、`code`、`message` |
| 网络错误 / 超时（无 response） | 原样 reject axios 错误（`isAxiosError`、无 `response`） |

### 6.3 其它纯函数

| 文件 | 要点 |
|---|---|
| `sanitize.js` | `sanitizeString` 删 NULL、保留 `\t\n\r`、非字符串原样返回；`sanitizePayload` 递归对象 / 数组 / 混合嵌套、不修改入参、`null` / 数字 / 布尔原样 |
| `throttle.js` | `useThrottleFn`：首次立即执行；窗口内多次调用只留 1 次尾调用且用**首次进入窗口时**的参数（现有实现行为，如实记录）；窗口外再次立即执行；`this` 透传。`useDebounceFn`：合并为最后一次调用，参数为最后一次 |
| `guest-access.js` | 白名单方法 / 前缀匹配（`/post/detail/1` ✓，`/post/detailx` ✗）；方法大小写不敏感；完整 URL / query / hash / 尾斜杠规范化；`/post/home` 无 tab / hot / latest ✓，recommend / following ✗；非 GET ✗；空入参 ✗；`isFeedTabRestricted` 精确匹配 |
| `mention.js` | 中英文用户名；合法前导（行首、空白、括号、中文书名号）；非法前导（邮箱 `a@b.com`）；后置边界（`@alice` 不中 `@alice2`、`@a@b`）；30 字长度上限；`MENTION_TAIL_RE` 编辑侧匹配；`knownNames` 带空格长名优先；正则缓存复用；`extractMentionTokens` 去重保序；`filterMentionedIds` |
| `mentionResolve.js` | `seedUsers` 大小写不敏感映射；无效项跳过；`knownUsernames` 保留原大小写；`seedContentMentions` 容错非数组 / 无 mentions；`peekUserId` 未知返回 `undefined`。模块级缓存 → 每个用例 `vi.resetModules()` 后动态导入 |

### 6.4 `tests/i18n.spec.js`

直接导入 `src/locales/zh-CN.js`、`en-US.js`（纯对象，无副作用），把嵌套对象拍平成 `key → value`：

1. **key 集合一致**：两种语言的 key 差集都为空；失败信息列出缺失的 key。
2. **生产编译器编译通过**：对每个字符串值调用 `baseCompile(msg, { onError })`（`@intlify/message-compiler`，与生产运行时同一编译器），收集所有错误，断言为空；失败信息列出 `locale.key: 错误信息`。
3. **占位符一致**：用编译产物的 AST 提取命名插值（`{name}`）和列表插值（`{0}`）集合，同一 key 在两种语言中必须相同。基于 AST 而不是正则，避免把 `{'{'}` 这类字面量转义误判为占位符。
4. **链接消息**：`@:key` 引用的 key 必须存在。
5. **数组文案**（`terms.sections`、`privacy.sections`、`privacy.summary`）：页面通过 `tm()` 取原始结构、以 `{{ }}` 直接渲染，**不经过消息编译器**，因此只参与 key 结构一致性（`a.b[0].c`）和「叶子均为字符串」校验，不做编译 / 占位符校验。

### 6.5 路由守卫 `tests/router/guard.spec.js`

| 场景 | 断言 |
|---|---|
| 未登录访问 `requiresAuth` 路由（`/profile`、`/notifications`、`/create-post`…） | 落到 `/`（name = `login`） |
| 已登录（有效 token） | 放行 |
| token 已过期 | 视为未登录，重定向 |
| 访客可读路由（`/home`、`/post/1`） | 未登录直接放行 |
| 不存在的路径 | 匹配 `not-found` |
| 路由表 meta | 需登录路由集合与预期一致 |

---

## 七、CI / 部署流水线设计

### 7.1 `.github/workflows/ci.yml`

```yaml
name: CI
on:
  pull_request:
    branches: [main]
  workflow_call:
  workflow_dispatch:
permissions:
  contents: read
concurrency:
  group: ci-${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}
jobs:
  unit:   npm ci → npm run test:coverage → 覆盖率摘要写入 $GITHUB_STEP_SUMMARY → 上传 coverage/
  build:  npm ci → npm run build → 上传 dist/（供阶段 6 的 e2e 复用）
```

与 issue 草案的差异及原因：

| 差异 | 原因 |
|---|---|
| 去掉 `push: [main]` 触发 | `deploy.yml` 在 push `main` 时已通过 `workflow_call` 调用 CI；两边都触发会让同一次 push 跑两遍 CI |
| 并发组加入 `github.workflow` | 被 `workflow_call` 调用时 `github.workflow` 是调用方名称，可避免与手动触发的 CI 抢同一个并发组、互相取消（这会导致部署被取消） |
| 仅 PR 事件 `cancel-in-progress` | PR 新提交取消旧运行；部署链路上的 CI 不被取消 |
| 各 job 设 `timeout-minutes: 10` | 防止挂死的任务占满 runner；正常耗时远低于此（issue 验收：≤ 5 分钟） |

Node 统一 20（与 deploy 一致），`actions/setup-node@v4` 开启 `cache: npm`。

覆盖率摘要：读取 `coverage/coverage-summary.json`（`json-summary` reporter 产出），用 Node 单行脚本生成 Markdown 表格追加到 `$GITHUB_STEP_SUMMARY`，`if: always()` 保证门槛失败时也能看到数值。

### 7.2 改造 `deploy.yml`

```yaml
jobs:
  ci:
    uses: ./.github/workflows/ci.yml
  build:
    needs: ci
    ...（原样）
  deploy:
    needs: build
    ...（原样）
```

- push `main` → `ci`（unit + build）通过后才进入原来的 `build` → `deploy`；任一检查失败则不部署。
- 被调用的 workflow 声明了 `permissions: contents: read`，只会收窄调用方的 `pages: write` / `id-token: write`，不会越权。
- 原 `build` job 保留（它产出的是 `upload-pages-artifact` 格式），不复用 CI 的 `dist/` artifact，保持部署链路与现在完全一致，降低改造风险。

### 7.3 分支保护（需管理员手动开启）

Settings → Branches → `main` 添加规则（或 Rulesets）：

- ✅ Require a pull request before merging
- ✅ Require status checks to pass：`unit`、`build`（PR 上显示为 `CI / unit (pull_request)`、`CI / build (pull_request)`；阶段 2、6 后再加 `lint`、`e2e`）
- ✅ Require branches to be up to date before merging

> 注意：必须等本 PR 的 CI 在 GitHub 上至少运行过一次，`unit` / `build` 才会出现在可选的 status check 列表里。

---

## 八、验证计划

本地（Node 22）与 CI（Node 20）均需通过：

1. `npm ci` 干净安装成功，`package-lock.json` 只新增 devDependencies。
2. `npm test` 全部通过；`npm run test:coverage` 门槛通过。
3. `npm run build` 通过，产物与改动前一致（`dist/404.html` 仍生成；测试文件不进包）。
4. **反向验证**（证明用例能拦截问题，结果记录在第九节，改动不提交）：
   - 把 en-US `likePost` 改回 `"{{snippet}}"` → i18n 编译用例失败并指出 key；
   - 删掉一条 zh-CN key → key 一致性用例失败；
   - 在 `useInteractionToggle` 中去掉「desired === confirmed 不发请求」→ 双击用例失败；
   - 在 `request.js` 去掉 `isAuthRequest` 判断 → 登录 401 用例失败；
   - 去掉路由守卫 → 守卫用例失败。
5. 连续跑 3 次 `npm test` 结果一致（无时序抖动）。
6. `ci.yml` / `deploy.yml` 用 `actionlint`（若可用）或 YAML 解析校验语法；推送后在 PR 上确认 `CI / unit`、`CI / build` 运行并通过。
7. `deploy.yml` 的门禁效果只能在合入 `main` 后观察：push `main` 时 Actions 页面应显示 `ci / unit`、`ci / build` → `build` → `deploy` 的依赖链。

---

## 九、实施记录（2026-09-28）

### 9.1 交付内容

| 类别 | 文件 |
|---|---|
| 配置 | `vitest.config.js`、`tests/setup.js`、`package.json`（4 个 scripts + 6 个 devDependencies）、`.gitignore`（`coverage`） |
| 用例 | `tests/composables/useInteractionToggle.spec.js`、`tests/utils/{request,sanitize,throttle,guest-access,mention,mentionResolve}.spec.js`、`tests/i18n.spec.js`、`tests/router/guard.spec.js` |
| 源码 | `src/router/index.js`：导出 `routes` / `authGuard`（忽略空白后 +10 / −6，行为不变）；`vite.config.js`：`generate-404` 加 `apply: 'build'` |
| CI | 新增 `.github/workflows/ci.yml`（`unit`、`build`）；`deploy.yml` 新增 `ci` job，`build` `needs: ci` |
| 文档 | 本文、`README.md`「测试与 CI」、`DEPLOY.md`「CI 检查与分支保护」 |

实际依赖版本：`vitest@3.2.7`、`@vitest/coverage-v8@3.2.7`、`jsdom@26.1.0`、`@vue/test-utils@2.5.1`、`axios-mock-adapter@2.1.0`、`@intlify/message-compiler@9.14.5`。
`package-lock.json` 只新增 153 个包，**原有 package 的解析版本零变化**（脚本逐项比对）。

### 9.2 用例统计

9 个测试文件、188 个用例：

| 文件 | 用例数 |
|---|---|
| `useInteractionToggle.spec.js` | 23（15 个时序场景 + 默认参数 + 7 个错误文案映射） |
| `request.spec.js` | 21 |
| `mention.spec.js` | 36 |
| `guard.spec.js` | 33 |
| `guest-access.spec.js` | 32 |
| `sanitize.spec.js` | 16 |
| `mentionResolve.spec.js` | 11 |
| `i18n.spec.js` | 9（校验 698 条经 `t()` 编译的文案 + 3 组数组文案，合计即 issue 所述 701 个 key） |
| `throttle.spec.js` | 7 |

### 9.3 覆盖率基线

统计范围 `src/utils/**`、`src/composables/**`、`src/router/**`：

| 指标 | 实测 | 全局门槛 |
|---|---|---|
| lines / statements | 46.79% | 46 |
| branches | 86.04% | 85 |
| functions | 50% | 50 |

P0 文件单独成组（`useInteractionToggle`、`request`、`sanitize`、`throttle`、`guest-access`、`mention`、`mentionResolve`、`router/**`）：lines 99.19%，门槛 lines / statements 95、branches 90。
已验证门槛生效：临时把全局 lines 调到 47、P0 组 lines 调到 99.9，`test:coverage` 均以 exit 1 失败并给出 `ERROR: Coverage for lines ... does not meet ... threshold`。

未覆盖的 P1 文件（`useNoticeStream`、`useSideNavCircles`、`useUserSearch`、`useMentions`、`useImageUpload`、`useCircleMeta`、`i18n.js`、`mentionDom.js`、`mentionHighlight.js`、`guest-action.js`）是全局 lines 偏低的原因，阶段 4 补齐后提到 80%。

### 9.4 反向验证（改动均已还原，未提交）

| 人为破坏 | 失败的用例 |
|---|---|
| en-US `likePost` 改回 `"{{snippet}}"` | `i18n 文案 > 所有文案都能通过生产编译器编译` |
| 删除 zh-CN `title.slogan` | `i18n 文案 > zh-CN 与 en-US 的 key 集合完全一致` |
| en-US `collectPost` 的 `{snippet}` 改为 `{title}` | `i18n 文案 > 同一 key 在两种语言中的插值占位符一致` |
| 去掉 `useInteractionToggle` 的「desired === confirmed 不发请求」 | 场景 2、5、13、14、15 共 5 个 |
| 去掉 `request.js` 业务码分支的 `isAuthRequest` 判断 | `认证请求 /auth/login|register|password/reset 的 401 不清 token、不跳转` 共 3 个 |
| 删除 `router.beforeEach(authGuard)` | `默认导出的 router > 已注册 authGuard 与页面标题同步` |

### 9.5 运行验证

- `npm test`：Node 22 连续 3 次 188/188 通过，单次约 1.5–4 s（含覆盖率约 4 s）。
- Node 20.20（与 CI 一致）下 `vitest run --coverage` 188/188 通过、门槛通过。
- `npm run check`（覆盖率测试 + 生产构建）本地约 27 s，exit 0；`dist/404.html` 正常生成，产物中不含测试代码。
- `actionlint 1.7.7` 校验 `ci.yml`、`deploy.yml` 无报错。
- PR 上 `CI / unit`、`CI / build` 的运行结果以 GitHub Actions 为准；`deploy.yml` 的门禁效果需在合入 `main` 后观察（Actions 中应出现 `ci / unit`、`ci / build` → `build` → `deploy`）。

### 9.6 与规划的差异

- `vite.config.js`：新增 `apply: 'build'`（原计划认为无需改动，见 4.1）。
- i18n 校验增加「链接消息引用存在」和「数组文案结构校验」两项（6.4 第 4、5 条）。
- 路由测试额外通过**真实默认导出的 router** 验证守卫已注册（利用全局 `beforeEach` 先于懒加载组件解析执行，不会加载真实页面）。

### 9.7 后续（按 issue 阶段）

1. **管理员操作**：本 PR 合入后为 `main` 开启分支保护，必需检查 `unit`、`build`（见 7.3）。
2. 阶段 2：ESLint（`lint` job）。
3. 阶段 4：P1 用例 + `src/utils/**`、`src/composables/**` 80% 门槛。
4. 阶段 5：P2 组件测试（`tests/utils/mount.js` 统一 Naive UI stub 与 i18n 插件）。
5. 阶段 6：Playwright E2E（复用 `build` job 上传的 `dist/`）。
