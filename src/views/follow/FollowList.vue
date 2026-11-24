<!-- 关注 / 粉丝列表（路由 user-following / user-followers，:id 为列表主人，访客可读） -->
<template>
  <div class="follow-list-page">
    <AppShell v-model:offset="offset" />
    <div class="content-wrapper" :style="contentStyle">
      <div class="main-content">
        <div class="list-container">
          <!-- 列表主人 -->
          <SmartLink
            v-if="owner"
            class="owner-link"
            :to="{ name: 'user-detail', params: { id: ownerId } }">
            <NAvatar round :size="32" :src="owner.avatar_url || undefined">
              <span v-if="!owner.avatar_url">{{ (owner.username || '?').charAt(0).toUpperCase() }}</span>
            </NAvatar>
            <span class="owner-name">{{ t('follow.listOwner', { name: owner.username || t('user.notSet') }) }}</span>
          </SmartLink>

          <!-- 关注 / 粉丝切换：两个路由共用本页，replace 不堆历史 -->
          <nav class="list-tabs">
            <SmartLink
              v-for="tab in tabs"
              :key="tab.name"
              class="list-tab"
              :class="{ active: tab.name === listType }"
              :to="{ name: tab.name, params: { id: ownerId } }"
              replace>
              {{ tab.label }}
              <span v-if="stats" class="tab-count">{{ tab.count }}</span>
            </SmartLink>
          </nav>

          <div v-if="ownerMissing" class="list-state">{{ t('follow.error.userNotFound') }}</div>
          <!-- stats 已说明不可见：不硬调列表接口（会 403） -->
          <div v-else-if="stats && !stats.list_visible" class="list-state">
            <NIcon :size="28" class="state-icon"><Lock /></NIcon>
            <p>{{ t('follow.listPrivate') }}</p>
          </div>
          <!-- 等 stats 有结果（成功或失败）再拉列表，私密时不发注定 403 的请求 -->
          <FollowUserList
            v-else-if="ownerId && statsSettled"
            :list-key="`${listType}:${ownerId}`"
            :fetch-page="fetchPage"
            :empty-text="emptyText" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NAvatar, NIcon } from 'naive-ui'
import { Lock } from '@vicons/tabler'
import AppShell from '@/components/layout/AppShell.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import FollowUserList from '@/components/follow/FollowUserList.vue'
import { useAppShell } from '@/composables/useAppShell'
import { getUserDetail } from '@/api/user'
import { getFollowStats, getFollowingList, getFollowerList } from '@/api/follow'
import { isFollowUserNotFoundError } from '@/utils/follow'
import { onFollowRelationChange } from '@/composables/useFollowToggle'

const { t } = useI18n()
const { offset, contentStyle } = useAppShell()
const route = useRoute()

const ownerId = computed(() => String(route.params.id || ''))
// 'user-following' | 'user-followers'
const listType = computed(() => (route.name === 'user-followers' ? 'user-followers' : 'user-following'))

const owner = ref(null)
const stats = ref(null)
const ownerMissing = ref(false)
const statsSettled = ref(false)

const tabs = computed(() => [
  { name: 'user-following', label: t('follow.followingCount'), count: stats.value?.following_count ?? 0 },
  { name: 'user-followers', label: t('follow.followerCount'), count: stats.value?.follower_count ?? 0 }
])

const emptyText = computed(() => t(listType.value === 'user-followers' ? 'follow.empty.followers' : 'follow.empty.following'))

const fetchPage = (params) => (listType.value === 'user-followers'
  ? getFollowerList(ownerId.value, params)
  : getFollowingList(ownerId.value, params))

const loadOwner = async (id) => {
  owner.value = null
  stats.value = null
  ownerMissing.value = false
  statsSettled.value = false
  const [detail, st] = await Promise.allSettled([getUserDetail(id), getFollowStats(id)])
  if (id !== ownerId.value) return
  if (detail.status === 'fulfilled' && detail.value?.data) {
    owner.value = detail.value.data
  }
  if (st.status === 'fulfilled' && st.value?.data) {
    stats.value = st.value.data
  } else if (st.status === 'rejected' && isFollowUserNotFoundError(st.reason)) {
    ownerMissing.value = true
  }
  statsSettled.value = true
}

watch(ownerId, (id) => { if (id) loadOwner(id) }, { immediate: true })

// 列表里关注 / 取关后（如在自己的列表里取关），Tab 上的计数以 stats 为准重新拉
const refreshStats = async () => {
  const id = ownerId.value
  try {
    const res = await getFollowStats(id)
    if (id === ownerId.value && res?.data) stats.value = res.data
  } catch {
    // 计数刷新失败保留旧值
  }
}
const offRelationChange = onFollowRelationChange(refreshStats)
onUnmounted(offRelationChange)
</script>

<style scoped>
.follow-list-page {
  min-height: 100vh;
}

.content-wrapper {
  padding-top: var(--header-height);
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.main-content {
  display: flex;
  justify-content: center;
  padding: 24px;
}

.list-container {
  width: 100%;
  max-width: 720px;
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  box-shadow: var(--shadow-md);
  padding: 16px 24px 20px;
}

.owner-link {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  max-width: 100%;
  padding: 4px 8px 4px 4px;
  border-radius: 10px;
  color: inherit;
  text-decoration: none;
  transition: background 0.2s ease;
}

@media (hover: hover) {
  .owner-link:hover {
    background: rgba(102, 234, 194, 0.08);
  }
}

.owner-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
}

/* 关注 / 粉丝切换：与消息中心同款下划线 Tab，吸顶 */
.list-tabs {
  position: sticky;
  top: var(--header-height);
  z-index: 10;
  display: flex;
  gap: 28px;
  margin: 8px -24px 8px;
  padding: 0 24px;
  border-bottom: 1px solid var(--glass-border);
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
}

.list-tab {
  position: relative;
  padding: 12px 2px;
  font-size: 14px;
  color: var(--text-secondary);
  text-decoration: none;
  transition: color 0.2s ease;
}

@media (hover: hover) {
  .list-tab:hover {
    color: var(--text-primary);
  }
}

.list-tab.active {
  color: #8af0d0;
  font-weight: 600;
}

.list-tab.active::after {
  content: '';
  position: absolute;
  left: 50%;
  bottom: -1px;
  transform: translateX(-50%);
  width: 24px;
  height: 3px;
  border-radius: 2px;
  background: var(--theme-color);
}

.tab-count {
  margin-left: 4px;
  font-size: 12px;
  color: var(--text-tertiary);
}

.list-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 48px 16px;
  text-align: center;
  font-size: 14px;
  color: var(--text-secondary);
}

.list-state p {
  margin: 0;
}

.state-icon {
  color: var(--text-tertiary);
}

@media (max-width: 768px) {
  .main-content {
    padding: 0;
  }

  /* 手机上通栏：去掉卡片外框，避免与顶栏 / 屏幕边缘双重留白 */
  .list-container {
    max-width: none;
    border: none;
    border-radius: 0;
    box-shadow: none;
    padding: 8px 4px 16px;
  }

  .list-tabs {
    margin: 8px -4px;
    padding: 0 16px;
  }
}
</style>
