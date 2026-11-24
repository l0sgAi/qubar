<template>
  <!-- 圈子邀请入口：后端不发邮件、没有站内通知，个人中心常驻一条；有待接受邀请时高亮并显示数量 -->
  <SmartLink to="/invites" class="invites-entry" :class="{ pending: pendingCount > 0 }">
    <span class="entry-icon">
      <NIcon size="20"><MailIcon /></NIcon>
      <span v-if="pendingCount > 0" class="entry-badge">{{ pendingCount > 99 ? '99+' : pendingCount }}</span>
    </span>
    <span class="entry-text">
      <span class="entry-title">{{ t('circle.invite.mine.title') }}</span>
      <span class="entry-desc">
        {{ pendingCount > 0
          ? t('circle.invite.mine.entryPending', { n: pendingCount })
          : t('circle.invite.mine.entryNone') }}
      </span>
    </span>
    <span class="entry-action">
      <span v-if="pendingCount > 0">{{ t('circle.invite.mine.entryAction') }}</span>
      <NIcon size="18"><ChevronRightIcon /></NIcon>
    </span>
  </SmartLink>
</template>

<script setup>
import { onMounted } from 'vue'
import { NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Mail as MailIcon, ChevronRight as ChevronRightIcon } from '@vicons/tabler'
import SmartLink from '@/components/common/SmartLink.vue'
import { usePendingInvites } from '@/composables/usePendingInvites'

const { t } = useI18n()
const { pendingCount, refreshPendingInvites } = usePendingInvites()

// 进入个人中心时强制刷新：这里是用户主动查看邀请的地方，不用缓存
onMounted(() => refreshPendingInvites({ force: true }))
</script>

<style scoped>
.invites-entry {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  margin-bottom: 16px;
  border-radius: 14px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(255, 255, 255, 0.03);
  color: var(--text-secondary);
  text-decoration: none;
  transition: background 0.2s cubic-bezier(0.4, 0, 0.2, 1), border-color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.invites-entry:hover {
  background: rgba(255, 255, 255, 0.06);
}

.invites-entry.pending {
  border-color: rgba(102, 234, 194, 0.35);
  background: rgba(102, 234, 194, 0.1);
  color: #8af0d0;
}

.invites-entry.pending:hover {
  background: rgba(102, 234, 194, 0.16);
}

.entry-icon {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.06);
}

.pending .entry-icon {
  background: rgba(102, 234, 194, 0.16);
}

.entry-badge {
  position: absolute;
  top: -5px;
  right: -6px;
  min-width: 18px;
  padding: 0 5px;
  border-radius: 9px;
  background: rgba(255, 90, 90, 0.95);
  color: #fff;
  font-size: 0.7rem;
  font-weight: 600;
  line-height: 18px;
  text-align: center;
}

.entry-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.entry-title {
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--text-primary, #fff);
}

.entry-desc {
  font-size: 0.8rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.entry-action {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
  font-size: 0.85rem;
  font-weight: 500;
}
</style>
