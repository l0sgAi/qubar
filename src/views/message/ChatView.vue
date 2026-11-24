<!-- 私信聊天页（路由 message-chat，:userId 为对方用户 ID） -->
<template>
  <div class="chat-page">
    <AppShell v-model:offset="offset" />

    <div class="chat-wrapper" :style="wrapperStyle">
      <div class="chat-panel">
        <!-- 对方信息 -->
        <div class="chat-peer">
          <SmartLink v-if="peer" class="peer-link" :to="`/user/${peerId}`">
            <NAvatar round :size="36" :src="peer.avatar_url || undefined">
              <span v-if="!peer.avatar_url">{{ (peer.username || '?').charAt(0).toUpperCase() }}</span>
            </NAvatar>
            <span class="peer-name">{{ peer.username }}</span>
          </SmartLink>
          <div v-else-if="!initLoading" class="peer-link deleted">
            <NAvatar round :size="36"><span>?</span></NAvatar>
            <span class="peer-name">{{ t('message.deletedUser') }}</span>
          </div>
        </div>

        <!-- 消息列表：旧在上、新在下；滚到顶部加载更旧 -->
        <div ref="listRef" class="chat-list" @scroll.passive="handleScroll">
          <div v-if="initLoading" class="chat-state"><NSpin size="small" /></div>
          <button v-else-if="initError" type="button" class="chat-retry" @click="init">
            {{ t('message.loadFailed') }}
          </button>
          <template v-else>
            <div class="chat-older">
              <NSpin v-if="loadingOlder" size="small" />
              <button v-else-if="hasMore" type="button" class="older-btn" @click="loadOlder">
                {{ t('message.loadOlder') }}
              </button>
            </div>

            <div v-if="!messages.length && !pending.length" class="chat-empty">{{ t('message.emptyChat') }}</div>

            <template v-for="(msg, i) in messages" :key="msg.id">
              <div v-if="msg.id === dividerId" class="chat-divider"><span>{{ t('message.newMessagesDivider') }}</span></div>
              <div v-if="needsTimeLabel(messages[i - 1], msg)" class="chat-time">{{ formatChatTime(msg.create_time, locale) }}</div>
              <div class="chat-row" :class="isMine(msg) ? 'mine' : 'theirs'">
                <div class="chat-bubble">{{ msg.content }}</div>
              </div>
            </template>

            <!-- 发送中 / 发送失败的本地消息 -->
            <div v-for="item in pending" :key="item.localId" class="chat-row mine">
              <div class="pending-status">
                <NSpin v-if="item.status === 'sending'" :size="14" />
                <template v-else>
                  <span class="pending-error">{{ item.errorText }}</span>
                  <button type="button" class="pending-action" @click="retrySend(item)">{{ t('message.retry') }}</button>
                  <button type="button" class="pending-action" @click="discardPending(item)">{{ t('common.delete') }}</button>
                </template>
              </div>
              <div class="chat-bubble pending" :class="{ failed: item.status === 'failed' }">{{ item.content }}</div>
            </div>
          </template>
        </div>

        <!-- 输入区 -->
        <div class="chat-composer">
          <div v-if="composerHint" class="composer-hint" :class="{ blocked: !canSend }">{{ composerHint }}</div>
          <div class="composer-row">
            <NInput
              v-model:value="draft"
              type="textarea"
              class="composer-input"
              :autosize="{ minRows: 1, maxRows: 5 }"
              :placeholder="canSend ? t('message.inputPlaceholder') : cannotSendText"
              :disabled="!canSend || initLoading || !!initError"
              @keydown="handleKeydown"
            />
            <NButton
              type="primary"
              class="send-btn"
              :disabled="!canSend || !!draftError || initLoading"
              @click="handleSend"
            >
              {{ t('message.send') }}
            </NButton>
          </div>
          <div v-if="showCounter" class="composer-counter" :class="{ over: draftLength > DM_CONTENT_MAX }">
            {{ t('message.charCount', { count: draftLength, max: DM_CONTENT_MAX }) }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, nextTick, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { NAvatar, NButton, NInput, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import { useAppShell } from '@/composables/useAppShell'
import { useKeyboardInset } from '@/composables/useKeyboardInset'
import { useUnreadNotice, refreshDmUnreadCount } from '@/composables/useUnreadNotice'
import { usePageTitle } from '@/composables/usePageTitle'
import {
  getConversationWith,
  getConversationMessages,
  markConversationRead,
  sendMessage
} from '@/api/message'
import { getUserDetail } from '@/api/user'
import {
  DM_CONTENT_MAX,
  DM_PAGE_SIZE,
  DM_REASON,
  dmReasonKey,
  dmSendErrorReason,
  isDmBlockedReason,
  validateDmContent,
  dmContentLength,
  createClientMsgId,
  hasDmRemainingLimit,
  mergeMessagesById
} from '@/utils/message'
import { isBadRequestError } from '@/utils/apiError'
import {
  toMessageVO,
  sentToMessageVO,
  newMessagesDividerId,
  needsTimeLabel,
  formatChatTime,
  SEND_RETRY_DELAYS
} from '@/components/message/dmHelpers'

const { t, locale } = useI18n()
const route = useRoute()
const toast = useMessage()
const { setTitleData } = usePageTitle()
const { offset, mobileShell, contentStyle } = useAppShell()
const { inset } = useKeyboardInset(mobileShell)

// 桌面让出侧栏；移动端无 TabBar，键盘弹出时整体抬高到键盘上方
const wrapperStyle = computed(() => (mobileShell.value
  ? { paddingBottom: `${inset.value}px` }
  : contentStyle.value))

const peerId = computed(() => String(route.params.userId || ''))

const peer = ref(null)
const conversationId = ref('')
const canSend = ref(false)
const cannotReason = ref(null)
const selfChat = ref(false)

const messages = ref([])
const hasMore = ref(false)
let cursor = ''
const loadingOlder = ref(false)
const dividerId = ref(null)

const initLoading = ref(true)
const initError = ref(false)

const pending = ref([])
const remainingCount = ref(null)
const draft = ref('')

// 页面世代：切换对方 / 卸载后作废进行中的请求与重试
let generation = 0
let alive = true
// 最近一次收到 dm-message 的时间（轮询兜底时判断是否需要主动拉取）
let lastDmEventAt = 0

const isMine = (msg) => msg.sender_id !== peerId.value

const cannotSendText = computed(() => {
  if (selfChat.value) return t('message.cannotMessageSelf')
  if (!peer.value && conversationId.value) return t('message.peerDeleted')
  return t(dmReasonKey(cannotReason.value || DM_REASON.UNKNOWN), { max: DM_CONTENT_MAX })
})

const composerHint = computed(() => {
  if (initLoading.value || initError.value) return ''
  if (!canSend.value) return cannotSendText.value
  if (hasDmRemainingLimit(remainingCount.value)) return t('message.remaining', { count: remainingCount.value })
  return ''
})

const draftLength = computed(() => dmContentLength(draft.value))
const draftError = computed(() => validateDmContent(draft.value))
const showCounter = computed(() => draftLength.value > DM_CONTENT_MAX - 200)

// ---- 滚动 ----
const listRef = ref(null)

const isNearBottom = () => {
  const el = listRef.value
  return !el || el.scrollHeight - el.scrollTop - el.clientHeight < 120
}

const scrollToBottom = () => nextTick(() => {
  const el = listRef.value
  if (el) el.scrollTop = el.scrollHeight
})

const handleScroll = () => {
  const el = listRef.value
  if (el && el.scrollTop < 60) loadOlder()
}

// ---- 加载 ----
const applyConversationState = (data) => {
  canSend.value = !!data?.can_send
  cannotReason.value = data?.cannot_send_reason || null
}

const markRead = async () => {
  if (!conversationId.value) return
  try {
    await markConversationRead(conversationId.value)
  } catch (error) {
    console.error('标记私信已读失败:', error)
  }
}

const fetchLatest = async (gen) => {
  const res = await getConversationMessages(conversationId.value, { size: DM_PAGE_SIZE })
  if (gen !== generation) return null
  return res.data || {}
}

const init = async () => {
  const gen = ++generation
  const userId = peerId.value
  peer.value = null
  conversationId.value = ''
  canSend.value = false
  cannotReason.value = null
  selfChat.value = false
  messages.value = []
  hasMore.value = false
  cursor = ''
  dividerId.value = null
  pending.value = []
  remainingCount.value = null
  draft.value = ''
  initError.value = false
  initLoading.value = true
  if (!userId) return

  try {
    let res
    try {
      res = await getConversationWith(userId)
    } catch (error) {
      if (gen !== generation) return
      // 自己的 ID：后端 400 Cannot have a conversation with yourself
      if (isBadRequestError(error)) {
        selfChat.value = true
        initLoading.value = false
        return
      }
      throw error
    }
    if (gen !== generation) return
    const data = res.data || {}
    const conversation = data.conversation
    applyConversationState(data)

    if (conversation) {
      conversationId.value = conversation.conversation_id
      peer.value = conversation.peer || null
      const page = await fetchLatest(gen)
      if (!page) return
      messages.value = mergeMessagesById([], page.items || [])
      hasMore.value = !!page.has_more
      cursor = page.next_cursor || ''
      // 「以下为新消息」：用标记已读之前的位置
      dividerId.value = newMessagesDividerId(messages.value, page.last_read_message_id, userId)
      await markRead()
      refreshDmUnreadCount()
    } else {
      // 还没聊过：取对方资料作页头；对方不存在时保持 null（输入框已按 can_send 置灰）
      try {
        const detail = await getUserDetail(userId)
        if (gen !== generation) return
        const u = detail.data
        if (u) peer.value = { id: u.id, username: u.username, avatar_url: u.avatar_url }
      } catch (error) {
        console.warn('获取私信对象资料失败:', error)
      }
    }
    if (gen !== generation) return
    if (peer.value?.username) setTitleData('title.userDetailName', { name: peer.value.username })
    initLoading.value = false
    await nextTick()
    scrollToDividerOrBottom()
    fillViewport()
  } catch (error) {
    if (gen !== generation) return
    console.error('加载私信失败:', error)
    initError.value = true
    initLoading.value = false
  }
}

const scrollToDividerOrBottom = () => {
  const el = listRef.value
  if (!el) return
  const divider = dividerId.value ? el.querySelector('.chat-divider') : null
  if (divider) {
    el.scrollTop = Math.max(0, divider.offsetTop - el.offsetTop - 40)
  } else {
    el.scrollTop = el.scrollHeight
  }
}

// 首屏不满一屏、还有更旧消息时继续往上加载，保证能上滑
const fillViewport = () => {
  const el = listRef.value
  // clientHeight 为 0 = 还没布局（或不可见），不判断，避免无限加载
  if (el && el.clientHeight > 0 && hasMore.value && el.scrollHeight <= el.clientHeight) loadOlder()
}

const loadOlder = async () => {
  if (!conversationId.value || !hasMore.value || loadingOlder.value) return
  const gen = generation
  const el = listRef.value
  loadingOlder.value = true
  try {
    const res = await getConversationMessages(conversationId.value, { cursor, size: DM_PAGE_SIZE })
    if (gen !== generation) return
    const data = res.data || {}
    const prevHeight = el ? el.scrollHeight : 0
    const prevTop = el ? el.scrollTop : 0
    messages.value = mergeMessagesById(messages.value, data.items || [])
    hasMore.value = !!data.has_more
    cursor = data.next_cursor || ''
    await nextTick()
    // 保持当前可见位置不跳动
    if (el) el.scrollTop = el.scrollHeight - prevHeight + prevTop
  } catch (error) {
    console.error('加载更早的私信失败:', error)
    toast.error(t('message.loadFailed'))
  } finally {
    if (gen === generation) loadingOlder.value = false
  }
  if (gen === generation) fillViewport()
}

// 重新拉第一页合并（截断消息取全文 / 轮询兜底），然后标记已读
const syncLatest = async () => {
  const gen = generation
  try {
    if (!conversationId.value) {
      // 还没有会话：对方可能刚发来第一条
      const res = await getConversationWith(peerId.value)
      if (gen !== generation) return
      applyConversationState(res.data)
      const conversation = res.data?.conversation
      if (!conversation?.last_message) return
      conversationId.value = conversation.conversation_id
      if (conversation.peer) peer.value = conversation.peer
    }
    const stick = isNearBottom()
    const page = await fetchLatest(gen)
    if (!page) return
    messages.value = mergeMessagesById(messages.value, page.items || [])
    if (!cursor && page.has_more) {
      hasMore.value = true
      cursor = page.next_cursor || ''
    }
    if (stick) scrollToBottom()
    await markRead()
  } catch (error) {
    console.error('同步私信失败:', error)
  }
}

// 对方回复后未关注限制解除 / 对方改了设置：重新查一次能否发送
const recheckCanSend = async () => {
  const gen = generation
  try {
    const res = await getConversationWith(peerId.value)
    if (gen !== generation) return
    applyConversationState(res.data)
    if (res.data?.can_send) remainingCount.value = null
  } catch (error) {
    console.error('查询私信状态失败:', error)
  }
}

// ---- 实时消息 ----
const handleDmMessage = (msg) => {
  lastDmEventAt = Date.now()
  const inThisChat = conversationId.value
    ? msg.conversation_id === conversationId.value
    : msg.sender_id === peerId.value
  if (!inThisChat) {
    const name = msg.sender?.username || t('message.someone')
    toast.info(t('message.newMessageToast', { name, content: msg.content }))
    return
  }
  if (!conversationId.value) conversationId.value = msg.conversation_id
  if (msg.truncated) {
    syncLatest()
  } else {
    const stick = isNearBottom()
    messages.value = mergeMessagesById(messages.value, [toMessageVO(msg)])
    if (stick) scrollToBottom()
    markRead()
  }
  if (!canSend.value || hasDmRemainingLimit(remainingCount.value)) recheckCanSend()
}

const { dmUnreadCount } = useUnreadNotice({ onDmMessage: handleDmMessage })

// SSE 断开时只有轮询更新的未读数：未读增加且最近没收到 dm-message，就主动拉一次
watch(dmUnreadCount, (count, prev) => {
  if (count > prev && !initLoading.value && Date.now() - lastDmEventAt > 3000) syncLatest()
})

// ---- 发送 ----
let localSeq = 0

const sleep = (ms) => new Promise(resolve => setTimeout(resolve, ms))

const doSend = async (item, gen) => {
  item.status = 'sending'
  try {
    const res = await sendMessage({ receiver_id: peerId.value, content: item.content, client_msg_id: item.clientMsgId })
    if (gen !== generation) return
    const data = res.data || {}
    pending.value = pending.value.filter(p => p !== item)
    messages.value = mergeMessagesById(messages.value, [sentToMessageVO(data)])
    if (!conversationId.value && data.conversation_id) conversationId.value = data.conversation_id
    if (hasDmRemainingLimit(data.remaining_count)) {
      remainingCount.value = data.remaining_count
      // 0 = 再发会被拒：直接置灰，不等第 6 条失败
      if (data.remaining_count === 0) {
        canSend.value = false
        cannotReason.value = DM_REASON.DM_LIMIT_REACHED
      }
    } else {
      remainingCount.value = null
    }
    scrollToBottom()
  } catch (error) {
    if (gen !== generation || !alive) return
    const reason = dmSendErrorReason(error)
    // 超时 / 网络错误 / 5xx：用同一个 client_msg_id 自动重试，服务端保证只发一条
    if (reason === DM_REASON.NETWORK && item.autoRetries < SEND_RETRY_DELAYS.length) {
      await sleep(SEND_RETRY_DELAYS[item.autoRetries++])
      if (gen !== generation || !alive || !pending.value.includes(item)) return
      return doSend(item, gen)
    }
    item.status = 'failed'
    item.errorText = t(dmReasonKey(reason), { max: DM_CONTENT_MAX })
    if (isDmBlockedReason(reason)) {
      canSend.value = false
      cannotReason.value = reason
    } else if (reason === DM_REASON.RATE_LIMITED) {
      // 429 不自动重试，由用户稍后手动点「重试」
      toast.warning(item.errorText)
    }
  }
}

const handleSend = () => {
  if (!canSend.value || initLoading.value) return
  const invalid = validateDmContent(draft.value)
  if (invalid) {
    if (invalid === DM_REASON.CONTENT_TOO_LONG) toast.warning(t(dmReasonKey(invalid), { max: DM_CONTENT_MAX }))
    return
  }
  // 每条新消息一个 client_msg_id，重试时复用
  const item = reactive({
    localId: `local-${++localSeq}`,
    clientMsgId: createClientMsgId(),
    content: draft.value,
    status: 'sending',
    errorText: '',
    autoRetries: 0
  })
  pending.value.push(item)
  draft.value = ''
  scrollToBottom()
  doSend(item, generation)
}

const retrySend = (item) => {
  item.autoRetries = 0
  item.errorText = ''
  doSend(item, generation)
}

const discardPending = (item) => {
  pending.value = pending.value.filter(p => p !== item)
}

// 桌面 Enter 发送、Shift + Enter 换行；输入法组字中不发送。移动端 Enter 换行，用按钮发送
const handleKeydown = (e) => {
  if (e.key !== 'Enter' || e.shiftKey || e.isComposing || e.keyCode === 229) return
  if (mobileShell.value) return
  e.preventDefault()
  handleSend()
}

watch(peerId, (id, old) => {
  if (id && id !== old && route.name === 'message-chat') init()
}, { immediate: true })

onBeforeUnmount(() => {
  alive = false
  generation++
})
</script>

<style scoped>
.chat-page {
  min-height: 100vh;
}

.chat-wrapper {
  padding-top: var(--header-height);
  height: 100vh;
  height: 100dvh;
  box-sizing: border-box;
  display: flex;
  justify-content: center;
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.chat-panel {
  width: 100%;
  max-width: 760px;
  margin: 16px 24px 24px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  box-shadow: var(--shadow-md);
  overflow: hidden;
}

.chat-peer {
  flex-shrink: 0;
  min-height: 56px;
  display: flex;
  align-items: center;
  padding: 10px 16px;
  border-bottom: 1px solid var(--glass-border);
}

.peer-link {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  color: var(--text-primary);
  text-decoration: none;
}

.peer-link.deleted {
  color: var(--text-tertiary);
}

.peer-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
  font-weight: 600;
}

.chat-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 8px 16px 16px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.chat-state,
.chat-older {
  display: flex;
  justify-content: center;
  padding: 8px 0;
  min-height: 20px;
}

.older-btn,
.chat-retry,
.pending-action {
  border: none;
  background: none;
  color: var(--text-tertiary);
  font-size: 12px;
  cursor: pointer;
}

.older-btn:hover,
.pending-action:hover {
  color: #8af0d0;
}

.chat-retry {
  margin: 32px auto;
  font-size: 13px;
}

.chat-empty {
  margin: auto;
  padding: 32px 0;
  color: var(--text-tertiary);
  font-size: 13px;
}

.chat-time {
  align-self: center;
  margin: 8px 0 2px;
  font-size: 11px;
  color: var(--text-tertiary);
}

.chat-divider {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 8px 0;
  font-size: 12px;
  color: #8af0d0;
}

.chat-divider::before,
.chat-divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: rgba(102, 234, 194, 0.35);
}

.chat-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.chat-row.mine {
  justify-content: flex-end;
}

.chat-bubble {
  max-width: min(72%, 520px);
  padding: 8px 12px;
  border-radius: 14px;
  font-size: 14px;
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
  overflow-wrap: anywhere;
}

.chat-row.theirs .chat-bubble {
  background: rgba(255, 255, 255, 0.07);
  color: var(--text-primary);
  border-bottom-left-radius: 4px;
}

.chat-row.mine .chat-bubble {
  background: rgba(102, 234, 194, 0.18);
  border: 1px solid rgba(102, 234, 194, 0.25);
  color: var(--text-primary);
  border-bottom-right-radius: 4px;
}

.chat-bubble.pending {
  opacity: 0.6;
}

.chat-bubble.pending.failed {
  opacity: 1;
  border-color: rgba(255, 107, 107, 0.5);
}

.pending-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  justify-content: flex-end;
  max-width: 40%;
}

.pending-error {
  font-size: 12px;
  color: #ff6b6b;
}

.chat-composer {
  flex-shrink: 0;
  padding: 10px 16px 12px;
  border-top: 1px solid var(--glass-border);
}

.composer-hint {
  margin-bottom: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}

.composer-hint.blocked {
  color: #ffb86b;
}

.composer-row {
  display: flex;
  align-items: flex-end;
  gap: 10px;
}

.composer-input {
  flex: 1;
  --n-border-radius: 12px !important;
}

.send-btn {
  flex-shrink: 0;
  border-radius: 12px;
}

.composer-counter {
  margin-top: 4px;
  text-align: right;
  font-size: 11px;
  color: var(--text-tertiary);
}

.composer-counter.over {
  color: #ff6b6b;
}

@media (max-width: 768px) {
  .chat-panel {
    margin: 0;
    border-radius: 0;
    border-left: none;
    border-right: none;
    border-bottom: none;
  }

  .chat-peer {
    min-height: 48px;
    padding: 6px 12px;
  }

  .chat-list {
    padding: 8px 10px 12px;
  }

  .chat-bubble {
    max-width: 80%;
  }

  .chat-composer {
    padding: 8px 10px calc(8px + var(--safe-bottom));
  }
}
</style>
