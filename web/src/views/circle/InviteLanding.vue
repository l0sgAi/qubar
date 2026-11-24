<template>
  <div class="invite-landing-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
    <AppShell v-model:offset="offset" />

    <div class="main-content" :style="contentStyle">
      <div class="invite-card">
        <!-- 加载中 -->
        <div v-if="state === 'loading'" class="card-skeleton">
          <div class="sk-avatar"></div>
          <div class="sk-line sk-line--title"></div>
          <div class="sk-line sk-line--sub"></div>
          <div class="sk-btn"></div>
        </div>

        <!-- 邀请已失效：预览 / 加入返回 404，不区分原因 -->
        <div v-else-if="state === 'invalid'" class="card-state">
          <div class="state-icon">
            <NIcon size="36"><LinkOffIcon /></NIcon>
          </div>
          <h2 class="state-title">{{ t('circle.invite.landing.invalidTitle') }}</h2>
          <p class="state-desc">{{ t('circle.invite.landing.invalidDesc') }}</p>
          <NButton round size="large" class="state-btn" @click="router.push('/discover')">
            {{ t('circle.invite.landing.goDiscover') }}
          </NButton>
        </div>

        <!-- 网络等其他错误：可重试 -->
        <div v-else-if="state === 'error'" class="card-state">
          <p class="state-desc">{{ t('circle.invite.landing.loadFailed') }}</p>
          <NButton round size="large" class="state-btn" @click="loadPreview">
            {{ t('circle.invite.landing.retry') }}
          </NButton>
        </div>

        <!-- 预览：圈名 / 头像 / 成员数（后端只返回这三个字段） -->
        <div v-else class="card-preview">
          <p class="invite-eyebrow">{{ t('circle.invite.landing.title') }}</p>
          <NAvatar round :size="88" :src="preview.avatar_url || undefined" class="invite-avatar">
            <span v-if="!preview.avatar_url" class="avatar-fallback">
              {{ (preview.name || '?').charAt(0).toUpperCase() }}
            </span>
          </NAvatar>
          <h1 class="invite-name">{{ preview.name }}</h1>
          <div class="invite-meta">
            <span class="meta-tag">
              <NIcon size="14"><LockIcon /></NIcon>
              {{ t('circle.invite.landing.privateTag') }}
            </span>
            <span class="meta-count">
              <NIcon size="14"><UsersIcon /></NIcon>
              {{ t('circle.invite.landing.memberCount', { n: preview.member_count || 0 }) }}
            </span>
          </div>
          <p class="invite-desc">{{ t('circle.invite.landing.desc') }}</p>

          <NButton
            type="primary"
            size="large"
            round
            block
            class="join-btn"
            :loading="accepting"
            @click="handleJoin"
          >
            {{ loggedIn ? t('circle.invite.landing.join') : t('circle.invite.landing.loginToJoin') }}
          </NButton>
          <p v-if="!loggedIn" class="login-hint">{{ t('circle.invite.landing.loginHint') }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NAvatar, NButton, NIcon, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Lock as LockIcon, Users as UsersIcon, Unlink as LinkOffIcon } from '@vicons/tabler'
import AppShell from '@/components/layout/AppShell.vue'
import { getInvitePreview, acceptInvite } from '@/api/circleInvite'
import { useAppShell } from '@/composables/useAppShell'
import { usePageTitle } from '@/composables/usePageTitle'
import { MEMBERSHIP_CHANGED_EVENT } from '@/composables/useSideNavCircles'
import { auth } from '@/utils/auth'
import { loginLocation } from '@/utils/mobileNav'
import { acceptInviteErrorKey, isInviteInvalidError } from '@/constants/circleInvite'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const { t } = useI18n()
const { setTitleData } = usePageTitle()
const { offset, contentStyle } = useAppShell()

const token = computed(() => String(route.params.token || ''))
const loggedIn = computed(() => auth.isAuthenticated())

// loading | ready | invalid | error
const state = ref('loading')
const preview = ref({})
const accepting = ref(false)
let gen = 0

const loadPreview = async () => {
  const myGen = ++gen
  state.value = 'loading'
  try {
    const res = await getInvitePreview(token.value)
    if (myGen !== gen) return
    preview.value = res.data || {}
    state.value = 'ready'
    if (preview.value.name) setTitleData('title.inviteName', { name: preview.value.name })
  } catch (error) {
    if (myGen !== gen) return
    state.value = isInviteInvalidError(error) ? 'invalid' : 'error'
  }
}

const handleJoin = async () => {
  // 未登录：跳登录页并带 redirect，登录成功后回到本页（LoginCard / OAuth 回调都会消费 redirect）
  if (!loggedIn.value) {
    router.push(loginLocation(route.fullPath))
    return
  }
  accepting.value = true
  try {
    const res = await acceptInvite({ token: token.value })
    const { circle_id: circleId, already_member: alreadyMember } = res.data || {}
    if (alreadyMember) {
      message.info(t('circle.invite.landing.alreadyMember'))
    } else {
      message.success(t('circle.invite.landing.joined', { name: preview.value.name || '' }))
      // 通知侧栏「我加入的圈子」刷新
      window.dispatchEvent(new CustomEvent(MEMBERSHIP_CHANGED_EVENT))
    }
    router.replace(`/circle/${circleId}`)
  } catch (error) {
    if (isInviteInvalidError(error)) {
      state.value = 'invalid'
      return
    }
    message.error(t(acceptInviteErrorKey(error)))
  } finally {
    accepting.value = false
  }
}

onMounted(loadPreview)

watch(token, (val, old) => {
  if (val && val !== old) loadPreview()
})
</script>

<style scoped>
.invite-landing-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 72px 24px 24px;
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.invite-card {
  width: 100%;
  max-width: 440px;
  padding: 36px 32px 32px;
  background: var(--glass-bg);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid var(--glass-border);
  border-radius: 24px;
  box-shadow: var(--shadow-md);
  text-align: center;
  animation: card-in 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
}

@keyframes card-in {
  from { opacity: 0; transform: scale(0.96) translateY(8px); }
  to { opacity: 1; transform: none; }
}

.card-preview,
.card-state,
.card-skeleton {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.invite-eyebrow {
  margin: 0 0 20px;
  font-size: 0.85rem;
  letter-spacing: 0.5px;
  color: var(--text-secondary);
}

.invite-avatar {
  border: 2px solid rgba(102, 234, 194, 0.35);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
}

.avatar-fallback {
  font-size: 2rem;
  font-weight: 700;
}

.invite-name {
  margin: 16px 0 10px;
  font-size: 1.5rem;
  font-weight: 700;
  line-height: 1.3;
  color: var(--text-primary, #fff);
  word-break: break-word;
}

.invite-meta {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 8px;
}

.meta-tag,
.meta-count {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 10px;
  border-radius: 8px;
  font-size: 0.8rem;
  color: var(--text-secondary);
  background: rgba(255, 255, 255, 0.06);
}

.meta-tag {
  color: #8af0d0;
  background: rgba(102, 234, 194, 0.12);
}

.invite-desc {
  margin: 18px 0 24px;
  font-size: 0.9rem;
  line-height: 1.6;
  color: var(--text-tertiary);
}

.join-btn {
  font-weight: 600;
}

.login-hint {
  margin: 10px 0 0;
  font-size: 0.78rem;
  color: var(--text-tertiary);
}

.state-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 72px;
  height: 72px;
  margin-bottom: 16px;
  border-radius: 50%;
  color: var(--text-secondary);
  background: rgba(255, 255, 255, 0.06);
}

.state-title {
  margin: 0 0 8px;
  font-size: 1.3rem;
  font-weight: 700;
  color: var(--text-primary, #fff);
}

.state-desc {
  margin: 0 0 24px;
  font-size: 0.9rem;
  line-height: 1.6;
  color: var(--text-secondary);
}

.sk-avatar,
.sk-line,
.sk-btn {
  background: linear-gradient(90deg, rgba(255, 255, 255, 0.04) 25%, rgba(255, 255, 255, 0.08) 37%, rgba(255, 255, 255, 0.04) 63%);
  background-size: 400% 100%;
  animation: sk-shimmer 1.4s ease infinite;
}

.sk-avatar {
  width: 88px;
  height: 88px;
  margin-top: 38px;
  border-radius: 50%;
}

.sk-line {
  height: 14px;
  border-radius: 7px;
  margin-top: 16px;
}

.sk-line--title {
  width: 60%;
  height: 22px;
}

.sk-line--sub {
  width: 40%;
}

.sk-btn {
  width: 100%;
  height: 40px;
  margin-top: 48px;
  border-radius: 20px;
}

@keyframes sk-shimmer {
  0% { background-position: 100% 50%; }
  100% { background-position: 0 50%; }
}

@media (max-width: 768px) {
  .main-content {
    padding: 32px 16px 16px;
  }

  .invite-card {
    padding: 28px 20px 24px;
    border-radius: 20px;
  }
}
</style>
