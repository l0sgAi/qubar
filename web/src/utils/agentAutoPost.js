// 全局机器人 auto_post 按子对象完整替换，active_hours 采用 UTC+8 整点。
export const normalizeAutoPost = (config = {}) => ({
  enabled: config.enabled === true,
  interval_min: config.interval_min ?? 0,
  daily_max: config.daily_max ?? 0,
  active_hours: [...(config.active_hours || [])],
  topics: [...new Set((config.topics || []).map(topic => topic.trim()))],
  require_review: false
})

const inRange = (value, min, max) => Number.isInteger(value) && value >= min && value <= max

/** 返回首个错误的翻译键。关键词按 Unicode 字符数计，与服务端 rune 计数一致。 */
export function autoPostError(config) {
  const { enabled, interval_min, daily_max, active_hours, topics } = normalizeAutoPost(config)
  if (!inRange(interval_min, enabled ? 10 : 0, 10080)) return 'agent.autoPost.errInterval'
  if (!inRange(daily_max, enabled ? 1 : 0, 48)) return 'agent.autoPost.errDaily'
  if (active_hours.length) {
    const [start, end] = active_hours
    if (active_hours.length !== 2 || !inRange(start, 0, 23) || !inRange(end, 1, 24) || start === end) {
      return 'agent.autoPost.errHours'
    }
  }
  if (topics.length > 10 || topics.some(topic => !inRange([...topic].length, 1, 50))) {
    return 'agent.autoPost.errTopics'
  }
  return null
}
