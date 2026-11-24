import { useI18n } from 'vue-i18n'

export const useFormatNumber = () => {
  const { locale } = useI18n()

  const isZh = () => locale.value === 'zh-CN'

  const stripZero = (val) => val.toFixed(1).replace(/\.0$/, '')

  const formatValue = (num) => {
    if (isZh()) {
      if (num >= 100_000_000) return stripZero(num / 100_000_000) + '亿'
      if (num >= 10_000) return stripZero(num / 10_000) + '万'
    } else {
      if (num >= 1_000_000_000) return stripZero(num / 1_000_000_000) + 'B'
      if (num >= 1_000_000) return stripZero(num / 1_000_000) + 'M'
      if (num >= 1_000) return stripZero(num / 1_000) + 'K'
    }
    return String(num)
  }

  const formatNumber = (value, cap) => {
    if (value === null || value === undefined) return '0'
    const num = Number(value)
    if (isNaN(num) || num <= 0) return '0'
    if (cap !== undefined && num >= cap) return formatValue(cap) + '+'
    return formatValue(num)
  }

  return { formatNumber }
}

export const useFormatTime = () => {
  const { t } = useI18n()

  const formatTime = (timestamp) => {
    if (!timestamp) return ''

    const now = Date.now()
    const time = new Date(timestamp).getTime()
    const diff = now - time

    // 小于1分钟
    if (diff < 60000) {
      return t('time.justNow')
    }

    // 小于1小时
    if (diff < 3600000) {
      const minutes = Math.floor(diff / 60000)
      return t('time.minutesAgo', { minutes })
    }

    // 小于1天
    if (diff < 86400000) {
      const hours = Math.floor(diff / 3600000)
      return t('time.hoursAgo', { hours })
    }

    // 小于30天
    if (diff < 2592000000) {
      const days = Math.floor(diff / 86400000)
      return t('time.daysAgo', { days })
    }

    // 超过30天显示具体日期
    return new Date(time).toLocaleDateString()
  }

  return { formatTime }
}
