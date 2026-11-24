import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const api = vi.hoisted(() => ({ addEmailInvites: null, getEmailInvites: null, revokeEmailInvite: null }))
const msg = vi.hoisted(() => ({ success: null, error: null, warning: null, info: null }))
const dialogs = vi.hoisted(() => [])

vi.mock('@/api/circle', () => ({
  getCircleDetail: vi.fn(async id => ({ data: { id, name: id, join_type: 2, member_role: 30 } }))
}))
vi.mock('@/api/circleInvite', () => ({
  addEmailInvites: (...a) => api.addEmailInvites(...a),
  getEmailInvites: (...a) => api.getEmailInvites(...a),
  revokeEmailInvite: (...a) => api.revokeEmailInvite(...a)
}))
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

const { default: EmailInvitePanel } = await import('@/components/circle/EmailInvitePanel.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')
const { default: CircleInvites } = await import('@/views/circle/CircleInvites.vue')

const fail = (fields) => Promise.reject(Object.assign(new Error('backend english message'), fields))
const row = (extra = {}) => ({
  id: 'e1',
  email: 'alice@example.com',
  status: 0,
  inviter_id: 'u1',
  inviter_name: '圈主小王',
  create_time: '2026-10-30T12:00:00+08:00',
  update_time: '2026-10-30T12:00:00+08:00',
  ...extra
})

let wrapper

const mountPanel = async () => {
  wrapper = mount(EmailInvitePanel, {
    attachTo: document.body,
    props: { circleId: 'c1' },
    global: { plugins: [createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

const typeEmails = async (text) => {
  await wrapper.find('textarea').setValue(text)
}

function deferAdd() {
  let resolve, reject
  const promise = new Promise((onResolve, onReject) => {
    resolve = onResolve
    reject = onReject
  })
  api.addEmailInvites.mockReturnValueOnce(promise)
  return { resolve, reject }
}

beforeEach(() => {
  dialogs.length = 0
  api.getEmailInvites = vi.fn(() => Promise.resolve({ data: [row()], total: 1, page: 1, per_page: 20 }))
  api.addEmailInvites = vi.fn(() => Promise.resolve({ data: { added: [], ignored: [] } }))
  api.revokeEmailInvite = vi.fn(() => Promise.resolve({ code: 200 }))
  Object.keys(msg).forEach((k) => { msg[k] = vi.fn() })
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('EmailInvitePanel 添加', () => {
  it('换行 / 逗号分隔批量提交，按原因分组展示被忽略的邮箱', async () => {
    api.addEmailInvites = vi.fn(() => Promise.resolve({
      data: {
        added: ['alice@example.com', 'bob@example.com'],
        ignored: [
          { email: 'bad', reason: 'invalid' },
          { email: 'alice@example.com', reason: 'duplicate' },
          { email: 'carol@example.com', reason: 'already_invited' }
        ]
      }
    }))
    await mountPanel()
    await typeEmails('Alice@Example.com\n bob@example.com, bad，alice@example.com\ncarol@example.com')
    expect(wrapper.find('.email-count').text()).toBe('5 / 50')
    await wrapper.find('.add-btn').trigger('click')
    await flushPromises()

    expect(api.addEmailInvites).toHaveBeenCalledWith({
      circle_id: 'c1',
      emails: ['Alice@Example.com', 'bob@example.com', 'bad', 'alice@example.com', 'carol@example.com']
    })
    expect(msg.success).toHaveBeenCalledWith('已添加 2 个邀请')
    const groups = wrapper.findAll('.ignored-group').map(g => g.text())
    expect(groups).toHaveLength(3)
    expect(groups[0]).toContain('格式不正确')
    expect(groups[0]).toContain('bad')
    expect(groups[1]).toContain('本次重复填写')
    expect(groups[2]).toContain('已有待接受或已接受的邀请')
    expect(groups[2]).toContain('carol@example.com')
    // 有新增时刷新列表
    expect(api.getEmailInvites).toHaveBeenCalledTimes(2)
  })

  it('切换圈子后旧提交返回，不改新圈草稿、结果或列表', async () => {
    const old = deferAdd()
    await mountPanel()
    await typeEmails('old@example.test')
    await wrapper.get('.add-btn').trigger('click')
    await wrapper.setProps({ circleId: 'c2' })
    await typeEmails('new@example.test')
    old.resolve({ data: { added: ['old@example.test'], ignored: [] } })
    await flushPromises()

    expect(wrapper.get('textarea').element.value).toBe('new@example.test')
    expect(wrapper.find('.add-result').exists()).toBe(false)
    expect(msg.success).not.toHaveBeenCalled()
    expect(api.getEmailInvites).toHaveBeenCalledTimes(2)
  })

  it('切走再切回同一圈，迟到的 403 不提示、不解锁新提交', async () => {
    const old = deferAdd()
    const current = deferAdd()
    await mountPanel()
    await typeEmails('old@example.test')
    await wrapper.get('.add-btn').trigger('click')
    await wrapper.setProps({ circleId: 'c2' })
    await wrapper.setProps({ circleId: 'c1' })
    expect(wrapper.get('textarea').element.disabled).toBe(false)
    await typeEmails('new@example.test')
    await wrapper.get('.add-btn').trigger('click')

    old.reject(Object.assign(new Error('Forbidden'), { status: 403 }))
    await flushPromises()
    expect(msg.error).not.toHaveBeenCalled()
    expect(wrapper.emitted('forbidden')).toBeUndefined()
    expect(wrapper.get('textarea').element.disabled).toBe(true)
    current.resolve({ data: { added: ['new@example.test'], ignored: [] } })
    await flushPromises()
    expect(wrapper.get('textarea').element.disabled).toBe(false)
    expect(wrapper.get('.add-result').text()).toContain('已添加 1 个邀请')
  })

  it('从 A 圈邀请页导航到 B，已卸载的 A 面板不弹成功提示或重拉列表', async () => {
    const old = deferAdd()
    const router = createTestRouter([{ path: '/circle/:id/invites' }])
    await router.push('/circle/c1/invites?tab=email')
    wrapper = mount(CircleInvites, { global: {
      plugins: [router, createTestI18n()],
      stubs: { AppShell: true, CircleAdminHeader: true, InviteLinkPanel: true }
    } })
    await flushPromises()
    await typeEmails('old@example.test')
    await wrapper.get('.add-btn').trigger('click')
    await router.push('/circle/c2/invites?tab=email')
    await flushPromises()
    old.resolve({ data: { added: ['old@example.test'], ignored: [] } })
    await flushPromises()

    expect(api.getEmailInvites.mock.calls.map(([params]) => params.circle_id)).toEqual(['c1', 'c2'])
    expect(msg.success).not.toHaveBeenCalled()
    expect(wrapper.find('.add-result').exists()).toBe(false)
  })

  it('超过 50 个不请求，提示数量', async () => {
    await mountPanel()
    await typeEmails(Array.from({ length: 51 }, (_, i) => `u${i}@x.io`).join('\n'))
    await wrapper.find('.add-btn').trigger('click')
    await flushPromises()
    expect(api.addEmailInvites).not.toHaveBeenCalled()
    expect(msg.warning).toHaveBeenCalledWith('一次最多添加 50 个邮箱，当前 51 个')
  })

  it('空输入不请求', async () => {
    await mountPanel()
    await typeEmails(' , \n ')
    await wrapper.find('.add-btn').trigger('click')
    expect(api.addEmailInvites).not.toHaveBeenCalled()
    expect(msg.warning).toHaveBeenCalledWith('请输入要邀请的邮箱')
  })
})

describe('EmailInvitePanel 列表', () => {
  it('按状态过滤并分页请求；只有待接受可撤销', async () => {
    api.getEmailInvites = vi.fn(() => Promise.resolve({
      data: [row(), row({ id: 'e2', status: 1 }), row({ id: 'e3', status: 2 }), row({ id: 'e4', status: 3 })],
      total: 4
    }))
    await mountPanel()
    expect(api.getEmailInvites).toHaveBeenLastCalledWith({ circle_id: 'c1', page: 1, size: 20 })
    const rows = wrapper.findAll('.invite-row')
    expect(rows.map(r => r.find('.n-tag').text())).toEqual(['待接受', '已接受', '已撤销', '已拒绝'])
    expect(rows.map(r => r.find('.revoke-btn').exists())).toEqual([true, false, false, false])

    await wrapper.findAll('.filter-item').find(b => b.text() === '已拒绝').trigger('click')
    await flushPromises()
    expect(api.getEmailInvites).toHaveBeenLastCalledWith({ circle_id: 'c1', page: 1, size: 20, status: 3 })
  })

  it('撤销需二次确认；409 提示「该邀请已处理」并刷新', async () => {
    api.revokeEmailInvite = vi.fn(() => fail({ code: 207, status: 409 }))
    await mountPanel()
    await wrapper.find('.revoke-btn').trigger('click')
    expect(api.revokeEmailInvite).not.toHaveBeenCalled()
    expect(dialogs[0].content).toContain('alice@example.com')
    await dialogs[0].onPositiveClick()
    await flushPromises()
    expect(api.revokeEmailInvite).toHaveBeenCalledWith({ invite_id: 'e1' })
    expect(msg.warning).toHaveBeenCalledWith('该邀请已处理')
    expect(api.getEmailInvites).toHaveBeenCalledTimes(2)
  })

  it('列表 404：只请求一次，显示加载失败，不弹提示', async () => {
    api.getEmailInvites = vi.fn(() => fail({ code: 204, status: 404 }))
    await mountPanel()
    await flushPromises()
    expect(api.getEmailInvites).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('邮箱邀请加载失败')
    expect(msg.warning).not.toHaveBeenCalled()
  })

  it('撤销成功提示并刷新', async () => {
    await mountPanel()
    await wrapper.find('.revoke-btn').trigger('click')
    await dialogs[0].onPositiveClick()
    await flushPromises()
    expect(msg.success).toHaveBeenCalledWith('已撤销邀请')
    expect(api.getEmailInvites).toHaveBeenCalledTimes(2)
  })
})
