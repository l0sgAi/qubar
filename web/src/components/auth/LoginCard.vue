<template>
  <NCard class="login-container" :bordered="false">
    <!-- 语言切换 -->
    <div class="language-switcher">
      <LanguageSwitcher />
    </div>

    <div class="logo-area">
      <div class="logo-brand">
        <img src="/favicon.svg" alt="logo" class="logo-icon" />
        <span class="logo-text">{{ t('common.appName') }}</span>
      </div>
      <p class="tagline">{{ t('login.tagline') }}</p>
    </div>

    <!-- 登录/注册 Tab 切换 -->
    <div
      class="auth-tabs-viewport"
      :style="{ height: authTabsHeight }"
    >
      <div ref="authTabsContentRef" class="auth-tabs-content">
        <NTabs
          v-model:value="activeTab"
          class="auth-tabs"
          @update:value="handleTabChange"
          justify-content="space-evenly"
        >
      <!-- ========== 登录 Tab ========== -->
      <NTabPane :name="'login'" :tab="t('login.tabs.login')">
        <NForm
          ref="loginFormRef"
          :model="loginData"
          :rules="loginRules"
          class="auth-form"
        >
          <NFormItem path="email" :show-label="false">
            <NInput
              v-model:value="loginData.email"
              :placeholder="t('login.email.placeholder')"
              size="large"
              :input-props="{ autocomplete: 'email' }"
            >
              <template #prefix>
                <NIcon :component="EmailIcon" />
              </template>
            </NInput>
          </NFormItem>

          <NFormItem path="password" :show-label="false">
            <NInput
              v-model:value="loginData.password"
              type="password"
              show-password-on="click"
              :placeholder="t('login.password.placeholder')"
              size="large"
              :input-props="{ autocomplete: 'current-password' }"
              @keyup.enter="handleLogin"
            >
              <template #prefix>
                <NIcon :component="Locked" />
              </template>
            </NInput>
          </NFormItem>

          <div class="forgot-row">
            <NButton text class="forgot-link" @click="forgotModalShow = true">{{ t('login.forgotPassword') }}</NButton>
          </div>

          <NButton
            type="primary"
            size="large"
            block
            class="gradient-btn"
            :loading="loginLoading"
            @click="handleLogin"
          >
            {{ t('login.submit') }}
          </NButton>
        </NForm>

      </NTabPane>

      <!-- ========== 注册 Tab ========== -->
      <NTabPane :name="'register'" :tab="t('login.tabs.register')">
        <!-- 步骤指示器 -->
        <div class="step-indicator">
          <div class="step-item" :class="{ active: registerStep >= 1, done: registerStep > 1 }">
            <div class="step-dot">
              <span v-if="registerStep > 1">&#10003;</span>
              <span v-else>1</span>
            </div>
            <span class="step-label">{{ t('login.register.sendCode') }}</span>
          </div>
          <div class="step-line" :class="{ active: registerStep > 1 }"></div>
          <div class="step-item" :class="{ active: registerStep >= 2, done: registerStep > 2 }">
            <div class="step-dot">
              <span v-if="registerStep > 2">&#10003;</span>
              <span v-else>2</span>
            </div>
            <span class="step-label">{{ t('login.register.verify') }}</span>
          </div>
          <div class="step-line" :class="{ active: registerStep > 2 }"></div>
          <div class="step-item" :class="{ active: registerStep >= 3 }">
            <div class="step-dot">3</div>
            <span class="step-label">{{ t('login.register.submit') }}</span>
          </div>
        </div>

        <!-- 步骤内容 -->
        <Transition name="fade-slide" mode="out-in">
          <!-- Step 1: 输入邮箱 -->
          <div v-if="registerStep === 1" key="step1" class="step-content">
            <NForm
              ref="step1FormRef"
              :model="registerData"
              :rules="step1Rules"
              class="auth-form"
            >
              <NFormItem path="email" :show-label="false">
                <NInput
                  v-model:value="registerData.email"
                  :placeholder="t('login.email.placeholder')"
                  size="large"
                  :input-props="{ autocomplete: 'email' }"
                >
                  <template #prefix>
                    <NIcon :component="EmailIcon" />
                  </template>
                </NInput>
              </NFormItem>

              <NButton
                type="primary"
                size="large"
                block
                class="gradient-btn"
                :loading="sendCodeLoading"
                :disabled="countdown > 0"
                @click="handleSendCode"
              >
                {{ countdown > 0 ? t('login.register.resendIn', { seconds: countdown }) : t('login.register.sendCode') }}
              </NButton>
            </NForm>
          </div>

          <!-- Step 2: 验证码 -->
          <div v-else-if="registerStep === 2" key="step2" class="step-content">
            <div class="code-hint">
              {{ t('login.register.codeSentTo', { email: registerData.email }) }}
            </div>

            <NForm
              ref="step2FormRef"
              :model="registerData"
              :rules="step2Rules"
              class="auth-form"
            >
              <NFormItem path="code" :show-label="false">
                <NInput
                  v-model:value="registerData.code"
                  :placeholder="t('login.register.codePlaceholder')"
                  maxlength="6"
                  class="code-input"
                  size="large"
                  @keyup.enter="handleVerifyCode"
                />
              </NFormItem>

              <NButton
                type="primary"
                size="large"
                block
                class="gradient-btn"
                :loading="verifyLoading"
                @click="handleVerifyCode"
              >
                {{ t('login.register.verify') }}
              </NButton>

              <div class="resend-row">
                <NButton
                  text
                  size="small"
                  :disabled="countdown > 0"
                  @click="handleSendCode"
                >
                  {{ countdown > 0 ? t('login.register.resendIn', { seconds: countdown }) : t('login.register.resendCode') }}
                </NButton>
              </div>
            </NForm>
          </div>

          <!-- Step 3: 设置账号 -->
          <div v-else-if="registerStep === 3" key="step3" class="step-content">
            <NForm
              ref="step3FormRef"
              :model="registerData"
              :rules="step3Rules"
              class="auth-form"
            >
              <NFormItem path="username" :show-label="false">
                <NInput
                  v-model:value="registerData.username"
                  :placeholder="t('login.register.usernamePlaceholder')"
                  size="large"
                  :input-props="{ autocomplete: 'username' }"
                >
                  <template #prefix>
                    <NIcon :component="UserIcon" />
                  </template>
                </NInput>
              </NFormItem>

              <NFormItem path="password" :show-label="false">
                <NInput
                  v-model:value="registerData.password"
                  type="password"
                  show-password-on="click"
                  :placeholder="t('login.password.placeholder')"
                  size="large"
                  :input-props="{ autocomplete: 'new-password' }"
                >
                  <template #prefix>
                    <NIcon :component="Locked" />
                  </template>
                </NInput>
              </NFormItem>

              <!-- 密码强度条 -->
              <div v-if="registerData.password" class="password-strength">
                <div class="strength-bar">
                  <div
                    class="strength-fill"
                    :class="passwordStrengthClass"
                    :style="{ width: passwordStrengthWidth }"
                  ></div>
                </div>
                <span class="strength-text" :class="passwordStrengthClass">
                  {{ passwordStrengthLabel }}
                </span>
              </div>

              <NFormItem path="confirmPassword" :show-label="false">
                <NInput
                  v-model:value="registerData.confirmPassword"
                  type="password"
                  show-password-on="click"
                  :placeholder="t('login.register.confirmPasswordPlaceholder')"
                  size="large"
                  :input-props="{ autocomplete: 'new-password' }"
                  @keyup.enter="handleRegister"
                >
                  <template #prefix>
                    <NIcon :component="Locked" />
                  </template>
                </NInput>
              </NFormItem>

              <NButton
                type="primary"
                size="large"
                block
                class="gradient-btn"
                :loading="registerLoading"
                @click="handleRegister"
              >
                {{ t('login.register.submit') }}
              </NButton>
            </NForm>
          </div>
        </Transition>
          </NTabPane>
        </NTabs>
      </div>
    </div>

    <!-- 访客入口：未登录先逛逛（跳发现页，访客可读） -->
    <div class="guest-entry">
      <button class="guest-entry-btn" @click="handleBrowseAsGuest">
        <NIcon :component="ExploreIcon" size="18" />
        <span>{{ t('login.browseAsGuest') }}</span>
      </button>
    </div>

    <NDivider class="auth-divider">{{ t('login.dividerText') }}</NDivider>

    <!-- OAuth 按钮 -->
    <div class="oauth-section">
      <NTooltip trigger="hover">
        <template #trigger>
          <button class="oauth-icon-btn" @click="handleGoogleLogin">
            <img
              src="https://www.svgrepo.com/show/475656/google-color.svg"
              alt="Google"
              class="oauth-icon"
            />
          </button>
        </template>
        {{ t('login.googleLogin') }}
      </NTooltip>

      <NTooltip trigger="hover">
        <template #trigger>
          <button class="oauth-icon-btn" @click="handleGithubLogin">
            <svg viewBox="0 0 24 24" class="oauth-icon github-icon" fill="currentColor">
              <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
            </svg>
          </button>
        </template>
        {{ t('login.githubLogin') }}
      </NTooltip>

      <NTooltip trigger="hover">
        <template #trigger>
          <button class="oauth-icon-btn" @click="handleAzureLogin">
            <svg viewBox="0 0 23 23" class="oauth-icon">
              <path fill="#f35325" d="M1 1h10v10H1z"/>
              <path fill="#81bc06" d="M12 1h10v10H12z"/>
              <path fill="#05a6f0" d="M1 12h10v10H1z"/>
              <path fill="#ffba08" d="M12 12h10v10H12z"/>
            </svg>
          </button>
        </template>
        {{ t('login.microsoftLogin') }}
      </NTooltip>
    </div>

    <p class="footer">
      {{ t('login.loginAgreement') }}
      <router-link to="/terms" class="footer-link">{{ t('login.userAgreement') }}</router-link>
      {{ t('login.and') }}
      <router-link to="/privacy" class="footer-link">{{ t('login.privacyPolicy') }}</router-link>
    </p>

    <!-- 找回密码弹窗 -->
    <ForgotPasswordModal v-model:show="forgotModalShow" @success="handleForgotSuccess" />
  </NCard>
</template>

<script setup>
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NCard, NButton, NTabs, NTabPane, NForm, NFormItem, NInput, NIcon, NDivider, NTooltip, useMessage } from 'naive-ui'
import { Email as EmailIcon, Locked, User as UserIcon } from '@vicons/carbon'
import { Compass as ExploreIcon } from '@vicons/tabler'
import { useI18n } from 'vue-i18n'
import LanguageSwitcher from '@/components/common/LanguageSwitcher.vue'
import ForgotPasswordModal from '@/components/auth/ForgotPasswordModal.vue'
import { loginWithEmail, loginWithOAuth, sendVerificationCode, verifyEmailCode, registerWithEmail } from '@/api/auth'
import { auth, OAUTH_RETURN_PATH_KEY } from '@/utils/auth'

const router = useRouter()
const route = useRoute()
const message = useMessage()
const { t, locale } = useI18n()

function getLangParam() {
  return locale.value.startsWith('zh') ? 'zh' : 'en'
}

// ---- 通用状态 ----
const activeTab = ref('login')
const authTabsContentRef = ref(null)
const authTabsHeight = ref(null)
let authTabsResizeObserver = null

function updateAuthTabsHeight() {
  const content = authTabsContentRef.value
  if (!content) return

  const nextHeight = Math.ceil(content.getBoundingClientRect().height)
  if (nextHeight > 0) authTabsHeight.value = `${nextHeight}px`
}

onMounted(() => {
  // 支持外部指定初始 tab（如 LoginPromptModal「去注册」按钮携带 query.tab=register）
  const tab = route.query.tab
  if (tab === 'register' || tab === 'login') {
    activeTab.value = tab
    if (tab === 'register') registerStep.value = 1
  }
  nextTick(() => {
    if (typeof ResizeObserver === 'undefined') return
    const content = authTabsContentRef.value
    if (!content) return
    updateAuthTabsHeight()
    authTabsResizeObserver = new ResizeObserver(updateAuthTabsHeight)
    authTabsResizeObserver.observe(content)
  })
})

// 访客浏览：跳发现页（已验证的访客落地页，所有接口 anonymous 可读）
function handleBrowseAsGuest() {
  router.push('/discover')
}

// 登录/注册成功后的跳转：优先返回来源页（query.redirect），无则默认 /home
function redirectAfterAuth() {
  const redirect = route.query.redirect
  if (typeof redirect === 'string' && redirect.startsWith('/') && redirect !== '/') {
    router.push(redirect)
  } else {
    router.push('/home')
  }
}

// ---- 登录状态 ----
const loginFormRef = ref(null)
const loginLoading = ref(false)
const oauthLoading = ref(false)
const loginData = ref({ email: '', password: '' })
const forgotModalShow = ref(false)

const loginRules = computed(() => ({
  email: [
    { required: true, message: t('login.validation.emailRequired'), trigger: 'blur' },
    { type: 'email', message: t('validation.invalidEmail'), trigger: 'blur' }
  ],
  password: [
    { required: true, message: t('login.validation.passwordRequired'), trigger: 'blur' },
    { min: 8, message: t('login.validation.passwordMinLength'), trigger: 'blur' },
    { pattern: /^(?=.*[a-zA-Z])(?=.*\d)/, message: t('login.validation.passwordPattern'), trigger: 'blur' }
  ]
}))

// ---- 注册状态 ----
const registerStep = ref(1)
const sendCodeLoading = ref(false)
const verifyLoading = ref(false)
const registerLoading = ref(false)
const countdown = ref(0)
let countdownTimer = null

const step1FormRef = ref(null)
const step2FormRef = ref(null)
const step3FormRef = ref(null)

const registerData = ref({
  email: '',
  code: '',
  username: '',
  password: '',
  confirmPassword: ''
})

const step1Rules = computed(() => ({
  email: [
    { required: true, message: t('login.validation.emailRequired'), trigger: 'blur' },
    { type: 'email', message: t('validation.invalidEmail'), trigger: 'blur' }
  ]
}))

const step2Rules = computed(() => ({
  code: [
    { required: true, message: t('login.validation.codeRequired'), trigger: 'blur' },
    { pattern: /^\d{6}$/, message: t('login.validation.codeFormat'), trigger: 'blur' }
  ]
}))

const step3Rules = computed(() => ({
  username: [
    { required: true, message: t('login.validation.usernameRequired'), trigger: 'blur' },
    { min: 2, max: 30, message: t('login.validation.usernameLength'), trigger: 'blur' },
    { pattern: /^[\w一-龥]+$/, message: t('login.validation.usernamePattern'), trigger: 'blur' }
  ],
  password: [
    { required: true, message: t('login.validation.passwordRequired'), trigger: 'blur' },
    { min: 8, message: t('login.validation.passwordMinLength'), trigger: 'blur' },
    { pattern: /^(?=.*[a-zA-Z])(?=.*\d)/, message: t('login.validation.passwordPattern'), trigger: 'blur' }
  ],
  confirmPassword: [
    { required: true, message: t('login.validation.confirmPasswordRequired'), trigger: 'blur' },
    {
      validator: (rule, value) => value === registerData.value.password,
      message: t('login.validation.passwordMismatch'),
      trigger: 'blur'
    }
  ]
}))

// ---- 密码强度 ----
const passwordStrength = computed(() => {
  const pwd = registerData.value.password
  if (!pwd) return 0
  let score = 0
  if (pwd.length >= 8) score++
  if (/[a-z]/.test(pwd) && /[A-Z]/.test(pwd)) score++
  if (/\d/.test(pwd)) score++
  if (/[^a-zA-Z0-9]/.test(pwd)) score++
  if (score <= 1) return 1
  if (score <= 2) return 2
  return 3
})

const passwordStrengthClass = computed(() => {
  const map = { 1: 'weak', 2: 'medium', 3: 'strong' }
  return map[passwordStrength.value] || ''
})

const passwordStrengthWidth = computed(() => {
  const map = { 1: '33%', 2: '66%', 3: '100%' }
  return map[passwordStrength.value] || '0%'
})

const passwordStrengthLabel = computed(() => {
  const map = {
    1: t('login.passwordStrength.weak'),
    2: t('login.passwordStrength.medium'),
    3: t('login.passwordStrength.strong')
  }
  return map[passwordStrength.value] || ''
})

// ---- 倒计时 ----
function startCountdown() {
  // 先清掉可能存在的旧定时器，保证任意时刻只有一个倒计时在跑
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
  countdown.value = 60
  countdownTimer = setInterval(() => {
    countdown.value--
    if (countdown.value <= 0) {
      clearInterval(countdownTimer)
      countdownTimer = null
    }
  }, 1000)
}

onUnmounted(() => {
  if (countdownTimer) clearInterval(countdownTimer)
  authTabsResizeObserver?.disconnect()
})

// ---- Tab 切换 ----
function handleTabChange(tab) {
  if (tab === 'register') {
    registerStep.value = 1
    resetRegisterData()
  }
}

function resetRegisterData() {
  registerData.value = { email: '', code: '', username: '', password: '', confirmPassword: '' }
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
  countdown.value = 0
}

// ---- 登录 ----
async function handleLogin() {
  try {
    await loginFormRef.value?.validate()
  } catch { return }

  loginLoading.value = true
  try {
    const res = await loginWithEmail({
      email: loginData.value.email,
      password: loginData.value.password
    })
    auth.setToken(res.data.token)
    auth.saveDeviceId(res.data.device_id)
    message.success(t('login.messages.loginSuccess'))
    redirectAfterAuth()
  } catch (err) {
    message.error(err.message || t('login.messages.loginFailed', { error: '' }))
  } finally {
    loginLoading.value = false
  }
}

// ---- 找回密码成功：回填邮箱，切回登录 Tab ----
function handleForgotSuccess(email) {
  loginData.value.email = email
  loginData.value.password = ''
  activeTab.value = 'login'
}

// ---- 注册 Step 1: 发送验证码 ----
async function handleSendCode() {
  // 防止请求 pending 期间重复点击造成并发请求 / 多个倒计时
  if (sendCodeLoading.value) return
  if (registerStep.value === 1) {
    try {
      await step1FormRef.value?.validate()
    } catch { return }
  }

  sendCodeLoading.value = true
  try {
    await sendVerificationCode(registerData.value.email, getLangParam())
    message.success(t('login.register.codeSent'))
    startCountdown()
    if (registerStep.value === 1) registerStep.value = 2
  } catch (err) {
    if (err.code === 409) {
      message.error(t('login.register.emailExists'))
    } else if (err.code === 429) {
      message.error(t('login.register.rateLimit'))
    } else {
      message.error(t('login.messages.sendCodeFailed') + (err.message ? ': ' + err.message : ''))
    }
  } finally {
    sendCodeLoading.value = false
  }
}

// ---- 注册 Step 2: 验证验证码 ----
async function handleVerifyCode() {
  try {
    await step2FormRef.value?.validate()
  } catch { return }

  verifyLoading.value = true
  try {
    await verifyEmailCode(registerData.value.email, registerData.value.code)
    message.success(t('login.messages.codeVerified'))
    registerStep.value = 3
  } catch (err) {
    message.error(err.message || t('login.messages.codeInvalid'))
  } finally {
    verifyLoading.value = false
  }
}

// ---- 注册 Step 3: 完成注册 ----
async function handleRegister() {
  try {
    await step3FormRef.value?.validate()
  } catch { return }

  registerLoading.value = true
  try {
    const res = await registerWithEmail({
      email: registerData.value.email,
      username: registerData.value.username,
      password: registerData.value.password
    })
    auth.setToken(res.data.token, res.data.expire)
    auth.saveDeviceId(res.data.device_id)
    message.success(t('login.register.success'))
    redirectAfterAuth()
  } catch (err) {
    message.error(t('login.messages.registerFailed', { error: err.message || '' }))
  } finally {
    registerLoading.value = false
  }
}

// ---- OAuth ----
// OAuth 为整页跳转，query.redirect 会在跳转中丢失：先校验并暂存站内回跳地址，
// 由回调页（Success.vue）在登录成功后消费。校验规则与 redirectAfterAuth 一致，
// 仅允许站内路径，防止开放重定向。
function storeOauthReturnPath() {
  const redirect = route.query.redirect
  if (typeof redirect === 'string' && redirect.startsWith('/') && redirect !== '/') {
    sessionStorage.setItem(OAUTH_RETURN_PATH_KEY, redirect)
  } else {
    sessionStorage.removeItem(OAUTH_RETURN_PATH_KEY)
  }
}

function startOauthLogin(provider) {
  oauthLoading.value = true
  storeOauthReturnPath()
  window.location.href = loginWithOAuth(provider)
}

const handleGoogleLogin = () => startOauthLogin('google')
const handleGithubLogin = () => startOauthLogin('github')
const handleAzureLogin = () => startOauthLogin('azure')
</script>

<style scoped>
.login-container {
  position: relative;
  z-index: 1;
  background: var(--glass-bg);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid var(--glass-border);
  border-radius: 24px;
  box-shadow: var(--shadow-md);
  width: 100%;
  max-width: 520px;
  padding: 40px 32px;
  text-align: center;
  transition: transform 0.3s ease, box-shadow 0.3s ease;
}

.login-container:hover {
  box-shadow: var(--shadow-lg);
}

/* 语言切换 */
.language-switcher {
  position: absolute;
  top: 16px;
  right: 16px;
  z-index: 2;
}

/* Logo */
.logo-area {
  margin-bottom: 24px;
}

.logo-brand {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-bottom: 12px;
}

.logo-icon {
  width: 48px;
  height: 48px;
  flex-shrink: 0;
}

.logo-text {
  font-size: 2.5rem;
  font-weight: 800;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  letter-spacing: -0.5px;
}

.tagline {
  color: var(--text-secondary);
  font-size: 0.95rem;
  line-height: 1.6;
  font-weight: 400;
}

/* Tab 切换 */
.auth-tabs {
  margin-top: 8px;
}

.auth-tabs-viewport {
  box-sizing: content-box;
  margin: 0 -16px -16px;
  padding: 0 16px 16px;
  overflow: hidden;
  transition: height 0.24s cubic-bezier(0.4, 0, 0.2, 1);
}

.auth-tabs-content {
  width: 100%;
}

:deep(.n-tabs .n-tabs-nav) {
  justify-content: center;
}

:deep(.n-tabs .n-tabs-tab) {
  font-size: 1rem;
  font-weight: 600;
  color: var(--text-tertiary);
  padding: 8px 20px;
  transition: color 0.3s ease;
}

:deep(.n-tabs .n-tabs-tab--active) {
  color: var(--text-primary) !important;
}

:deep(.n-tabs .n-tabs-bar) {
  background: var(--primary-gradient);
}

:deep(.n-tabs-pane-wrapper) {
  margin-top: 16px;
}

/* 表单 */
.auth-form {
  text-align: left;
  margin-top: 16px;
}

:deep(.n-input) {
  --n-border: 1px solid var(--glass-border) !important;
  --n-border-hover: var(--accent-color) !important;
  --n-border-focus: var(--accent-color) !important;
  --n-color: var(--bg-secondary) !important;
  --n-color-focus: var(--bg-tertiary) !important;
  --n-text-color: var(--text-primary) !important;
  --n-placeholder-color: var(--text-tertiary) !important;
  --n-border-radius: 12px !important;
  --n-height: 48px !important;
  --n-font-size: 15px !important;
  --n-count-text-color: var(--text-tertiary) !important;
}

:deep(.n-input .n-input__textarea-el),
:deep(.n-input .n-input__input-el) {
  background-color: transparent !important;
  /* color: var(--text-primary) !important; */
  /* caret-color: var(--text-primary); */
}

:deep(.n-input input:-webkit-autofill) {
  -webkit-text-fill-color: var(--text-primary) !important;
  -webkit-box-shadow: 0 0 0 1000px var(--bg-secondary) inset !important;
  transition: background-color 5000s ease-in-out 0s;
}

:deep(.n-input input:-webkit-autofill:hover),
:deep(.n-input input:-webkit-autofill:focus) {
  -webkit-text-fill-color: var(--text-primary) !important;
  -webkit-box-shadow: 0 0 0 1000px var(--bg-secondary) inset !important;
  transition: background-color 5000s ease-in-out 0s;
}

:deep(.n-input .n-input__eye) {
  color: var(--text-tertiary) !important;
}

:deep(.n-form-item) {
  margin-bottom: 16px;
}

:deep(.n-form-item-feedback-wrapper) {
  min-height: 0;
}

:deep(.n-form-item-feedback__line) {
  font-size: 12px;
  margin-top: 4px;
}

/* 忘记密码 */
.forgot-row {
  text-align: right;
  margin-bottom: 16px;
}

.forgot-link {
  font-size: 0.85rem;
  color: var(--theme-color);
  cursor: pointer;
  transition: color 0.2s ease;
}

.forgot-link:hover {
  color: var(--accent-secondary);
}

/* 主色按钮 */
.gradient-btn {
  height: 48px;
  border-radius: 14px;
  font-size: 16px;
  font-weight: 600;
  background: var(--primary-gradient);
  border: none;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 4px 16px rgba(102, 234, 194, 0.2);
}

.gradient-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 24px rgba(102, 234, 194, 0.35);
}

.gradient-btn:active {
  transform: translateY(0);
}

/* 分隔线 */
.auth-divider {
  margin: 20px 0 16px;
}

/* 访客入口：次级玻璃描边按钮（区别于主渐变登录按钮） */
.guest-entry {
  margin-top: 16px;
}

.guest-entry-btn {
  width: 100%;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: 14px;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-secondary);
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.1);
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.guest-entry-btn:hover {
  color: var(--theme-color, #66eac2);
  border-color: rgba(102, 234, 194, 0.4);
  background: rgba(102, 234, 194, 0.08);
}

.guest-entry-btn:active {
  transform: scale(0.99);
}

:deep(.n-divider .n-divider__title) {
  font-size: 0.8rem;
  color: var(--text-tertiary);
}

/* OAuth 按钮 */
.oauth-section {
  display: flex;
  justify-content: center;
  gap: 16px;
}

.oauth-icon-btn {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: var(--bg-tertiary);
  border: 1px solid var(--glass-border);
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.oauth-icon-btn:hover {
  border-color: var(--accent-color);
  box-shadow: 0 4px 20px rgba(102, 126, 234, 0.2);
  transform: translateY(-2px);
}

.oauth-icon-btn:active {
  transform: translateY(0);
}

.oauth-icon {
  width: 22px;
  height: 22px;
}

.github-icon {
  filter: brightness(0) invert(1);
}

/* 步骤指示器 */
.step-indicator {
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 24px;
  padding: 0 20px;
}

.step-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
}

.step-dot {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
  /* background: var(--bg-tertiary); */
  /* color: var(--text-tertiary); */
  /* border: 2px solid var(--glass-border); */
  /* transition: all 0.3s ease; */
}

.step-item.active .step-dot {
  background: var(--primary-gradient);
  color: #fff;
  border-color: transparent;
}

.step-item.done .step-dot {
  background: #22c55e;
  color: #fff;
  border-color: transparent;
}

.step-label {
  font-size: 0.7rem;
  color: var(--text-tertiary);
  white-space: nowrap;
  transition: color 0.3s ease;
}

.step-item.active .step-label {
  color: var(--text-primary);
}

.step-line {
  flex: 1;
  height: 2px;
  background: var(--glass-border);
  margin: 0 8px;
  margin-bottom: 20px;
  transition: background 0.3s ease;
}

.step-line.active {
  background: var(--primary-gradient);
}

/* 验证码 */
.code-hint {
  font-size: 0.85rem;
  color: var(--text-secondary);
  margin-bottom: 16px;
  text-align: center;
}

.code-input :deep(.n-input__input) {
  text-align: center;
  font-size: 24px;
  /* letter-spacing: 12px; */
  font-weight: 600;
}

.resend-row {
  text-align: center;
  margin-top: 12px;
}

/* 密码强度 */
.password-strength {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
  padding: 0 2px;
}

.strength-bar {
  flex: 1;
  height: 4px;
  background: var(--bg-tertiary);
  border-radius: 2px;
  overflow: hidden;
}

.strength-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.3s ease, background 0.3s ease;
}

.strength-fill.weak { background: #ef4444; }
.strength-fill.medium { background: #f59e0b; }
.strength-fill.strong { background: #22c55e; }

.strength-text {
  font-size: 0.75rem;
  font-weight: 600;
  min-width: 24px;
}

.strength-text.weak { color: #ef4444; }
.strength-text.medium { color: #f59e0b; }
.strength-text.strong { color: #22c55e; }

/* 步骤内容过渡 */
.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}

.fade-slide-enter-from {
  opacity: 0;
  transform: translateY(12px);
}

.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(-12px);
}

/* Footer */
.footer {
  margin-top: 24px;
  font-size: 0.8rem;
  color: var(--text-tertiary);
  line-height: 1.5;
  text-align: center;
}

.footer-link {
  color: var(--theme-color);
  text-decoration: none;
  font-weight: 500;
  transition: all 0.3s ease;
  border-bottom: 1px dashed transparent;
}

.footer-link:hover {
  color: var(--theme-color);
  border-bottom-color: var(--theme-color);
  opacity: 0.8;
}

/* NCard 深色覆盖 */
:deep(.n-card) {
  background: rgba(14, 17, 32, 0.78) !important;
}

/* 响应式 */
@media (max-width: 560px) {
  .login-container {
    padding: 28px 20px;
    border-radius: 20px;
  }

  .logo-text {
    font-size: 2rem;
  }

  .step-label {
    font-size: 0.6rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .auth-tabs-viewport {
    transition: none;
  }
}
</style>
