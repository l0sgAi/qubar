<template>
  <!-- 发现圈子卡：两种形态
       mini  —— 横向紧凑行，插在「探索流」帖子之间
       card  —— 纵向卡片，用于「分区」网格与「灵感墙」方块 -->
  <SmartLink
    class="discover-circle-card"
    :class="`variant-${variant}`"
    :to="circle.id ? `/circle/${circle.id}` : null"
  >
    <!-- mini 形态：头像 + 文案 + 统计 + 箭头 -->
    <template v-if="variant === 'mini'">
      <div class="badge">✦ {{ t('discover.exploreHint') }}</div>
      <div class="avatar-wrap">
        <NAvatar round :size="44" :src="avatarUrl" :style="!avatarUrl ? { background: avatarBg } : null">
          <span v-if="!avatarUrl" class="avatar-fallback">{{ fallbackChar }}</span>
        </NAvatar>
      </div>
      <div class="body">
        <div class="name">{{ name }}</div>
        <div v-if="description" class="desc">{{ description }}</div>
        <div class="stats">
          <span>{{ formatNumber(memberCount) }} {{ t('discover.members') }}</span>
          <span class="dot">·</span>
          <span>{{ formatNumber(postCount) }} {{ t('discover.posts') }}</span>
          <span v-if="isPrivate" class="join-tag private">{{ t('discover.joinPrivate') }}</span>
          <span v-else class="join-tag">{{ t('discover.joinDirect') }}</span>
        </div>
      </div>
      <div class="chevron">›</div>
    </template>

    <!-- card 形态：纵向 -->
    <template v-else>
      <div class="card-top">
        <NAvatar round :size="40" :src="avatarUrl" :style="!avatarUrl ? { background: avatarBg } : null">
          <span v-if="!avatarUrl" class="avatar-fallback">{{ fallbackChar }}</span>
        </NAvatar>
        <div class="name">{{ name }}</div>
      </div>
      <p v-if="description" class="desc">{{ description }}</p>
      <div class="card-stats">
        <span>{{ formatNumber(memberCount) }} {{ t('discover.members') }}</span>
        <span class="dot">·</span>
        <span>{{ formatNumber(postCount) }} {{ t('discover.posts') }}</span>
      </div>
      <div class="card-join" :class="{ private: isPrivate }">
        {{ isPrivate ? t('discover.joinPrivate') : t('discover.joinDirect') }}
      </div>
    </template>
  </SmartLink>
</template>

<script setup>
import { computed } from 'vue'
import { NAvatar } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useFormatNumber } from '@/utils/i18n'
import SmartLink from '@/components/common/SmartLink.vue'
import { isPrivateJoinType } from '@/constants/circle'

const props = defineProps({
  // 原始 API 条目（snake_case DiscoverCircleItem）
  circle: {
    type: Object,
    required: true
  },
  variant: {
    type: String,
    default: 'card',
    validator: (v) => ['mini', 'card'].includes(v)
  }
})

const { t } = useI18n()
const { formatNumber } = useFormatNumber()

const name = computed(() => props.circle.name || t('circle.interestCircle'))
const description = computed(() => props.circle.description || '')
const avatarUrl = computed(() => props.circle.avatar_url || '')
const memberCount = computed(() => props.circle.member_count || 0)
const postCount = computed(() => props.circle.post_count || 0)
// 发现页后端已过滤私密圈，这里只做兜底（含已下线的需审核 1）
const isPrivate = computed(() => isPrivateJoinType(props.circle.join_type))

// 无头像时的渐变背景：沿用项目主题色（薄荷绿 #60F8BB），区别于热点的粉紫
const avatarBg = 'var(--primary-gradient)'
const fallbackChar = computed(() => (name.value || '?').charAt(0))
</script>

<style scoped>
.discover-circle-card {
  position: relative;
  cursor: pointer;
  border: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(255, 255, 255, 0.03);
  transition: all 0.2s ease;
}

@media (hover: hover) {
  .discover-circle-card:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(96, 248, 187,0.35);
    transform: translateY(-2px);
  }
}

.avatar-fallback {
  color: #fff;
  font-weight: 700;
  font-size: 16px;
}

.name {
  font-weight: 600;
  color: rgba(255, 255, 255, 0.95);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.desc {
  color: rgba(255, 255, 255, 0.55);
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.dot {
  margin: 0 6px;
  opacity: 0.4;
}

/* ---------- mini ---------- */
.variant-mini {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 14px;
  overflow: hidden;
}

.variant-mini .badge {
  position: absolute;
  top: 6px;
  right: 10px;
  font-size: 10px;
  letter-spacing: 0.3px;
  color: #60F8BB;
  background: rgba(96, 248, 187,0.14);
  border: 1px solid rgba(96, 248, 187,0.3);
  border-radius: 999px;
  padding: 1px 8px;
}

.variant-mini .body {
  flex: 1;
  min-width: 0;
}

.variant-mini .body .name {
  font-size: 15px;
}

.variant-mini .body .desc {
  margin-top: 3px;
  font-size: 12px;
  -webkit-line-clamp: 1;
}

.variant-mini .stats {
  margin-top: 6px;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
}

.variant-mini .chevron {
  flex-shrink: 0;
  color: rgba(255, 255, 255, 0.3);
  font-size: 22px;
  line-height: 1;
}

.join-tag {
  margin-left: 8px;
  font-size: 11px;
  color: #60F8BB;
  background: rgba(96, 248, 187,0.12);
  border-radius: 6px;
  padding: 1px 6px;
}

.join-tag.private {
  color: #fbbf24;
  background: rgba(245, 158, 11, 0.12);
}

/* ---------- card ---------- */
.variant-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 16px;
  border-radius: 16px;
  height: 100%;
  box-sizing: border-box;
}

.variant-card .card-top {
  display: flex;
  align-items: center;
  gap: 10px;
}

.variant-card .card-top .name {
  font-size: 15px;
  flex: 1;
  min-width: 0;
}

.variant-card .desc {
  font-size: 13px;
  line-height: 1.5;
  min-height: 1.5em;
}

.variant-card .card-stats {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
  display: flex;
  align-items: center;
}

.variant-card .card-join {
  margin-top: auto;
  align-self: flex-start;
  font-size: 12px;
  font-weight: 600;
  color: #60F8BB;
  background: rgba(96, 248, 187,0.12);
  border: 1px solid rgba(96, 248, 187,0.3);
  border-radius: 999px;
  padding: 3px 12px;
}

.variant-card .card-join.private {
  color: #fbbf24;
  background: rgba(245, 158, 11, 0.12);
  border-color: rgba(245, 158, 11, 0.3);
}
</style>
