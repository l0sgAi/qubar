<!-- 私信会话列表（路由 messages）：按最后一条消息倒序，dm-message 实时置顶 -->
<template>
  <div class="conversations-page">
    <AppShell v-model:offset="offset" />

    <div class="content-wrapper" :style="contentStyle">
      <div class="main-content">
        <div class="dm-container">
          <div v-if="!mobileShell" class="dm-header">
            <h1 class="page-title">{{ t('message.title') }}</h1>
          </div>

          <div class="conv-list">
            <!-- 整条为真实链接（右键 / 中键可新标签页打开）；路由参数是对方用户 ID -->
            <SmartLink
              v-for="conv in conversations"
              :key="conv.conversation_id"
              class="conv-item"
              :class="{ unread: conv.unread_count > 0 }"
              :to="`/messages/${conv.peer_id}`"
            >
              <NAvatar round :size="46" :src="conv.peer?.avatar_url || undefined" class="conv-avatar">
                <span v-if="!conv.peer?.avatar_url">{{ peerName(conv).charAt(0).toUpperCase() }}</span>
              </NAvatar>
              <div class="conv-body">
                <div class="conv-top">
                  <span class="conv-name" :class="{ deleted: !conv.peer }">{{ peerName(conv) }}</span>
                  <span class="conv-time">{{ formatTime(conv.last_message?.create_time) }}</span>
                </div>
                <div class="conv-bottom">
                  <span class="conv-preview">{{ previewText(conv) }}</span>
                  <span v-if="conv.unread_count > 0" class="conv-badge">
                    {{ conv.unread_count > 99 ? '99+' : conv.unread_count }}
                  </span>
                </div>
              </div>
              <button
                type="button"
                class="conv-delete"
                :aria-label="t('message.deleteConversation')"
                :title="t('message.deleteConversation')"
                @click.stop.prevent="confirmDelete(conv)"
              >
                <NIcon size="18"><Trash /></NIcon>
              </button>
            </SmartLink>

            <NEmpty
              v-if="!loading && !loadError && !conversations.length"
              :description="t('message.empty')"
              class="conv-empty"
            />

            <div v-if="loading" class="conv-loading"><NSpin size="small" /></div>

            <button v-if="loadError && !loading" type="button" class="conv-retry" @click="loadConversations(!conversations.length)">
              {{ t('message.loadFailed') }}
            </button>

            <div v-if="hasMore && !loadError" ref="loadMoreTrigger" class="load-more-trigger"></div>
            <div v-else-if="!hasMore && conversations.length" class="no-more">{{ t('common.noMore') }}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { NAvatar, NEmpty, NIcon, NSpin, useDialog, useMessage } from 'naive-ui'
import { Trash } from '@vicons/tabler'
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import { useAppShell } from '@/composables/useAppShell'
import { useUnreadNotice, refreshDmUnreadCount } from '@/composables/useUnreadNotice'
import { getConversations, deleteConversation } from '@/api/message'
import { DM_PAGE_SIZE } from '@/utils/message'
import { useFormatTime } from '@/utils/i18n'
import { bumpConversation, sumUnread, isLastMessageMine } from '@/components/message/dmHelpers'

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const { formatTime } = useFormatTime()
const { offset, mobileShell, contentStyle } = useAppShell()

const conversations = ref([])
const loading = ref(false)
const loadError = ref(false)
const hasMore = ref(true)
let cursor = ''
// 请求世代：刷新第一页时作废进行中的翻页请求
let generation = 0

const peerName = (conv) => conv.peer?.username || t('message.deletedUser')

// 摘要（后端只给前 100 字）；我发的加「我：」前缀
const previewText = (conv) => {
  const content = conv.last_message?.content || ''
  return isLastMessageMine(conv) ? t('message.ownPreview', { content }) : content
}

const loadConversations = async (reset = false) => {
  if (reset) {
    generation++
    cursor = ''
    hasMore.value = true
  } else if (loading.value || !hasMore.value) {
    return
  }
  const gen = generation
  loading.value = true
  loadError.value = false
  try {
    const res = await getConversations({ cursor: cursor || undefined, size: DM_PAGE_SIZE })
    if (gen !== generation) return
    const data = res.data || {}
    const items = data.items || []
    if (reset) {
      conversations.value = items
    } else {
      // 翻页期间有会话被顶到第一页时可能重复出现：按 conversation_id 去重
      const seen = new Set(conversations.value.map(c => c.conversation_id))
      conversations.value = [...conversations.value, ...items.filter(c => !seen.has(c.conversation_id))]
    }
    hasMore.value = !!data.has_more
    cursor = data.next_cursor || ''
  } catch (error) {
    if (gen !== generation) return
    console.error('加载私信会话失败:', error)
    loadError.value = true
  } finally {
    if (gen === generation) {
      loading.value = false
      nextTick(setupObserver)
    }
  }
}

// 收到新私信：命中的会话置顶并更新摘要；没命中（新会话 / 删除后重新出现）刷新第一页
const { dmUnreadCount } = useUnreadNotice({
  onDmMessage: (msg) => {
    const { list, found } = bumpConversation(conversations.value, msg)
    if (found) conversations.value = list
    else loadConversations(true)
  }
})

// SSE 断开、靠轮询更新角标时收不到 dm-message：私信未读总数与列表对不上就刷新第一页
let refreshTimer = null
watch(dmUnreadCount, (count) => {
  clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    if (!loading.value && count !== sumUnread(conversations.value)) loadConversations(true)
  }, 1500)
})

const confirmDelete = (conv) => {
  dialog.warning({
    title: t('message.deleteConversation'),
    content: t('message.deleteConfirm'),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await deleteConversation(conv.conversation_id)
        conversations.value = conversations.value.filter(c => c.conversation_id !== conv.conversation_id)
        message.success(t('message.deleted'))
        refreshDmUnreadCount()
      } catch (error) {
        console.error('删除会话失败:', error)
        message.error(t('message.deleteFailed'))
      }
    }
  })
}

// 无限滚动
const loadMoreTrigger = ref(null)
let observer = null

const cleanupObserver = () => {
  if (observer) {
    observer.disconnect()
    observer = null
  }
}

const setupObserver = () => {
  cleanupObserver()
  if (!loadMoreTrigger.value || typeof IntersectionObserver === 'undefined') return
  observer = new IntersectionObserver((entries) => {
    if (entries.some(e => e.isIntersecting) && hasMore.value && !loading.value && !loadError.value) {
      loadConversations()
    }
  }, { rootMargin: '120px' })
  observer.observe(loadMoreTrigger.value)
}

onMounted(() => loadConversations(true))

onBeforeUnmount(() => {
  cleanupObserver()
  clearTimeout(refreshTimer)
})
</script>

<style scoped>
.conversations-page {
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

.dm-container {
  width: 100%;
  max-width: 720px;
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  box-shadow: var(--shadow-md);
  padding: 20px 16px;
}

.dm-header {
  padding: 0 8px 12px;
  border-bottom: 1px solid var(--glass-border);
  margin-bottom: 8px;
}

.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: var(--text-primary);
}

.conv-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.conv-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border-radius: 10px;
  color: inherit;
  text-decoration: none;
  transition: background 0.2s ease;
}

@media (hover: hover) {
  .conv-item:hover {
    background: rgba(255, 255, 255, 0.05);
  }
}

.conv-item:focus-visible {
  outline: 2px solid var(--theme-color);
  outline-offset: -2px;
}

.conv-avatar {
  flex-shrink: 0;
  font-weight: 600;
}

.conv-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.conv-top,
.conv-bottom {
  display: flex;
  align-items: center;
  gap: 8px;
}

.conv-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}

.conv-name.deleted {
  color: var(--text-tertiary);
  font-weight: 500;
}

.conv-time {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--text-tertiary);
}

.conv-preview {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  color: var(--text-secondary);
}

.conv-item.unread .conv-preview {
  color: var(--text-primary);
}

.conv-badge {
  flex-shrink: 0;
  min-width: 20px;
  height: 20px;
  padding: 0 6px;
  border-radius: 10px;
  background: #e5484d;
  color: #fff;
  font-size: 11px;
  font-weight: 600;
  line-height: 20px;
  text-align: center;
}

/* 删除按钮：桌面悬浮才出现，触屏常显 */
.conv-delete {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 8px;
  background: none;
  color: var(--text-tertiary);
  cursor: pointer;
  transition: color 0.2s ease, background 0.2s ease, opacity 0.2s ease;
}

@media (hover: hover) {
  .conv-delete {
    opacity: 0;
  }

  .conv-item:hover .conv-delete,
  .conv-delete:focus-visible {
    opacity: 1;
  }

  .conv-delete:hover {
    color: #ff6b6b;
    background: rgba(255, 107, 107, 0.12);
  }
}

.conv-empty,
.conv-loading {
  padding: 32px 0;
  display: flex;
  justify-content: center;
}

.conv-retry {
  margin: 16px auto;
  padding: 6px 14px;
  border: 1px solid var(--glass-border);
  border-radius: 8px;
  background: none;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
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

@media (max-width: 768px) {
  .main-content {
    padding: 8px;
  }

  .dm-container {
    padding: 8px 4px;
    border-radius: 12px;
  }

  .conv-item {
    padding: 10px 8px;
  }
}
</style>
