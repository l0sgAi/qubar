<template>
  <div ref="wrapper" class="animated-bg-wrapper" :style="cursorStyle">
    <!-- 基础氛围层：深色渐变 + 模糊光晕 -->
    <div class="bg-base"></div>
    <div class="bg-orbs" aria-hidden="true">
      <span class="orb orb-1"></span>
      <span class="orb orb-2"></span>
      <span class="orb orb-3"></span>
      <span class="orb orb-4"></span>
    </div>

    <!-- 远景层：超大图标，强模糊，缓慢漂移 -->
    <div class="bg-layer layer-deep" aria-hidden="true">
      <div
        v-for="(s, i) in deepSlots"
        :key="'deep-' + i"
        class="floating-icon"
        :style="slotStyle(s)"
      >
        <div class="icon-inner" :class="'float-' + s.variant" :style="{ '--dur': s.dur + 's' }">
          <div class="icon-art" v-html="iconSvg(ICONS[s.icon])"></div>
        </div>
      </div>
    </div>

    <!-- 中景层：中等图标，轻微模糊，视差跟随 -->
    <div class="bg-layer layer-mid" aria-hidden="true">
      <div
        v-for="(s, i) in midSlots"
        :key="'mid-' + i"
        class="floating-icon"
        :style="slotStyle(s)"
      >
        <div class="icon-inner" :class="'float-' + s.variant" :style="{ '--dur': s.dur + 's' }">
          <div class="icon-art" v-html="iconSvg(ICONS[s.icon])"></div>
        </div>
      </div>
    </div>

    <!-- 前景层：清晰图标，可悬浮交互 -->
    <div class="bg-layer layer-front">
      <div
        v-for="(s, i) in frontSlots"
        :key="'front-' + i"
        class="floating-icon"
        :style="slotStyle(s)"
      >
        <div class="icon-inner" :class="'float-' + s.variant" :style="{ '--dur': s.dur + 's' }">
          <div class="icon-art icon-art--interactive" v-html="iconSvg(ICONS[s.icon])"></div>
        </div>
      </div>
    </div>

    <!-- 纹理与聚焦层 -->
    <div class="bg-grid" aria-hidden="true"></div>
    <div class="bg-vignette" aria-hidden="true"></div>
    <div class="bg-cursor" aria-hidden="true"></div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'

/* ------------------------------------------------------------------ *
 * 兴趣爱好图标库（24x24 viewBox，stroke = currentColor）
 * 每个图标带主色与匹配的辉光色，呈现彩色但克制的配色
 * ------------------------------------------------------------------ */
const ICONS = [
  { color: '#667eea', glow: 'rgba(102,126,234,.55)', paths: `<!-- gamepad / 游戏 -->
    <path d="M6 11h4"/><path d="M8 9v4"/>
    <circle cx="15" cy="11" r=".7" fill="currentColor" stroke="none"/>
    <circle cx="18" cy="9" r=".7" fill="currentColor" stroke="none"/>
    <path d="M17.32 5H6.68a4 4 0 0 0-3.98 3.59c-.01.05-.02.1-.02.15C2.6 9.42 2 14.46 2 16a3 3 0 0 0 3 3c1 0 1.5-.5 2-1l1.41-1.41A2 2 0 0 1 9.83 16h4.34a2 2 0 0 1 1.41.59L17 18c.5.5 1 1 2 1a3 3 0 0 0 3-3c0-1.54-.6-6.58-.69-7.26 0-.05 0-.1-.01-.15A4 4 0 0 0 17.32 5z"/>` },
  { color: '#f8b133', glow: 'rgba(248,177,51,.55)', paths: `<!-- pencil / 纸笔 -->
    <path d="M21.17 6.81a1 1 0 0 0-3.98-3.98L3.84 16.17a2 2 0 0 0-.5.83l-1.32 4.35a.5.5 0 0 0 .62.62l4.35-1.32a2 2 0 0 0 .83-.5z"/>
    <path d="m15 5 4 4"/>` },
  { color: '#ec4899', glow: 'rgba(236,72,153,.55)', paths: `<!-- clapperboard / 电影 -->
    <path d="M20.2 6 3 11l-.9-2.4c-.3-1.1.3-2.2 1.3-2.5l13.5-4 1.7 4.9z"/>
    <path d="M6.2 5.3 3.1 6.7c-.6.2-.9.9-.7 1.5l.8 2.7"/>
    <path d="M3 11h18v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>
    <path d="M7 15h2"/><path d="M14 15h2"/><path d="M7 19h2"/><path d="M14 19h2"/>` },
  { color: '#8b5cf6', glow: 'rgba(139,92,246,.55)', paths: `<!-- music / 音乐 -->
    <path d="M9 18V5l12-2v13"/>
    <circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>` },
  { color: '#4facfe', glow: 'rgba(79,172,254,.55)', paths: `<!-- book / 阅读 -->
    <path d="M12 7v14"/>
    <path d="M3 18a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-6a3 3 0 0 0-3 3 3 3 0 0 0-3-3z"/>` },
  { color: '#22d3ee', glow: 'rgba(34,211,238,.55)', paths: `<!-- camera / 摄影 -->
    <path d="M14.5 4h-5L7 7H4a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-3z"/>
    <circle cx="12" cy="13" r="3.2"/>` },
  { color: '#fb7185', glow: 'rgba(251,113,133,.55)', paths: `<!-- palette / 绘画 -->
    <circle cx="13.5" cy="6.5" r="1.1" fill="currentColor" stroke="none"/>
    <circle cx="17.5" cy="10.5" r="1.1" fill="currentColor" stroke="none"/>
    <circle cx="8.5" cy="7.5" r="1.1" fill="currentColor" stroke="none"/>
    <circle cx="6.5" cy="12.5" r="1.1" fill="currentColor" stroke="none"/>
    <path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.93 0 1.65-.75 1.65-1.69 0-.44-.18-.83-.44-1.12-.29-.29-.44-.65-.44-1.13a1.64 1.64 0 0 1 1.67-1.67h2c3.05 0 5.55-2.5 5.55-5.55C21.96 6.01 17.46 2 12 2z"/>` },
  { color: '#fbbf24', glow: 'rgba(251,191,36,.55)', paths: `<!-- coffee / 咖啡 -->
    <path d="M6 2v3"/><path d="M10 2v3"/><path d="M14 2v3"/>
    <path d="M5 8h14v8a4 4 0 0 1-4 4H9a4 4 0 0 1-4-4z"/>
    <path d="M19 8h2a2 2 0 0 1 2 2v2a3 3 0 0 1-3 3"/>` },
  { color: '#66eac2', glow: 'rgba(102,234,194,.55)', paths: `<!-- headphones / 播客 -->
    <path d="M3 14a9 9 0 0 1 18 0"/>
    <path d="M21 14h-3a2 2 0 0 0-2 2v3a2 2 0 0 0 2 2h2a1 1 0 0 0 1-1z"/>
    <path d="M3 14h3a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H4a1 1 0 0 1-1-1z"/>` },
  { color: '#43e97b', glow: 'rgba(67,233,123,.55)', paths: `<!-- plane / 旅行 -->
    <path d="M17.8 19.2 16 11l3.5-3.5C21 6 21.5 4 21 3c-1-.5-3 0-4.5 1.5L13 8 4.8 6.2c-.5-.1-.9.1-1.1.5l-.3.5c-.2.5-.1 1 .3 1.3L9 12l-2 3H4l-1 1 3 2 2 3 1-1v-3l3-2 3.5 5.3c.3.4.8.5 1.3.3l.5-.2c.4-.3.6-.7.5-1.2z"/>` },
  { color: '#7c8cff', glow: 'rgba(124,140,255,.55)', paths: `<!-- code / 编程 -->
    <polyline points="16 18 22 12 16 6"/>
    <polyline points="8 6 2 12 8 18"/>` },
  { color: '#ff6b6b', glow: 'rgba(255,107,107,.55)', paths: `<!-- heart / 热爱 -->
    <path d="M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.3 1.5 4.05 3 5.5l7 7z"/>` },
  { color: '#fbbf24', glow: 'rgba(251,191,36,.6)', paths: `<!-- star / 收藏 -->
    <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>` },
  { color: '#22d3ee', glow: 'rgba(34,211,238,.6)', paths: `<!-- sparkles / 灵感 -->
    <path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z"/>
    <path d="M19 14l.8 2.2L22 17l-2.2.8L19 20l-.8-2.2L16 17l2.2-.8z"/>` }
]

/* ------------------------------------------------------------------ *
 * 图标层布局：远景（大而模糊）/ 中景 / 前景（可交互）
 * 位置使用百分比，自适应任意尺寸屏幕
 * ------------------------------------------------------------------ */
const deepSlots = [
  { icon: 0,  top: '2%',  left: '1%',  size: 280, rot: -18, variant: 'a', dur: 16, delay: .05, op: .13 },
  { icon: 6,  top: '48%', left: '74%', size: 260, rot: 12,  variant: 'b', dur: 18, delay: .20, op: .12 },
  { icon: 4,  top: '68%', left: '3%',  size: 230, rot: 8,   variant: 'c', dur: 15, delay: .35, op: .12 },
  { icon: 5,  top: '22%', left: '82%', size: 210, rot: -10, variant: 'a', dur: 17, delay: .15, op: .13 },
  { icon: 2,  top: '78%', left: '38%', size: 300, rot: 6,   variant: 'b', dur: 20, delay: .30, op: .10 },
  { icon: 8,  top: '-4%', left: '42%', size: 250, rot: -6,  variant: 'c', dur: 19, delay: .25, op: .12 }
]

const midSlots = [
  { icon: 4,  top: '30%', left: '28%', size: 108, rot: -8,  variant: 'b', dur: 12, delay: .40, op: .30 },
  { icon: 6,  top: '62%', left: '34%', size: 100, rot: 10,  variant: 'a', dur: 13, delay: .55, op: .28 },
  { icon: 1,  top: '18%', left: '60%', size: 96,  rot: 20,  variant: 'c', dur: 11, delay: .50, op: .32 },
  { icon: 8,  top: '64%', left: '60%', size: 104, rot: -12, variant: 'b', dur: 14, delay: .60, op: .26 },
  { icon: 9,  top: '46%', left: '88%', size: 92,  rot: -18, variant: 'a', dur: 12, delay: .45, op: .30 },
  { icon: 2,  top: '84%', left: '20%', size: 92,  rot: 8,   variant: 'c', dur: 13, delay: .65, op: .26 },
  { icon: 13, top: '7%',  left: '22%', size: 70,  rot: 0,   variant: 'b', dur: 10, delay: .70, op: .34 },
  { icon: 3,  top: '86%', left: '66%', size: 80,  rot: 6,   variant: 'a', dur: 12, delay: .75, op: .28 }
]

const frontSlots = [
  { icon: 0,  top: '18%', left: '13%', size: 74, rot: -8,  variant: 'a', dur: 9,   delay: .50, op: .85 },
  { icon: 3,  top: '24%', left: '78%', size: 66, rot: 6,   variant: 'b', dur: 10,  delay: .60, op: .80 },
  { icon: 5,  top: '66%', left: '9%',  size: 70, rot: 8,   variant: 'c', dur: 9.5, delay: .70, op: .82 },
  { icon: 7,  top: '70%', left: '82%', size: 66, rot: -6,  variant: 'a', dur: 10.5,delay: .80, op: .80 },
  { icon: 10, top: '44%', left: '5%',  size: 58, rot: -10, variant: 'b', dur: 8.5, delay: .65, op: .78 },
  { icon: 11, top: '82%', left: '46%', size: 52, rot: 4,   variant: 'c', dur: 9,   delay: .90, op: .85 },
  { icon: 12, top: '10%', left: '48%', size: 50, rot: 0,   variant: 'b', dur: 8,   delay: .55, op: .85 }
]

/* 生成单个图标的内联 SVG（带主色 + 辉光） */
const iconSvg = (icon) =>
  `<svg viewBox="0 0 24 24" width="100%" height="100%" fill="none"
     stroke="${icon.color}" stroke-width="1.5"
     stroke-linecap="round" stroke-linejoin="round"
     aria-hidden="true" focusable="false"
     style="color:${icon.color};filter:drop-shadow(0 0 16px ${icon.glow}) drop-shadow(0 0 4px ${icon.glow});">
     ${icon.paths}
  </svg>`

/* 把插槽配置转换为内联样式 */
const slotStyle = (s) => ({
  top: s.top,
  left: s.left,
  width: s.size + 'px',
  height: s.size + 'px',
  '--target-opacity': s.op,
  '--enter-rot': s.rot + 'deg',
  '--delay': s.delay + 's'
})

/* ------------------------------------------------------------------ *
 * 鼠标交互：视差跟随 + 光晕跟随
 * 直接写 CSS 变量到 DOM，避免响应式重渲染，保证流畅
 * ------------------------------------------------------------------ */
const wrapper = ref(null)
const cursor = ref({ x: 50, y: 50, active: false })
let rafId = null
let pending = null

const cursorStyle = computed(() => ({
  '--hx': cursor.value.x + '%',
  '--hy': cursor.value.y + '%'
}))

const applyParallax = (mx, my) => {
  const el = wrapper.value
  if (!el) return
  // mx / my 取值范围 -0.5 ~ 0.5
  el.style.setProperty('--mx', mx.toFixed(4))
  el.style.setProperty('--my', my.toFixed(4))
}

const onPointerMove = (e) => {
  pending = {
    mx: e.clientX / window.innerWidth - 0.5,
    my: e.clientY / window.innerHeight - 0.5,
    hx: (e.clientX / window.innerWidth) * 100,
    hy: (e.clientY / window.innerHeight) * 100
  }
  if (rafId == null) {
    rafId = requestAnimationFrame(flush)
  }
}

const flush = () => {
  rafId = null
  if (!pending) return
  applyParallax(pending.mx, pending.my)
  cursor.value = { x: pending.hx, y: pending.hy, active: true }
  pending = null
}

const onPointerLeave = () => {
  applyParallax(0, 0)
  cursor.value = { ...cursor.value, active: false }
}

onMounted(() => {
  applyParallax(0, 0)
  window.addEventListener('pointermove', onPointerMove, { passive: true })
  window.addEventListener('pointerout', onPointerLeave)
})

onUnmounted(() => {
  if (rafId != null) cancelAnimationFrame(rafId)
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerout', onPointerLeave)
})
</script>

<style scoped>
.animated-bg-wrapper {
  position: fixed;
  inset: 0;
  z-index: 0;
  overflow: hidden;
  pointer-events: none;
  /* 鼠标视差变量，由 JS 实时写入 */
  --mx: 0;
  --my: 0;
  --hx: 50%;
  --hy: 50%;
}

/* ---------- 基础氛围 ---------- */
.bg-base {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(ellipse 80% 60% at 18% 12%, rgba(102, 126, 234, 0.22) 0%, transparent 60%),
    radial-gradient(ellipse 70% 60% at 85% 18%, rgba(236, 72, 153, 0.18) 0%, transparent 60%),
    radial-gradient(ellipse 90% 70% at 50% 110%, rgba(34, 211, 238, 0.18) 0%, transparent 60%),
    radial-gradient(ellipse 60% 60% at 80% 90%, rgba(139, 92, 246, 0.18) 0%, transparent 60%),
    linear-gradient(180deg, #07091c 0%, #0a0e27 50%, #08081f 100%);
}

/* 模糊光晕：缓慢漂浮，营造层次 */
.bg-orbs { position: absolute; inset: 0; }
.orb {
  position: absolute;
  border-radius: 50%;
  filter: blur(90px);
  opacity: 0.55;
  animation: orb-float 16s ease-in-out infinite;
  will-change: transform;
}
.orb-1 {
  width: 460px; height: 460px;
  top: -120px; left: -120px;
  background: radial-gradient(circle, rgba(102, 126, 234, 0.55) 0%, transparent 70%);
}
.orb-2 {
  width: 540px; height: 540px;
  bottom: -160px; right: -160px;
  background: radial-gradient(circle, rgba(236, 72, 153, 0.45) 0%, transparent 70%);
  animation-delay: 3s;
}
.orb-3 {
  width: 360px; height: 360px;
  top: 35%; left: 18%;
  background: radial-gradient(circle, rgba(34, 211, 238, 0.4) 0%, transparent 70%);
  animation-delay: 6s;
  opacity: 0.4;
}
.orb-4 {
  width: 320px; height: 320px;
  top: 10%; right: 22%;
  background: radial-gradient(circle, rgba(139, 92, 246, 0.4) 0%, transparent 70%);
  animation-delay: 9s;
  opacity: 0.4;
}
@keyframes orb-float {
  0%, 100% { transform: translate(0, 0) scale(1); }
  33%      { transform: translate(40px, -30px) scale(1.08); }
  66%      { transform: translate(-30px, 25px) scale(0.96); }
}

/* ---------- 图标层通用 ---------- */
.bg-layer {
  position: absolute;
  inset: -8%;
  will-change: transform;
}

.floating-icon {
  position: absolute;
  /* 进入动画：放大 + 旋转 + 淡入 */
  opacity: 0;
  transform: scale(0.3) rotate(var(--enter-rot, -20deg));
  animation: icon-enter 1.2s cubic-bezier(0.2, 0.85, 0.25, 1) var(--delay, 0s) forwards;
}
@keyframes icon-enter {
  0%   { opacity: 0; transform: scale(0.3) rotate(var(--enter-rot, -20deg)); }
  55%  { opacity: var(--target-opacity, 1); transform: scale(1.12) rotate(calc(var(--enter-rot, -20deg) * -0.15)); }
  100% { opacity: var(--target-opacity, 1); transform: scale(1) rotate(0deg); }
}

.icon-inner {
  width: 100%;
  height: 100%;
  will-change: transform;
}
.icon-art {
  width: 100%;
  height: 100%;
  transition: transform 0.45s cubic-bezier(0.2, 0.85, 0.25, 1), filter 0.45s ease;
}

/* ---------- 远景层：大、模糊、慢 ---------- */
.layer-deep {
  filter: blur(13px) saturate(1.2);
  transform: translate3d(calc(var(--mx) * 14px), calc(var(--my) * 14px), 0);
  transition: transform 0.7s cubic-bezier(0.2, 0.85, 0.25, 1);
}
.layer-deep .icon-inner { animation: deep-drift 22s ease-in-out infinite; }
@keyframes deep-drift {
  0%, 100% { transform: translate(0, 0); }
  50%      { transform: translate(14px, -10px); }
}

/* ---------- 中景层 ---------- */
.layer-mid {
  filter: blur(2.5px) saturate(1.25);
  transform: translate3d(calc(var(--mx) * 30px), calc(var(--my) * 30px), 0);
  transition: transform 0.55s cubic-bezier(0.2, 0.85, 0.25, 1);
}

/* ---------- 前景层：清晰、可交互 ---------- */
.layer-front {
  transform: translate3d(calc(var(--mx) * 54px), calc(var(--my) * 54px), 0);
  transition: transform 0.4s cubic-bezier(0.2, 0.85, 0.25, 1);
}
.icon-art--interactive {
  pointer-events: auto;
  cursor: pointer;
}
.icon-art--interactive:hover {
  transform: scale(1.28) rotate(10deg);
  filter: drop-shadow(0 0 30px var(--glow, rgba(255,255,255,0))) brightness(1.25);
}

/* ---------- 平面漂浮动画：四种变体 ---------- */
.float-a { animation: float-a var(--dur, 12s) ease-in-out infinite; }
.float-b { animation: float-b var(--dur, 12s) ease-in-out infinite; }
.float-c { animation: float-c var(--dur, 12s) ease-in-out infinite; }

@keyframes float-a {
  0%, 100% { transform: translateY(-10px) rotate(-4deg); }
  50%      { transform: translateY(12px)  rotate(4deg); }
}
@keyframes float-b {
  0%, 100% { transform: translateX(-12px) rotate(5deg); }
  50%      { transform: translateX(12px)  rotate(-5deg); }
}
@keyframes float-c {
  0%, 100% { transform: translate(8px, -8px) rotate(6deg); }
  50%      { transform: translate(-8px, 10px) rotate(-6deg); }
}

/* ---------- 纹理 / 聚焦 / 光标光晕 ---------- */
.bg-grid {
  position: absolute;
  inset: 0;
  background-image: radial-gradient(rgba(255, 255, 255, 0.07) 1px, transparent 1px);
  background-size: 30px 30px;
  opacity: 0.45;
  -webkit-mask-image: radial-gradient(ellipse 70% 70% at center, black 25%, transparent 78%);
          mask-image: radial-gradient(ellipse 70% 70% at center, black 25%, transparent 78%);
}

.bg-vignette {
  position: absolute;
  inset: 0;
  background: radial-gradient(ellipse 90% 90% at center, transparent 45%, rgba(0, 0, 0, 0.6) 100%);
  pointer-events: none;
}

.bg-cursor {
  position: absolute;
  inset: 0;
  background: radial-gradient(circle 380px at var(--hx) var(--hy),
              rgba(255, 255, 255, 0.10), transparent 70%);
  mix-blend-mode: soft-light;
  transition: background 0.25s ease;
}

/* ---------- 尊重“减少动态效果”偏好 ---------- */
@media (prefers-reduced-motion: reduce) {
  .orb,
  .floating-icon,
  .icon-inner,
  .layer-deep .icon-inner { animation: none !important; }
  .floating-icon { opacity: var(--target-opacity, 1); transform: scale(1) rotate(0); }
  .bg-layer { transition: none; }
}
</style>
