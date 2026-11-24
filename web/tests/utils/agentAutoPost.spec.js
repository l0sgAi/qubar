import { describe, it, expect } from 'vitest'
import { autoPostError, normalizeAutoPost } from '@/utils/agentAutoPost'

describe('global auto_post', () => {
  const defaults = { enabled: false, interval_min: 0, daily_max: 0, active_hours: [], topics: [], require_review: false }
  const active = { enabled: true, interval_min: 10, daily_max: 1 }
  it('旧数据按完整零值回填，关键词 trim 去重，require_review 固定 false', () => {
    expect(normalizeAutoPost()).toEqual(defaults)
    expect(normalizeAutoPost({ topics: [' AI ', 'AI', '游戏'], require_review: true })).toEqual({
      ...defaults, topics: ['AI', '游戏']
    })
  })
  it.each([
    {}, active, { ...active, interval_min: 10080, daily_max: 48, active_hours: [0, 24] },
    { ...active, active_hours: [22, 6] }, { topics: Array(12).fill('one') },
    { topics: ['🎨'.repeat(50)] }, { interval_min: 0, daily_max: 0, active_hours: [23, 1] }
  ])('接受合法边界 %j', config => expect(autoPostError(config)).toBeNull())

  it.each([
    [{ ...active, interval_min: 9 }, 'errInterval'], [{ interval_min: -1 }, 'errInterval'],
    [{ interval_min: 10081 }, 'errInterval'], [{ interval_min: 10.5 }, 'errInterval'],
    [{ ...active, daily_max: 0 }, 'errDaily'], [{ daily_max: 49 }, 'errDaily'],
    [{ daily_max: -1 }, 'errDaily'], [{ active_hours: [0] }, 'errHours'],
    [{ active_hours: [24, 1] }, 'errHours'], [{ active_hours: [2, 0] }, 'errHours'],
    [{ active_hours: [2, 25] }, 'errHours'], [{ active_hours: [2, 2] }, 'errHours'],
    [{ active_hours: [null, 4] }, 'errHours'], [{ topics: ['  '] }, 'errTopics'],
    [{ topics: ['🎨'.repeat(51)] }, 'errTopics'],
    [{ topics: Array.from({ length: 11 }, (_, i) => String(i)) }, 'errTopics']
  ])('拒绝非法值 %j', (config, error) => expect(autoPostError(config)).toBe('agent.autoPost.' + error))
})
