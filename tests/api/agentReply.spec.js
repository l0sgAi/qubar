import { describe, expect, it, vi } from 'vitest'
import request from '@/utils/request'
import { triggerAgentReply, getAgentDetail } from '@/api/agent'
import { triggerCircleAgentReply, getCircleAgentDetail } from '@/api/circle'

vi.mock('@/utils/request', () => ({ default: vi.fn() }))

describe('机器人手动回复请求', () => {
  it.each([
    ['/agent', triggerAgentReply, getAgentDetail],
    ['/circle/agent', triggerCircleAgentReply, getCircleAgentDetail]
  ])('%s 仅同步回复延长等待', (prefix, trigger, detail) => {
    trigger('bot', 'post')
    expect(request).toHaveBeenLastCalledWith({ method: 'post', url: prefix + '/bot/reply/post', timeout: 120000 })
    detail('bot')
    expect(request).toHaveBeenLastCalledWith({ method: 'get', url: prefix + '/bot' })
  })
})
