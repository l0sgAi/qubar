<template>
  <!-- 关注数 / 粉丝数：list_visible=true 时是进入列表的链接，false（对方设为私密）时只显示数字 -->
  <div class="follow-stats">
    <component
      :is="listVisible ? SmartLink : 'span'"
      v-for="item in items"
      :key="item.key"
      class="stat-item"
      :class="{ clickable: listVisible }"
      :to="listVisible ? item.to : undefined"
      :title="listVisible ? undefined : t('follow.listPrivate')"
    >
      <span class="stat-count">{{ item.count }}</span>
      <span class="stat-label">{{ item.label }}</span>
    </component>
    <!-- 额外入口（如个人中心的「互关」） -->
    <slot />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import SmartLink from '@/components/common/SmartLink.vue'

const props = defineProps({
  userId: { type: String, required: true },
  followingCount: { type: Number, default: 0 },
  followerCount: { type: Number, default: 0 },
  listVisible: { type: Boolean, default: true }
})

const { t } = useI18n()

const items = computed(() => [
  {
    key: 'following',
    count: props.followingCount,
    label: t('follow.followingCount'),
    to: { name: 'user-following', params: { id: props.userId } }
  },
  {
    key: 'followers',
    count: props.followerCount,
    label: t('follow.followerCount'),
    to: { name: 'user-followers', params: { id: props.userId } }
  }
])
</script>

<style scoped>
.follow-stats {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 24px;
}

.stat-item {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  padding: 4px 8px;
  border-radius: 8px;
  color: inherit;
  text-decoration: none;
  transition: background 0.2s ease, color 0.2s ease;
}

.stat-item.clickable {
  cursor: pointer;
}

@media (hover: hover) {
  .stat-item.clickable:hover {
    background: rgba(102, 234, 194, 0.14);
  }

  .stat-item.clickable:hover .stat-label {
    color: #8af0d0;
  }
}

.stat-item.clickable:focus-visible {
  outline: 2px solid var(--theme-color);
  outline-offset: 2px;
}

.stat-count {
  font-size: 17px;
  font-weight: 700;
  color: var(--text-primary);
}

.stat-label {
  font-size: 13px;
  color: var(--text-secondary);
}
</style>
