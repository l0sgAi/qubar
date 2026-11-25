<template>
  <div class="my-invites-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
    <AppShell v-model:offset="offset" />

    <div class="main-content" :style="contentStyle">
      <div class="invites-wrap">
        <div v-if="!mobileShell" class="page-head">
          <h2 class="page-title">{{ t('circle.invite.mine.title') }}</h2>
          <p class="page-desc">{{ t('circle.invite.mine.desc') }}</p>
        </div>
        <p v-else class="page-desc mobile-desc">{{ t('circle.invite.mine.desc') }}</p>

        <div class="invites-panel">
          <div v-if="loading && invites.length === 0" class="list-skeleton">
            <div class="sk-row" v-for="n in 3" :key="n"></div>
          </div>

          <div v-else-if="loadFailed && invites.length === 0" class="list-state">
            <p>{{ t('circle.invite.mine.loadFailed') }}</p>
            <NButton round @click="loadInvites">{{ t('circle.invite.mine.retry') }}</NButton>
          </div>

          <NEmpty
            v-else-if="invites.length === 0"
            :description="t('circle.invite.mine.empty')"
            class="list-empty"
          >
            <template #icon>
              <NIcon><InboxOutlined /></NIcon>
            </template>
          </NEmpty>

          <div v-for="item in invites" :key="item.invite_id" class="invite-item">
            <SmartLink :to="`/circle/${item.circle_id}`" class="circle-link">
              <NAvatar round :size="48" :src="item.circle_avatar || undefined">
                <span v-if="!item.circle_avatar" class="avatar-fallback">
                  {{ (item.circle_name || '?').charAt(0).toUpperCase() }}
                </span>
              </NAvatar>
              <div class="item-text">
                <div class="item-name">{{ item.circle_name }}</div>
                <div class="item-meta">
                  {{ t('circle.invite.mine.invitedBy', { name: item.inviter_name || t('circle.invite.mine.unknownInviter') }) }}
                  · {{ formatTime(item.create_time) }}
                </div>
              </div>
            </SmartLink>
            <div class="item-actions">
              <NButton
                size="small"
                round
                quaternary
                class="reject-btn"
                :disabled="!!actingId"
                @click="confirmReject(item)"
              >
                {{ t('circle.invite.mine.reject') }}
              </NButton>
              <NButton
                size="small"
                round
                type="primary"
                class="accept-btn"
                :loading="actingId === item.invite_id && acting === 'accept'"
                :disabled="!!actingId"
                @click="accept(item)"
              >
                {{ t('circle.invite.mine.accept') }}
              </NButton>
            </div>
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
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { NAvatar, NButton, NEmpty, NIcon, NPagination, useDialog, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { InboxOutlined } from '@vicons/antd'
import AppShell from '@/components/layout/AppShell.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import { getMyInvites, respondEmailInvite } from '@/api/circleInvite'
import { useAppShell } from '@/composables/useAppShell'
import { getInviteSession, usePendingInvites } from '@/composables/usePendingInvites'
import { MEMBERSHIP_CHANGED_EVENT } from '@/composables/useSideNavCircles'
import { useFormatTime } from '@/utils/i18n'
import { INVITE_PAGE_SIZE, respondInviteErrorKey, isInviteInvalidError } from '@/constants/circleInvite'

const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const { t } = useI18n()
const { formatTime } = useFormatTime()
const { offset, mobileShell, contentStyle } = useAppShell()
const { beginPendingInvitesRead, setPendingInvites, refreshPendingInvites } = usePendingInvites()

const invites = ref([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const loadFailed = ref(false)
const actingId = ref('')
const acting = ref('')
let gen = 0
let viewVersion = 0
let listSession = null
const ownsList = () => listSession !== null && listSession === getInviteSession()

const loadInvites = async () => {
  const myGen = ++gen
  const countVersion = beginPendingInvitesRead()
  loading.value = true
  loadFailed.value = false
  try {
    const res = await getMyInvites({ page: page.value, size: INVITE_PAGE_SIZE })
    if (myGen !== gen || !ownsList()) return
    invites.value = Array.isArray(res.data) ? res.data : []
    total.value = res.total || 0
    // 列表只含待接受邀请：总数直接同步给侧栏 / TabBar 角标
    setPendingInvites(total.value, countVersion)
    if (invites.value.length === 0 && page.value > 1 && total.value > 0) {
      page.value = Math.ceil(total.value / INVITE_PAGE_SIZE)
      loadInvites()
    }
  } catch {
    if (myGen !== gen || !ownsList()) return
    loadFailed.value = true
  } finally {
    if (myGen === gen) loading.value = false
  }
}

const changePage = (p) => {
  page.value = p
  loadInvites()
}

const handleRespondError = (error) => {
  message.error(t(respondInviteErrorKey(error)))
  // 404：邀请已失效（撤销 / 圈子不可用等），刷新列表；403 被拉黑时邀请保持待接受，不刷新
  if (isInviteInvalidError(error)) loadInvites()
}

const respond = async (item, accepted, version) => {
  if (version !== viewVersion || actingId.value || !ownsList()) return
  const session = listSession
  actingId.value = item.invite_id
  acting.value = accepted ? 'accept' : 'reject'
  try {
    const res = await respondEmailInvite({ invite_id: item.invite_id, accept: accepted })
    if (session !== getInviteSession()) return
    const { circle_id: circleId, already_member: alreadyMember } = res.data || {}
    if (accepted && !alreadyMember) window.dispatchEvent(new CustomEvent(MEMBERSHIP_CHANGED_EVENT))
    // 已离开页面时仍同步同账号的角标，但不跳转、不重置后来页面的状态。
    if (version !== viewVersion) {
      refreshPendingInvites({ force: true })
      return
    }
    if (accepted) {
      ++gen
      if (alreadyMember) message.info(t('circle.invite.mine.alreadyMember'))
      else message.success(t('circle.invite.mine.accepted', { name: item.circle_name || '' }))
      refreshPendingInvites({ force: true })
      router.push(`/circle/${circleId || item.circle_id}`)
    } else {
      message.success(t('circle.invite.mine.rejected'))
      await loadInvites()
    }
  } catch (error) {
    if (version === viewVersion && ownsList()) handleRespondError(error)
  } finally {
    if (version === viewVersion) {
      actingId.value = ''
      acting.value = ''
    }
  }
}

const accept = (item) => respond(item, true, viewVersion)
const confirmReject = (item) => {
  const version = viewVersion
  dialog.warning({
    title: t('circle.invite.mine.rejectTitle'),
    content: t('circle.invite.mine.rejectContent', { name: item.circle_name || '' }),
    positiveText: t('circle.invite.mine.reject'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => respond(item, false, version)
  })
}

watch(getInviteSession, (session) => {
  listSession = session
  ++gen
  ++viewVersion
  invites.value = []
  total.value = 0
  page.value = 1
  loading.value = false
  loadFailed.value = false
  actingId.value = ''
  acting.value = ''
  if (session) loadInvites()
}, { immediate: true, flush: 'sync' })

onBeforeUnmount(() => {
  ++gen
  ++viewVersion
})
</script>

<style scoped>
.my-invites-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
  padding: 24px;
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.invites-wrap {
  max-width: 760px;
  margin: 0 auto;
}

.page-head {
  margin: 12px 0 16px;
}

.page-title {
  margin: 0 0 6px;
  font-size: 1.4rem;
  font-weight: 700;
  color: var(--text-primary, #fff);
}

.page-desc {
  margin: 0;
  font-size: 0.88rem;
  line-height: 1.5;
  color: var(--text-secondary);
}

.mobile-desc {
  margin: 4px 4px 12px;
  font-size: 0.82rem;
}

.invites-panel {
  padding: 6px 20px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
}

.invite-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.invite-item:last-of-type {
  border-bottom: none;
}

.circle-link {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
  color: inherit;
  text-decoration: none;
}

.avatar-fallback {
  font-size: 1.1rem;
  font-weight: 700;
}

.item-text {
  min-width: 0;
}

.item-name {
  font-size: 1rem;
  font-weight: 600;
  color: var(--text-primary, #fff);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.circle-link:hover .item-name {
  color: #8af0d0;
}

.item-meta {
  margin-top: 4px;
  font-size: 0.8rem;
  color: var(--text-tertiary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.list-empty {
  padding: 48px 0;
}

.list-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 40px 0;
  color: var(--text-secondary);
}

.list-pagination {
  display: flex;
  justify-content: center;
  padding: 12px 0;
}

.list-skeleton .sk-row {
  height: 64px;
  margin: 12px 0;
  border-radius: 12px;
  background: linear-gradient(90deg, rgba(255, 255, 255, 0.04) 25%, rgba(255, 255, 255, 0.08) 37%, rgba(255, 255, 255, 0.04) 63%);
  background-size: 400% 100%;
  animation: sk-shimmer 1.4s ease infinite;
}

@keyframes sk-shimmer {
  0% { background-position: 100% 50%; }
  100% { background-position: 0 50%; }
}

@media (max-width: 768px) {
  .main-content {
    padding: 12px;
  }

  .invites-panel {
    padding: 4px 14px;
  }

  .invite-item {
    flex-wrap: wrap;
  }

  .item-actions {
    width: 100%;
    justify-content: flex-end;
  }

  .item-actions .n-button {
    flex: 1;
  }
}
</style>
