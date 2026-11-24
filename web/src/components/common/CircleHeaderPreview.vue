<template>
  <!--
    CircleDetail.vue 头部的等比缩略复刻：
    - banner 用 aspect-ratio 锁定真实头部宽高比（≈ 横幅），使封面在预览中的展示
      与详情页 background-size:cover 的实际效果一致（WYSIWYG）。
    - 头像/文字按 banner 高度等比缩小，整体保持详情页头部的视觉结构。
    - 默认 BANNER_ASPECT 与 CreateCircleModal 的封面裁剪比例保持同步。
  -->
  <div class="circle-header" :style="{ backgroundImage: headerBg, aspectRatio: bannerAspect }">
    <div class="header-overlay">
      <div class="header-content">
        <div class="circle-avatar-wrapper">
          <img v-if="props.avatarUrl" :src="props.avatarUrl" class="circle-avatar" />
          <div v-else class="circle-avatar-placeholder">
            {{ initial }}
          </div>
          <div class="circle-info">
            <h1 class="circle-name" :class="{ 'is-placeholder': !props.name }">{{ displayName }}</h1>
            <p class="circle-slug">c/{{ displaySlug }}</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// 真实 CircleDetail 头部宽高比 ≈ 页面主内容宽 / 头部高(头像 72 + padding 32×2 = 136px)
// 常见视窗下约 7:1，取 6 兼顾横幅观感与可裁剪性。
// 注意：需与 CreateCircleModal.vue 的 COVER_ASPECT_RATIO 保持一致。
const BANNER_ASPECT = 6

const props = defineProps({
  coverUrl: { type: String, default: '' },
  avatarUrl: { type: String, default: '' },
  name: { type: String, default: '' },
  slug: { type: String, default: '' },
  // banner 横幅宽高比，默认取真实头部比例；与 CreateCircleModal 封面裁剪比例同步
  bannerAspect: { type: Number, default: BANNER_ASPECT }
})

const { t } = useI18n()

// 与 CircleDetail.vue coverImageStyle 保持一致
const headerBg = computed(() => {
  if (props.coverUrl) {
    return `linear-gradient(to bottom, rgba(0,0,0,0.3), rgba(16,16,28,1)), url(${props.coverUrl})`
  }
  // 未设置封面：使用全局默认封面（src/assets/main.css 的 --circle-cover-default）
  return 'var(--circle-cover-default)'
})

const initial = computed(() => props.name?.charAt(0)?.toUpperCase() || '?')
const displayName = computed(() => props.name || t('circle.form.previewNamePlaceholder'))
const displaySlug = computed(() => props.slug || 'circle')
</script>

<style scoped>
/*
  等比缩略复刻 CircleDetail.vue 的 .circle-header。
  尺寸按 banner 高度等比缩小（banner 由 aspect-ratio + 宽度决定高度），
  保证在窄弹窗内仍呈现真实头部的横幅比例。
*/
.circle-header {
  position: relative;
  width: 100%;
  /* aspect-ratio 由内联 style 注入（bannerAspect），锁定横幅比例 */
  background-size: cover;
  background-position: center;
  background-repeat: no-repeat;
  color: white;
  border-radius: 12px;
  overflow: hidden;
}

.header-overlay {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  padding: 10px 16px;
  box-sizing: border-box;
}

.header-content {
  display: flex;
  align-items: stretch; /* 让 avatar-wrapper 撑满 banner 高度，供头像 % 取值 */
  justify-content: space-between;
  gap: 16px;
  width: 100%;
  height: 100%;
}

.circle-avatar-wrapper {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
}

/* 头像按 banner 高度等比缩小（真实页 72px，此处用 % 跟随 banner 高度） */
.circle-avatar,
.circle-avatar-placeholder {
  height: 62%;
  aspect-ratio: 1 / 1;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.3);
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(10px);
  flex-shrink: 0;
  object-fit: cover;
}

.circle-avatar-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.1rem;
  font-weight: bold;
}

.circle-info {
  flex: 1;
  min-width: 0;
}

.circle-name {
  font-size: 1.15rem;
  font-weight: 700;
  margin: 0 0 2px 0;
  line-height: 1.2;
  text-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.circle-name.is-placeholder {
  opacity: 0.6;
  font-weight: 500;
  font-style: italic;
}

.circle-slug {
  font-size: 0.78rem;
  opacity: 0.92;
  margin: 0;
  font-weight: 400;
  /* 默认封面偏亮（橙→黄→绿），加阴影保证白字可读 */
  text-shadow: 0 1px 6px rgba(0, 0, 0, 0.45);
}
</style>
