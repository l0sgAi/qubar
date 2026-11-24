# Naive UI 弹出层样式覆盖范式

本文件给出 teleport 弹出层（dropdown / modal / popover 等）的标准化覆盖写法。
核心原则：**这些样式必须写在全局 `src/assets/main.css`，不能写在组件 `<style scoped>` 里。**

## 为什么 scoped 覆盖会失效

Vue 的 `<style scoped>` 给组件根元素加 `[data-v-xxxx]` 属性，` :deep(.foo)` 编译成
`[data-v-xxxx] .foo`，要求 `.foo` 是组件根的后代。

Naive UI 默认 `render-to-body=true` 的组件会把弹出 DOM 传送到 `<body>` 下，
**脱离了组件根的子树**，于是 `[data-v-xxxx] .foo` 永远匹配不到——样式静默失效，
加 `!important` 也没用（是选择器没命中，不是优先级问题）。

## teleport 组件清单（默认 render-to-body=true）

`NDropdown` `NModal` `NDrawer` `NPopover` `NTooltip` `NSelect`（菜单部分）
`NCascader` `NDatePicker` `NColorPicker` `NContextMenu` `NPopconfirm`

## 标准范式：以下拉菜单为例

直接套用这套全局样式即可，所有下拉共享统一暗色玻璃主题：

```css
/* src/assets/main.css（全局，非 scoped） */

/* 菜单容器：暗色玻璃 + 大圆角 */
.n-dropdown-menu {
  background: rgba(22, 22, 38, 0.82) !important;
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid rgba(255, 255, 255, 0.1) !important;
  border-radius: 16px !important;
  padding: 6px !important;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5) !important;
}

/* 进入/离开动画：进入回弹，离开标准缓动 */
.n-dropdown-menu.fade-in-scale-up-transition-enter-active,
.n-dropdown-menu.fade-in-scale-up-transition-leave-active {
  transition: opacity 0.22s cubic-bezier(0.4, 0, 0.2, 1),
              transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1) !important;
}
.n-dropdown-menu.fade-in-scale-up-transition-enter-from,
.n-dropdown-menu.fade-in-scale-up-transition-leave-to {
  opacity: 0;
  transform: scale(0.92) translateY(-6px);
}

/* 选项：内层圆角比容器小一档 */
.n-dropdown-option .n-dropdown-option-body {
  border-radius: 10px !important;
  transition: background 0.18s ease, color 0.18s ease;
}
.n-dropdown-option-body__label { color: rgba(255, 255, 255, 0.85) !important; }

/* hover：主题绿高亮 */
.n-dropdown-option:hover .n-dropdown-option-body,
.n-dropdown-option-body--pending {
  background: rgba(102, 234, 194, 0.14) !important;
}
.n-dropdown-option:hover .n-dropdown-option-body__label { color: #8af0d0 !important; }

/* 分隔线 */
.n-dropdown-divider {
  background: rgba(255, 255, 255, 0.08) !important;
  margin: 4px 8px;
  height: 1px;
}
```

## 自定义选项内容（render-label）

当需要在选项里放图标、勾选标记、富文本时，用 `render-label` 渲染函数，
配合全局类（同样写在 main.css）做样式：

```js
// 组件 <script setup>
const renderLabel = (option) => h('span', {
  class: ['lang-option', option.key === current.value ? 'is-active' : '']
}, [
  h('span', null, option.label),
  option.key === current.value ? checkIcon() : null
])
```

```html
<NDropdown :options="opts" :render-label="renderLabel" />
```

```css
/* main.css */
.lang-option { display: flex; align-items: center; gap: 8px; width: 100%; }
.lang-option .lang-check {
  margin-left: auto; color: #66eac2;
  opacity: 0; transform: scale(0.6);
  transition: opacity 0.2s ease, transform 0.2s ease;
}
.lang-option.is-active .lang-check { opacity: 1; transform: scale(1); }
```

## 其它弹出层

`NModal` / `NDrawer` / `NPopover` 同理——把对应的 `.n-modal`、`.n-drawer`、
`.n-popover` 覆盖样式写到全局 main.css，结构遵循同一套暗色玻璃配方。
模态框/抽屉这类大容器可用更大的 `border-radius: 20px~24px`，内层元素递减。

## 对话框 / 模态框 / 消息 / 气泡（弹窗四件套）

这四类全部 teleport 到 `<body>`，**样式统一写在全局 main.css**。每类的结构关键点
（这些是反复验证过的，别再重新踩坑）：

### NDialog（useDialog）

- **底层就是 NModal**：`DialogEnvironment` 用 `h(NModal, { internalDialog: true })` 包裹，
  所以遮罩走 `.n-modal-mask`，进入动画走 `.n-modal.fade-in-scale-up-transition-*`——
  **对话框的动画/遮罩样式直接复用 NModal 的规则即可，不要重复写**。
- 对话框本体是 `.n-dialog`，内部 BEM：`.n-dialog__title` / `__content` / `__icon` /
  `__action` / `__close`；按钮是 `NButton`（已有全局按钮样式）。
- 圆角 20px（比下拉 16px 更大，弹窗层级最高）。

### NModal（preset="card"）

- 容器层级：`.n-modal-mask` → `.n-modal-body-wrapper` → `.n-modal`。
- preset="card" 时内部渲染 **`.n-card`**，所以要同时覆盖 `.n-modal .n-card`，
  含 `.n-card-header` / `__title` / `__close` / `__content` / `__action`。
- 动画类是 `.n-modal.fade-in-scale-up-transition-enter-active`（与下拉同名）。
- 进入回弹 `cubic-bezier(0.34,1.56,0.64,1)` + `scale(0.94)`，离开标准缓动。

### NMessage（useMessage）

- `MessageProvider` 把消息列表 `Teleport to body`；每条消息结构：
  `.n-message` + `__icon` / `__content` / `__close`。
- **类型修饰符是 `--${type}-type`**，即 `.n-message--success-type`（不是 `--success`）。
- 图标同理：`.n-message__icon--success-type`。
- 过渡类 `fade-in-height-expand-transition` 由 `NFadeInExpandTransition` 生成，
  直接挂在 `.n-message` 上（外层 `MessageEnvironment` 用 `<Transition>` 包裹 NMessage）。
  它用 JS 控制 `maxHeight`，所以 CSS 只补 `opacity`/`transform`，不要碰 height。
- 玻璃胶囊 + `border-radius:12px`，左侧 4px 类型色条（用 `::before`）区分四种类型。

### NTooltip / NPopover（共用）

- 都渲染为 `.n-popover`（tooltip 加 `.n-tooltip` 类）。`.n-popover__content`、
  `.n-popover-arrow`。
- 动画类 `popover-transition`（不是 fade-in-scale-up）。
- 紧凑型浮层，圆角 10px。

## 替代方案：render-to-body=false

如果某次覆盖只针对单个组件、不想污染全局，可让 DOM 留在组件内：

```html
<NDropdown :render-to-body="false" ... />
```

此时 scoped 的 `:deep()` 能命中。但注意：浮层会受父级 `overflow: hidden` / `transform`
影响，定位可能错乱。顶栏等正常场景推荐全局样式；复杂嵌套布局再考虑此选项。
