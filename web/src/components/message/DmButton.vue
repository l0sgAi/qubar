<!-- 用户主页「私信」按钮：机器人（role=2）和自己不显示；访客点击弹登录引导 -->
<template>
  <NButton
    v-if="visible"
    secondary
    round
    :size="size"
    class="dm-button"
    @click="handleClick"
  >
    <template #icon>
      <NIcon :size="16"><MessageCircle /></NIcon>
    </template>
    {{ t('message.entry') }}
  </NButton>
</template>

<script>
import { getUserInfo } from '@/api/auth'

// 当前登录用户 ID（模块级缓存，多个用户主页之间复用；登录态变化后页面整体重载，无需失效处理）
let myIdPromise = null
const loadMyId = () => {
  if (!myIdPromise) {
    myIdPromise = getUserInfo()
      .then(res => res.data?.id || null)
      .catch(() => {
        myIdPromise = null
        return null
      })
  }
  return myIdPromise
}

// 仅供单测：清空缓存
export const __resetDmButtonCache = () => {
  myIdPromise = null
}
</script>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NIcon } from 'naive-ui'
import { MessageCircle } from '@vicons/tabler'
import { useI18n } from 'vue-i18n'
import { auth } from '@/utils/auth'
import { requireLogin } from '@/utils/guest-action'

const props = defineProps({
  // 主页用户（getUserDetail 的 data）：至少含 id、role
  user: { type: Object, default: null },
  size: { type: String, default: 'small' }
})

const BOT_ROLE = 2

const { t } = useI18n()
const router = useRouter()

const loggedIn = auth.isAuthenticated()
// 登录时需要先知道自己的 ID 才能决定是否显示（避免在自己主页闪一下）
const myId = ref(null)
const myIdReady = ref(!loggedIn)

onMounted(async () => {
  if (!loggedIn) return
  myId.value = await loadMyId()
  myIdReady.value = true
})

const visible = computed(() => {
  const user = props.user
  if (!user?.id || user.role === BOT_ROLE) return false
  if (!myIdReady.value) return false
  return !myId.value || String(myId.value) !== String(user.id)
})

const handleClick = () => {
  if (!auth.isAuthenticated()) {
    requireLogin('message')
    return
  }
  router.push(`/messages/${props.user.id}`)
}
</script>
