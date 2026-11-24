import { useI18n } from 'vue-i18n'
import { POST_STATUS } from '@/composables/usePostReview'

/**
 * 帖子状态的文案映射（依赖 i18n，勿 hardcode 中文）。
 * 状态枚举见 usePostReview.js 的 POST_STATUS（与后端 docs/pgsql-ddl/post.md 一致）。
 * 「我的帖子」与详情页共用，避免各自维护一份映射。
 */
export function usePostStatus() {
  const { t } = useI18n()

  // 状态 → 标签文案 + NTag 类型 + 说明（仅作者可见的状态才有说明）+ 卡片色调
  // tone：驳回 / 屏蔽的帖子别人看不到，列表里用色条 + 降低不透明度突出
  const getPostStatusInfo = (status) => {
    const statuses = {
      [POST_STATUS.DRAFT]: { text: t('post.status.draft'), type: 'default' },
      [POST_STATUS.PUBLISHED]: { text: t('post.status.normal'), type: 'success' },
      [POST_STATUS.REVIEWING]: { text: t('post.status.reviewing'), type: 'info', hint: t('post.reviewing.banner') },
      [POST_STATUS.REJECTED]: { text: t('post.status.rejected'), type: 'warning', hint: t('post.status.hint.rejected'), tone: 'warning' },
      [POST_STATUS.BLOCKED]: { text: t('post.status.blocked'), type: 'error', hint: t('post.status.hint.blocked'), tone: 'error' }
    }
    return { hint: '', tone: '', ...(statuses[status] || { text: t('post.status.normal'), type: 'default' }) }
  }

  return { getPostStatusInfo }
}
