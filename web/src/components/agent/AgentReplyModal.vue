<template>
  <NModal :show="true" preset="card" style="width: min(460px, 92vw)"
    :title="t('agent.manualReply.title', { name: agent.name })"
    :closable="!busy" :mask-closable="!busy" :close-on-esc="!busy"
    @update:show="show => { if (!show && !busy) emit('close') }">
    <NFormItem :label="t('agent.manualReply.target')">
      <NInput v-model:value="target" :disabled="busy || finished" @keydown.enter.prevent="submit" />
    </NFormItem>
    <NText depth="3">{{ circle ? t('agent.manualReply.circleHint') : t('agent.manualReply.globalHint') }}</NText>
    <p v-if="feedback" role="status">{{ t(feedback) }}</p>
    <SmartLink v-if="finished" class="reply-link" :to="{ name: 'post-detail', params: { id: postId }, query: commentId ? { comment_id: commentId } : {} }">
      {{ commentId ? t('agent.manualReply.viewComment') : t('agent.manualReply.checkPost') }}
    </SmartLink>
    <template #footer>
      <NSpace justify="end">
        <NButton :disabled="busy" @click="emit('close')">{{ t('agent.manualReply.close') }}</NButton>
        <NButton v-if="!finished" type="primary" :loading="busy" :disabled="busy || !target.trim()" @click="submit">
          {{ t('agent.manualReply.action') }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<script setup>
import { computed, ref } from 'vue'
import { NButton, NFormItem, NInput, NModal, NSpace, NText } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import SmartLink from '@/components/common/SmartLink.vue'
import { triggerAgentReply } from '@/api/agent'
import { triggerCircleAgentReply } from '@/api/circle'
import {
  isBadRequestError, isConflictError, isForbiddenError, isNetworkError, isNotFoundError,
  isRateLimitedError, isServiceUnavailableError
} from '@/utils/apiError'

const props = defineProps({ agent: { type: Object, required: true }, circle: Boolean })
const emit = defineEmits(['close'])
const { t } = useI18n()
const busy = ref(false)
const target = ref('')
const feedback = ref('')
const commentId = ref('')
const uncertain = ref(false)
const finished = computed(() => !!commentId.value || uncertain.value)
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

// 分享链接可带 comment_id 等查询参数，只将帖子 UUID 送给本机配置的 API。
const postId = computed(() => {
  const input = target.value.trim()
  if (uuid.test(input)) return input
  try {
    const url = new URL(input, window.location.origin)
    if (!['https:', 'http:'].includes(url.protocol)) return ''
    const id = url.pathname.match(/^\/post\/([^/]+)\/?$/)?.[1]
    return id && uuid.test(id) ? id : ''
  } catch {
    return ''
  }
})

const submit = async () => {
  if (busy.value || finished.value) return
  if (!postId.value) {
    feedback.value = 'agent.manualReply.invalid'
    return
  }
  busy.value = true
  feedback.value = ''
  try {
    const trigger = props.circle ? triggerCircleAgentReply : triggerAgentReply
    const { data } = await trigger(props.agent.id, postId.value)
    if (typeof data === 'string' && uuid.test(data)) {
      commentId.value = data
      feedback.value = 'agent.manualReply.success'
    } else {
      uncertain.value = true
      feedback.value = 'agent.manualReply.uncertain'
    }
  } catch (error) {
    if (isNetworkError(error) || [502, 504].includes(error.status)) {
      uncertain.value = true
      feedback.value = 'agent.manualReply.uncertain'
    } else if (isBadRequestError(error)) feedback.value = 'agent.manualReply.unavailable'
    else if (isForbiddenError(error)) feedback.value = 'agent.manualReply.forbidden'
    else if (isNotFoundError(error)) feedback.value = 'agent.manualReply.notFound'
    else if (isConflictError(error)) feedback.value = 'agent.manualReply.conflict'
    else if (isRateLimitedError(error)) feedback.value = 'agent.manualReply.rateLimit'
    else if (isServiceUnavailableError(error)) feedback.value = 'agent.manualReply.failed'
    else feedback.value = 'common.operationFailed'
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.reply-link {
  color: var(--theme-color);
  text-decoration: underline;
}
</style>
