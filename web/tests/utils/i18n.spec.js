import { describe, it, expect, vi, afterEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'
import { useFormatNumber, useFormatTime } from '@/utils/i18n'

// 在组件上下文里取出 composable 返回值
const setupIn = (locale, composable) => {
  let result
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  mount(defineComponent({ setup() { result = composable(); return () => h('div') } }), {
    global: { plugins: [i18n] }
  })
  return result
}

describe('useFormatNumber', () => {
  it('空值 / 非数字 / 非正数返回 0', () => {
    const { formatNumber } = setupIn('zh-CN', useFormatNumber)
    expect(formatNumber(null)).toBe('0')
    expect(formatNumber(undefined)).toBe('0')
    expect(formatNumber('abc')).toBe('0')
    expect(formatNumber(-5)).toBe('0')
  })

  it('中文：万 / 亿，去掉多余的 .0', () => {
    const { formatNumber } = setupIn('zh-CN', useFormatNumber)
    expect(formatNumber(9999)).toBe('9999')
    expect(formatNumber(10_000)).toBe('1万')
    expect(formatNumber(15_000)).toBe('1.5万')
    expect(formatNumber(230_000_000)).toBe('2.3亿')
  })

  it('英文：K / M / B', () => {
    const { formatNumber } = setupIn('en-US', useFormatNumber)
    expect(formatNumber(999)).toBe('999')
    expect(formatNumber(1_500)).toBe('1.5K')
    expect(formatNumber(2_000_000)).toBe('2M')
    expect(formatNumber(3_100_000_000)).toBe('3.1B')
  })

  it('超过上限显示「上限+」', () => {
    const { formatNumber } = setupIn('zh-CN', useFormatNumber)
    expect(formatNumber(200_000_000, 100_000_000)).toBe('1亿+')
    expect(formatNumber(99, 100_000_000)).toBe('99')
  })
})

describe('useFormatTime', () => {
  const NOW = new Date('2026-10-03T12:00:00Z').getTime()

  afterEach(() => {
    vi.useRealTimers()
  })

  it('按时间差返回相对时间，超过 30 天显示日期', () => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
    const { formatTime } = setupIn('zh-CN', useFormatTime)
    expect(formatTime('')).toBe('')
    expect(formatTime(NOW - 30_000)).toBe('刚刚')
    expect(formatTime(NOW - 5 * 60_000)).toBe('5 分钟前')
    expect(formatTime(NOW - 3 * 3_600_000)).toBe('3 小时前')
    expect(formatTime(NOW - 2 * 86_400_000)).toBe('2 天前')
    const old = NOW - 40 * 86_400_000
    expect(formatTime(old)).toBe(new Date(old).toLocaleDateString())
  })
})
