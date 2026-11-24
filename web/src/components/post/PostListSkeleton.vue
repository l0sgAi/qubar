<template>
  <!-- 帖子列表骨架屏：按 PostCard 真实布局逐块占位（16px 圆角卡片 + 圈子头像/圈名 +
       作者/时间 + 标题/摘要 + 图片 + 统计栏）；扫光动画与 SideNav 骨架同款 shimmer，
       线条加强为胶囊圆角；各卡用负延迟错开相位，形成自上而下的扫光节奏 -->
  <div class="post-list-skeleton" aria-hidden="true">
    <div
      v-for="n in count"
      :key="n"
      class="skeleton-card"
      :style="{ '--sk-delay': `${-0.2 * (n - 1)}s` }"
    >
      <!-- 头部：圈子头像 + 圈名（左）、作者名/时间（右），对应 post-header -->
      <div class="sk-header">
        <div class="sk sk-avatar" />
        <div class="sk sk-circle-name" />
        <div class="sk-header-spacer" />
        <div class="sk-user-meta">
          <div class="sk sk-line sk-name" />
          <div class="sk sk-line sk-time" />
        </div>
      </div>

      <!-- 内容：标题 + 摘要行，16px 缩进与真实卡片文字对齐 -->
      <div class="sk-content">
        <div class="sk sk-title" :style="{ width: titleWidth(n) }" />
        <div
          v-for="(w, i) in textLines(n)"
          :key="i"
          class="sk sk-text"
          :style="{ width: w }"
        />
      </div>

      <!-- 图片占位：约一半卡片带图，模拟轮播帖子的密度 -->
      <div v-if="hasImage(n)" class="sk sk-image" />

      <!-- 统计栏：分隔线 + 4 组「图标 + 数字」，对应 post-stats -->
      <div class="sk-stats">
        <div v-for="s in 4" :key="s" class="sk-stat">
          <div class="sk sk-stat-icon" />
          <div class="sk sk-stat-num" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
defineProps({
  // 渲染的骨架卡片数
  count: {
    type: Number,
    default: 3
  }
})

// 各卡的行宽做确定性变化（不用随机，避免每次加载跳动），让整列不那么机械
const titleWidth = (n) => (n % 2 === 1 ? '56%' : '70%')
const textLines = (n) => (n % 2 === 1 ? ['92%', '58%'] : ['100%', '76%', '42%'])
const hasImage = (n) => n % 2 === 0
</script>

<style scoped>
/* 卡片容器：复刻 .post-card 的圆角玻璃卡片外观，骨架与真实内容形状一致 */
.skeleton-card {
  margin-bottom: 20px;
  padding: 20px 24px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
}

/* 所有占位块共用：SideNav 同款 shimmer 渐变（400% 底纹平移扫光）；
   各卡错相：负延迟让首屏即处于不同扫光相位，无启动空拍 */
.sk {
  background: linear-gradient(
    90deg,
    rgba(255, 255, 255, 0.08) 25%,
    rgba(255, 255, 255, 0.2) 37%,
    rgba(255, 255, 255, 0.08) 63%
  );
  background-size: 400% 100%;
  animation: post-skel-shimmer 1.4s ease infinite;
  animation-delay: var(--sk-delay, 0s);
}

/* 头部：48px 圆头像 + 胶囊圈名，右侧作者/时间两行 */
.sk-header {
  display: flex;
  align-items: center;
  gap: 15px;
  margin-bottom: 18px;
}

.sk-avatar {
  flex-shrink: 0;
  width: 48px;
  height: 48px;
  border-radius: 50%;
}

.sk-circle-name {
  width: 110px;
  height: 16px;
  border-radius: 999px;
}

.sk-header-spacer {
  flex: 1;
}

.sk-user-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
}

.sk-line {
  height: 12px;
  border-radius: 999px;
}

.sk-name {
  width: 88px;
}

.sk-time {
  width: 60px;
  height: 10px;
}

/* 内容：标题 + 摘要行，全部胶囊圆角 */
.sk-content {
  margin-left: 16px;
  margin-bottom: 16px;
}

.sk-title {
  height: 18px;
  margin-bottom: 14px;
  border-radius: 999px;
}

.sk-text {
  height: 12px;
  margin-bottom: 12px;
  border-radius: 999px;
}

.sk-text:last-child {
  margin-bottom: 0;
}

/* 图片占位：12px 大圆角块 */
.sk-image {
  height: 200px;
  margin-left: 16px;
  margin-bottom: 16px;
  border-radius: 12px;
}

/* 统计栏：分隔线 + 4 组「图标 + 数字」，与 post-stats 同构 */
.sk-stats {
  display: flex;
  gap: 24px;
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.sk-stat {
  display: flex;
  align-items: center;
  gap: 6px;
}

.sk-stat-icon {
  width: 18px;
  height: 18px;
  border-radius: 8px;
}

.sk-stat-num {
  width: 30px;
  height: 10px;
  border-radius: 999px;
}

/* 移动端：对应 MobilePostItem 的紧凑列表项（单行小头部、无大图、小统计栏） */
@media (max-width: 768px) {
  .skeleton-card {
    margin-bottom: 8px;
    padding: 12px;
    border-radius: 12px;
  }

  .sk-header {
    gap: 6px;
    margin-bottom: 10px;
  }

  .sk-avatar {
    width: 22px;
    height: 22px;
  }

  .sk-circle-name {
    width: 80px;
    height: 12px;
  }

  .sk-user-meta,
  .sk-image {
    display: none;
  }

  .sk-content {
    margin: 0 0 10px;
  }

  .sk-title {
    height: 14px;
    margin-bottom: 8px;
  }

  .sk-text {
    height: 10px;
    margin-bottom: 8px;
  }

  .sk-stats {
    gap: 18px;
    padding-top: 0;
    border-top: none;
  }

  .sk-stat-icon {
    width: 14px;
    height: 14px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .sk {
    animation: none;
  }
}
</style>

<style>
/* shimmer 关键帧（全局块，同 SideNav 的 s-nav-skel-shimmer 参数） */
@keyframes post-skel-shimmer {
  0% {
    background-position: 100% 50%;
  }
  100% {
    background-position: 0 50%;
  }
}
</style>
