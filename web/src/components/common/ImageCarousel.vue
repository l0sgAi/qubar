<template>
  <div ref="rootRef" class="image-carousel" :class="{ 'is-mobile': mobileShell }" :style="{ marginTop: '25px' }">
    <div
      class="carousel-viewport"
      :style="{ width: containerWidth + 'px', height: containerHeight + 'px' }"
    >
      <div
        class="carousel-track"
        :style="{ transform: `translateX(-${currentIndex * containerWidth}px)` }"
      >
        <!-- 每张图独立 slide：flex 基准锁死为 containerWidth 且禁止伸缩，
             slide 占位与图片自然尺寸/比例完全解耦，translateX(index*width) 永不错位，
             彻底杜绝相邻图片漏边。图片在 slide 内 100% 填充并 contain 居中。 -->
        <div
          v-for="(img, idx) in images"
          :key="idx"
          class="carousel-slide"
          :style="{
            width: containerWidth + 'px',
            height: containerHeight + 'px',
            flexBasis: containerWidth + 'px',
            flexShrink: 0,
            flexGrow: 0
          }"
        >
          <!-- 裁剪背景：同图放大铺满 cover + 模糊 + 暗化，
               填充 object-fit:contain 产生的留白，原渐变背景弃用。 -->
          <img
            class="slide-bg"
            :src="img"
            alt=""
            aria-hidden="true"
            :style="{ width: containerWidth + 'px', height: containerHeight + 'px' }"
          />
          <div class="slide-bg-overlay"></div>
          <NImage
            class="slide-fg"
            :src="img"
            :alt="`图片 ${idx + 1}`"
            object-fit="contain"
            :width="containerWidth"
            :height="containerHeight"
          />
        </div>
      </div>

      <template v-if="images.length > 1">
        <button class="carousel-arrow left" :style="arrowStyle" @click.stop="prev">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" :width="iconSize" :height="iconSize">
            <polyline points="15 18 9 12 15 6"></polyline>
          </svg>
        </button>
        <button class="carousel-arrow right" :style="arrowStyle" @click.stop="next">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" :width="iconSize" :height="iconSize">
            <polyline points="9 18 15 12 9 6"></polyline>
          </svg>
        </button>
        <div class="carousel-dots" :style="dotsStyle">
          <span
            v-for="(_, idx) in images"
            :key="idx"
            class="dot"
            :class="{ active: idx === currentIndex }"
            :style="dotStyle"
            @click.stop="goTo(idx)"
          ></span>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, computed, onMounted, onBeforeUnmount } from 'vue'
import { NImage } from 'naive-ui'
import { useMobileShell } from '@/composables/useAppShell'

const props = defineProps({
  images: {
    type: Array,
    required: true,
    default: () => []
  },
  parentWidth: {
    type: Number,
    default: 600
  },
  widthRatio: {
    type: Number,
    default: 0.75
  },
  heightRatio: {
    type: Number,
    default: 0.6
  }
})

const currentIndex = ref(0)

// 切换图片组时回到第一张
watch(() => props.images, () => {
  currentIndex.value = 0
})

// 固定整体尺寸：宽度按父宽比例，高度按宽高比。
// 轮播框大小恒定，不随图片内容变化，配合 object-fit:contain 完整展示图片，
// 留白区域由渐变背景填充，从而清晰区分轮播区域。
// 移动端外框下再按容器实际可用宽度收窄（parentWidth 是按桌面布局写死的像素值，
// 在手机上会超出屏幕被裁切）；桌面保持原逻辑不变
const mobileShell = useMobileShell()
const rootRef = ref(null)
const availableWidth = ref(0)
let resizeObserver = null

onMounted(() => {
  if (!rootRef.value || typeof ResizeObserver === 'undefined') return
  resizeObserver = new ResizeObserver(([entry]) => {
    availableWidth.value = entry.contentRect.width
  })
  resizeObserver.observe(rootRef.value)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
})

const containerWidth = computed(() => {
  const desktopWidth = props.parentWidth * props.widthRatio
  if (!mobileShell.value || !availableWidth.value) return Math.round(desktopWidth)
  return Math.round(Math.min(desktopWidth, availableWidth.value))
})
const containerHeight = computed(() => Math.round(containerWidth.value * props.heightRatio))

const iconSize = computed(() => Math.max(14, Math.round(containerWidth.value / 20)))
const arrowSize = computed(() => iconSize.value + 14)

const arrowStyle = computed(() => ({
  width: arrowSize.value + 'px',
  height: arrowSize.value + 'px',
  top: `calc(50% - ${arrowSize.value / 2}px)`
}))

const dotSize = computed(() => Math.max(5, Math.round(containerWidth.value / 80)))
const dotsStyle = computed(() => ({
  bottom: Math.round(containerHeight.value * 0.04) + 'px'
}))
const dotStyle = computed(() => ({
  width: dotSize.value + 'px',
  height: dotSize.value + 'px'
}))

const prev = () => {
  currentIndex.value = currentIndex.value > 0
    ? currentIndex.value - 1
    : props.images.length - 1
}

const next = () => {
  currentIndex.value = currentIndex.value < props.images.length - 1
    ? currentIndex.value + 1
    : 0
}

const goTo = (idx) => {
  currentIndex.value = idx
}
</script>

<style scoped>
.image-carousel {
  position: relative;
  display: flex;
  justify-content: left;
}

.carousel-viewport {
  position: relative;
  overflow: hidden;
  line-height: 0;
  flex-shrink: 0;
  border-radius: 8px;
  /* 背景由每张幻灯片的 .slide-bg 提供（同图放大+模糊+暗化），
     viewport 仅保留深色兜底，避免图片加载前留白突兀。 */
  background: var(--bg-tertiary);
  border: 1px solid var(--glass-border);
}

.carousel-track {
  display: flex;
  transition: transform 0.35s ease;
}

.carousel-slide {
  position: relative;
  overflow: hidden;
}

/* 裁剪背景：同图 object-fit:cover 铺满 → blur 模糊 + brightness 暗化，
   scale 放大 1.1 防止模糊后边缘出现透明白边。
   层级最低，仅作氛围背景。 */
.slide-bg {
  position: absolute;
  inset: 0;
  object-fit: cover;
  filter: blur(22px) brightness(0.45);
  transform: scale(1.1);
  z-index: 0;
  pointer-events: none;
}

/* 暗化叠加层，压住模糊背景，凸显上层 contain 主图。 */
.slide-bg-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.35);
  z-index: 0;
  pointer-events: none;
}

/* 主图层级高于背景，确保 contain 图为视觉焦点。 */
.slide-fg {
  position: relative;
  z-index: 1;
}


.carousel-arrow {
  position: absolute;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  background: rgba(0, 0, 0, 0.45);
  border: none;
  border-radius: 50%;
  color: rgba(255, 255, 255, 0.85);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s, background 0.2s;
  z-index: 2;
}

@media (hover: hover) {
  .carousel-arrow:hover {
    background: rgba(0, 0, 0, 0.65);
  }
}

.carousel-arrow.left {
  left: 6px;
}

.carousel-arrow.right {
  right: 6px;
}

@media (hover: hover) {
  .image-carousel:hover .carousel-arrow {
    opacity: 1;
  }
}

/* 移动端：容器不被内容撑开（用于测量可用宽度）；触屏没有 hover，箭头常显 */
.image-carousel.is-mobile {
  min-width: 0;
  overflow: hidden;
}

.image-carousel.is-mobile .carousel-arrow {
  opacity: 1;
}

.carousel-dots {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  gap: 6px;
  z-index: 2;
  padding: 4px 8px;
  border-radius: 10px;
  background: rgba(0, 0, 0, 0.4);
}

.dot {
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.4);
  cursor: pointer;
  transition: background 0.2s, transform 0.2s;
}

.dot.active {
  background: #fff;
  transform: scale(1.3);
}

@media (hover: hover) {
  .dot:hover {
    background: rgba(255, 255, 255, 0.7);
  }
}
</style>
