import { describe, it, expect, vi } from 'vitest'
import { computed } from 'vue'
import { auth } from '@/utils/auth'

const DEVICE_ID_KEY = 'qubar_device_id'
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
const ID_A = '3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31'
const ID_B = '9b2e7c10-1a2b-4c3d-8e4f-0a1b2c3d4e5f'

describe('utils/auth device_id', () => {
  it('首次获取时生成 UUID 并存入 localStorage，之后复用同一个值', () => {
    const id = auth.getDeviceId()
    expect(id).toMatch(UUID_RE)
    expect(localStorage.getItem(DEVICE_ID_KEY)).toBe(id)
    expect(auth.getDeviceId()).toBe(id)
  })

  it('本地值不合法时重新生成', () => {
    for (const bad of ['not-a-uuid', '00000000-0000-0000-0000-000000000000', `urn:uuid:${ID_A}`, ID_A.replace(/-/g, '')]) {
      localStorage.setItem(DEVICE_ID_KEY, bad)
      const id = auth.getDeviceId()
      expect(id).not.toBe(bad)
      expect(id).toMatch(UUID_RE)
    }
  })

  it('本地没有时优先采用合法的 fallback（统一小写），本地有则忽略 fallback', () => {
    expect(auth.getDeviceId(ID_A.toUpperCase())).toBe(ID_A)
    expect(localStorage.getItem(DEVICE_ID_KEY)).toBe(ID_A)
    expect(auth.getDeviceId(ID_B)).toBe(ID_A)
  })

  it('fallback 不合法时生成新值', () => {
    const id = auth.getDeviceId('bogus')
    expect(id).toMatch(UUID_RE)
  })

  it('crypto.randomUUID 不可用（非安全上下文）时回退 getRandomValues', () => {
    vi.stubGlobal('crypto', { getRandomValues: (arr) => arr.fill(0xab) })
    expect(auth.getDeviceId()).toBe('abababab-abab-4bab-abab-abababababab')
  })

  it('saveDeviceId 以服务端返回为准覆盖本地值；没返回或不合法时保持不变', () => {
    localStorage.setItem(DEVICE_ID_KEY, ID_A)
    auth.saveDeviceId(undefined)
    auth.saveDeviceId('bogus')
    expect(localStorage.getItem(DEVICE_ID_KEY)).toBe(ID_A)
    auth.saveDeviceId(ID_B.toUpperCase())
    expect(localStorage.getItem(DEVICE_ID_KEY)).toBe(ID_B)
  })

  it('clearToken（登出 / 401）不清 device_id', () => {
    localStorage.setItem(DEVICE_ID_KEY, ID_A)
    auth.setToken('tok', 3600)
    auth.clearToken()
    expect(auth.getToken()).toBeNull()
    expect(localStorage.getItem(DEVICE_ID_KEY)).toBe(ID_A)
  })
})

describe('utils/auth 会话变更', () => {
  it('同标签页登录、换号和登出时更新订阅', () => {
    const current = computed(() => auth.getToken())
    expect(current.value).toBeNull()
    auth.setToken('session-a', 60)
    expect(current.value).toBe('session-a')
    auth.setToken('session-b', 60)
    expect(current.value).toBe('session-b')
    auth.clearToken()
    expect(current.value).toBeNull()
  })

  it('其它标签页更换 token 或清空存储时更新订阅', () => {
    auth.setToken('session-a')
    const current = computed(() => auth.getToken())
    expect(current.value).toBe('session-a')
    localStorage.setItem('qubar_token', 'session-b')
    window.dispatchEvent(new StorageEvent('storage', { key: 'qubar_token' }))
    expect(current.value).toBe('session-b')
    localStorage.clear()
    window.dispatchEvent(new StorageEvent('storage', { key: null }))
    expect(current.value).toBeNull()
  })
})
