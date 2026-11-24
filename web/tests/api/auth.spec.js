import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import request from '@/utils/request'
import { loginWithEmail, registerWithEmail, loginWithOAuth } from '@/api/auth'

const ID = '3f1c9a52-7d4e-4b1a-9c2f-5e8d7a6b4c31'

describe('api/auth 登录入口带 device_id', () => {
  let mock

  beforeEach(() => {
    mock = new MockAdapter(request)
    localStorage.setItem('qubar_device_id', ID)
  })

  afterEach(() => {
    mock.restore()
  })

  const bodyOf = (method, url) => JSON.parse(mock.history[method].find(r => r.url === url).data)

  it('邮箱登录请求体带 device / device_id', async () => {
    mock.onPost('/auth/login').reply(200, { code: 200, data: {} })
    await loginWithEmail({ email: 'a@b.c', password: 'secret123' })
    expect(bodyOf('post', '/auth/login')).toEqual({
      email: 'a@b.c', password: 'secret123', device: 'web', device_id: ID
    })
  })

  it('注册完成请求体带 device / device_id', async () => {
    mock.onPost('/auth/register/complete').reply(200, { code: 200, data: {} })
    await registerWithEmail({ email: 'a@b.c', username: 'u', password: 'secret123', code: '123456' })
    expect(bodyOf('post', '/auth/register/complete')).toEqual({
      email: 'a@b.c', username: 'u', password: 'secret123', device: 'web', device_id: ID
    })
  })

  it('OAuth 跳转地址 query 带 device / device_id', () => {
    const url = new URL(loginWithOAuth('github'), 'http://localhost')
    expect(url.pathname.endsWith('/auth/github/login')).toBe(true)
    expect(url.searchParams.get('device')).toBe('web')
    expect(url.searchParams.get('device_id')).toBe(ID)
  })

  it('本地没有 device_id 时，登录前先生成并持久化', async () => {
    localStorage.clear()
    mock.onPost('/auth/login').reply(200, { code: 200, data: {} })
    await loginWithEmail({ email: 'a@b.c', password: 'secret123' })
    const stored = localStorage.getItem('qubar_device_id')
    expect(stored).toBeTruthy()
    expect(bodyOf('post', '/auth/login').device_id).toBe(stored)
  })
})
