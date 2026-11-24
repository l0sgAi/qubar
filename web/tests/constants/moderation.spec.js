import { describe, it, expect } from 'vitest'
import {
  HAZARD_KEYS, SEVERITY_LEVELS, VERDICT_DECISIONS, toPercent, severityLevel, verdictState, needsCare, decisionKey
} from '@/constants/moderation'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

describe('constants/moderation', () => {
  it('风险 key 与危害等级都有两种语言的文案', () => {
    for (const messages of [zhCN, enUS]) {
      const mod = messages.circle.moderation
      HAZARD_KEYS.forEach(k => expect(typeof mod.hazards[k], k).toBe('string'))
      SEVERITY_LEVELS.forEach(l => expect(typeof mod.severity[l], l).toBe('string'))
    }
  })

  it('toPercent：四舍五入取整，越界截断，非法值为 0', () => {
    expect(toPercent(0.914)).toBe(91)
    expect(toPercent(0.005)).toBe(1)
    expect(toPercent(1.2)).toBe(100)
    expect(toPercent(-0.1)).toBe(0)
    expect(toPercent(undefined)).toBe(0)
    expect(toPercent('abc')).toBe(0)
  })

  it('severityLevel：0–3 期望分四舍五入映射等级，null 不映射', () => {
    expect(severityLevel(0)).toBe('none')
    expect(severityLevel(0.6)).toBe('minor')
    expect(severityLevel(2.1)).toBe('severe')
    expect(severityLevel(2.6)).toBe('extreme')
    expect(severityLevel(5)).toBe('extreme')
    expect(severityLevel(-1)).toBe('none')
    expect(severityLevel(null)).toBeNull()
    expect(severityLevel(undefined)).toBeNull()
    expect(severityLevel('x')).toBeNull()
  })

  it('verdictState：null=pending，error_msg 非空=failed，其余 ready', () => {
    expect(verdictState(null)).toBe('pending')
    expect(verdictState({ error_msg: 'timeout', hazards: {}, severity: null })).toBe('failed')
    expect(verdictState({ error_msg: null, hazards: { spam_ad: 0.4 }, severity: 1 })).toBe('ready')
    expect(verdictState({ error_msg: '', hazards: {} })).toBe('ready')
  })

  it('needsCare：仅命中 self_harm 时提示', () => {
    expect(needsCare({ hit_hazards: ['self_harm', 'spam_ad'] })).toBe(true)
    expect(needsCare({ hit_hazards: ['spam_ad'] })).toBe(false)
    expect(needsCare({ hit_hazards: null })).toBe(false)
    expect(needsCare(null)).toBe(false)
  })

  it('decisionKey：publish / review_publish / fail_open 有文案，其余不标注', () => {
    expect(decisionKey('publish')).toBe('publish')
    expect(decisionKey('review_publish')).toBe('review_publish')
    expect(decisionKey('fail_open')).toBe('fail_open')
    expect(decisionKey('review')).toBeNull()
    expect(decisionKey(undefined)).toBeNull()
    for (const messages of [zhCN, enUS]) {
      VERDICT_DECISIONS.forEach(k => expect(typeof messages.circle.moderation.decision[k], k).toBe('string'))
    }
  })
})
