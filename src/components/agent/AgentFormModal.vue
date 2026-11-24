<template>
  <NModal
    v-model:show="visible"
    :mask-closable="false"
    preset="card"
    :style="{ width: '640px', borderRadius: '24px' }"
    :title="modalTitle"
    :segmented="{ content: 'soft' }"
    :bordered="false"
    size="huge"
  >
    <NForm
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-placement="top"
      class="agent-form"
    >
      <!-- 分区一：基本信息 -->
      <div class="form-section">
        <div class="section-header">
          <span class="section-dot" />
          <span class="section-title">{{ t('agent.form.sectionBasic') }}</span>
        </div>
        <NFormItem :label="t('agent.form.name')" path="name">
          <NInput
            v-model:value="formData.name"
            :placeholder="isCircle ? t('agent.form.namePlaceholderCircle') : t('agent.form.namePlaceholder')"
            maxlength="50"
            show-count
          />
        </NFormItem>

        <NFormItem v-if="!isEdit">
          <NCheckbox v-model:checked="formData.createDisabled">{{ t('agent.form.createDisabled') }}</NCheckbox>
        </NFormItem>

        <NFormItem :label="t('agent.form.avatarUrl')">
          <div class="avatar-row">
            <NAvatar
              round
              :size="56"
              :src="formData.avatar_url || undefined"
              class="avatar-preview"
            >
              <span v-if="!formData.avatar_url" class="avatar-fallback">🤖</span>
            </NAvatar>
            <NUpload
              :show-file-list="false"
              :custom-request="handleAvatarUpload"
              @before-upload="beforeAvatarUpload"
              accept="image/*"
            >
              <NButton secondary size="small" round>
                {{ t('agent.form.avatarUpload') }}
              </NButton>
            </NUpload>
            <NButton
              v-if="formData.avatar_url"
              quaternary
              size="small"
              round
              @click="formData.avatar_url = ''"
            >
              {{ t('agent.form.avatarRemove') }}
            </NButton>
          </div>
        </NFormItem>
      </div>

      <!-- 分区二：模型接入 -->
      <div class="form-section">
        <div class="section-header">
          <span class="section-dot" />
          <span class="section-title">{{ t('agent.form.sectionApi') }}</span>
        </div>
        <!-- 圈内管理员编辑：凭据字段（协议/地址/Key）仅圈主可改，后端对管理员混提
             凭据字段的请求整体 403，故此处整组只读，diff 时也跳过这些键 -->
        <NAlert
          v-if="isEdit && !canEditCredentials"
          :title="t('agent.form.sectionApi')"
          :bordered="false"
          class="cred-readonly-alert"
        >
          {{ t('agent.circlePage.credentialsOwnerOnly') }}
        </NAlert>
        <template v-else>
          <div class="api-grid">
            <NFormItem :label="t('agent.form.protocol')" path="api_protocol">
              <NSelect
                v-model:value="formData.api_protocol"
                :options="protocolOptions"
                :placeholder="t('agent.form.protocol')"
              />
            </NFormItem>

            <NFormItem :label="t('agent.form.baseUrl')" path="base_url">
              <NInput
                v-model:value="formData.base_url"
                :placeholder="t('agent.form.baseUrlPlaceholder')"
              />
            </NFormItem>
          </div>

          <!-- api_key：创建=明文输入；编辑=掩码展示 + 独立换 key 输入框（不回填旧值） -->
          <NFormItem v-if="!isEdit" :label="t('agent.form.apiKey')" path="api_key">
            <NInput
              v-model:value="formData.api_key"
              type="password"
              show-password-on="click"
              :placeholder="t('agent.form.apiKeyPlaceholder')"
            />
          </NFormItem>
          <NFormItem v-else :label="t('agent.form.apiKey')">
            <div class="key-block">
              <NText depth="3" class="key-masked">
                {{ agent.has_api_key
                  ? t('agent.form.apiKeyCurrent', { mask: agent.api_key_masked })
                  : t('agent.form.apiKeyNone') }}
              </NText>
              <NInput
                v-model:value="formData.new_api_key"
                type="password"
                show-password-on="click"
                :placeholder="t('agent.form.apiKeyNewPlaceholder')"
                :disabled="formData.clear_api_key"
                class="key-new-input"
              />
              <NCheckbox
                v-model:checked="formData.clear_api_key"
                size="small"
                class="key-clear-check"
              >
                {{ t('agent.form.apiKeyClear') }}
              </NCheckbox>
            </div>
          </NFormItem>
        </template>

        <NFormItem :label="t('agent.form.model')" path="model">
          <NInput
            v-model:value="formData.model"
            :placeholder="t('agent.form.modelPlaceholder')"
            maxlength="100"
          />
        </NFormItem>

        <!-- llm_params：白名单 5 键，只提交填写的；整体替换语义 -->
        <NFormItem :label="t('agent.form.llmParams')">
          <div class="llm-params">
            <NInputNumber
              v-for="key in LLM_PARAM_KEYS"
              :key="key"
              v-model:value="formData.llm[key]"
              :placeholder="key"
              :min="0"
            >
              <template #prefix><span class="llm-key">{{ key }}:</span></template>
            </NInputNumber>
          </div>
        </NFormItem>

        <NFormItem :label="t('agent.form.systemPrompt')" path="system_prompt">
          <NInput
            v-model:value="formData.system_prompt"
            type="textarea"
            :placeholder="t('agent.form.systemPromptPlaceholder')"
            :autosize="{ minRows: 3, maxRows: 6 }"
            maxlength="2000"
          />
        </NFormItem>
      </div>

      <!-- 分区三：触发与限频 -->
      <div class="form-section">
        <div class="section-header">
          <span class="section-dot" />
          <span class="section-title">{{ t('agent.form.sectionTrigger') }}</span>
        </div>
        <NFormItem :label="t('agent.table.trigger')">
          <NSelect v-model:value="formData.trigger_mode" :options="triggerOptions" />
        </NFormItem>
        <NFormItem
          v-if="formData.trigger_mode === 2"
          :label="t('agent.form.keywords')"
          path="trigger_keywords"
        >
          <NDynamicTags
            v-model:value="formData.trigger_keywords"
            :placeholder="t('agent.form.keywordsPlaceholder')"
          />
        </NFormItem>

        <!-- filter_prompt：两阶段回复判定（高级选项）；
             空 = 命中关键词即回复，非空 = LLM 先判定该不该回 -->
        <NFormItem v-if="formData.trigger_mode === 2" :label="t('agent.form.filterPrompt')" path="filter_prompt">
          <NInput
            v-model:value="formData.filter_prompt"
            type="textarea"
            :placeholder="t('agent.form.filterPromptPlaceholder')"
            :autosize="{ minRows: 2, maxRows: 4 }"
            maxlength="2000"
            show-count
          />
        </NFormItem>

        <div class="rate-row">
          <NFormItem :label="t('agent.form.maxReplies')" path="max_replies_per_hour">
            <NInputNumber
              v-model:value="formData.max_replies_per_hour"
              :min="0"
              :placeholder="t('agent.form.maxReplies')"
              style="width: 100%"
            >
              <template #suffix>{{ t('agent.form.perHour') }}</template>
            </NInputNumber>
          </NFormItem>
          <NFormItem :label="t('agent.form.minInterval')" path="min_interval_sec">
            <NInputNumber
              v-model:value="formData.min_interval_sec"
              :min="0"
              :placeholder="t('agent.form.minInterval')"
              style="width: 100%"
            >
              <template #suffix>{{ t('agent.form.seconds') }}</template>
            </NInputNumber>
          </NFormItem>
        </div>
      </div>

      <!-- 分区四：自动回复高热度帖子 / 评论（auto_config.auto_reply）；
           关闭时折叠其余字段，但保存时仍按整体替换语义提交完整对象 -->
      <div class="form-section">
        <div class="section-header">
          <span class="section-dot" />
          <span class="section-title">{{ t('agent.autoReply.section') }}</span>
        </div>
        <div class="auto-switch-row">
          <NSwitch
            :value="formData.auto_reply.enabled"
            @update:value="toggleAutoReply"
          />
          <div class="auto-switch-text">
            <div class="auto-switch-label">{{ t('agent.autoReply.enabled') }}</div>
            <NText depth="3" class="auto-switch-hint">{{ t('agent.autoReply.enabledHint') }}</NText>
          </div>
        </div>

        <template v-if="formData.auto_reply.enabled">
          <NFormItem
            :label="t('agent.autoReply.targets')"
            path="auto_reply.targets"
            :rule="autoReplyRule('targets')"
          >
            <NCheckboxGroup v-model:value="formData.auto_reply.targets">
              <NSpace :size="20">
                <NCheckbox value="post">{{ t('agent.autoReply.targetPost') }}</NCheckbox>
                <NCheckbox value="comment">{{ t('agent.autoReply.targetComment') }}</NCheckbox>
              </NSpace>
            </NCheckboxGroup>
          </NFormItem>

          <NFormItem
            v-if="formData.auto_reply.targets.includes('post')"
            :label="t('agent.autoReply.postHotMin')"
            path="auto_reply.post_hot_min"
            :rule="autoReplyRule('post_hot_min')"
          >
            <NInputNumber
              v-model:value="formData.auto_reply.post_hot_min"
              :min="0"
              :max="AUTO_REPLY_THRESHOLD_MAX"
              :precision="0"
              :placeholder="t('agent.autoReply.postHotMinPlaceholder')"
              style="width: 100%"
            />
          </NFormItem>

          <!-- 两个评论门槛满足任一即可，联合校验挂在同一个表单项上 -->
          <NFormItem
            v-if="formData.auto_reply.targets.includes('comment')"
            :label="t('agent.autoReply.commentThreshold')"
            path="auto_reply.comment_threshold"
            :rule="autoReplyRule('comment_like_min', 'comment_reply_min', 'comment_threshold')"
          >
            <div class="rate-row auto-threshold-row">
              <NInputNumber
                v-model:value="formData.auto_reply.comment_like_min"
                :min="0"
                :max="AUTO_REPLY_THRESHOLD_MAX"
                :precision="0"
              >
                <template #prefix><span class="llm-key">{{ t('agent.autoReply.commentLikeMin') }}</span></template>
              </NInputNumber>
              <NInputNumber
                v-model:value="formData.auto_reply.comment_reply_min"
                :min="0"
                :max="AUTO_REPLY_THRESHOLD_MAX"
                :precision="0"
              >
                <template #prefix><span class="llm-key">{{ t('agent.autoReply.commentReplyMin') }}</span></template>
              </NInputNumber>
            </div>
          </NFormItem>

          <div class="rate-row">
            <NFormItem
              :label="t('agent.autoReply.windowHours')"
              path="auto_reply.window_hours"
              :rule="autoReplyRule('window_hours')"
            >
              <NInputNumber
                v-model:value="formData.auto_reply.window_hours"
                :min="1"
                :max="AUTO_REPLY_WINDOW_HOURS_MAX"
                :precision="0"
                :placeholder="t('agent.autoReply.windowHoursPlaceholder')"
                style="width: 100%"
              >
                <template #suffix>{{ t('agent.autoReply.hours') }}</template>
              </NInputNumber>
            </NFormItem>
            <NFormItem
              :label="t('agent.autoReply.maxPerHour')"
              path="auto_reply.max_per_hour"
              :rule="autoReplyRule('max_per_hour')"
            >
              <NInputNumber
                v-model:value="formData.auto_reply.max_per_hour"
                :min="0"
                :max="AUTO_REPLY_MAX_PER_HOUR_MAX"
                :precision="0"
                :placeholder="t('agent.autoReply.maxPerHourPlaceholder')"
                style="width: 100%"
              >
                <template #suffix>{{ t('agent.form.perHour') }}</template>
              </NInputNumber>
            </NFormItem>
          </div>
        </template>

        <NAlert
          :title="t('agent.form.noteTitle')"
          :bordered="false"
          class="auto-note"
        >
          <div>{{ t('agent.autoReply.note') }}</div>
          <div>{{ isCircle ? t('agent.autoReply.scopeCircle') : t('agent.autoReply.scopeGlobal') }}</div>
        </NAlert>
      </div>
      <div v-if="!isCircle" class="form-section">
        <div class="section-header">
          <span class="section-dot" />
          <span class="section-title">{{ t('agent.autoPost.section') }}</span>
        </div>
        <div class="auto-switch-row">
          <NSwitch :value="formData.auto_post.enabled" @update:value="toggleAutoPost" />
          <div class="auto-switch-text">
            <div class="auto-switch-label">{{ t('agent.autoPost.enabled') }}</div>
            <NText depth="3" class="auto-switch-hint">{{ t('agent.autoPost.hint') }}</NText>
          </div>
        </div>
        <template v-if="formData.auto_post.enabled">
          <div class="rate-row">
            <NFormItem :label="t('agent.autoPost.interval')">
              <NInputNumber v-model:value="formData.auto_post.interval_min" :min="10" :max="10080" :precision="0" />
            </NFormItem>
            <NFormItem :label="t('agent.autoPost.daily')">
              <NInputNumber v-model:value="formData.auto_post.daily_max" :min="1" :max="48" :precision="0" />
            </NFormItem>
          </div>
          <NFormItem :label="t('agent.autoPost.hours')">
            <NCheckbox :checked="!formData.auto_post.active_hours.length"
              @update:checked="allDay => formData.auto_post.active_hours = allDay ? [] : [8, 23]">
              {{ t('agent.autoPost.allDay') }}
            </NCheckbox>
          </NFormItem>
          <div v-if="formData.auto_post.active_hours.length" class="rate-row">
            <NFormItem :label="t('agent.autoPost.start')">
              <NInputNumber v-model:value="formData.auto_post.active_hours[0]" :min="0" :max="23" :precision="0" />
            </NFormItem>
            <NFormItem :label="t('agent.autoPost.end')">
              <NInputNumber v-model:value="formData.auto_post.active_hours[1]" :min="1" :max="24" :precision="0" />
            </NFormItem>
          </div>
          <NFormItem :label="t('agent.autoPost.topics')">
            <NDynamicTags v-model:value="formData.auto_post.topics" />
          </NFormItem>
        </template>
      </div>
    </NForm>

    <!-- 头像裁剪弹窗（1:1，仅预览圆形头像；确认后直接上传回填 URL） -->
    <ImageCropperModal
      v-model:show="cropperVisible"
      :image-src="cropperImageSrc"
      :aspect-ratio="1"
      :title="t('agent.form.cropAvatarTitle')"
      type="avatar"
      avatar-only
      :avatar-url="formData.avatar_url"
      :upload-handler="handleCropUpload"
      @confirm="handleCropConfirm"
      @cancel="releaseCropperSrc"
    />

    <template #footer>
      <div class="modal-footer">
        <NButton quaternary @click="visible = false">{{ t('common.cancel') }}</NButton>
        <NButton
          type="primary"
          :loading="submitting"
          @click="handleSubmit"
        >
          {{ isEdit ? t('common.save') : (isCircle ? t('agent.circlePage.create') : t('agent.create')) }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import {
  NModal, NForm, NFormItem, NInput, NInputNumber, NSelect,
  NDynamicTags, NCheckbox, NCheckboxGroup, NSwitch, NSpace, NButton, NAlert, NText,
  NAvatar, NUpload, useMessage
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { createAgent, updateAgent } from '@/api/agent'
import { createCircleAgent, updateCircleAgent } from '@/api/circle'
import ImageCropperModal from '@/components/common/ImageCropperModal.vue'
import { autoPostError, normalizeAutoPost } from '@/utils/agentAutoPost'
import { useImageUpload } from '@/composables/useImageUpload'
import {
  AUTO_REPLY_THRESHOLD_MAX, AUTO_REPLY_WINDOW_HOURS_MAX, AUTO_REPLY_MAX_PER_HOUR_MAX,
  emptyAutoReply, autoReplyFromAgent, prefillOnEnable, validateAutoReply,
  autoReplyChanged, buildAutoConfig
} from '@/utils/agentAutoReply'

// llm_params 后端白名单键（值为数字，其余键一律 400）
const LLM_PARAM_KEYS = [
  'temperature', 'top_p', 'max_tokens', 'presence_penalty', 'frequency_penalty'
]

const props = defineProps({
  show: { type: Boolean, default: false },
  // null=创建模式；AgentVO=编辑模式
  agent: { type: Object, default: null },
  // 作用域：global=平台超管的全局机器人（/agent）；circle=圈内机器人（/circle/agent）
  scope: { type: String, default: 'global' },
  // 圈子作用域必传：创建请求体的 circle_id（编辑时从 agent.circle_id 取）
  circleId: { type: String, default: '' },
  // 编辑模式下凭据字段（api_protocol/base_url/api_key）是否可改：
  // 圈内仅圈主（30）可改，管理员编辑时传 false——整组隐藏且 diff 跳过，
  // 避免混提凭据字段触发后端 403。创建不受限（admin+ 均可带凭据创建）
  canEditCredentials: { type: Boolean, default: true }
})
const emit = defineEmits(['update:show', 'success'])

const { t } = useI18n()
const message = useMessage()

const visible = computed({
  get: () => props.show,
  set: v => emit('update:show', v)
})
const isEdit = computed(() => !!props.agent)
const isCircle = computed(() => props.scope === 'circle')

// 弹窗标题：圈内机器人用独立文案（与全局控制台区分）
const modalTitle = computed(() => {
  if (isCircle.value) return isEdit.value ? t('agent.circlePage.edit') : t('agent.circlePage.create')
  return isEdit.value ? t('agent.edit') : t('agent.create')
})

// 后端当前只支持 openai / anthropic 两种模型协议
const protocolOptions = computed(() => {
  const options = ['openai', 'anthropic'].map(value => ({ label: value, value }))
  const saved = props.agent?.api_protocol
  if (saved && !options.some(option => option.value === saved)) {
    options.push({ label: saved, value: saved, disabled: true })
  }
  return options
})

const triggerOptions = computed(() => [
  ...(props.agent?.trigger_mode === 1 ? [{ value: 1, label: t('agent.triggerModes.all'), disabled: true }] : []),
  { value: 2, label: t('agent.triggerModes.keyword') },
  { value: 3, label: t('agent.triggerModes.manual') }
])

const formRef = ref(null)
const submitting = ref(false)

const emptyForm = () => ({
  name: '',
  createDisabled: false,
  avatar_url: '',
  api_protocol: 'openai',
  base_url: '',
  // 创建模式：明文 key，可空
  api_key: '',
  // 编辑模式：换 key 用独立输入框，不回填旧值（后端只回掩码，明文拿不到）
  new_api_key: '',
  clear_api_key: false,
  model: '',
  // 创建模式：常见 LLM 默认参数（编辑模式会逐一被 agent.llm_params 覆盖）
  llm: {
    temperature: 0.7,
    top_p: 1,
    max_tokens: 1024,
    presence_penalty: 0,
    frequency_penalty: 0
  },
  system_prompt: '',
  // 回复判定条件：空 = 命中关键词即回复
  filter_prompt: '',
  trigger_mode: 2,
  trigger_keywords: [],
  max_replies_per_hour: 30,
  min_interval_sec: 60,
  // 自动回复高热度帖子 / 评论（auto_config.auto_reply），默认关闭
  auto_reply: emptyAutoReply(),
  auto_post: normalizeAutoPost()
})

const formData = ref(emptyForm())

// ===== 头像：NUpload 选图 -> ImageCropperModal 裁剪 -> 上传回填 URL =====
const { uploadOne } = useImageUpload()
const cropperVisible = ref(false)
const cropperImageSrc = ref('')

// 上传前校验：类型 + 大小（与 UserProfile 头像限制一致）
const beforeAvatarUpload = ({ file }) => {
  const allowedTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/gif', 'image/webp', 'image/svg+xml']
  if (!allowedTypes.includes(file.type)) {
    message.error(t('agent.form.avatarTypeError'))
    return false
  }
  if (file.size > 10 * 1024 * 1024) {
    message.error(t('agent.form.avatarSizeError'))
    return false
  }
  return true
}

// 选图后不直接上传，先打开裁剪弹窗（1:1）
const handleAvatarUpload = ({ file }) => {
  releaseCropperSrc()
  cropperImageSrc.value = URL.createObjectURL(file.file)
  cropperVisible.value = true
}

const releaseCropperSrc = () => {
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
    cropperImageSrc.value = ''
  }
}

// 裁剪确认 -> 在裁剪弹窗内上传（由 ImageCropperModal 调用，进度条由弹窗展示）
const handleCropUpload = async (blob, { onProgress } = {}) => {
  const avatarFile = new File([blob], 'agent-avatar.jpg', { type: 'image/jpeg' })
  try {
    return await uploadOne(avatarFile, { onProgress })
  } catch (e) {
    console.error('机器人头像上传失败:', e)
    message.error(t('agent.form.avatarUploadFailed'))
    throw e // rethrow -> 裁剪弹窗保持打开，供重试
  }
}

// 上传成功 -> 回填头像 URL（avatar_url 为空串时后端存 NULL，diff 逻辑不受影响）
const handleCropConfirm = (payload) => {
  if (payload?.url) formData.value.avatar_url = payload.url
  releaseCropperSrc()
}

// 弹窗打开时以当前 agent 初始化表单（编辑）或重置为默认值（创建）
watch(
  () => props.show,
  show => {
    if (!show) return
    const fresh = emptyForm()
    if (props.agent) {
      const a = props.agent
      fresh.name = a.name || ''
      fresh.avatar_url = a.avatar_url || ''
      // 旧协议可读回；只有显式改选支持的协议时才提交凭据变更。
      fresh.api_protocol = a.api_protocol || 'openai'
      fresh.base_url = a.base_url || ''
      fresh.model = a.model || ''
      for (const k of LLM_PARAM_KEYS) {
        fresh.llm[k] = a.llm_params?.[k] ?? null
      }
      fresh.system_prompt = a.system_prompt || ''
      fresh.filter_prompt = a.filter_prompt || ''
      fresh.trigger_mode = a.trigger_mode ?? 2
      fresh.trigger_keywords = [...(a.trigger_keywords || [])]
      fresh.max_replies_per_hour = a.max_replies_per_hour ?? 30
      fresh.min_interval_sec = a.min_interval_sec ?? 60
      fresh.auto_reply = autoReplyFromAgent(a)
      if (!isCircle.value) fresh.auto_post = normalizeAutoPost(a.auto_config?.auto_post ?? {})
    }
    formData.value = fresh
  },
  { immediate: true }
)

const toggleAutoPost = enabled => {
  const post = formData.value.auto_post
  post.enabled = enabled
  if (enabled && !post.interval_min) post.interval_min = 60
  if (enabled && !post.daily_max) post.daily_max = 1
}

// 开关自动回复：首次开启时预填目标与时间窗口（门槛交给运营填写）
const toggleAutoReply = on => {
  formData.value.auto_reply.enabled = on
  if (on) prefillOnEnable(formData.value.auto_reply)
}

// 自动回复表单项校验：复用 validateAutoReply（规则与后端 §2.5 一致），取所给字段的第一条错误
const autoReplyRule = (...fields) => ({
  trigger: ['input', 'blur', 'change'],
  validator: () => {
    const errors = validateAutoReply(formData.value.auto_reply)
    const key = fields.map(f => errors[f]).find(Boolean)
    return key ? new Error(t(key)) : true
  }
})

const rules = computed(() => ({
  name: [
    { required: true, trigger: ['blur', 'input'], message: t('agent.form.namePlaceholder') }
  ],
  api_protocol: [
    { required: true, trigger: ['blur', 'change'], message: t('agent.form.protocol') }
  ],
  model: [
    { required: true, trigger: ['blur', 'input'], message: t('agent.form.modelPlaceholder') }
  ]
}))

// 只把填写了的白名单键组装进 llm_params（值须为数字）
const buildLlmParams = () => {
  const params = {}
  for (const k of LLM_PARAM_KEYS) {
    const v = formData.value.llm[k]
    if (v !== null && v !== undefined && v !== '') params[k] = v
  }
  return params
}

const sortedJson = obj => JSON.stringify(obj, Object.keys(obj).sort())

// 后端分别替换提供的子对象，另一个子对象缺席即保留。
const putAutoConfigChanges = (payload, agent) => {
  const f = formData.value
  const config = autoReplyChanged(f.auto_reply, agent) ? buildAutoConfig(f.auto_reply) : {}
  if (!isCircle.value) {
    const post = normalizeAutoPost(f.auto_post)
    const original = normalizeAutoPost(agent?.auto_config?.auto_post ?? {})
    if (JSON.stringify(post) !== JSON.stringify(original)) config.auto_post = post
  }
  if (Object.keys(config).length) payload.auto_config = config
}

// 创建：全量字段（后端对未传字段有默认值）；api_key 为空不传；
// 圈子作用域额外带 circle_id（编辑时后端以路径 id 定位，无须携带）
const buildCreatePayload = () => {
  const f = formData.value
  const payload = {
    name: f.name.trim(),
    avatar_url: f.avatar_url.trim(),
    api_protocol: f.api_protocol,
    base_url: f.base_url.trim(),
    model: f.model.trim(),
    system_prompt: f.system_prompt,
    filter_prompt: f.filter_prompt.trim(),
    trigger_mode: f.trigger_mode,
    trigger_keywords: [...f.trigger_keywords],
    max_replies_per_hour: f.max_replies_per_hour ?? 0,
    min_interval_sec: f.min_interval_sec ?? 0
  }
  if (f.createDisabled) payload.status = 0
  if (isCircle.value) payload.circle_id = props.circleId
  if (f.api_key.trim()) payload.api_key = f.api_key.trim()
  const llm = buildLlmParams()
  if (Object.keys(llm).length) payload.llm_params = llm
  putAutoConfigChanges(payload, null)
  return payload
}

// 编辑：部分更新，diff 出变更字段（llm_params/trigger_keywords 整体替换语义）
const buildUpdatePayload = () => {
  const f = formData.value
  const a = props.agent
  const payload = {}

  if (f.name.trim() !== a.name) payload.name = f.name.trim()
  if ((f.avatar_url || '').trim() !== (a.avatar_url || '')) payload.avatar_url = f.avatar_url.trim()
  if (f.model.trim() !== a.model) payload.model = f.model.trim()
  if ((f.system_prompt || '') !== (a.system_prompt || '')) payload.system_prompt = f.system_prompt
  // filter_prompt：传空串 = 关闭判定（回到命中即回复），不传 = 保持原条件
  if (f.filter_prompt.trim() !== (a.filter_prompt || '')) payload.filter_prompt = f.filter_prompt.trim()

  // 凭据字段（base_url/api_protocol/api_key）：无权限时整组跳过——
  // 后端对含任一凭据字段的更新请求整体要求圈主，管理员混提即 403
  if (props.canEditCredentials) {
    if ((f.base_url || '').trim() !== (a.base_url || '')) payload.base_url = f.base_url.trim()
    if (f.api_protocol !== (a.api_protocol || 'openai')) payload.api_protocol = f.api_protocol
    // api_key：空串=清除，不传=保持不变
    if (f.clear_api_key) payload.api_key = ''
    else if (f.new_api_key.trim()) payload.api_key = f.new_api_key.trim()
  }

  const llm = buildLlmParams()
  if (sortedJson(llm) !== sortedJson(a.llm_params || {})) payload.llm_params = llm

  const keywordsChanged =
    JSON.stringify([...f.trigger_keywords].sort()) !== JSON.stringify([...(a.trigger_keywords || [])].sort())
  if (f.trigger_mode !== (a.trigger_mode ?? 2)) payload.trigger_mode = f.trigger_mode
  if (f.trigger_mode === 2 && (keywordsChanged || payload.trigger_mode === 2)) {
    payload.trigger_keywords = [...f.trigger_keywords]
  }

  if ((f.max_replies_per_hour ?? 0) !== (a.max_replies_per_hour ?? 30)) {
    payload.max_replies_per_hour = f.max_replies_per_hour ?? 0
  }
  if ((f.min_interval_sec ?? 0) !== (a.min_interval_sec ?? 60)) {
    payload.min_interval_sec = f.min_interval_sec ?? 0
  }

  putAutoConfigChanges(payload, a)

  return payload
}

const handleSubmit = async () => {
  try {
    await formRef.value?.validate()
  } catch {
    return
  }

  const f = formData.value
  // 后端 400：mode 2 requires keywords -- 前端先拦。
  // 创建必填；编辑仅在关键词有变更（将提交 trigger 字段）时才强制非空，
  // 避免遗留 mode=1/3 的机器人只改其他字段时被卡住
  const willSubmitTrigger = !isEdit.value ||
    JSON.stringify([...f.trigger_keywords].sort()) !==
      JSON.stringify([...(props.agent.trigger_keywords || [])].sort())
  if (f.trigger_mode === 2 && (willSubmitTrigger || props.agent?.trigger_mode !== 2) && !f.trigger_keywords.length) {
    message.warning(t('agent.form.keywordsRequired'))
    return
  }

  // 自动回复兜底校验：关闭时字段已折叠、不走表单项校验，这里按完整对象再校验一次
  const configError = Object.values(validateAutoReply(f.auto_reply))[0] ||
    (!isCircle.value && autoPostError(f.auto_post))
  if (configError) {
    message.warning(t(configError))
    return
  }

  try {
    submitting.value = true
    if (isEdit.value) {
      const payload = buildUpdatePayload()
      if (!Object.keys(payload).length) {
        message.warning(t('agent.form.atLeastOne'))
        return
      }
      // 409 撞名 / 400 校验 / 403 凭据越权等：直接透出后端 message
      const res = isCircle.value
        ? await updateCircleAgent(props.agent.id, payload)
        : await updateAgent(props.agent.id, payload)
      message.success(t('agent.form.updateSuccess'))
      emit('success', res.data)
    } else {
      const res = isCircle.value
        ? await createCircleAgent(buildCreatePayload())
        : await createAgent(buildCreatePayload())
      message.success(t('agent.form.createSuccess'))
      emit('success', res.data)
    }
    visible.value = false
  } catch (e) {
    // 409 撞名 / 400 校验等：直接透出后端 message
    message.error(e.message || t('common.operationFailed'))
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
/* ===== 弹窗标题：主题绿渐变文字（沿用 CreateCircleModal 范式；
 * NModal 虽 teleport 到 body，但 scoped data-v 属性随 DOM 一起传送，
 * :deep 仍可命中弹窗内部元素；NSelect 菜单除外（独立容器，走全局样式） ===== */
:deep(.n-card-header__title) {
  font-size: 20px !important;
  font-weight: 700 !important;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

/* ===== 表单滚动区 ===== */
.agent-form {
  max-height: 65vh;
  overflow-y: auto;
  padding-right: 8px;
}

/* ===== 分区面板：玻璃内衬，圆角较弹窗外壳小一档（24 -> 16） ===== */
.form-section {
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  padding: 16px 16px 4px;
  margin-bottom: 14px;
  transition: border-color 0.2s ease;
}

.form-section:hover {
  border-color: rgba(255, 255, 255, 0.14);
}

.form-section:last-of-type {
  margin-bottom: 0;
}

.section-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
}

/* 分区圆点：主题绿 + 柔光 */
.section-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--primary-gradient);
  box-shadow: 0 0 8px rgba(102, 234, 194, 0.6);
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
  letter-spacing: 0.02em;
}

/* ===== 输入 / 选择 / 数字输入：圆润 + 聚焦绿光晕（圆角 12 < 分区 16） ===== */
:deep(.n-input),
:deep(.n-base-selection),
:deep(.n-input-number) {
  --n-border-radius: 12px !important;
}

:deep(.n-input:not(.n-input--textarea)) {
  --n-height: 42px !important;
}

:deep(.n-input__textarea-el) {
  font-size: 14px;
  line-height: 1.7;
}

:deep(.n-input--focus),
:deep(.n-base-selection:focus-within),
:deep(.n-input-number:focus-within) {
  --n-border-focus: 1px solid rgba(102, 234, 194, 0.65) !important;
  --n-box-shadow-focus: 0 0 0 3px rgba(102, 234, 194, 0.15) !important;
}

/* ===== 关键词标签：胶囊 ===== */
:deep(.n-dynamic-tags .n-tag) {
  border-radius: 999px !important;
}

/* ===== 表单项间距收紧（分区已提供外距） ===== */
:deep(.n-form-item) {
  margin-bottom: 14px;
}

/* ===== 提示条：绿色玻璃风 ===== */
:deep(.n-alert) {
  border-radius: 12px !important;
  background: rgba(102, 234, 194, 0.06) !important;
  border: 1px solid rgba(102, 234, 194, 0.18) !important;
}

:deep(.n-alert .n-alert-body__title) {
  color: #8af0d0 !important;
}

/* ===== 局部布局 ===== */
/* 头像行：圆形预览 + 上传/移除按钮 */
.avatar-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.avatar-preview {
  border: 2px solid rgba(255, 255, 255, 0.12);
  background: rgba(255, 255, 255, 0.06);
  flex-shrink: 0;
}

.avatar-fallback {
  font-size: 26px;
}

.api-grid {
  display: grid;
  grid-template-columns: 180px 1fr;
  gap: 12px;
}

.key-block {
  width: 100%;
}

.key-masked {
  font-size: 13px;
}

.key-new-input {
  margin-top: 8px;
}

.key-clear-check {
  margin-top: 8px;
}

.llm-params {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  width: 100%;
}

/* 参数名前缀：弱化色 + 冒号，与数值视觉分离（插槽内容在本组件作用域，scoped 可命中） */
.llm-key {
  color: var(--text-tertiary);
  font-size: 13px;
}

/* 数值：等宽系统字体栈 + 表格数字，数字对齐易读；macOS/Windows/Linux 均有回落 */
:deep(.n-input-number .n-input__input-el) {
  font-family: ui-monospace, 'SF Mono', 'Cascadia Mono', Menlo, Consolas,
    'Liberation Mono', monospace;
  font-variant-numeric: tabular-nums;
}

.rate-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

/* ===== 自动回复分区 ===== */
.auto-switch-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}

.auto-switch-label {
  font-size: 14px;
  color: var(--text-primary);
}

.auto-switch-hint {
  font-size: 12px;
}

.auto-threshold-row {
  width: 100%;
}

.auto-note {
  margin-bottom: 12px;
  font-size: 13px;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}
</style>
