<template>
  <SmartLink class="trending-card" :class="`type-${type}`" :to="targetUrl">
    <!-- 排名：纯数字，Top 1/2/3 金银铜文字色 -->
    <div class="rank-badge" :style="rankStyle">
      {{ rank }}
    </div>

    <!-- 头像 -->
    <div class="avatar-wrap">
      <NAvatar round :size="44" :src="avatarUrl" :style="!avatarUrl ? { background: avatarBg } : null">
        <span v-if="!avatarUrl" class="avatar-fallback">{{ fallbackChar }}</span>
      </NAvatar>
    </div>

    <!-- 文案 -->
    <div class="card-body">
      <div class="primary">{{ primary }}</div>
      <div v-if="secondary" class="secondary">{{ secondary }}</div>
    </div>

    <ShareCount
      v-if="type === 'post'"
      :post-id="item.id"
      :share-count="item.share_count"
      :is-shared="item.is_shared"
    />

    <!-- 热度：SVG 火焰 + 暖色胶囊（语义色，同全局 warning 琥珀一脉） -->
    <div class="hot-score">
      <svg class="flame-svg" viewBox="0 0 24 24" aria-hidden="true">
        <defs>
          <linearGradient :id="flameGradId" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="#fbbf24" />
            <stop offset="55%" stop-color="#f59e0b" />
            <stop offset="100%" stop-color="#f97316" />
          </linearGradient>
        </defs>
        <path
          :fill="`url(#${flameGradId})`"
          d="M13.5.67s.74 2.65.74 4.8c0 2.06-1.35 3.73-3.41 3.73-2.07 0-3.63-1.67-3.63-3.73l.03-.36C5.21 7.51 4 10.62 4 14c0 4.42 3.58 8 8 8s8-3.58 8-8C20 8.61 17.41 3.8 13.5.67zM11.71 19c-1.78 0-3.22-1.4-3.22-3.14 0-1.62 1.05-2.76 2.81-3.12 1.77-.36 3.6-1.21 4.62-2.58.39 1.29.59 2.65.59 4.04 0 2.65-2.15 4.8-4.8 4.8z"
        />
      </svg>
      <span>{{ formatNumber(item.hot_score) }}</span>
    </div>
  </SmartLink>
</template>

<script setup>
import { computed, useId } from 'vue'
import { NAvatar } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useFormatNumber } from '@/utils/i18n'
import ShareCount from '@/components/post/ShareCount.vue'
import SmartLink from '@/components/common/SmartLink.vue'

const props = defineProps({
  // 条目类型：post | circle | user
  type: {
    type: String,
    required: true,
    validator: (v) => ['post', 'circle', 'user'].includes(v)
  },
  // 排名（从 1 开始）
  rank: {
    type: Number,
    required: true
  },
  // 原始 API 条目（snake_case）
  item: {
    type: Object,
    required: true
  }
})

// 渐变 id 用 useId 保证每实例唯一：同页多卡片时避免 SVG defs id 冲突，
// 防止时间窗切换卸载首个实例后其余火焰丢失渐变
const flameGradId = `hot-flame-${useId()}`

const { t } = useI18n()
const { formatNumber } = useFormatNumber()

// 跳转目标：按类型归一到真实 URL，交给 SmartLink 渲染 <a href>
const targetUrl = computed(() => {
  const id = props.item.id
  if (!id) return null
  return {
    post: `/post/${id}`,
    circle: `/circle/${id}`,
    user: `/user/${id}`
  }[props.type]
})

// 头像 URL：post 取作者头像，circle/user 取 avatar_url
const avatarUrl = computed(() => {
  if (props.type === 'post') return props.item.author_avatar || ''
  return props.item.avatar_url || ''
})

// 主题色背景（无头像时的首字母占位），绿色家族三档深浅区分类型
const avatarBg = computed(() => {
  return {
    post: 'linear-gradient(135deg, #66eac2, #22b36a)',
    circle: 'linear-gradient(135deg, #2dd4bf, #0e9f6e)',
    user: 'linear-gradient(135deg, #a7f3d0, #34d399)'
  }[props.type]
})

const fallbackChar = computed(() => {
  return (primary.value || '?').charAt(0)
})

// 主标题：post=标题，circle=圈子名，user=昵称
const primary = computed(() => {
  if (props.type === 'post') return props.item.title || ''
  if (props.type === 'circle') return props.item.name || ''
  return props.item.username || ''
})

// 副标题
const secondary = computed(() => {
  if (props.type === 'post') {
    const segs = [props.item.author_name, props.item.circle_name].filter(Boolean)
    return segs.length ? segs.join(' · ') : ''
  }
  if (props.type === 'circle') {
    return `${formatNumber(props.item.member_count)} ${t('circle.members')} · ${formatNumber(props.item.post_count)} ${t('circle.posts')}`
  }
  // user 无额外字段
  return ''
})

// Top 1/2/3 金银铜文字色，其余走基础灰
const rankStyle = computed(() => {
  const colors = { 1: '#fcd34d', 2: '#d1d5db', 3: '#e8a866' }
  return colors[props.rank] ? { color: colors[props.rank] } : null
})
</script>

<style scoped>
/* 玻璃拟态榜单卡片：深色半透 + 轻模糊 + 主题绿 hover 光晕 */
.trending-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border-radius: 14px;
  background: rgba(22, 22, 38, 0.55);
  backdrop-filter: blur(16px) saturate(150%);
  -webkit-backdrop-filter: blur(16px) saturate(150%);
  border: 1px solid rgba(255, 255, 255, 0.06);
  text-decoration: none;
  cursor: pointer;
  transition:
    transform 0.2s cubic-bezier(0.34, 1.56, 0.64, 1),
    background 0.2s cubic-bezier(0.4, 0, 0.2, 1),
    border-color 0.2s cubic-bezier(0.4, 0, 0.2, 1),
    box-shadow 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

@media (hover: hover) {
  .trending-card:hover {
    background: rgba(102, 234, 194, 0.06);
    border-color: rgba(102, 234, 194, 0.35);
    transform: translateY(-2px);
    box-shadow:
      0 8px 24px rgba(0, 0, 0, 0.35),
      0 4px 18px rgba(102, 234, 194, 0.1);
  }
}

/* 排名：无底框纯数字，固定宽度保证标题纵向对齐 */
.rank-badge {
  flex-shrink: 0;
  width: 26px;
  text-align: center;
  font-size: 14px;
  font-weight: 800;
  font-variant-numeric: tabular-nums;
  color: rgba(255, 255, 255, 0.4);
}

/* 头像 */
.avatar-wrap {
  flex-shrink: 0;
}

.avatar-fallback {
  color: #fff;
  font-weight: 600;
  font-size: 16px;
}

/* 文案 */
.card-body {
  flex: 1;
  min-width: 0;
}

.primary {
  font-size: 14px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.92);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

@media (hover: hover) {
  .trending-card:hover .primary {
    color: #8af0d0;
  }
}

.secondary {
  margin-top: 3px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.45);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 热度胶囊：暖色语义（对应全局 warning 琥珀），hover 提亮 */
.hot-score {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px 4px 8px;
  border-radius: 999px;
  font-size: 12.5px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: #fbbf24;
  background: rgba(251, 191, 36, 0.1);
  border: 1px solid rgba(251, 191, 36, 0.22);
  transition:
    background 0.2s cubic-bezier(0.4, 0, 0.2, 1),
    border-color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

@media (hover: hover) {
  .trending-card:hover .hot-score {
    background: rgba(251, 191, 36, 0.16);
    border-color: rgba(251, 191, 36, 0.4);
  }
}

.flame-svg {
  width: 14px;
  height: 14px;
  transition:
    transform 0.2s cubic-bezier(0.34, 1.56, 0.64, 1),
    filter 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

@media (hover: hover) {
  .trending-card:hover .flame-svg {
    transform: scale(1.15);
    filter: drop-shadow(0 0 6px rgba(251, 191, 36, 0.55));
  }
}
</style>
