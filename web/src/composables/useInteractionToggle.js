// 点赞 / 收藏等布尔互动的提交器：UI 先乐观更新，再按目标 key 防抖、串行地提交「期望状态」。
//
// 后端 /like/toggle、/collect/toggle 支持显式 action（设为该状态，幂等），因此：
// - 请求永远携带 UI 当前状态作为期望值，快速双击回到原状态时不发请求；
// - 同一 key 同时最多一个请求在途，在途期间的点击只改 UI，返回后按最新 UI 补发，避免乱序；
// - 超时 / 断网 / 503（服务端已回滚）自动重试一次，重试是安全的；
// - 最终失败时把 UI 回滚到最后一次服务端确认的状态，而不是盲目取反。
//
// 用法：先乐观更新 UI，再调用 schedule(key, ctx)。
//   ctx.get()        → 读 UI 当前状态（即期望状态）
//   ctx.apply(state) → 把 UI 设为 state（调用方负责联动计数，状态未变时不应改计数）
//   ctx.onError(err) → 最终失败时的提示
// request(key, desired) 发请求并返回服务端最终状态（boolean）。
// 组件卸载时不取消待发请求，保证离开页面前的操作不丢。

const isRetryable = (err) => {
  // 超时 / 断网：axios 原生错误且没有响应
  if (err?.isAxiosError && !err.response) return true
  // 503：事件投递失败，服务端已回滚本次操作
  return (err?.status ?? err?.response?.status) === 503
}

const wait = (ms) => new Promise(resolve => setTimeout(resolve, ms))

// 最终失败时的提示文案：404 内容已删除；503 / 500 / 网络错误提示稍后重试；其它带上后端 message
export const interactionErrorMessage = (err, t) => {
  const status = err?.status ?? err?.response?.status
  if (status === 404) return t('messages.contentUnavailable')
  if (status === undefined || status >= 500) return t('messages.interactionRetryLater')
  return t('messages.operationFailed', { error: err.message })
}

export const useInteractionToggle = ({ request, delay = 600, retryDelay = 800 }) => {
  // key → { confirmed, ctx, timer, inflight }；一轮操作结算后删除，下次点击以当时 UI 为基准重建
  const entries = new Map()

  const flush = (key) => {
    const entry = entries.get(key)
    if (!entry || entry.inflight || entry.timer) return
    const desired = entry.ctx.get()
    if (desired === entry.confirmed) {
      entries.delete(key)
      return
    }
    entry.inflight = true
    send(key, entry, desired, 0)
  }

  const send = async (key, entry, desired, attempt) => {
    try {
      const server = await request(key, desired)
      entry.confirmed = server
      entry.inflight = false
      // 期间用户没再点击：以服务端最终状态为准对齐 UI
      if (entry.ctx.get() === desired && server !== desired) {
        entry.ctx.apply(server)
      }
      flush(key)
    } catch (err) {
      if (attempt === 0 && isRetryable(err)) {
        await wait(retryDelay)
        // 用最新的 UI 状态重试：等待期间用户可能又改了主意
        return send(key, entry, entry.ctx.get(), 1)
      }
      clearTimeout(entry.timer)
      entries.delete(key)
      entry.ctx.apply(entry.confirmed)
      entry.ctx.onError?.(err)
    }
  }

  const schedule = (key, ctx) => {
    let entry = entries.get(key)
    if (!entry) {
      // 调用前 UI 已乐观翻转，翻转前的值即服务端确认值
      entry = { confirmed: !ctx.get(), ctx, timer: null, inflight: false }
      entries.set(key, entry)
    }
    entry.ctx = ctx
    clearTimeout(entry.timer)
    entry.timer = setTimeout(() => {
      entry.timer = null
      flush(key)
    }, delay)
  }

  return { schedule }
}
