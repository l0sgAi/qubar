<!-- 关注 / 粉丝 / 互关通用列表：游标分页 + 无限滚动，结束只看 has_more；403 私密 / 404 / 失败重试各有状态 -->
<template>
  <div class="follow-user-list">
    <FollowUserItem
      v-for="item in items"
      :key="item.user.id"
      :user="item.user"
      :relation="item.relation ?? 0"
      :viewer-id="viewerId || ''"
      @update:relation="(v) => setRelation(item.user.id, v)" />

    <!-- 私密：对方把列表设为不公开 -->
    <div v-if="error === FOLLOW_LIST_ERROR.PRIVATE" class="list-state">
      <NIcon :size="28" class="state-icon"><Lock /></NIcon>
      <p>{{ t('follow.listPrivate') }}</p>
    </div>
    <div v-else-if="error === FOLLOW_LIST_ERROR.NOT_FOUND" class="list-state">
      <p>{{ t('follow.error.userNotFound') }}</p>
    </div>
    <div v-else-if="error === FOLLOW_LIST_ERROR.GENERIC" class="list-state">
      <p>{{ t('follow.loadFailed') }}</p>
      <NButton size="small" round secondary @click="loadMore">{{ t('follow.retry') }}</NButton>
    </div>

    <NEmpty
      v-else-if="loaded && !loading && !hasMore && !items.length"
      class="list-empty"
      :description="emptyText" />

    <div v-if="loading" class="list-loading"><NSpin size="small" /></div>

    <!-- 无限滚动触发器 / 到底提示（出错时不挂，避免触发器常驻视口无限重试） -->
    <div v-if="hasMore && !error" ref="trigger" class="load-more-trigger"></div>
    <div v-else-if="!hasMore && !error && items.length" class="no-more">{{ t('follow.noMore') }}</div>
  </div>
</template>

<script setup>
import { onMounted, onUnmounted, ref, watch, nextTick } from 'vue'
import { NButton, NEmpty, NIcon, NSpin } from 'naive-ui'
import { Lock } from '@vicons/tabler'
import { useI18n } from 'vue-i18n'
import FollowUserItem from '@/components/follow/FollowUserItem.vue'
import { useFollowList, FOLLOW_LIST_ERROR } from '@/composables/useFollowList'
import { ensureViewer } from '@/composables/usePostReport'

const props = defineProps({
  // (params: { cursor?, size }) => Promise<响应信封>
  fetchPage: { type: Function, required: true },
  // 列表标识（如 `following:<id>`）：变化时清空重拉
  listKey: { type: String, required: true },
  emptyText: { type: String, default: '' }
})

const { t } = useI18n()

const { items, hasMore, loading, loaded, error, loadMore, reset, setRelation } = useFollowList({
  fetchPage: (params) => props.fetchPage(params)
})

// 列表里出现自己时不显示关注按钮
const viewerId = ref(null)
ensureViewer().then(id => { viewerId.value = id })

const trigger = ref(null)
let observer = null

const observe = () => {
  observer?.disconnect()
  if (!trigger.value || typeof IntersectionObserver === 'undefined') return
  observer = new IntersectionObserver((entries) => {
    if (entries.some(e => e.isIntersecting)) loadMore()
  }, { rootMargin: '200px' })
  observer.observe(trigger.value)
}

// 每次加载结束后重新挂观察器：触发器仍在视口内时（如首屏不满一屏）会继续加载
watch(loading, async (v) => {
  if (v) return
  await nextTick()
  observe()
})

watch(() => props.listKey, () => reset())

onMounted(() => {
  loadMore()
})

onUnmounted(() => {
  observer?.disconnect()
})

defineExpose({ reload: () => reset() })
</script>

<style scoped>
.follow-user-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
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

.list-empty {
  padding: 48px 0;
}

.list-loading {
  display: flex;
  justify-content: center;
  padding: 16px 0;
}

.load-more-trigger {
  height: 1px;
}

.no-more {
  padding: 16px 0;
  text-align: center;
  font-size: 12px;
  color: var(--text-tertiary);
}
</style>
