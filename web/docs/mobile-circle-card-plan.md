# 移动端「已加入的圈子」卡片紧凑化 — 优化计划

> 版本：2026-10-15 · 分支：`feature/mobile-circle-card-20261015`（基于 `main@fbdaad6`）
> 关联 issue：[l0sgAi/qubar-frontend#20](https://github.com/l0sgAi/qubar-frontend/issues/20)
> 状态：**已实施**（2026-10-15，未提交）。第八节「待决策」全部按推荐项执行。
>
> **实施偏差**：
> 1. 所在页 `.tabs-card :deep(.n-card__content)` 的移动端 `padding: 0 0 8px` 会穿透到嵌套的圈子卡片，选择器改为 `.groups-grid .group-card :deep(.n-card__content)` 提高优先级
> 2. 全局 line-height 1.6 让文字列高约 47px、超过头像，移动端名称和成员数改为 `line-height: 1.3`
>
> 实测（375×812）：单卡 66px，间距 8px，一屏约 9.5 个；900、1280 宽下卡片尺寸和元素位置与改动前逐像素一致

---

## 一、问题

移动端（≤768px）「我的 → 兴趣圈」和「用户主页 → 兴趣圈」两个 Tab 中，圈子卡片单卡约 180px+ 高，一屏只能看到 3~4 个圈子。

## 二、根因

组件：`src/components/user/MyGroups.vue`，由 `views/user/UserProfile.vue` 和 `views/user/UserDetail.vue` 复用。

| 项 | 现值 | 问题 |
|---|---|---|
| 网格 | ≤768 `grid-template-columns: 1fr` | 已单列，整行宽度可用 |
| 卡片内布局 | `.group-info` `flex-direction: column` 居中 | 单列时仍纵向堆叠，浪费宽度、拉高高度 |
| 头像 | `NAvatar :size="60"` | 移动端过大 |
| 内边距 | `.group-info` `padding: 18px 12px` + `NCard` 默认 `.n-card__content` padding | 两层叠加 |
| 卡片间距 | `gap: 16px` | 偏大 |

高度估算：NCard padding（约 20×2）+ 18×2 + 头像 60 + gap 10×2 + 名称约 22 + 成员数约 18 ≈ **196px**。

## 三、目标

- 移动端单卡高度降到 **约 64~68px**，一屏可见数量从约 3.5 个升到约 9 个（812 高视口）
- 桌面端（≥769px）、平板 2 列（769~1024px）**零视觉变化**
- 保留现有行为：整卡 `SmartLink` 跳转、首字母居中修复、无限滚动、搜索、截断提示、空状态

## 四、方案（推荐）：移动端横向紧凑行

### 4.1 目标布局

```
┌──────────────────────────────────────────┐
│ (●)  圈子名称很长会省略号截断……            │
│ 40px  👥 1.2k                             │
└──────────────────────────────────────────┘
  12px 内边距，卡片间距 8px
```

头像在左，右侧纵向排列名称和成员数，名称单行省略。

### 4.2 模板改动（MyGroups.vue）

1. 名称和成员数包一层 `.group-meta`，横向布局时右侧文字列可以设置 `min-width: 0`，省略号才会生效：

   ```html
   <div class="group-info">
     <NAvatar round :size="avatarSize" :src="group.avatar">…</NAvatar>
     <div class="group-meta">
       <h3 class="group-name">{{ group.name }}</h3>
       <span class="group-stat">…</span>
     </div>
   </div>
   ```

   桌面端 `.group-meta` 为 `flex-direction: column; align-items: center; gap: 10px`，视觉和现在一致（头像 → 名称 → 成员数，间距都是 10px）。

2. 头像尺寸响应式：复用现有 `useBreakpoint()`，`avatarSize = isMobile ? 40 : 60`。
   - 用 prop 而不是 CSS 覆盖：`NAvatar` 通过内联 CSS 变量设置尺寸，CSS 覆盖需要 `!important`，容易和 naive-ui 升级冲突
   - `useBreakpoint` 是模块级单例，首屏同步读取，不会先渲染 60 再闪成 40

### 4.3 样式改动（仅 `@media (max-width: 768px)` 内）

```css
.groups-grid { gap: 8px; }

.group-card { border-radius: 12px !important; }
.group-card :deep(.n-card__content) { padding: 0; }

.group-info {
  flex-direction: row;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  text-align: left;
}

.group-meta {
  flex: 1;
  min-width: 0;
  align-items: flex-start;
  gap: 2px;
}

.group-name { font-size: 0.95rem; }

.group-info :deep(.n-avatar) {
  font-size: 1.1rem;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}
```

已有的 `@media (hover: hover)` 上浮效果在触屏上不触发，不用动。

### 4.4 高度复核

12 + max(头像 40, 名称约 21 + 2 + 成员数约 17 = 40) + 12 + 边框 2 ≈ **66px**。

## 五、备选方案（不推荐）

| 方案 | 说明 | 不选原因 |
|---|---|---|
| B. 移动端 2 列网格保留竖排 | 只把头像缩到 44、压 padding | 单卡仍约 130px，375 宽下每列约 170px，长名称截断更严重 |
| C. 新建 `MobileGroupItem` 组件，`v-if` 切换 | 仿 `MobilePostItem` | 圈子卡片只有 3 个字段，拆组件属于过度设计，两份模板以后容易改漏 |
| D. 纯 CSS 覆盖头像尺寸 | 不引入 `useBreakpoint` | 要 `!important` 覆盖 naive-ui 内联变量，脆弱 |

## 六、改动清单

| 文件 | 改动 |
|---|---|
| `src/components/user/MyGroups.vue` | 模板加 `.group-meta` 包裹层；`avatarSize` 计算属性；移动端 media 样式 |

不改接口、i18n、路由，也不改 `UserProfile.vue` / `UserDetail.vue`。

## 七、验证

1. **移动端 375×812**：「我的 → 兴趣圈」与「用户主页 → 兴趣圈」两个入口
   - JS 读 `.group-card` 的 `getBoundingClientRect().height`，确认 ≤ 72px
   - 长名称单行省略；无头像时首字母居中；点击整卡跳转圈子详情
   - 滚到底触发下一页；搜索、清空搜索正常
2. **桌面端 1280 / 平板 900**：改动前后各截图对比，卡片高度和布局一致
3. **断点切换**：从 1280 拖到 375 再拖回，头像尺寸跟随切换，首字母不偏移
4. `npm run lint`（如有）+ `npm test` 通过

## 八、待决策

1. **右侧加 `›` 箭头？** 推荐**不加**：整卡可点击已足够明显，和 `MobilePostItem` 风格一致
2. **成员数和名称同一行（单行卡片，约 52px）？** 推荐**不合并**：长名称会挤掉成员数，两行更稳
3. **头像 40px 还是 44px？** 推荐 **40px**：卡片里只有头像一个视觉焦点，40 已足够辨识，高度刚好和两行文字对齐
