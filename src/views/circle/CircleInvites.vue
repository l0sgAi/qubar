<template>
  <div class="circle-invites-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell）：邀请需要随手分享，移动端也支持 -->
    <AppShell v-model:offset="offset" />

    <div class="main-content" :style="contentStyle">
      <!-- 页面级加载骨架 -->
      <div v-if="pageLoading" class="page-skeleton">
        <div class="sk-brief"></div>
        <div class="sk-row" v-for="n in 4" :key="n"></div>
      </div>

      <template v-else>
        <!-- 管理页共享页头：返回 + 圈子信息 + 角色 + 管理区切换 -->
        <CircleAdminHeader active="invites" :circle="circle" />

        <div class="invite-panel">
          <!-- 公开圈不能生成邀请（后端 400），可能是刚从私密改为公开 -->
          <NEmpty
            v-if="!isPrivate"
            :description="t('circle.invite.publicCircle')"
            class="public-empty"
          />
          <!-- 邀请链接 / 邮箱邀请：?tab=email 可直达邮箱邀请 -->
          <NTabs
            v-else
            :value="activeTab"
            type="line"
            animated
            size="large"
            class="invite-tabs"
            @update:value="changeTab"
          >
            <NTabPane name="link" :tab="t('circle.invite.tabs.link')" display-directive="show:lazy">
              <InviteLinkPanel :circle-id="circleId" @forbidden="refreshRoleAndGuard" />
            </NTabPane>
            <NTabPane name="email" :tab="t('circle.invite.tabs.email')" display-directive="show:lazy">
              <EmailInvitePanel :circle-id="circleId" @forbidden="refreshRoleAndGuard" />
            </NTabPane>
          </NTabs>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NEmpty, NTabs, NTabPane, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import CircleAdminHeader from '@/components/circle/CircleAdminHeader.vue'
import InviteLinkPanel from '@/components/circle/InviteLinkPanel.vue'
import EmailInvitePanel from '@/components/circle/EmailInvitePanel.vue'
import { getCircleDetail } from '@/api/circle'
import { useAppShell } from '@/composables/useAppShell'
import { usePageTitle } from '@/composables/usePageTitle'
import { isManager, isPrivateJoinType } from '@/constants/circle'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const { t } = useI18n()
const { setTitleData } = usePageTitle()
const { offset, contentStyle } = useAppShell()

const circleId = computed(() => route.params.id)
const circle = ref({})
const pageLoading = ref(true)
const isPrivate = computed(() => isPrivateJoinType(circle.value.join_type))

// 当前子 Tab 与 query.tab 同步（replace，不产生历史记录）
const activeTab = computed(() => (route.query.tab === 'email' ? 'email' : 'link'))
const changeTab = (tab) => {
  router.replace({ query: { ...route.query, tab: tab === 'link' ? undefined : tab } })
}

// 管理接口 403：角色可能已变更，重查详情，已降级则退回圈子详情页
const refreshRoleAndGuard = async () => {
  try {
    const res = await getCircleDetail(circleId.value)
    if (res.data) circle.value = res.data
    if (!isManager(circle.value.member_role)) {
      message.warning(t('circle.manage.error.roleDemoted'))
      router.replace(`/circle/${circleId.value}`)
    }
  } catch {
    // 详情拉取失败不打断当前页
  }
}

const initPage = async () => {
  pageLoading.value = true
  try {
    const res = await getCircleDetail(circleId.value)
    circle.value = res.data || {}
    // 页面内自查角色：非管理侧一律退回详情页
    if (!isManager(circle.value.member_role)) {
      message.error(t('circle.manage.noPermissionVisit'))
      router.replace(`/circle/${circleId.value}`)
      return
    }
    if (circle.value.name) {
      setTitleData('title.circleInvitesName', { name: circle.value.name })
    }
  } catch (error) {
    console.error('加载圈子信息失败:', error)
    message.error(t('messages.getDetailFailed', { error: error.message || t('common.unknownError') }))
    router.replace(`/circle/${circleId.value}`)
  } finally {
    pageLoading.value = false
  }
}

onMounted(initPage)

watch(() => route.params.id, (newId, oldId) => {
  if (!newId || newId === oldId) return
  circle.value = {}
  initPage()
})
</script>

<style scoped>
.circle-invites-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
  padding: 24px;
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.invite-panel {
  max-width: 900px;
  margin: 20px auto 0;
  padding: 0 20px 20px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
}

.invite-tabs {
  padding-top: 4px;
}

.public-empty {
  padding: 56px 0;
}

.page-skeleton {
  max-width: 900px;
  margin: 0 auto;
}

.sk-brief,
.sk-row {
  border-radius: 16px;
  background: linear-gradient(90deg, rgba(255, 255, 255, 0.04) 25%, rgba(255, 255, 255, 0.08) 37%, rgba(255, 255, 255, 0.04) 63%);
  background-size: 400% 100%;
  animation: sk-shimmer 1.4s ease infinite;
}

.sk-brief {
  height: 80px;
  margin: 36px 0 20px;
}

.sk-row {
  height: 56px;
  margin-bottom: 10px;
  border-radius: 10px;
}

@keyframes sk-shimmer {
  0% { background-position: 100% 50%; }
  100% { background-position: 0 50%; }
}

@media (max-width: 768px) {
  .main-content {
    padding: 16px 12px;
  }

  .invite-panel {
    margin-top: 12px;
    padding: 0 14px 14px;
  }
}
</style>
