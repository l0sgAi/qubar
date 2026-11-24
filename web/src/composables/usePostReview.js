import { ref, onScopeDispose } from 'vue'

// 帖子状态（与后端 docs/pgsql-ddl/post.md 一致）：只有 PUBLISHED 对作者以外的人可见、可互动
export const POST_STATUS = {
  DRAFT: 0,
  PUBLISHED: 1,
  REVIEWING: 2,
  REJECTED: 3,
  BLOCKED: 4
}

export const isPostPublished = (post) => post?.status === POST_STATUS.PUBLISHED

// 默认每 3 秒一次、最多 10 次（约 30 秒，覆盖后端 fail_open 的最坏放行延迟）
export const REVIEW_POLL_INTERVAL = 3000
export const REVIEW_POLL_MAX_ATTEMPTS = 10

/**
 * 审核中（status=2）帖子的状态轮询。
 * 每次由 fetchStatus 拉一次最新状态：不再是 2 → 停止并回调 onSettled；
 * 连续 maxAttempts 次仍是 2（hold 模式）→ 停止并置 exhausted。
 * 所在 effect scope / 组件卸载时自动停止；stop 之后返回的请求结果会被丢弃。
 *
 * @param {Object} options
 * @param {() => Promise<number|null|undefined>} options.fetchStatus - 拉详情并返回最新 status；
 *   返回 null / undefined 表示无法继续（如帖子已不可见），直接停止
 * @param {(status: number) => void} [options.onSettled] - 状态离开 2 时回调
 * @param {number} [options.interval]
 * @param {number} [options.maxAttempts]
 */
export function useReviewPolling({
  fetchStatus,
  onSettled = () => {},
  interval = REVIEW_POLL_INTERVAL,
  maxAttempts = REVIEW_POLL_MAX_ATTEMPTS
}) {
  const polling = ref(false)
  const refreshing = ref(false)
  const exhausted = ref(false)
  const attempts = ref(0)

  let timer = null
  // 每次 start / stop 递增，丢弃过期的在途请求
  let generation = 0

  const clearTimer = () => {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  }

  const stop = () => {
    generation++
    clearTimer()
    polling.value = false
    refreshing.value = false
  }

  // 处理一次拉取结果；返回 true 表示仍在审核中
  const handleStatus = (status) => {
    if (status === POST_STATUS.REVIEWING) return true
    stop()
    if (status !== null && status !== undefined) onSettled(status)
    return false
  }

  const schedule = () => {
    clearTimer()
    const gen = generation
    timer = setTimeout(async () => {
      timer = null
      attempts.value++
      let status = POST_STATUS.REVIEWING
      try {
        status = await fetchStatus()
      } catch (error) {
        // 网络抖动等：本次不算出结果，按仍在审核中继续计数
        console.error('刷新帖子审核状态失败:', error)
      }
      if (gen !== generation) return
      if (!handleStatus(status)) return
      if (attempts.value >= maxAttempts) {
        exhausted.value = true
        stop()
        return
      }
      schedule()
    }, interval)
  }

  const start = () => {
    stop()
    attempts.value = 0
    exhausted.value = false
    polling.value = true
    schedule()
  }

  // 手动刷新：立即拉一次，不计入自动轮询次数；轮询中则从现在起重新计时
  const refresh = async () => {
    if (refreshing.value) return
    clearTimer()
    const gen = generation
    refreshing.value = true
    try {
      const status = await fetchStatus()
      if (gen !== generation) return
      if (handleStatus(status) && polling.value) schedule()
    } catch (error) {
      console.error('刷新帖子审核状态失败:', error)
      if (gen === generation && polling.value) schedule()
    } finally {
      if (gen === generation) refreshing.value = false
    }
  }

  onScopeDispose(stop)

  return { polling, refreshing, exhausted, attempts, start, stop, refresh }
}
