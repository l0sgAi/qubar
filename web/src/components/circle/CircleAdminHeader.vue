<template>
  <div class="admin-header">
    <!-- 移动端外框只有邀请页可用，顶栏已带返回，这里不再重复返回和管理区切换 -->
    <div v-if="!mobileShell" class="admin-top">
      <NButton text size="small" @click="goDetail">
        <template #icon>
          <NIcon><ArrowLeftIcon /></NIcon>
        </template>
        {{ t('circle.manage.backToCircle') }}
      </NButton>
    </div>

    <div class="admin-brief">
      <NAvatar round :size="48" :src="circle.avatar_url || undefined">
        <div v-if="!circle.avatar_url">{{ (circle.name || '').charAt(0).toUpperCase() }}</div>
      </NAvatar>

      <div class="brief-info">
        <h2 class="brief-name">{{ circle.name }}</h2>
        <NTag size="small" round :type="getRoleInfo(myRole).type">
          {{ getRoleInfo(myRole).text }}
        </NTag>
      </div>

      <!-- 管理区切换：成员管理 / 编辑资料 / 内容审核 / 邀请成员（仅私密圈） -->
      <div v-if="!mobileShell" class="seg-nav">
        <button
          class="seg-item"
          :class="{ active: active === 'members' }"
          type="button"
          @click="go('members')"
        >
          <NIcon size="16"><UsersIcon /></NIcon>
          <span>{{ t('circle.manage.title') }}</span>
        </button>
        <button
          class="seg-item"
          :class="{ active: active === 'edit' }"
          type="button"
          @click="go('edit')"
        >
          <NIcon size="16"><SettingsIcon /></NIcon>
          <span>{{ t('circle.edit.title') }}</span>
        </button>
        <button
          class="seg-item"
          :class="{ active: active === 'moderation' }"
          type="button"
          @click="go('moderation')"
        >
          <NIcon size="16"><ShieldCheckIcon /></NIcon>
          <span>{{ t('circle.moderation.title') }}</span>
          <span v-if="moderationBadgeCount > 0" class="seg-badge">
            {{ moderationBadgeFull ? t('circle.manage.badgeFull') : moderationBadgeCount }}
          </span>
        </button>
        <button
          v-if="isPrivate"
          class="seg-item"
          :class="{ active: active === 'invites' }"
          type="button"
          @click="go('invites')"
        >
          <NIcon size="16"><UserPlusIcon /></NIcon>
          <span>{{ t('circle.invite.title') }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NAvatar, NButton, NIcon, NTag } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  ArrowLeft as ArrowLeftIcon,
  Users as UsersIcon,
  Settings as SettingsIcon,
  ShieldCheck as ShieldCheckIcon,
  UserPlus as UserPlusIcon
} from '@vicons/tabler'
import { getModerationCounts } from '@/api/moderation'
import { useCircleMeta } from '@/composables/useCircleMeta'
import { useMobileShell } from '@/composables/useAppShell'
import { isManager, isPrivateJoinType } from '@/constants/circle'

const props = defineProps({
  // 圈子详情对象（含 member_role / name / avatar_url）
  circle: {
    type: Object,
    default: () => ({})
  },
  // 当前激活的管理页：'members' | 'edit' | 'moderation' | 'invites'
  active: {
    type: String,
    required: true
  }
})

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { getRoleInfo } = useCircleMeta()

const myRole = computed(() => props.circle.member_role || 0)
// 邀请只对私密圈开放（公开圈生成链接返回 400）
const isPrivate = computed(() => isPrivateJoinType(props.circle.join_type))
const mobileShell = useMobileShell()

const goDetail = () => router.push(`/circle/${route.params.id}`)
// seg key 与管理页子路径一一对应：/circle/:id/<key>
const go = (key) => {
  if (key === props.active) return
  router.push(`/circle/${route.params.id}/${key}`)
}

// 内容审核角标：被举报 + 待审（getModerationCounts：优先 #156 summary，否则各拉一页、满页显示「20+」）；
// 审核页处理后经 ref 调 refreshModerationBadge 同步
const moderationBadgeCount = ref(0)
const moderationBadgeFull = ref(false)

const refreshModerationBadge = async () => {
  if (!isManager(myRole.value)) return
  const { reported, pending, full } = await getModerationCounts(route.params.id)
  moderationBadgeCount.value = reported + pending
  moderationBadgeFull.value = full
}

onMounted(() => {
  refreshModerationBadge()
})

defineExpose({ refreshModerationBadge })
</script>

<style scoped>
.admin-header {
  max-width: 900px;
  margin: 0 auto;
}

.admin-top {
  margin-bottom: 12px;
}

.admin-brief {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
}

.brief-info {
  flex: 1;
  min-width: 0;
}

.brief-name {
  margin: 0 0 6px 0;
  font-size: 1.3rem;
  font-weight: 700;
  color: var(--text-primary, #fff);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 管理区切换：胶囊分段，激活态用主题绿 */
.seg-nav {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 12px;
  flex-shrink: 0;
}

.seg-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: rgba(255, 255, 255, 0.6);
  font-size: 0.85rem;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s cubic-bezier(0.4, 0, 0.2, 1), color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.seg-item:hover {
  background: rgba(102, 234, 194, 0.14);
  color: #8af0d0;
}

.seg-item.active {
  background: rgba(102, 234, 194, 0.18);
  color: #8af0d0;
  font-weight: 600;
}

.seg-badge {
  min-width: 18px;
  padding: 0 5px;
  border-radius: 9px;
  background: rgba(255, 90, 90, 0.9);
  color: #fff;
  font-size: 0.7rem;
  font-weight: 600;
  line-height: 18px;
  text-align: center;
}

@media (max-width: 640px) {
  .admin-brief {
    flex-wrap: wrap;
  }

  .seg-nav {
    width: 100%;
    justify-content: stretch;
  }

  .seg-item {
    flex: 1;
    justify-content: center;
  }
}
</style>
