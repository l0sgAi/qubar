import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useThrottleFn, useDebounceFn } from '@/utils/throttle'

describe('useThrottleFn', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  it('首次调用立即执行并返回结果', () => {
    const fn = vi.fn(x => x * 2)
    const throttled = useThrottleFn(fn, 100)
    expect(throttled(2)).toBe(4)
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('窗口内多次调用只保留一次尾调用（参数取窗口内首次调用）', () => {
    const fn = vi.fn()
    const throttled = useThrottleFn(fn, 100)
    throttled('a')
    vi.advanceTimersByTime(10)
    throttled('b')
    throttled('c')
    expect(fn).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(89)
    expect(fn).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(1)
    expect(fn).toHaveBeenCalledTimes(2)
    expect(fn).toHaveBeenLastCalledWith('b')
  })

  it('窗口过后再次调用立即执行', () => {
    const fn = vi.fn()
    const throttled = useThrottleFn(fn, 100)
    throttled(1)
    vi.advanceTimersByTime(100)
    throttled(2)
    expect(fn).toHaveBeenCalledTimes(2)
    expect(fn).toHaveBeenLastCalledWith(2)
  })

  it('尾调用定时器未触发但窗口已过时，立即执行并清掉尾调用', () => {
    const fn = vi.fn()
    const throttled = useThrottleFn(fn, 100)
    throttled(1)
    vi.advanceTimersByTime(50)
    throttled(2) // 挂起尾调用（50ms 后）
    vi.setSystemTime(Date.now() + 100) // 只推进时钟，不触发定时器
    throttled(3)
    expect(fn).toHaveBeenCalledTimes(2)
    expect(fn).toHaveBeenLastCalledWith(3)
    vi.advanceTimersByTime(200)
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('透传 this', () => {
    const obj = { v: 7, fn: useThrottleFn(function () { return this.v }, 100) }
    expect(obj.fn()).toBe(7)
  })
})

describe('useDebounceFn', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  it('合并连续调用，只以最后一次参数执行一次', () => {
    const fn = vi.fn()
    const debounced = useDebounceFn(fn, 100)
    debounced(1)
    vi.advanceTimersByTime(50)
    debounced(2)
    vi.advanceTimersByTime(50)
    debounced(3)
    vi.advanceTimersByTime(99)
    expect(fn).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(fn).toHaveBeenCalledTimes(1)
    expect(fn).toHaveBeenCalledWith(3)
  })

  it('间隔超过 delay 的调用各执行一次，并透传 this', () => {
    const calls = []
    const obj = { name: 'o', fn: useDebounceFn(function (x) { calls.push([this.name, x]) }, 100) }
    obj.fn(1)
    vi.advanceTimersByTime(100)
    obj.fn(2)
    vi.advanceTimersByTime(100)
    expect(calls).toEqual([['o', 1], ['o', 2]])
  })
})
