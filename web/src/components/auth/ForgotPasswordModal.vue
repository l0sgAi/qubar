<template>
  <NModal
    :show="show"
    @update:show="handleShowChange"
    preset="card"
    class="forgot-password-modal"
    :style="{ width: '460px', maxWidth: '90vw', borderRadius: '24px' }"
    :title="t('login.forgotPasswordModal.title')"
    header-style="font-size: 22px; font-weight: 700;"
    :bordered="false"
    size="huge"
    :mask-closable="false"
  >
    <!-- 步骤指示器 -->
    <div class="step-indicator">
      <div class="step-item" :class="{ active: step >= 1, done: step > 1 }">
        <div class="step-dot">
          <span v-if="step > 1">&#10003;</span>
          <span v-else>1</span>
        </div>
        <span class="step-label">{{ t('login.forgotPasswordModal.stepSendCode') }}</span>
      </div>
      <div class="step-line" :class="{ active: step > 1 }"></div>
      <div class="step-item" :class="{ active: step >= 2, done: step > 2 }">
        <div class="step-dot">
          <span v-if="step > 2">&#10003;</span>
          <span v-else>2</span>
        </div>
        <span class="step-label">{{ t('login.forgotPasswordModal.stepVerify') }}</span>
      </div>
      <div class="step-line" :class="{ active: step > 2 }"></div>
      <div class="step-item" :class="{ active: step >= 3 }">
        <div class="step-dot">3</div>
        <span class="step-label">{{ t('login.forgotPasswordModal.stepReset') }}</span>
      </div>
    </div>

    <!-- 步骤内容 -->
    <Transition name="fade-slide" mode="out-in">
      <!-- Step 1: 输入邮箱 -->
      <div v-if="step === 1" key="step1" class="step-content">
        <NForm
          ref="step1FormRef"
          :model="formData"
          :rules="emailRules"
          class="auth-form"
        >
          <NFormItem path="email" :show-label="false">
            <NInput
              v-model:value="formData.email"
              :placeholder="t('login.email.placeholder')"
              size="large"
              :input-props="{ autocomplete: 'email' }"
              @keyup.enter="handleSendCode"
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
            {{ countdown > 0 ? t('login.register.resendIn', { seconds: countdown }) : t('login.forgotPasswordModal.sendCode') }}
          </NButton>
        </NForm>
      </div>

      <!-- Step 2: 验证码 -->
      <div v-else-if="step === 2" key="step2" class="step-content">
        <div class="code-hint">
          {{ t('login.register.codeSentTo', { email: formData.email }) }}
        </div>

        <NForm
          ref="step2FormRef"
          :model="formData"
          :rules="codeRules"
          class="auth-form"
        >
          <NFormItem path="code" :show-label="false">
            <NInput
              v-model:value="formData.code"
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

      <!-- Step 3: 设置新密码 -->
      <div v-else-if="step === 3" key="step3" class="step-content">
        <NForm
          ref="step3FormRef"
          :model="formData"
          :rules="passwordRules"
          class="auth-form"
        >
          <NFormItem path="password" :show-label="false">
            <NInput
              v-model:value="formData.password"
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
          <div v-if="formData.password" class="password-strength">
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
              v-model:value="formData.confirmPassword"
              type="password"
              show-password-on="click"
              :placeholder="t('login.register.confirmPasswordPlaceholder')"
              size="large"
              :input-props="{ autocomplete: 'new-password' }"
              @keyup.enter="handleReset"
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
            :loading="resetLoading"
            @click="handleReset"
          >
            {{ t('login.forgotPasswordModal.resetButton') }}
          </NButton>
        </NForm>
      </div>
    </Transition>
  </NModal>
</template>

<script setup>
import { ref, computed, watch, onUnmounted } from 'vue'
import { NModal, NForm, NFormItem, NInput, NButton, NIcon, useMessage } from 'naive-ui'
import { Email as EmailIcon, Locked } from '@vicons/carbon'
import { useI18n } from 'vue-i18n'
import { sendPasswordResetCode, verifyPasswordResetCode, resetPassword } from '@/api/auth'

const props = defineProps({
  show: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:show', 'success'])

const message = useMessage()
const { t, locale } = useI18n()

function getLangParam() {
  return locale.value.startsWith('zh') ? 'zh' : 'en'
}

// ---- 状态 ----
const step = ref(1)
const sendCodeLoading = ref(false)
const verifyLoading = ref(false)
const resetLoading = ref(false)
const countdown = ref(0)
let countdownTimer = null

const step1FormRef = ref(null)
const step2FormRef = ref(null)
const step3FormRef = ref(null)

const formData = ref({
  email: '',
  code: '',
  password: '',
  confirmPassword: ''
})

// ---- 校验规则 ----
const emailRules = computed(() => ({
  email: [
    { required: true, message: t('login.validation.emailRequired'), trigger: 'blur' },
    { type: 'email', message: t('validation.invalidEmail'), trigger: 'blur' }
  ]
}))

const codeRules = computed(() => ({
  code: [
    { required: true, message: t('login.validation.codeRequired'), trigger: 'blur' },
    { pattern: /^\d{6}$/, message: t('login.validation.codeFormat'), trigger: 'blur' }
  ]
}))

const passwordRules = computed(() => ({
  password: [
    { required: true, message: t('login.validation.passwordRequired'), trigger: 'blur' },
    { min: 8, message: t('login.validation.passwordMinLength'), trigger: 'blur' },
    { pattern: /^(?=.*[a-zA-Z])(?=.*\d)/, message: t('login.validation.passwordPattern'), trigger: 'blur' }
  ],
  confirmPassword: [
    { required: true, message: t('login.validation.confirmPasswordRequired'), trigger: 'blur' },
    {
      validator: (rule, value) => value === formData.value.password,
      message: t('login.validation.passwordMismatch'),
      trigger: 'blur'
    }
  ]
}))

// ---- 密码强度（复用注册流程逻辑）----
const passwordStrength = computed(() => {
  const pwd = formData.value.password
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

// ---- 重置状态（打开/关闭时）----
function resetState() {
  step.value = 1
  formData.value = { email: '', code: '', password: '', confirmPassword: '' }
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
  countdown.value = 0
}

// 弹窗显隐变化：关闭时重置状态
function handleShowChange(val) {
  emit('update:show', val)
  if (!val) {
    // 等过渡动画再重置，避免用户看到内容突变
    setTimeout(resetState, 200)
  }
}

// 外部关闭（如点 X / Esc）时也需同步
watch(() => props.show, (val) => {
  if (!val) {
    setTimeout(resetState, 200)
  }
})

onUnmounted(() => {
  if (countdownTimer) clearInterval(countdownTimer)
})

// ---- Step 1: 发送验证码 ----
async function handleSendCode() {
  // 防止请求 pending 期间重复点击造成并发请求 / 多个倒计时
  if (sendCodeLoading.value) return
  if (step.value === 1) {
    try {
      await step1FormRef.value?.validate()
    } catch { return }
  }

  sendCodeLoading.value = true
  try {
    await sendPasswordResetCode(formData.value.email, getLangParam())
    message.success(t('login.register.codeSent'))
    startCountdown()
    if (step.value === 1) step.value = 2
  } catch (err) {
    if (err.code === 404) {
      // 邮箱未注册（与注册端点「邮箱已存在 → 409」行为对齐）
      message.error(t('login.forgotPasswordModal.emailNotFound'))
    } else if (err.code === 429) {
      message.error(t('login.register.rateLimit'))
      startCountdown()
    } else {
      message.error(t('login.messages.sendCodeFailed') + (err.message ? ': ' + err.message : ''))
    }
  } finally {
    sendCodeLoading.value = false
  }
}

// ---- Step 2: 校验验证码 ----
async function handleVerifyCode() {
  try {
    await step2FormRef.value?.validate()
  } catch { return }

  verifyLoading.value = true
  try {
    await verifyPasswordResetCode(formData.value.email, formData.value.code)
    message.success(t('login.messages.codeVerified'))
    step.value = 3
  } catch (err) {
    // Redis 读取失败也归为 expired，前端统一按过期/错误提示
    message.error(err.message || t('login.messages.codeInvalid'))
  } finally {
    verifyLoading.value = false
  }
}

// ---- Step 3: 重置密码 ----
async function handleReset() {
  try {
    await step3FormRef.value?.validate()
  } catch { return }

  resetLoading.value = true
  try {
    await resetPassword(formData.value.email, formData.value.password)
    // 重置成功：旧 token 全部失效，不会自动登录，引导用户用新密码重新登录
    message.success(t('login.forgotPasswordModal.resetSuccess'))
    emit('success', formData.value.email)
    emit('update:show', false)
    setTimeout(resetState, 200)
  } catch (err) {
    if (err.code === 403) {
      // 账号被禁用（status≠1）
      message.error(t('login.forgotPasswordModal.accountDisabled'))
    } else if (err.code === 400 && /expire/i.test(err.message || '')) {
      // 已验证标记过期（超过 10min 或跳过了 Step 2），引导回 ①
      message.error(t('login.forgotPasswordModal.verificationExpired'))
      resetState()
    } else {
      message.error(err.message || t('login.forgotPasswordModal.resetFailed'))
    }
  } finally {
    resetLoading.value = false
  }
}
</script>

<style scoped>
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
}

:deep(.n-input .n-input__input-el) {
  background-color: transparent !important;
}

:deep(.n-input input:-webkit-autofill) {
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
}

.step-item.active .step-dot {
  background: var(--primary-gradient);
  color: #fff;
}

.step-item.done .step-dot {
  background: #22c55e;
  color: #fff;
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
</style>

<!--
  全局样式（非 scoped）：
  NModal 会把内容 teleport 到 body，scoped 样式（含 :deep）无法命中 teleport 出去的 DOM，
  因此模态框卡片的背景必须用全局样式覆盖。用 .forgot-password-modal 类限定作用域，避免污染其他 NCard。
-->
<style>
.forgot-password-modal {
  background: rgb(14, 17, 32) !important;
}

.forgot-password-modal .n-card-header {
  background: transparent !important;
}

.forgot-password-modal .n-card-header__main {
  color: var(--text-primary) !important;
}
</style>
