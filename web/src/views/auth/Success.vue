<template>
  <div class="success-page">
    <!-- 动态背景 -->
    <AnimatedBackground />

    <NCard class="success-container" :bordered="false">
      <div class="success-view">
        <h2 class="welcome-title">{{ t('authCallback.welcome') }}</h2>
        <NButton
          type="primary"
          size="large"
          class="enter-btn"
          @click="handleEnterCommunity"
          :loading="loading"
        >
          {{ loading ? t('authCallback.processing') : t('authCallback.enterCommunity') }}
        </NButton>
      </div>
    </NCard>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { NCard, NButton, NScrollbar, useMessage } from 'naive-ui'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AnimatedBackground from '@/components/auth/AnimatedBackground.vue'
import { auth, OAUTH_RETURN_PATH_KEY } from '@/utils/auth'
import { API_BASE } from '@/config'

const router = useRouter()
const message = useMessage()
const { t } = useI18n()

const token = ref('')
const userEmail = ref('')
const loading = ref(false)

const userInitial = computed(() => {
  return userEmail.value ? userEmail.value.charAt(0).toUpperCase() : '✓'
})

onMounted(async () => {
  const urlParams = new URLSearchParams(window.location.search)

  // 安全要求：不接受 URL 直接携带的 token（会泄漏到浏览器历史/日志），
  // 仅支持后端下发的一次性授权 code，页面加载后由前端换取 token
  const code = urlParams.get('code')
  if (code) {
    // 后端跳转 URL 携带 provider/device（契约见 OAUTH_CALLBACK.md）：
    // code 必须 POST 回签发它的 provider 端点，投错端点会被判 invalid_grant
    const providerParam = urlParams.get('provider')
    const provider = ['google', 'github', 'azure'].includes(providerParam) ? providerParam : 'google'
    const device = urlParams.get('device') || 'web'
    // device_id 以本地为准；本地没有（跳转期间清了站点数据）才用回跳 URL 带回的，都没有就新生成
    const deviceId = auth.getDeviceId(urlParams.get('device_id'))
    // OAuth 回调，需要发送给后端
    loading.value = true
    try {
      message.loading(t('authCallback.signingIn'), { duration: 0 })

      const response = await fetch(`${API_BASE}/auth/${provider}/callback`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ code, device, device_id: deviceId })
      })

      const data = await response.json()

      if (data.code === 200 && data.data && data.data.token) {
        auth.saveDeviceId(data.data.device_id)
        handleLoginSuccess(data.data.token, data.data.expire, data.data.email || 'user@example.com')
      } else {
        throw new Error(data.message || t('common.unknownError'))
      }
    } catch (error) {
      loading.value = false
      message.destroyAll()
      message.error(t('authCallback.loginFailed') + ': ' + error.message)
      // console.error('OAuth 登录错误:', error)
      setTimeout(() => {
        router.push('/')
      }, 2000)
    }
    return
  }

  // 没有 code，可能是直接访问
  message.warning(t('authCallback.noLoginInfo'))
  setTimeout(() => {
    router.push('/')
  }, 2000)
})

// 消费 OAuth 跳转前暂存的站内回跳地址（一次性，读后即删），无则回主页
const consumeReturnPath = () => {
  const path = sessionStorage.getItem(OAUTH_RETURN_PATH_KEY)
  sessionStorage.removeItem(OAUTH_RETURN_PATH_KEY)
  return (typeof path === 'string' && path.startsWith('/') && path !== '/') ? path : '/home'
}

const handleLoginSuccess = (tokenValue, expire, email) => {
  token.value = tokenValue
  userEmail.value = email

  // 保存 token
  auth.setToken(tokenValue, expire)

  // 清除 URL 中的参数
  window.history.replaceState({}, document.title, window.location.pathname)

  loading.value = false
  message.destroyAll()
  message.success(t('login.messages.loginSuccess'), { duration: 2000 })
  // console.log('登录成功，Token已保存:', {
  //   token: tokenValue.substring(0, 20) + '...',
  //   expire: expire + '秒',
  //   email: email
  // })

  // 自动跳转到回跳地址或主页
  setTimeout(() => {
    router.push(consumeReturnPath())
  }, 1500)
}

const handleEnterCommunity = () => {
  // 跳转到回跳地址或系统主页
  router.push(consumeReturnPath())
}
</script>

<style scoped>
.success-page {
  min-height: 100vh;
  display: flex;
  justify-content: center;
  align-items: center;
  position: relative;
}

.success-container {
  position: relative;
  z-index: 1;
  background: var(--glass-bg);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid var(--glass-border);
  border-radius: 24px;
  box-shadow: var(--shadow-md);
  width: 100%;
  max-width: 480px;
  padding: 40px 32px;
  text-align: center;
}

.success-view {
  display: flex;
  flex-direction: column;
  align-items: center;
  animation: fadeIn 0.5s ease;
}

.avatar-placeholder {
  width: 90px;
  height: 90px;
  background: var(--primary-gradient);
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 36px;
  font-weight: bold;
  margin-bottom: 24px;
  box-shadow: 0 8px 24px rgba(102, 126, 234, 0.4);
  border: 3px solid var(--glass-border);
}

.welcome-title {
  color: var(--text-primary);
  margin-bottom: 12px;
  font-size: 1.75rem;
  font-weight: 700;
  letter-spacing: -0.5px;
}

.user-email {
  color: var(--text-secondary);
  margin-bottom: 24px;
  font-size: 0.95rem;
}

.token-section {
  width: 100%;
  text-align: left;
  margin-bottom: 28px;
}

.token-label {
  font-size: 0.85rem;
  color: var(--text-secondary);
  font-weight: 600;
  margin-bottom: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.token-box {
  background: var(--bg-secondary);
  border: 1px solid var(--glass-border);
  padding: 16px;
  border-radius: 12px;
  word-break: break-all;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 0.75rem;
  color: var(--text-secondary);
  text-align: left;
  line-height: 1.6;
  transition: all 0.3s ease;
}

.token-box:hover {
  border-color: var(--accent-color);
  box-shadow: 0 2px 12px rgba(102, 126, 234, 0.2);
}

.enter-btn {
  width: 100%;
  height: 52px;
  border-radius: 16px;
  font-size: 16px;
  font-weight: 600;
  background: var(--primary-gradient);
  border: none;
  color: white;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: var(--shadow-sm);
}

.enter-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 24px rgba(102, 126, 234, 0.5);
}

.enter-btn:active {
  transform: translateY(0);
}

/* NaiveUI 组件样式覆盖 */
:deep(.n-card) {
  background: transparent !important;
}

:deep(.n-scrollbar) {
  background: var(--bg-secondary) !important;
  border-radius: 12px;
  border: 1px solid var(--glass-border);
}

:deep(.n-scrollbar-container) {
  padding: 4px;
}

:deep(.n-scrollbar-rail) {
  background: var(--bg-tertiary) !important;
}

:deep(.n-scrollbar-handle) {
  background: var(--accent-color) !important;
}
</style>
