import { describe, it, expect } from 'vitest'
import {
  emptyAutoReply,
  normalizeAutoReply,
  autoReplyFromAgent,
  prefillOnEnable,
  validateAutoReply,
  autoReplyChanged,
  buildAutoConfig,
  AUTO_REPLY_THRESHOLD_MAX
} from '@/utils/agentAutoReply'

const valid = () => ({
  enabled: true,
  targets: ['post', 'comment'],
  post_hot_min: 200,
  comment_like_min: 20,
  comment_reply_min: 10,
  window_hours: 24,
  max_per_hour: 5
})

describe('normalizeAutoReply', () => {
  it('缺省 / 非对象返回零值', () => {
    expect(normalizeAutoReply()).toEqual(emptyAutoReply())
    expect(normalizeAutoReply(null)).toEqual(emptyAutoReply())
    expect(normalizeAutoReply('x')).toEqual(emptyAutoReply())
  })

  it('补齐字段、null 转 0、targets 按固定顺序去重', () => {
    expect(normalizeAutoReply({
      enabled: true,
      targets: ['comment', 'post', 'comment'],
      post_hot_min: null,
      window_hours: 12
    })).toEqual({
      enabled: true,
      targets: ['post', 'comment'],
      post_hot_min: 0,
      comment_like_min: 0,
      comment_reply_min: 0,
      window_hours: 12,
      max_per_hour: 0
    })
  })

  it('非法 target 保留给校验报错；enabled 只认 true', () => {
    const c = normalizeAutoReply({ enabled: 'yes', targets: ['topic', 'post'] })
    expect(c.enabled).toBe(false)
    expect(c.targets).toEqual(['post', 'topic'])
  })
})

describe('autoReplyFromAgent', () => {
  it('读 auto_config.auto_reply；旧后端无字段时为零值', () => {
    expect(autoReplyFromAgent({ auto_config: { auto_reply: valid() } })).toEqual(valid())
    expect(autoReplyFromAgent({})).toEqual(emptyAutoReply())
    expect(autoReplyFromAgent(null)).toEqual(emptyAutoReply())
    expect(autoReplyFromAgent({ auto_config: { auto_reply: { enabled: false, targets: null } } }))
      .toEqual(emptyAutoReply())
  })
})

describe('prefillOnEnable', () => {
  it('从未配置时预填目标与窗口，不预填门槛', () => {
    const c = emptyAutoReply()
    prefillOnEnable(c)
    expect(c.targets).toEqual(['post'])
    expect(c.window_hours).toBe(24)
    expect(c.post_hot_min).toBe(0)
  })

  it('已有配置时不覆盖', () => {
    const c = { ...valid(), enabled: false, targets: ['comment'], window_hours: 6 }
    prefillOnEnable(c)
    expect(c.targets).toEqual(['comment'])
    expect(c.window_hours).toBe(6)
  })
})

describe('validateAutoReply', () => {
  it('合法配置无错误；关闭时零值也合法', () => {
    expect(validateAutoReply(valid())).toEqual({})
    expect(validateAutoReply(emptyAutoReply())).toEqual({})
  })

  it('开启时缺目标 / 窗口', () => {
    expect(validateAutoReply({ ...valid(), targets: [], window_hours: 0 })).toEqual({
      targets: 'agent.autoReply.errTargetsRequired',
      window_hours: 'agent.autoReply.errWindowRange'
    })
  })

  it('含 post 时帖子门槛必须 >= 1', () => {
    expect(validateAutoReply({ ...valid(), post_hot_min: 0 }))
      .toEqual({ post_hot_min: 'agent.autoReply.errPostHotRequired' })
    // 不含 post 时帖子门槛为 0 合法
    expect(validateAutoReply({ ...valid(), targets: ['comment'], post_hot_min: 0 })).toEqual({})
  })

  it('含 comment 时两个评论门槛至少一个 >= 1', () => {
    expect(validateAutoReply({ ...valid(), comment_like_min: 0, comment_reply_min: 0 }))
      .toEqual({ comment_threshold: 'agent.autoReply.errCommentThresholdRequired' })
    expect(validateAutoReply({ ...valid(), comment_like_min: 0 })).toEqual({})
    expect(validateAutoReply({ ...valid(), comment_reply_min: 0 })).toEqual({})
    expect(validateAutoReply({ ...valid(), targets: ['post'], comment_like_min: 0, comment_reply_min: 0 }))
      .toEqual({})
  })

  it('非法 / 重复 target', () => {
    expect(validateAutoReply({ ...valid(), targets: ['post', 'topic'] }).targets)
      .toBe('agent.autoReply.errTargetsInvalid')
    expect(validateAutoReply({ ...valid(), enabled: false, targets: ['post', 'post'] }).targets)
      .toBe('agent.autoReply.errTargetsInvalid')
  })

  it('范围校验不论是否开启都生效', () => {
    const off = { ...emptyAutoReply() }
    expect(validateAutoReply({ ...off, window_hours: 169 }))
      .toEqual({ window_hours: 'agent.autoReply.errWindowRange' })
    expect(validateAutoReply({ ...off, max_per_hour: 1001 }))
      .toEqual({ max_per_hour: 'agent.autoReply.errMaxPerHourRange' })
    expect(validateAutoReply({ ...off, max_per_hour: -1 }))
      .toEqual({ max_per_hour: 'agent.autoReply.errMaxPerHourRange' })
    expect(validateAutoReply({ ...off, post_hot_min: -1, comment_like_min: 1.5, comment_reply_min: AUTO_REPLY_THRESHOLD_MAX + 1 }))
      .toEqual({
        post_hot_min: 'agent.autoReply.errThresholdRange',
        comment_like_min: 'agent.autoReply.errThresholdRange',
        comment_reply_min: 'agent.autoReply.errThresholdRange'
      })
  })

  it('边界值合法', () => {
    expect(validateAutoReply({ ...valid(), window_hours: 168, max_per_hour: 1000 })).toEqual({})
    expect(validateAutoReply({ ...valid(), window_hours: 1, max_per_hour: 0 })).toEqual({})
  })

  it('开启时范围错误优先于必填错误', () => {
    expect(validateAutoReply({ ...valid(), post_hot_min: -5 }).post_hot_min)
      .toBe('agent.autoReply.errThresholdRange')
  })
})

describe('autoReplyChanged / buildAutoConfig', () => {
  it('与机器人当前配置比较（忽略 targets 顺序与 null）', () => {
    const agent = { auto_config: { auto_reply: valid() } }
    expect(autoReplyChanged({ ...valid(), targets: ['comment', 'post'] }, agent)).toBe(false)
    expect(autoReplyChanged({ ...valid(), max_per_hour: 6 }, agent)).toBe(true)
    expect(autoReplyChanged({ ...emptyAutoReply(), max_per_hour: null }, {})).toBe(false)
  })

  it('始终提交完整 auto_reply 对象', () => {
    expect(buildAutoConfig({ enabled: false, targets: ['comment'], window_hours: 6 })).toEqual({
      auto_reply: {
        enabled: false,
        targets: ['comment'],
        post_hot_min: 0,
        comment_like_min: 0,
        comment_reply_min: 0,
        window_hours: 6,
        max_per_hour: 0
      }
    })
  })
})
