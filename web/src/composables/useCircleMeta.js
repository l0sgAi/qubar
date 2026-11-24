import { useI18n } from 'vue-i18n'
import { JOIN_TYPE, normalizeJoinType } from '@/constants/circle'

/**
 * 圈子枚举的文案映射（依赖 i18n，勿 hardcode 中文）。
 * 枚举数值定义见 src/constants/circle.js。
 */
export function useCircleMeta() {
  const { t } = useI18n()

  // 角色 → 文案 + NTag 类型
  const getRoleInfo = (role) => {
    const roles = {
      10: { text: t('circle.roleMember'), type: 'default' },
      20: { text: t('circle.roleAdmin'), type: 'info' },
      30: { text: t('circle.roleOwner'), type: 'warning' }
    }
    return roles[role] || { text: t('common.unknown'), type: 'default' }
  }

  // 成员状态 → 文案 + NTag 类型（0 待审核已下线，不再映射）
  const getMemberStatusInfo = (status) => {
    const statuses = {
      1: { text: t('circle.statusNormal'), type: 'success' },
      2: { text: t('circle.statusMuted'), type: 'error' },
      3: { text: t('circle.statusBanned'), type: 'error' },
      4: { text: t('circle.statusExited'), type: 'default' }
    }
    return statuses[status] || { text: t('common.unknown'), type: 'default' }
  }

  // 加入方式 → 文案（已下线的 1 按私密展示）
  const getJoinTypeText = (type) => {
    return normalizeJoinType(type) === JOIN_TYPE.PRIVATE
      ? t('circle.joinTypePrivate')
      : t('circle.joinTypeDirect')
  }

  return { getRoleInfo, getMemberStatusInfo, getJoinTypeText }
}
