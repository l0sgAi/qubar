<script setup>
// 不渲染任何内容：在私信列表 / 聊天页以外的页面，收到 dm-message 时弹一条提示（点击进入聊天）。
// 由 AppShell 挂载（每页一个实例）；私信列表与聊天页自己处理 dm-message，这里跳过。
// 只在登录时注册，复用 useUnreadNotice 单例连接。
import { h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { auth } from '@/utils/auth'
import { useUnreadNotice } from '@/composables/useUnreadNotice'

const DM_ROUTES = ['messages', 'message-chat']

const route = useRoute()
const router = useRouter()
const toast = useMessage()
const { t } = useI18n()

if (auth.isAuthenticated()) {
  useUnreadNotice({
    onDmMessage: (msg) => {
      if (DM_ROUTES.includes(route.name)) return
      const name = msg.sender?.username || t('message.someone')
      const text = t('message.newMessageToast', { name, content: msg.content })
      const target = `/messages/${msg.sender_id}`
      const handle = toast.info(() => h('span', {
        role: 'link',
        style: { cursor: 'pointer' },
        onClick: () => {
          handle?.destroy()
          router.push(target)
        }
      }, text), { duration: 5000, closable: true })
    }
  })
}
</script>

<template>
  <span v-if="false" />
</template>
