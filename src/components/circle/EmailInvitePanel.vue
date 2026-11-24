<template>
  <div class="email-invite-panel">
    <!-- 说明 + 批量录入 -->
    <div class="panel-head">
      <h3 class="head-title">{{ t('circle.invite.email.title') }}</h3>
      <p class="head-desc">{{ t('circle.invite.email.desc') }}</p>
    </div>

    <div class="add-box">
      <NInput
        v-model:value="emailText"
        type="textarea"
        class="email-input"
        :placeholder="t('circle.invite.email.placeholder')"
        :autosize="{ minRows: 3, maxRows: 8 }"
        :disabled="adding"
      />
      <div class="add-actions">
        <span class="email-count" :class="{ over: emailCount > EMAIL_INVITE_MAX_BATCH }">
          {{ t('circle.invite.email.count', { n: emailCount, max: EMAIL_INVITE_MAX_BATCH }) }}
        </span>
        <NButton type="primary" round class="add-btn" :loading="adding" @click="submitAdd">
          <template #icon>
            <NIcon><MailIcon /></NIcon>
          </template>
          {{ t('circle.invite.email.add') }}
        </NButton>
      </div>

      <!-- 添加结果：新增数 + 被忽略的邮箱按原因分组 -->
      <div v-if="addResult" class="add-result">
        <div class="result-line" :class="{ none: addResult.added.length === 0 }">
          {{ addResult.added.length > 0
            ? t('circle.invite.email.addedCount', { n: addResult.added.length })
            : t('circle.invite.email.noneAdded') }}
        </div>
        <template v-if="ignoredGroups.length > 0">
          <div class="ignored-title">{{ t('circle.invite.email.ignoredTitle') }}</div>
          <div v-for="group in ignoredGroups" :key="group.reason" class="ignored-group">
            <span class="ignored-reason">{{ t(`circle.invite.email.ignored.${group.reason}`) }}</span>
            <div class="ignored-emails">
              <NTag
                v-for="(email, i) in group.emails"
                :key="`${group.reason}-${i}`"
                size="small"
                round
                :bordered="false"
                :type="group.reason === 'invalid' ? 'error' : 'warning'"
              >
                {{ email }}
              </NTag>
            </div>
          </div>
        </template>
      </div>
    </div>

    <!-- 状态过滤 -->
    <div class="filter-row">
      <button
        v-for="f in filters"
        :key="f.key"
        type="button"
        class="filter-item"
        :class="{ active: statusFilter === f.status }"
        @click="changeFilter(f.status)"
      >
        {{ t(`circle.invite.email.filter.${f.key}`) }}
      </button>
    </div>

    <!-- 邀请列表 -->
    <div class="invite-list">
      <div v-if="loading && invites.length === 0" class="list-skeleton">
        <div class="sk-row" v-for="n in 3" :key="n"></div>
      </div>
      <NEmpty
        v-else-if="!loading && invites.length === 0"
        :description="loadFailed
          ? t('circle.invite.email.loadFailed')
          : (statusFilter === ALL ? t('circle.invite.email.empty') : t('circle.invite.email.emptyFiltered'))"
        class="list-empty"
      />

      <div v-for="item in invites" :key="item.id" class="invite-row">
        <div class="invite-main">
          <div class="invite-line">
            <span class="invite-email">{{ item.email }}</span>
            <NTag size="small" round :bordered="false" :type="STATUS_TAG[item.status] || 'default'">
              {{ t(`circle.invite.email.filter.${EMAIL_INVITE_STATUS_KEY[item.status] || 'pending'}`) }}
            </NTag>
          </div>
          <div class="invite-meta">
            <span>{{ t('circle.invite.email.invitedBy', { name: item.inviter_name || t('circle.invite.email.unknownInviter') }) }}</span>
            <span class="meta-dot">·</span>
            <span>{{ t('circle.invite.email.createdAt', { time: formatTime(item.create_time) }) }}</span>
            <template v-if="item.status !== EMAIL_INVITE_STATUS.PENDING && item.update_time && item.update_time !== item.create_time">
              <span class="meta-dot">·</span>
              <span>{{ t('circle.invite.email.updatedAt', { time: formatTime(item.update_time) }) }}</span>
            </template>
          </div>
        </div>
        <NButton
          v-if="item.status === EMAIL_INVITE_STATUS.PENDING"
          size="small"
          quaternary
          round
          type="error"
          class="revoke-btn"
          :loading="revokingId === item.id"
          @click="confirmRevoke(item)"
        >
          {{ t('circle.invite.email.revoke') }}
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
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { NButton, NEmpty, NIcon, NInput, NPagination, NTag, useDialog, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Mail as MailIcon } from '@vicons/tabler'
import { addEmailInvites, getEmailInvites, revokeEmailInvite } from '@/api/circleInvite'
import { useFormatTime } from '@/utils/i18n'
import { isForbiddenError } from '@/constants/circle'
import {
  EMAIL_INVITE_MAX_BATCH, EMAIL_INVITE_STATUS, EMAIL_INVITE_STATUS_KEY, INVITE_PAGE_SIZE,
  parseEmailInput, groupIgnoredEmails, isInviteHandledError
} from '@/constants/circleInvite'

const props = defineProps({
  circleId: {
    type: String,
    required: true
  }
})

// 403：管理身份可能已变更，交给页面重查角色
const emit = defineEmits(['forbidden'])

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const { formatTime } = useFormatTime()

// 全部 = -1（后端默认值）
const ALL = -1
const filters = [
  { key: 'all', status: ALL },
  { key: 'pending', status: EMAIL_INVITE_STATUS.PENDING },
  { key: 'accepted', status: EMAIL_INVITE_STATUS.ACCEPTED },
  { key: 'rejected', status: EMAIL_INVITE_STATUS.REJECTED },
  { key: 'revoked', status: EMAIL_INVITE_STATUS.REVOKED }
]

const STATUS_TAG = {
  [EMAIL_INVITE_STATUS.PENDING]: 'info',
  [EMAIL_INVITE_STATUS.ACCEPTED]: 'success',
  [EMAIL_INVITE_STATUS.REVOKED]: 'default',
  [EMAIL_INVITE_STATUS.REJECTED]: 'warning'
}

// 添加 / 撤销的错误处理：按 code / HTTP 状态给自有文案（203↔403、204↔404、201↔400、207↔409）
const handleError = (error) => {
  const code = error?.code
  const status = error?.status
  if (isInviteHandledError(error)) {
    message.warning(t('circle.invite.email.error.handled'))
    loadInvites()
    return
  }
  if (code === 203 || status === 403) {
    message.error(t('circle.manage.error.noPermission'))
    emit('forbidden')
    return
  }
  if (code === 204 || status === 404) {
    message.warning(t('circle.invite.email.error.notFound'))
    loadInvites()
    return
  }
  if (code === 201 || status === 400) {
    message.error(/private/i.test(error?.message || '')
      ? t('circle.invite.email.error.notPrivate')
      : t('circle.invite.email.error.badRequest'))
    return
  }
  message.error(t('circle.invite.email.error.generic'))
}

// ---------- 列表 ----------

const invites = ref([])
const total = ref(0)
const page = ref(1)
const statusFilter = ref(ALL)
const loading = ref(false)
const loadFailed = ref(false)
const revokingId = ref('')
let gen = 0

const loadInvites = async () => {
  const myGen = ++gen
  loading.value = true
  loadFailed.value = false
  try {
    const params = { circle_id: props.circleId, page: page.value, size: INVITE_PAGE_SIZE }
    if (statusFilter.value !== ALL) params.status = statusFilter.value
    const res = await getEmailInvites(params)
    if (myGen !== gen) return
    invites.value = Array.isArray(res.data) ? res.data : []
    total.value = res.total || 0
    // 撤销后末页可能清空：回退到最后一页
    if (invites.value.length === 0 && page.value > 1 && total.value > 0) {
      page.value = Math.ceil(total.value / INVITE_PAGE_SIZE)
      loadInvites()
    }
  } catch (error) {
    if (myGen !== gen) return
    invites.value = []
    loadFailed.value = true
    // 列表自身失败只显示空态里的「加载失败」，不走操作类错误处理：
    // 那里 404 / 409 会刷新列表，列表持续失败时会无限重试。403 仍交给页面重查角色
    if (isForbiddenError(error)) handleError(error)
  } finally {
    if (myGen === gen) loading.value = false
  }
}

const changeFilter = (status) => {
  if (statusFilter.value === status) return
  statusFilter.value = status
  page.value = 1
  invites.value = []
  loadInvites()
}

const changePage = (p) => {
  page.value = p
  loadInvites()
}

// ---------- 添加 ----------

const emailText = ref('')
const adding = ref(false)
let addVersion = 0

// 旧请求仍可能在服务端完成；切圈或离开面板后不再处理它的返回。
const resetAdd = () => {
  addVersion++
  adding.value = false
}
// 最近一次添加的结果：{ added, ignored }
const addResult = ref(null)
const emailCount = computed(() => parseEmailInput(emailText.value).length)
const ignoredGroups = computed(() => groupIgnoredEmails(addResult.value?.ignored))

const submitAdd = async () => {
  if (adding.value) return
  const emails = parseEmailInput(emailText.value)
  if (emails.length === 0) {
    message.warning(t('circle.invite.email.emptyInput'))
    return
  }
  if (emails.length > EMAIL_INVITE_MAX_BATCH) {
    message.warning(t('circle.invite.email.tooMany', { n: emails.length, max: EMAIL_INVITE_MAX_BATCH }))
    return
  }
  const version = addVersion
  adding.value = true
  try {
    const res = await addEmailInvites({ circle_id: props.circleId, emails })
    if (version !== addVersion) return
    addResult.value = {
      added: res.data?.added || [],
      ignored: res.data?.ignored || []
    }
    // 全部处理完（含被忽略）后清空输入，结果区保留被忽略的原文便于修改重提
    emailText.value = ''
    if (addResult.value.added.length > 0) {
      message.success(t('circle.invite.email.addedCount', { n: addResult.value.added.length }))
      page.value = 1
      loadInvites()
    }
  } catch (error) {
    if (version === addVersion) handleError(error)
  } finally {
    if (version === addVersion) adding.value = false
  }
}

// ---------- 撤销 ----------

const confirmRevoke = (item) => {
  dialog.warning({
    title: t('circle.invite.email.revokeTitle'),
    content: t('circle.invite.email.revokeContent', { email: item.email }),
    positiveText: t('circle.invite.email.revoke'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      revokingId.value = item.id
      try {
        await revokeEmailInvite({ invite_id: item.id })
        message.success(t('circle.invite.email.revoked'))
        await loadInvites()
      } catch (error) {
        // 409：对方已接受 / 拒绝，提示「该邀请已处理」并刷新
        handleError(error)
      } finally {
        revokingId.value = ''
      }
    }
  })
}

onMounted(loadInvites)
onBeforeUnmount(resetAdd)

watch(() => props.circleId, () => {
  resetAdd()
  invites.value = []
  total.value = 0
  page.value = 1
  statusFilter.value = ALL
  addResult.value = null
  emailText.value = ''
  loadInvites()
})
</script>

<style scoped>
.panel-head {
  padding: 20px 0 12px;
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

.add-box {
  padding-bottom: 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.email-input {
  --n-border-radius: 12px !important;
}

.add-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 10px;
}

.email-count {
  font-size: 0.8rem;
  color: var(--text-tertiary);
  font-variant-numeric: tabular-nums;
}

.email-count.over {
  color: #ff7a7a;
}

.add-result {
  margin-top: 12px;
  padding: 12px 14px;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.06);
}

.result-line {
  font-size: 0.9rem;
  font-weight: 500;
  color: #8af0d0;
}

.result-line.none {
  color: var(--text-secondary);
}

.ignored-title {
  margin: 10px 0 6px;
  font-size: 0.8rem;
  color: var(--text-secondary);
}

.ignored-group {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-top: 6px;
}

.ignored-reason {
  flex-shrink: 0;
  min-width: 96px;
  padding-top: 2px;
  font-size: 0.8rem;
  color: var(--text-tertiary);
}

.ignored-emails {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}

.filter-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 14px 0 6px;
}

.filter-item {
  padding: 5px 12px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 999px;
  background: transparent;
  color: rgba(255, 255, 255, 0.6);
  font-size: 0.8rem;
  cursor: pointer;
  transition: background 0.2s cubic-bezier(0.4, 0, 0.2, 1), color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.filter-item:hover {
  background: rgba(102, 234, 194, 0.14);
  color: #8af0d0;
}

.filter-item.active {
  border-color: rgba(102, 234, 194, 0.3);
  background: rgba(102, 234, 194, 0.18);
  color: #8af0d0;
  font-weight: 600;
}

.invite-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 4px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.invite-row:last-of-type {
  border-bottom: none;
}

.invite-main {
  flex: 1;
  min-width: 0;
}

.invite-line {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
  min-width: 0;
}

.invite-email {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.92rem;
  font-weight: 500;
  color: var(--text-primary, #fff);
}

.invite-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 6px;
  font-size: 0.8rem;
  color: var(--text-tertiary);
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

@media (max-width: 640px) {
  .add-btn {
    flex: 1;
  }

  .ignored-group {
    flex-direction: column;
    gap: 4px;
  }

  .invite-row {
    align-items: flex-start;
  }
}
</style>
