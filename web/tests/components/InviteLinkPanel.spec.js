import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const api = vi.hoisted(() => ({ createInviteLink: null, getInviteLinks: null, revokeInviteLink: null }))
const msg = vi.hoisted(() => ({ success: null, error: null, warning: null, info: null }))
const dialogs = vi.hoisted(() => [])
const share = vi.hoisted(() => ({ copyText: null }))

vi.mock('@/api/circleInvite', () => ({
  createInviteLink: (...a) => api.createInviteLink(...a),
  getInviteLinks: (...a) => api.getInviteLinks(...a),
  revokeInviteLink: (...a) => api.revokeInviteLink(...a)
}))
vi.mock('@/utils/share', () => ({ copyText: (...a) => share.copyText(...a) }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({
    success: (...a) => msg.success(...a),
    error: (...a) => msg.error(...a),
    warning: (...a) => msg.warning(...a),
    info: (...a) => msg.info(...a)
  }),
  useDialog: () => ({ warning: (opts) => dialogs.push(opts) })
}))

const { default: InviteLinkPanel } = await import('@/components/circle/InviteLinkPanel.vue')
const { createTestI18n } = await import('../helpers/plugins')

const TOKEN = 'q3vAbc-_0123456789abcdefghijklmnopqrstuvwxy'
const fail = (fields) => Promise.reject(Object.assign(new Error('backend english message'), fields))

const row = (extra = {}) => ({
  id: 'l1',
  expire_time: '2026-11-06T11:47:48+08:00',
  max_uses: 10,
  used_count: 3,
  revoked: false,
  expired: false,
  creator_id: 'u1',
  creator_name: '圈主小王',
  creator_avatar: '',
  create_time: '2026-10-30T11:47:48+08:00',
  ...extra
})

let wrapper

const mountPanel = async () => {
  wrapper = mount(InviteLinkPanel, {
    attachTo: document.body,
    props: { circleId: 'c1' },
    global: { plugins: [createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

const bodyButton = (text) => [...document.body.querySelectorAll('button')].find(b => b.textContent.trim() === text)

beforeEach(() => {
  dialogs.length = 0
  api.getInviteLinks = vi.fn(() => Promise.resolve({ data: [row()], total: 1, page: 1, per_page: 20 }))
  api.createInviteLink = vi.fn(() => Promise.resolve({
    data: { id: 'l2', token: TOKEN, expire_time: '2026-11-06T11:47:48+08:00', max_uses: null }
  }))
  api.revokeInviteLink = vi.fn(() => Promise.resolve({ code: 200 }))
  share.copyText = vi.fn(() => Promise.resolve(true))
  Object.keys(msg).forEach((k) => { msg[k] = vi.fn() })
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('InviteLinkPanel 列表', () => {
  it('按圈子分页拉取，展示状态 / 次数 / 创建人', async () => {
    api.getInviteLinks = vi.fn(() => Promise.resolve({
      data: [
        row(),
        row({ id: 'l2', max_uses: null, used_count: 5 }),
        row({ id: 'l3', revoked: true }),
        row({ id: 'l4', expired: true }),
        row({ id: 'l5', max_uses: 2, used_count: 2 })
      ],
      total: 5
    }))
    await mountPanel()
    expect(api.getInviteLinks).toHaveBeenCalledWith({ circle_id: 'c1', page: 1, size: 20 })
    const rows = wrapper.findAll('.link-row')
    expect(rows).toHaveLength(5)
    expect(rows[0].text()).toContain('有效')
    expect(rows[0].text()).toContain('已用 3 / 10 次')
    expect(rows[0].text()).toContain('圈主小王')
    expect(rows[1].text()).toContain('已用 5 次 · 不限')
    expect(rows[2].text()).toContain('已撤销')
    expect(rows[3].text()).toContain('已过期')
    expect(rows[4].text()).toContain('已用完')
    // 只有有效链接可以撤销
    expect(rows.map(r => r.find('.revoke-btn').exists())).toEqual([true, true, false, false, false])
  })

  it('撤销需二次确认，确认后调接口并刷新', async () => {
    await mountPanel()
    await wrapper.find('.revoke-btn').trigger('click')
    expect(api.revokeInviteLink).not.toHaveBeenCalled()
    expect(dialogs).toHaveLength(1)
    expect(dialogs[0].title).toBe('撤销邀请链接')
    await dialogs[0].onPositiveClick()
    await flushPromises()
    expect(api.revokeInviteLink).toHaveBeenCalledWith({ link_id: 'l1' })
    expect(msg.success).toHaveBeenCalledWith('已撤销邀请链接')
    expect(api.getInviteLinks).toHaveBeenCalledTimes(2)
  })

  it('列表 404：只请求一次，显示加载失败，不弹提示', async () => {
    api.getInviteLinks = vi.fn(() => fail({ code: 204, status: 404 }))
    await mountPanel()
    await flushPromises()
    expect(api.getInviteLinks).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('邀请链接加载失败')
    expect(msg.warning).not.toHaveBeenCalled()
    expect(msg.error).not.toHaveBeenCalled()
  })

  it('撤销返回 404：提示链接不存在并刷新一次', async () => {
    api.revokeInviteLink = vi.fn(() => fail({ code: 204, status: 404 }))
    await mountPanel()
    await wrapper.find('.revoke-btn').trigger('click')
    await dialogs[0].onPositiveClick()
    await flushPromises()
    expect(msg.warning).toHaveBeenCalledWith('该邀请链接不存在，列表已刷新')
    expect(api.getInviteLinks).toHaveBeenCalledTimes(2)
  })

  it('列表 403：提示无权限并通知页面重查角色', async () => {
    api.getInviteLinks = vi.fn(() => fail({ code: 203, status: 403 }))
    await mountPanel()
    expect(msg.error).toHaveBeenCalledWith('无权限执行该操作')
    expect(wrapper.emitted('forbidden')).toHaveLength(1)
  })
})

describe('InviteLinkPanel 生成链接', () => {
  it('默认 7 天、不限次数；结果只显示一次并可复制', async () => {
    await mountPanel()
    await wrapper.find('.create-btn').trigger('click')
    await flushPromises()
    const submit = [...document.body.querySelectorAll('.n-modal button')].find(b => b.textContent.trim() === '生成链接')
    submit.click()
    await flushPromises()
    expect(api.createInviteLink).toHaveBeenCalledWith({ circle_id: 'c1', expire_hours: 168 })

    const modalText = document.body.querySelector('.n-modal').textContent
    expect(modalText).toContain('链接只显示这一次')
    expect(modalText).toContain('不限次数')
    const url = `${window.location.origin}/invite/${TOKEN}`
    expect(document.body.querySelector('.n-modal input').value).toBe(url)

    bodyButton('复制链接').click()
    await flushPromises()
    expect(share.copyText).toHaveBeenCalledWith(url)
    expect(msg.success).toHaveBeenCalledWith('已复制邀请链接')
  })

  it('400 非私密圈：给出私密圈提示', async () => {
    api.createInviteLink = vi.fn(() => fail({ code: 201, status: 400, message: 'Invite links are only available for private circles' }))
    await mountPanel()
    await wrapper.find('.create-btn').trigger('click')
    await flushPromises()
    const submit = [...document.body.querySelectorAll('.n-modal button')].find(b => b.textContent.trim() === '生成链接')
    submit.click()
    await flushPromises()
    expect(msg.error).toHaveBeenCalledWith('只有私密圈可以生成邀请链接')
  })
})
