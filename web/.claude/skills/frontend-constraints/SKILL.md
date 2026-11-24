---
name: frontend-constraints
description: Enforces this project's frontend constraints when writing or reviewing Vue components, Naive UI usage, CSS, and i18n locale text. Use whenever the task involves adding/editing styles, overriding Naive UI component appearance (dropdown, modal, popover, button, etc.), theming, animations, design tokens, writing/editing i18n locale messages (src/locales/), or fixing "styles not applying" / "i18n message compile error" bugs. Also triggers on phrases like "改成项目风格", "统一样式", "主题样式", "为什么样式没生效", "暗色风格", "毛玻璃", "组件样式覆盖", "Invalid linked format", "Message compilation error".
---

# 项目前端约束（qubar-frontend）

本项目是 Vue 3 + Naive UI 的深色主题应用。本 skill 总结了项目级的前端约束，
在写新组件、改样式、覆盖 Naive UI 外观、写 i18n 文案、或排查"样式不生效"时必须遵守。

核心设计语言：**深色玻璃拟态（dark glassmorphism）+ 主题绿强调色 + 大圆角 + 弹性动效**。

## 设计 Token：必须复用，禁止凭空造色

项目唯一的样式入口是 `src/assets/main.css`，其 `:root` 定义了全部设计 token。
写任何样式前先打开它，**复用已有变量**，不要写死十六进制色值。

关键 token（当前值，以文件实际内容为准）：

| Token | 用途 |
|---|---|
| `--bg-primary` / `--bg-secondary` / `--bg-tertiary` | 三级背景，从深到浅 |
| `--text-primary` / `--text-secondary` / `--text-tertiary` | 三级文字 |
| `--primary-gradient` | 主题渐变（绿） |
| `--theme-color` | 主题绿强调色 |
| `--glass-bg` / `--glass-border` | 玻璃拟态容器背景/边框 |
| `--header-height` | 顶栏高度 |
| `--shadow-sm` / `--md` / `--lg` | 三级阴影 |

**主题强调色统一用绿色系**（`#66eac2` / `#8af0d0` / `rgba(102,234,194,...)`）。
不要引入粉色、紫蓝等其它色系当强调色——历史上 RightSidebar 用过粉色 `#ec4899`、
body 背景用过紫蓝 `rgba(102,126,234,...)`，那是不一致遗留，新代码不要继续扩散。

## 最关键的坑：teleport 组件不能用 scoped 覆盖

**这是本项目最常见的"样式不生效"根因，优先排查。**

Naive UI 这些组件默认 `render-to-body=true`，会把弹出 DOM **传送到 `<body>` 下**：
`NDropdown`、`NModal`、`NDrawer`、`NPopover`、`NSelect`（菜单部分）、`NTooltip`、
`NCascader`、`NDatePicker`、`NColorPicker`、`NContextMenu`。
**`useDialog`（底层是 NModal）和 `useMessage`（MessageProvider Teleport 到 body）同理**，
弹出 DOM 也不在组件子树内，覆盖样式同样要写在全局 main.css。

传送后的 DOM **不再是组件根元素的后代**。而在 `<style scoped>` 里写的 `:deep(.xxx)`
会被编译成 `[data-v-xxxx] .xxx`，要求目标必须是组件根的后代——所以：

```vue
<!-- ❌ 失效：scoped 的 :deep 永远选不中传送到 body 的菜单 -->
<style scoped>
:deep(.n-dropdown-menu) { background: ...; }
</style>
```

这条规则即使加了 `!important` 也救不回来，因为问题是**选择器根本没命中**，不是优先级。

正确做法二选一：

1. **把覆盖样式写进全局 `src/assets/main.css`**（推荐，便于全项目复用）。
2. 给组件加 `:render-to-body="false"`，让 DOM 留在组件内，scoped `:deep()` 才能命中。

判断流程：看到 `.n-dropdown-menu` / `.n-modal` / `.n-popover` 等弹出层样式不生效时，
立刻确认它是不是 teleport 组件 + 是不是写在 scoped 里。详见 `references/naive-ui-override.md`。

## scoped 样式 vs 全局样式的分工

| 样式类型 | 写在哪 |
|---|---|
| 组件内部元素（自己的 DOM） | `<style scoped>` |
| Naive UI teleport 弹出层（菜单/弹窗/气泡） | `src/assets/main.css`（全局） |
| 新的设计 token / 通用类 | `src/assets/main.css` 的 `:root` 或新增类 |
| 多组件共用的主题（如所有下拉的暗色玻璃风） | `src/assets/main.css`，不要在每个组件重复 |

新增全局类时用项目命名习惯（小写连字符，如 `.lang-option`、`.glass-card`）。

## 暗色玻璃拟态的视觉配方

需要做悬浮卡片、下拉、弹窗、抽屉等浮层时，套这套配方，保持全项目观感一致：

```css
/* 容器 */
background: rgba(22, 22, 38, 0.82);          /* 深色 + 半透，不要纯黑 */
backdrop-filter: blur(24px) saturate(160%);  /* 玻璃模糊 + 饱和度增强 */
-webkit-backdrop-filter: blur(24px) saturate(160%);
border: 1px solid rgba(255, 255, 255, 0.1);
border-radius: 16px;                          /* 浮层用大圆角 12-16px */
box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5);

/* 内部选项/条目 */
border-radius: 10px;                          /* 内层比容器小一档 */

/* hover / 高亮：主题绿 */
background: rgba(102, 234, 194, 0.14);
color: #8af0d0;
```

圆角层次原则：**外层 > 内层**。例如下拉容器 16px，内部选项 10px，标签/小元素 8px。

## 动画效果约束

- **进入动画带轻微回弹**：用 `cubic-bezier(0.34, 1.56, 0.64, 1)`，配 `scale(0.92)` + `translateY(-6px)` 起始态。
- **离开动画用标准缓动**：`cubic-bezier(0.4, 0, 0.2, 1)`，不要回弹（回弹只在进入时舒服）。
- 时长 `0.2s ~ 0.25s`，太短显得跳，太长显得肉。
- Naive UI 的过渡类名形如 `.fade-in-scale-up-transition-enter-active`，覆盖这些类即可改其内置动画。
- Vue `<Transition>` 自定义动画：name 用语义化名（如 `suggestion-fade`），保持 enter/leave 成对。

## Naive UI 触发与位置约定

顶栏/导航区的下拉、气泡，统一：

- `trigger="hover"`（鼠标悬浮触发，比点击更顺滑，符合 Web 顶栏习惯）
- `placement="bottom-end"`（弹出位置贴右下，适合顶栏右侧元素）

列表/内容区的上下文菜单按需用 `trigger="click"`，不要一刀切。

## i18n 文案约束：@、{、} 是保留字符，必须转义

vue-i18n 的消息编译器把以下字符当作语法符号，**不能**直接出现在文案里：

| 字符 | 语法含义 | 想显示字面量时的写法 |
|---|---|---|
| `@` | 链接消息（`@:some.key`） | `{'@'}`（字面量插值） |
| `{` / `}` | 插值占位符（`{name}`） | `{'{'}` / `{'}'}` |
| `|` | 复数分支分隔符 | 避免使用，或改写文案 |

正确示例（`src/locales/en-US.js` 的 `notice.mention.suggestHint`）：

```js
suggestHint: "Type {'@'} to mention someone"   // ✅ 字面量插值渲染出 @
suggestHint: 'Type @ to mention someone'        // ❌ 编译器当链接消息解析，报错
```

**这个坑最危险的地方：dev 和 prod 行为不一致。**
dev 构建捕获消息编译错误并降级为原样渲染（只在控制台报 warning，功能看起来正常）；
prod 构建砍掉了这层容错，编译错误直接抛 `SyntaxError: Invalid linked format`，
异常发生在 `t()` 的模板渲染期间，会把整个组件的渲染打断——表现为线上功能完全失效但本地复现不了。
排查"本地好的、线上组件不渲染/不弹出"类问题时，优先检查该组件用到的 i18n 文案是否含未转义的保留字符。

写或改 `src/locales/` 文案时：

1. 所有语言（zh-CN / en-US）要同步检查，同一种语言的文案修了另一种往往也踩了同样的坑。
2. 含 `@` 的邮箱、账号名（如 `legal@qubar.com`）经 `t()` 渲染时同样会炸；经 `tm()` 当数据取出的数组/对象不编译，暂时安全，但新增纯字符串文案不要依赖这个侥幸。

## 排查清单（样式不生效时按序检查）

1. 是 teleport 组件吗？（dropdown/modal/popover...）→ 覆盖样式是否误写在 scoped 里？移到全局或设 `:render-to-body="false"`。
2. 颜色对不上主题吗？→ 是否又写死了十六进制？换成 `:root` token 或绿色强调色。
3. `!important` 堆了一堆还不生效？→ 多半是选择器没命中（见第 1 条），不是优先级问题。
4. 动画卡顿/生硬？→ 检查进入是否用了回弹缓动、离开是否用了标准缓动、时长是否在 0.2–0.25s。

## 参考资料

- 弹出层组件覆盖样式的完整范式与示例：`references/naive-ui-override.md`
