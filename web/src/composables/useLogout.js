import { useRouter } from 'vue-router'
import { useMessage, useDialog } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import request from '@/utils/request'
import { auth } from '@/utils/auth'
import { stopUnreadNotice } from '@/composables/useUnreadNotice'

/**
 * 退出登录（二次确认）。桌面顶栏头像菜单与移动端「我的」页共用。
 * 需在 NMessageProvider / NDialogProvider 内的组件 setup 中调用。
 */
export function useLogout() {
  const router = useRouter()
  const message = useMessage()
  const dialog = useDialog()
  const { t } = useI18n()

  const confirmLogout = () => {
    dialog.warning({
      title: t('common.logout'),
      content: t('common.logoutConfirm'),
      positiveText: t('common.confirm'),
      negativeText: t('common.cancel'),
      onPositiveClick: async () => {
        // 先断开未读数推送，避免登出后服务端推 auth-expired 再触发一次跳转提示
        stopUnreadNotice()
        try {
          // 调用后端登出接口
          await request.post('/auth/logout')

          // 清除本地 token
          auth.clearToken()
          message.success(t('common.logoutSuccess'))
          router.push('/')
        } catch (error) {
          // 即使接口调用失败，也清除本地 token
          auth.clearToken()
          message.warning(t('common.logout'))
          router.push('/')
        }
      }
    })
  }

  return { confirmLogout }
}
