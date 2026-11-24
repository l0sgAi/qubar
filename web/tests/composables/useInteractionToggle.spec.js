import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useInteractionToggle, interactionErrorMessage } from '@/composables/useInteractionToggle'

const DELAY = 600
const RETRY_DELAY = 800

// 与 request.js 拦截器产出的错误形态保持一致
const networkError = () => Object.assign(new Error('timeout of 10000ms exceeded'), { isAxiosError: true, code: 'ECONNABORTED' })
const httpError = (status, message = `HTTP ${status}`) => Object.assign(new Error(message), { status, code: 200 + status })
const businessError = (message = 'biz') => Object.assign(new Error(message), { code: 201 })

// 可控的 request mock：每次调用挂起，由用例决定何时 resolve / reject
const createRequest = () => {
  const calls = []
  const request = vi.fn((key, desired) => new Promise((resolve, reject) => {
    calls.push({ key, desired, resolve, reject })
  }))
  return { request, calls }
}

// 模拟一个按钮：UI 状态 + 乐观翻转 + schedule
const createTarget = (toggle, key, initial = false) => {
  const target = { state: initial, applied: [], errors: [] }
  const ctx = {
    get: () => target.state,
    apply: (s) => { target.state = s; target.applied.push(s) },
    onError: (err) => target.errors.push(err)
  }
  target.click = () => {
    target.state = !target.state
    toggle.schedule(key, ctx)
  }
  return target
}

const tick = (ms) => vi.advanceTimersByTimeAsync(ms)

describe('useInteractionToggle', () => {
  let request, calls, toggle

  beforeEach(() => {
    vi.useFakeTimers()
    ;({ request, calls } = createRequest())
    toggle = useInteractionToggle({ request, delay: DELAY, retryDelay: RETRY_DELAY })
  })

  it('1. 单击：防抖结束后发送一次期望状态', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY - 1)
    expect(request).not.toHaveBeenCalled()
    await tick(1)
    expect(request).toHaveBeenCalledTimes(1)
    expect(request).toHaveBeenCalledWith('post:1', true)
    calls[0].resolve(true)
    await tick(0)
    expect(t.state).toBe(true)
    expect(t.applied).toEqual([])
  })

  it('2. 双击回到原状态：不发请求', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(100)
    t.click()
    await tick(DELAY * 2)
    expect(request).not.toHaveBeenCalled()
    expect(t.state).toBe(false)
  })

  it('3. 三击：只发一次，期望值为最终 UI 状态', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click(); t.click(); t.click()
    await tick(DELAY)
    expect(request).toHaveBeenCalledTimes(1)
    expect(request).toHaveBeenCalledWith('post:1', true)
  })

  it('4. 在途期间再点：返回后按最新 UI 补发，避免乱序', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY)
    t.click() // 在途时取消
    await tick(DELAY)
    expect(request).toHaveBeenCalledTimes(1) // 在途期间不并发
    calls[0].resolve(true)
    await tick(0)
    expect(request).toHaveBeenCalledTimes(2)
    expect(calls[1].desired).toBe(false)
    calls[1].resolve(false)
    await tick(0)
    expect(t.state).toBe(false)
    expect(t.errors).toEqual([])
  })

  it('5. 在途期间双击（回到在途请求的期望值）：返回后不补发', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY)
    t.click(); t.click()
    await tick(DELAY)
    calls[0].resolve(true)
    await tick(DELAY * 2)
    expect(request).toHaveBeenCalledTimes(1)
    expect(t.state).toBe(true)
  })

  it('6. 超时（无响应）自动重试一次并成功', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY)
    calls[0].reject(networkError())
    await tick(RETRY_DELAY - 1)
    expect(request).toHaveBeenCalledTimes(1)
    await tick(1)
    expect(request).toHaveBeenCalledTimes(2)
    expect(calls[1].desired).toBe(true)
    calls[1].resolve(true)
    await tick(0)
    expect(t.state).toBe(true)
    expect(t.errors).toEqual([])
  })

  it('7. 503（服务端已回滚）自动重试一次并成功', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY)
    calls[0].reject(httpError(503))
    await tick(RETRY_DELAY)
    expect(request).toHaveBeenCalledTimes(2)
    calls[1].resolve(true)
    await tick(0)
    expect(t.state).toBe(true)
    expect(t.errors).toEqual([])
  })

  it('8. 重试仍失败：回滚到服务端确认值并提示一次', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY)
    calls[0].reject(httpError(503))
    await tick(RETRY_DELAY)
    const err = httpError(503)
    calls[1].reject(err)
    await tick(DELAY * 2)
    expect(request).toHaveBeenCalledTimes(2) // 只重试一次
    expect(t.state).toBe(false)
    expect(t.errors).toEqual([err])
  })

  it.each([
    ['9. 404', () => httpError(404)],
    ['10. 500', () => httpError(500)],
    ['11. 业务码错误（HTTP 200、无 status）', () => businessError()]
  ])('%s：不重试，回滚并提示', async (_, makeErr) => {
    const t = createTarget(toggle, 'post:1', true)
    t.click() // true → false
    await tick(DELAY)
    const err = makeErr()
    calls[0].reject(err)
    await tick(RETRY_DELAY * 2)
    expect(request).toHaveBeenCalledTimes(1)
    expect(t.state).toBe(true)
    expect(t.applied).toEqual([true])
    expect(t.errors).toEqual([err])
  })

  it('12. 多个 key 互不覆盖：各自发送', async () => {
    const a = createTarget(toggle, 'comment:1')
    const b = createTarget(toggle, 'comment:2')
    a.click()
    await tick(100)
    b.click()
    await tick(DELAY)
    expect(request).toHaveBeenCalledTimes(2)
    expect(request).toHaveBeenNthCalledWith(1, 'comment:1', true)
    expect(request).toHaveBeenNthCalledWith(2, 'comment:2', true)
  })

  it('13. 结算后第二轮操作：以新的 UI 为基准重新发送', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY)
    calls[0].resolve(true)
    await tick(0)
    t.click()
    await tick(DELAY)
    expect(request).toHaveBeenCalledTimes(2)
    expect(calls[1].desired).toBe(false)
    // 第二轮失败时回滚到第一轮的服务端结果
    calls[1].reject(httpError(404))
    await tick(0)
    expect(t.state).toBe(true)
  })

  it('14. 重试等待期间再点：重试使用最新 UI 状态', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY)
    calls[0].reject(networkError())
    await tick(100)
    t.click() // 用户改主意：true → false
    await tick(RETRY_DELAY)
    expect(request).toHaveBeenCalledTimes(2)
    expect(calls[1].desired).toBe(false)
    calls[1].resolve(false)
    await tick(DELAY * 2)
    expect(request).toHaveBeenCalledTimes(2)
    expect(t.state).toBe(false)
  })

  it('15. 服务端结果与期望不一致：UI 对齐服务端', async () => {
    const t = createTarget(toggle, 'post:1')
    t.click()
    await tick(DELAY)
    calls[0].resolve(false)
    await tick(DELAY * 2)
    expect(t.state).toBe(false)
    expect(t.applied).toEqual([false])
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('使用默认 delay / retryDelay', async () => {
    const { request: req, calls: c } = createRequest()
    const t = createTarget(useInteractionToggle({ request: req }), 'post:1')
    t.click()
    await tick(599)
    expect(req).not.toHaveBeenCalled()
    await tick(1)
    c[0].reject(networkError())
    await tick(799)
    expect(req).toHaveBeenCalledTimes(1)
    await tick(1)
    expect(req).toHaveBeenCalledTimes(2)
  })
})

describe('interactionErrorMessage', () => {
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key)

  it('404 → 内容已不存在', () => {
    expect(interactionErrorMessage(httpError(404), t)).toBe('messages.contentUnavailable')
  })

  it.each([503, 500, 502])('%i → 稍后重试', (status) => {
    expect(interactionErrorMessage(httpError(status), t)).toBe('messages.interactionRetryLater')
  })

  it('无 status（网络错误）→ 稍后重试', () => {
    expect(interactionErrorMessage(networkError(), t)).toBe('messages.interactionRetryLater')
  })

  it('兼容 axios 原生错误的 response.status', () => {
    const err = Object.assign(new Error('x'), { response: { status: 404 } })
    expect(interactionErrorMessage(err, t)).toBe('messages.contentUnavailable')
  })

  it('其它 4xx → 带后端 message 的通用失败', () => {
    expect(interactionErrorMessage(httpError(400, 'bad action'), t))
      .toBe('messages.operationFailed:{"error":"bad action"}')
  })
})
