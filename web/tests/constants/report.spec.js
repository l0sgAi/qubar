import { describe, it, expect } from 'vitest'
import { REPORT_REASON_TYPES, reportReasonKey } from '@/constants/report'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

describe('constants/report', () => {
  it('reason_type 1–8 一一对应，且两种语言都有文案', () => {
    expect(REPORT_REASON_TYPES.map(r => r.value)).toEqual([1, 2, 3, 4, 5, 6, 7, 8])
    for (const messages of [zhCN, enUS]) {
      REPORT_REASON_TYPES.forEach(r => expect(typeof messages.report.reasons[r.key], r.key).toBe('string'))
    }
  })

  it('reportReasonKey：已知值取对应 key，未知值落到 other', () => {
    expect(reportReasonKey(1)).toBe('spam')
    expect(reportReasonKey(7)).toBe('selfHarm')
    expect(reportReasonKey(99)).toBe('other')
    expect(reportReasonKey(undefined)).toBe('other')
  })
})
