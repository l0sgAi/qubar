<template>
  <!-- iOS Safari 只在用户手势的同步调用里 focus() 才弹键盘，而抽屉内容要等路由更新后才渲染：
       点击时先同步聚焦这个常驻的代理输入框把键盘弹出来，抽屉渲染后再把焦点移到真正的输入框（键盘保持弹出） -->
  <Teleport to="body">
    <input ref="proxyRef" class="composer-focus-proxy" type="text" tabindex="-1" aria-hidden="true">
  </Teleport>

  <!-- 输入抽屉：高度随内容；开合跟随路由 query.compose，返回手势先关它。
       回复抽屉开着时后打开的抽屉 z-index 更高，叠在其上 -->
  <NDrawer
    :show="isOpen"
    placement="bottom"
    height="auto"
    class="comment-composer"
    :style="{ bottom: `${inset}px` }"
    :auto-focus="false"
    :trap-focus="false"
    @update:show="(show) => !show && close()"
    @after-leave="handleAfterLeave"
  >
    <div v-if="displayTarget" class="composer" :class="{ 'keyboard-open': inset > 0 }">
      <div class="composer-header">
        <span class="composer-title">{{ title }}</span>
        <button type="button" class="composer-close" :aria-label="t('common.cancel')" @click="close">
          <NIcon size="20"><CloseIcon /></NIcon>
        </button>
      </div>
      <PlainCommentInput
        ref="inputRef"
        v-model:value="drafts[displayKey]"
        :post-id="postId"
        :root-id="displayTarget.rootId"
        :reply-to-id="displayTarget.replyToId"
        :reply-to-name="displayTarget.replyToName"
        @submit="handleSubmit"
      />
    </div>
  </NDrawer>
</template>

<script>
// 路由 query key：CommentList 挂载时清理残留 query 也要用到
export const COMPOSE_QUERY_KEY = 'compose'
</script>

<script setup>
import { ref, reactive, computed, watch, nextTick } from 'vue'
import { NDrawer, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { X as CloseIcon } from '@vicons/tabler'
import PlainCommentInput from '@/components/post/detail/PlainCommentInput.vue'
import { useKeyboardInset } from '@/composables/useKeyboardInset'
import { auth } from '@/utils/auth'
import { requireLogin } from '@/utils/guest-action'

defineProps({
  postId: {
    type: String,
    required: true
  }
})

// submit(created, target)：target 为 { rootId, replyToId, replyToName }，父组件据此插入列表
// closed：离场动画结束（页面滚动锁已解除）
const emit = defineEmits(['submit', 'closed'])

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

// 回复目标的 key：顶层评论 / 根评论 id / 被回复的回复 id（评论 id 全局唯一）。也用作 query 值和草稿 key
const targetKey = (target) => (target.rootId ? (target.replyToId || target.rootId) : 'post')

// 草稿按目标保留在内存里：关闭再打开同一目标时恢复，发送成功后清空。随评论区卸载（离开帖子）丢弃
const drafts = reactive({})

// 本实例打开的目标 key：只认自己打开的，挂载时残留的 query 不会弹出抽屉（由 CommentList 清理）
const openKey = ref('')
const routeActive = () => !!openKey.value && route.query[COMPOSE_QUERY_KEY] === openKey.value
const isOpen = computed(() => routeActive())

// 抽屉渲染用的目标：关闭后保留到离场动画结束，避免空白面板滑出
const displayTarget = ref(null)
const displayKey = computed(() => (displayTarget.value ? targetKey(displayTarget.value) : ''))

const title = computed(() => {
  const target = displayTarget.value
  if (!target?.rootId) return t('comment.editor.title')
  return t('mobile.comment.replyTitle', { name: target.replyToName })
})

const proxyRef = ref(null)
const inputRef = ref(null)

// 键盘弹出时抽屉底边贴住键盘顶部
const { inset } = useKeyboardInset(isOpen)

// 关闭在飞（back 的 popstate 尚未到达）：防止重复关闭多退一步
let closing = false

const queryWithoutCompose = () => {
  const { [COMPOSE_QUERY_KEY]: _omit, ...query } = route.query
  return query
}

// 与回复抽屉相同：上一条历史正是「同页无输入抽屉」时后退，否则原地 replace
const leaveComposeRoute = () => {
  const back = router.options.history.state?.back
  if (back && back === router.resolve({ query: queryWithoutCompose() }).fullPath) {
    router.back()
  } else {
    router.replace({ query: queryWithoutCompose() })
  }
}

const dismissKeyboard = () => {
  inputRef.value?.blur()
  if (document.activeElement === proxyRef.value) proxyRef.value.blur()
}

/**
 * 打开输入抽屉。必须在点击处理里同步调用（代理输入框的 focus 依赖用户手势）。
 * @param {{ rootId: string|null, replyToId: string|null, replyToName: string }} target
 */
const open = (target) => {
  // 访客：弹登录引导，不打开抽屉
  if (!auth.isAuthenticated()) {
    requireLogin('comment')
    return
  }
  const key = targetKey(target)
  if (routeActive() && openKey.value === key) {
    inputRef.value?.focus()
    return
  }
  proxyRef.value?.focus({ preventScroll: true })
  const switching = routeActive()
  openKey.value = key
  displayTarget.value = { rootId: target.rootId ?? null, replyToId: target.replyToId ?? null, replyToName: target.replyToName || '' }
  const query = { ...route.query, [COMPOSE_QUERY_KEY]: key }
  if (switching) {
    router.replace({ query })
  } else {
    router.push({ query })
  }
}

const close = () => {
  if (!routeActive() || closing) return
  closing = true
  dismissKeyboard()
  leaveComposeRoute()
}

// 抽屉内容渲染后把焦点从代理输入框移到真正的输入框
watch(isOpen, (show) => {
  if (show) nextTick(() => inputRef.value?.focus())
}, { flush: 'post' })

// 抽屉关闭（返回手势 / 后退 / 遮罩 / 发送成功，query 被移除）：复位并收起键盘
watch(() => route.query[COMPOSE_QUERY_KEY], (key) => {
  if (key || !openKey.value) return
  openKey.value = ''
  closing = false
  dismissKeyboard()
})

const handleAfterLeave = () => {
  if (isOpen.value) return
  displayTarget.value = null
  emit('closed')
}

const handleSubmit = (created) => {
  const target = displayTarget.value
  close()
  emit('submit', created, target)
}

defineExpose({ open, close, isOpen })
</script>

<style scoped>
/* 代理输入框：可聚焦（不能 display: none / visibility: hidden），但看不见、点不到 */
.composer-focus-proxy {
  position: fixed;
  left: 0;
  bottom: 0;
  width: 1px;
  height: 1px;
  padding: 0;
  border: 0;
  opacity: 0;
  pointer-events: none;
  /* ≥ 16px：iOS 聚焦时不缩放页面 */
  font-size: 16px;
}

/* 抽屉容器样式在 main.css（.comment-composer），以下是本模板内元素 */
.composer {
  padding: 12px 16px calc(var(--safe-bottom) + 12px);
}

/* 键盘弹出时底部是键盘而不是 Home Indicator，不再让出安全区 */
.composer.keyboard-open {
  padding-bottom: 12px;
}

.composer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.composer-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}

.composer-close {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  margin-right: -6px;
  padding: 0;
  background: transparent;
  border: none;
  border-radius: 50%;
  color: var(--text-tertiary);
  cursor: pointer;
}

.composer-close:active {
  background: rgba(255, 255, 255, 0.08);
}
</style>
