<template>
  <div class="invite-link-panel">
    <!-- 说明 + 生成按钮 -->
    <div class="panel-head">
      <div class="head-text">
        <h3 class="head-title">{{ t('circle.invite.link.title') }}</h3>
        <p class="head-desc">{{ t('circle.invite.link.desc') }}</p>
      </div>
      <NButton type="primary" round class="create-btn" @click="openCreate">
        <template #icon>
          <NIcon><LinkIcon /></NIcon>
        </template>
        {{ t('circle.invite.link.create') }}
      </NButton>
    </div>

    <!-- 链接列表：含已撤销 / 已过期，不返回 token -->
    <div class="link-list">
      <div v-if="loading && links.length === 0" class="list-skeleton">
        <div class="sk-row" v-for="n in 3" :key="n"></div>
      </div>
      <NEmpty
        v-else-if="!loading && links.length === 0"
        :description="loadFailed ? t('circle.invite.link.loadFailed') : t('circle.invite.link.empty')"
        class="list-empty"
      />

      <div
        v-for="link in links"
        :key="link.id"
        class="link-row"
        :class="{ inactive: statusOf(link) !== INVITE_LINK_STATUS.ACTIVE }"
      >
        <div class="link-main">
          <div class="link-line">
            <NTag size="small" round :bordered="false" :type="statusTagType(link)">
              {{ t(`circle.invite.link.status.${statusOf(link)}`) }}
            </NTag>
            <span class="link-expire">{{ t('circle.invite.link.expireAt', { time: formatDateTime(link.expire_time) }) }}</span>
          </div>
          <div class="link-meta">
            <span class="meta-item">{{ usedText(link) }}</span>
            <span class="meta-dot">·</span>
            <span class="meta-item creator">
              <NAvatar round :size="18" :src="link.creator_avatar || undefined">
                <span v-if="!link.creator_avatar" class="avatar-fallback">{{ creatorName(link).charAt(0).toUpperCase() }}</span>
              </NAvatar>
              <span class="creator-name">{{ creatorName(link) }}</span>
            </span>
            <span class="meta-dot">·</span>
            <span class="meta-item">{{ t('circle.invite.link.createdAt', { time: formatTime(link.create_time) }) }}</span>
          </div>
        </div>
        <NButton
          v-if="statusOf(link) === INVITE_LINK_STATUS.ACTIVE"
          size="small"
          quaternary
          round
          type="error"
          class="revoke-btn"
          :loading="revokingId === link.id"
          @click="confirmRevoke(link)"
        >
          {{ t('circle.invite.link.revoke') }}
        </NButton>
      </div>

      <div v-if="total > INVITE_PAGE_SIZE" class="list-pagination">
        <NPagination
          :page="page"
          :page-size="INVITE_PAGE_SIZE"
          :item-count="total"
          :page-slot="5"
          @update:page="changePage"
        />
      </div>
    </div>

    <!-- 生成链接弹窗：表单 → 结果（token 只出现这一次） -->
    <NModal
      v-model:show="modalVisible"
      preset="card"
      class="invite-link-modal"
      :title="created ? t('circle.invite.link.createdTitle') : t('circle.invite.link.createTitle')"
      :style="{ width: 'min(460px, calc(100vw - 32px))' }"
      :mask-closable="!submitting && !created"
      :close-on-esc="!submitting"
      @after-leave="resetModal"
    >
      <!-- 结果：醒目提示只显示一次 + 一键复制 -->
      <div v-if="created" class="created-body">
        <NAlert type="warning" :bordered="false" class="once-alert">
          {{ t('circle.invite.link.onlyOnce') }}
        </NAlert>
        <div class="url-box">
          <NInput :value="createdUrl" readonly class="url-input" @focus="selectAll" />
          <NButton type="primary" round @click="copyLink">
            <template #icon>
              <NIcon><CopyIcon /></NIcon>
            </template>
            {{ t('circle.invite.link.copy') }}
          </NButton>
        </div>
        <p class="created-summary">
          {{ t('circle.invite.link.expireAt', { time: formatDateTime(created.expire_time) }) }}
          ·
          {{ created.max_uses != null
            ? t('circle.invite.link.usesLimit', { n: created.max_uses })
            : t('circle.invite.link.usesUnlimited') }}
        </p>
      </div>

      <!-- 表单：有效期（预设 + 自定义小时）+ 次数上限（留空不限） -->
      <div v-else class="create-body">
        <div class="field-label">{{ t('circle.invite.link.expire') }}</div>
        <div class="expire-presets">
          <NButton
            v-for="p in expirePresets"
            :key="p.hours"
            size="small"
            round
            :type="expireHours === p.hours ? 'primary' : 'default'"
            :secondary="expireHours === p.hours"
            @click="expireHours = p.hours"
          >
            {{ p.label }}
          </NButton>
        </div>
        <NInputNumber
          v-model:value="expireHours"
          :min="INVITE_EXPIRE_MIN_HOURS"
          :max="INVITE_EXPIRE_MAX_HOURS"
          :precision="0"
          :show-button="false"
          class="field-input"
        >
          <template #suffix>{{ t('circle.invite.link.hoursUnit') }}</template>
        </NInputNumber>
        <p class="field-hint">{{ t('circle.invite.link.expireHint') }}</p>

        <div class="field-label">{{ t('circle.invite.link.maxUses') }}</div>
        <NInputNumber
          v-model:value="maxUses"
          :min="INVITE_MAX_USES_MIN"
          :max="INVITE_MAX_USES_MAX"
          :precision="0"
          :show-button="false"
          clearable
          :placeholder="t('circle.invite.link.maxUsesPlaceholder')"
          class="field-input"
        />
        <p class="field-hint">{{ t('circle.invite.link.maxUsesHint') }}</p>
      </div>

      <template #footer>
        <div class="modal-footer">
          <template v-if="created">
            <NButton type="primary" round @click="modalVisible = false">
              {{ t('circle.invite.link.done') }}
            </NButton>
          </template>
          <template v-else>
            <NButton quaternary round :disabled="submitting" @click="modalVisible = false">
              {{ t('common.cancel') }}
            </NButton>
            <NButton type="primary" round :loading="submitting" @click="submitCreate">
              {{ t('circle.invite.link.create') }}
            </NButton>
          </template>
        </div>
      </template>
    </NModal>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import {
  NAlert, NAvatar, NButton, NEmpty, NIcon, NInput, NInputNumber,
  NModal, NPagination, NTag, useDialog, useMessage
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Link as LinkIcon, Copy as CopyIcon } from '@vicons/tabler'
import { createInviteLink, getInviteLinks, revokeInviteLink } from '@/api/circleInvite'
import { useFormatTime } from '@/utils/i18n'
import { isForbiddenError } from '@/constants/circle'
import { copyText } from '@/utils/share'
import {
  INVITE_EXPIRE_DEFAULT_HOURS, INVITE_EXPIRE_MIN_HOURS, INVITE_EXPIRE_MAX_HOURS,
  INVITE_MAX_USES_MIN, INVITE_MAX_USES_MAX, INVITE_PAGE_SIZE, INVITE_LINK_STATUS,
  inviteLinkStatus, inviteUrl
} from '@/constants/circleInvite'

const props = defineProps({
  circleId: {
    type: String,
    required: true
  }
})

// 403：管理身份可能已变更，交给页面重查角色
const emit = defineEmits(['forbidden'])

const { t, locale } = useI18n()
const message = useMessage()
const dialog = useDialog()
const { formatTime } = useFormatTime()

// ---------- 列表 ----------

const links = ref([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const loadFailed = ref(false)
const revokingId = ref('')
let gen = 0

const loadLinks = async () => {
  const myGen = ++gen
  loading.value = true
  loadFailed.value = false
  try {
    const res = await getInviteLinks({ circle_id: props.circleId, page: page.value, size: INVITE_PAGE_SIZE })
    if (myGen !== gen) return
    links.value = Array.isArray(res.data) ? res.data : []
    total.value = res.total || 0
    // 撤销后末页可能清空：回退一页
    if (links.value.length === 0 && page.value > 1 && total.value > 0) {
      page.value = Math.ceil(total.value / INVITE_PAGE_SIZE)
      loadLinks()
    }
  } catch (error) {
    if (myGen !== gen) return
    loadFailed.value = true
    // 列表自身失败只显示空态里的「加载失败」，不走操作类错误处理：
    // 那里 404 会刷新列表，列表持续 404 时会无限重试。403 仍交给页面重查角色
    if (isForbiddenError(error)) handleError(error)
  } finally {
    if (myGen === gen) loading.value = false
  }
}

const changePage = (p) => {
  page.value = p
  loadLinks()
}

const statusOf = (link) => inviteLinkStatus(link)
const STATUS_TAG = {
  [INVITE_LINK_STATUS.ACTIVE]: 'success',
  [INVITE_LINK_STATUS.REVOKED]: 'error',
  [INVITE_LINK_STATUS.EXPIRED]: 'default',
  [INVITE_LINK_STATUS.USED_UP]: 'warning'
}
const statusTagType = (link) => STATUS_TAG[statusOf(link)]

const usedText = (link) => (link.max_uses != null
  ? t('circle.invite.link.used', { used: link.used_count || 0, max: link.max_uses })
  : t('circle.invite.link.usedUnlimited', { used: link.used_count || 0 }))

const creatorName = (link) => link.creator_name || t('circle.invite.link.unknownCreator')

// 有效期是将来时间：展示具体日期时刻，跨年时补年份
const formatDateTime = (time) => {
  if (!time) return ''
  const date = new Date(time)
  const options = { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }
  if (date.getFullYear() !== new Date().getFullYear()) options.year = 'numeric'
  return date.toLocaleString(locale.value, options)
}

// 撤销 / 生成的错误处理：按 code / HTTP 状态给自有文案（203↔403、204↔404、201↔400）
const handleError = (error) => {
  const code = error?.code
  const status = error?.status
  if (code === 203 || status === 403) {
    message.error(t('circle.manage.error.noPermission'))
    emit('forbidden')
    return
  }
  if (code === 204 || status === 404) {
    message.warning(t('circle.invite.link.error.notFound'))
    loadLinks()
    return
  }
  if (code === 201 || status === 400) {
    // 400 两种：非私密圈 / 参数越界（前端已限制范围，越界基本只会是圈子刚改成公开）
    message.error(/private/i.test(error?.message || '')
      ? t('circle.invite.link.error.notPrivate')
      : t('circle.invite.link.error.badRequest'))
    return
  }
  message.error(t('circle.invite.link.error.generic'))
}

// ---------- 撤销 ----------

const confirmRevoke = (link) => {
  dialog.warning({
    title: t('circle.invite.link.revokeTitle'),
    content: t('circle.invite.link.revokeContent'),
    positiveText: t('circle.invite.link.revoke'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      revokingId.value = link.id
      try {
        await revokeInviteLink({ link_id: link.id })
        message.success(t('circle.invite.link.revoked'))
        await loadLinks()
      } catch (error) {
        handleError(error)
      } finally {
        revokingId.value = ''
      }
    }
  })
}

// ---------- 生成 ----------

const modalVisible = ref(false)
const submitting = ref(false)
const expireHours = ref(INVITE_EXPIRE_DEFAULT_HOURS)
const maxUses = ref(null)
// 生成结果：{ id, token, expire_time, max_uses }
const created = ref(null)
const createdUrl = computed(() => (created.value ? inviteUrl(created.value.token) : ''))

const expirePresets = computed(() => [1, 7, 30].map(n => ({
  hours: n * 24,
  label: t('circle.invite.link.expireDays', { n })
})))

const openCreate = () => {
  resetModal()
  modalVisible.value = true
}

const resetModal = () => {
  created.value = null
  expireHours.value = INVITE_EXPIRE_DEFAULT_HOURS
  maxUses.value = null
}

const submitCreate = async () => {
  const hours = Number(expireHours.value)
  if (!Number.isInteger(hours) || hours < INVITE_EXPIRE_MIN_HOURS || hours > INVITE_EXPIRE_MAX_HOURS) {
    message.warning(t('circle.invite.link.expireHint'))
    return
  }
  const uses = maxUses.value
  if (uses != null && (!Number.isInteger(uses) || uses < INVITE_MAX_USES_MIN || uses > INVITE_MAX_USES_MAX)) {
    message.warning(t('circle.invite.link.maxUsesHint'))
    return
  }
  submitting.value = true
  try {
    const res = await createInviteLink({
      circle_id: props.circleId,
      expire_hours: hours,
      ...(uses != null ? { max_uses: uses } : {})
    })
    created.value = res.data
    // 新链接排在第一页最前
    page.value = 1
    loadLinks()
  } catch (error) {
    handleError(error)
  } finally {
    submitting.value = false
  }
}

const copyLink = async () => {
  if (await copyText(createdUrl.value)) {
    message.success(t('circle.invite.link.copied'))
  } else {
    message.error(t('circle.invite.link.copyFailed'))
  }
}

const selectAll = (e) => e?.target?.select?.()

onMounted(loadLinks)

watch(() => props.circleId, () => {
  links.value = []
  total.value = 0
  page.value = 1
  loadLinks()
})
</script>

<style scoped>
.panel-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 20px 0 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.head-title {
  margin: 0 0 4px;
  font-size: 1.05rem;
  font-weight: 600;
  color: var(--text-primary, #fff);
}

.head-desc {
  margin: 0;
  font-size: 0.85rem;
  line-height: 1.5;
  color: var(--text-secondary);
}

.create-btn {
  flex-shrink: 0;
}

.link-list {
  padding-top: 8px;
}

.link-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 4px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.link-row:last-of-type {
  border-bottom: none;
}

.link-row.inactive .link-expire,
.link-row.inactive .link-meta {
  opacity: 0.6;
}

.link-main {
  flex: 1;
  min-width: 0;
}

.link-line {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}

.link-expire {
  font-size: 0.92rem;
  font-weight: 500;
  color: var(--text-primary, #fff);
}

.link-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 6px;
  font-size: 0.8rem;
  color: var(--text-tertiary);
}

.meta-item.creator {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.creator-name {
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.avatar-fallback {
  font-size: 0.65rem;
}

.revoke-btn {
  flex-shrink: 0;
}

.list-empty {
  padding: 40px 0;
}

.list-pagination {
  display: flex;
  justify-content: center;
  padding-top: 16px;
}

.list-skeleton .sk-row {
  height: 56px;
  margin: 10px 0;
  border-radius: 10px;
  background: linear-gradient(90deg, rgba(255, 255, 255, 0.04) 25%, rgba(255, 255, 255, 0.08) 37%, rgba(255, 255, 255, 0.04) 63%);
  background-size: 400% 100%;
  animation: sk-shimmer 1.4s ease infinite;
}

@keyframes sk-shimmer {
  0% { background-position: 100% 50%; }
  100% { background-position: 0 50%; }
}

/* 弹窗内容：元素出自本组件模板，传送到 body 后仍带 scoped 属性，可直接命中（不经 :deep） */
.field-label {
  margin: 4px 0 8px;
  font-size: 0.85rem;
  font-weight: 500;
  color: var(--text-secondary);
}

.expire-presets {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 10px;
}

.field-input {
  width: 100%;
}

.field-hint {
  margin: 6px 0 16px;
  font-size: 0.78rem;
  color: var(--text-tertiary);
}

.once-alert {
  margin-bottom: 14px;
  font-weight: 500;
}

.url-box {
  display: flex;
  gap: 8px;
}

.url-input {
  flex: 1;
  min-width: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.created-summary {
  margin: 10px 0 0;
  font-size: 0.8rem;
  color: var(--text-tertiary);
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

@media (max-width: 640px) {
  .panel-head {
    flex-direction: column;
    align-items: stretch;
  }

  .link-row {
    align-items: flex-start;
  }

  .url-box {
    flex-direction: column;
  }
}
</style>
