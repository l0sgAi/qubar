import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { NRadioGroup, NSwitch } from 'naive-ui'
import ProfileSettings from '@/components/user/ProfileSettings.vue'
import { getUserSettings, updateUserSettings } from '@/api/user'
import { auth } from '@/utils/auth'
import { createTestI18n } from '../helpers/plugins'

vi.mock('@/api/user', () => ({ getUserSettings: vi.fn(), updateUserSettings: vi.fn() }))
const message = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock('naive-ui', async (original) => ({ ...(await original()), useMessage: () => message }))

const settings = (visibility, permission) => ({ data: { follow_list_visibility: visibility, dm_permission: permission } })
const deferred = () => {
  let resolve, reject
  const promise = new Promise((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

let wrapper
const open = async () => {
  wrapper = mount(ProfileSettings, { global: { plugins: [createTestI18n()] } })
  await flushPromises()
  return {
    visibility: wrapper.getComponent(NSwitch),
    permission: wrapper.getComponent(NRadioGroup)
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  auth.setToken('session-a')
  getUserSettings.mockResolvedValue(settings(1, 3))
})

afterEach(() => wrapper.unmount())

describe('ProfileSettings', () => {
  it('首次打开一次 GET，返回前两处均不可改，返回后保留两个 0', async () => {
    const read = deferred()
    getUserSettings.mockReturnValueOnce(read.promise)
    const { visibility, permission } = await open()
    expect(visibility.props('disabled')).toBe(true)
    expect(permission.props('disabled')).toBe(true)
    expect(permission.props('value')).toBeNull()
    read.resolve(settings(0, 0))
    await flushPromises()
    expect(getUserSettings).toHaveBeenCalledTimes(1)
    expect(visibility.props('value')).toBe(true)
    expect(permission.props('value')).toBe(0)
    expect(updateUserSettings).not.toHaveBeenCalled()
  })

  it('连续修改两项只 PUT 各自的 0，迟到的全量信封不覆盖另一项', async () => {
    const a = deferred()
    const b = deferred()
    updateUserSettings.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const { visibility, permission } = await open()
    await visibility.trigger('click')
    permission.vm.$emit('update:value', 0)
    expect(updateUserSettings.mock.calls).toEqual([[{ follow_list_visibility: 0 }], [{ dm_permission: 0 }]])
    b.resolve(settings(1, 0))
    await flushPromises()
    expect(visibility.props('value')).toBe(true)
    a.resolve(settings(0, 3))
    await flushPromises()
    expect(visibility.props('value')).toBe(true)
    expect(permission.props('value')).toBe(0)
    expect(getUserSettings).toHaveBeenCalledTimes(1)
  })

  it.each(['dm_permission', 'follow_list_visibility'])('失败只回滚 %s；另一项的乐观值及成功结果均保留', async (field) => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const reads = { follow_list_visibility: deferred(), dm_permission: deferred() }
    updateUserSettings.mockImplementation((data) => reads[Object.keys(data)[0]].promise)
    const { visibility, permission } = await open()
    await visibility.trigger('click')
    permission.vm.$emit('update:value', 0)
    reads[field].reject(new Error('offline'))
    await flushPromises()
    expect(message.error).toHaveBeenCalledTimes(1)
    const failedDm = field === 'dm_permission'
    expect(visibility.props('value')).toBe(failedDm)
    expect(permission.props('value')).toBe(failedDm ? 3 : 0)
    reads[failedDm ? 'follow_list_visibility' : 'dm_permission'].resolve(settings(0, 0))
    await flushPromises()
    expect(visibility.props('value')).toBe(failedDm)
    expect(permission.props('value')).toBe(failedDm ? 3 : 0)
  })

  it('离开设置页后丢弃迟到的保存响应', async () => {
    const write = deferred()
    updateUserSettings.mockReturnValueOnce(write.promise)
    const { visibility } = await open()
    await visibility.trigger('click')
    wrapper.unmount()
    write.resolve(settings(0, 0))
    await flushPromises()
    expect(message.success).not.toHaveBeenCalled()
  })
})
