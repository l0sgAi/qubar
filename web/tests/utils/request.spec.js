import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import request from '@/utils/request'

const NUL = String.fromCharCode(0)
const TOKEN_KEY = 'qubar_token'
const EXPIRE_KEY = 'qubar_token_expire'

describe('utils/request', () => {
  let mock
  let location

  beforeEach(() => {
    mock = new MockAdapter(request)
    // jsdom 不支持真实跳转：替换 location 以断言 401 跳转
    location = { href: 'http://localhost/home' }
    vi.stubGlobal('location', location)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    localStorage.setItem(TOKEN_KEY, 'tok-123')
    localStorage.setItem(EXPIRE_KEY, String(Date.now() + 60_000))
  })

  afterEach(() => {
    mock.restore()
  })

  const expectLoggedOut = () => {
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull()
    expect(localStorage.getItem(EXPIRE_KEY)).toBeNull()
    expect(location.href).toBe('/')
  }
  const expectStillLoggedIn = () => {
    expect(localStorage.getItem(TOKEN_KEY)).toBe('tok-123')
    expect(location.href).toBe('http://localhost/home')
  }

  describe('请求拦截器', () => {
    it('有 token 时注入 satoken 请求头', async () => {
      mock.onGet('/user/info').reply(200, { code: 200, data: {} })
      await request.get('/user/info')
      expect(mock.history.get[0].headers.satoken).toBe('tok-123')
    })

    it('分享从操作到请求派发之间换号时取消请求', async () => {
      const share = request.post('/post/share', { post_id: 'p' }, { expectedSession: 'tok-123' })
      localStorage.setItem(TOKEN_KEY, 'new-session')
      await expect(share).rejects.toMatchObject({ code: 'ERR_CANCELED' })
      expect(mock.history.post).toHaveLength(0)
    })

    it('无 token 时不注入 satoken', async () => {
      localStorage.clear()
      mock.onGet('/post/list').reply(200, { code: 200, data: [] })
      await request.get('/post/list')
      expect(mock.history.get[0].headers.satoken).toBeUndefined()
    })

    it('清洗 JSON 请求体中的 NULL 字节（含嵌套）', async () => {
      mock.onPost('/post/create').reply(200, { code: 200, data: null })
      await request.post('/post/create', {
        title: `a${NUL}b`,
        tags: [`x${NUL}`, 'y'],
        meta: { body: `${NUL}md${NUL}`, count: 3 }
      })
      expect(JSON.parse(mock.history.post[0].data)).toEqual({
        title: 'ab',
        tags: ['x', 'y'],
        meta: { body: 'md', count: 3 }
      })
    })

    it('FormData 原样透传（不会被清洗成空对象，文件不丢）', async () => {
      mock.onPost('/upload/image').reply(200, { code: 200, data: 'url' })
      const form = new FormData()
      form.append('file', new Blob(['img']), 'a.png')
      await request.post('/upload/image', form)
      const sent = mock.history.post[0].data
      expect(sent).toBe(form)
      expect(sent.get('file')).toBeInstanceOf(Blob)
    })
  })

  describe('响应拦截器：成功', () => {
    it('code === 200 时 resolve 为响应体', async () => {
      mock.onGet('/post/detail/1').reply(200, { code: 200, data: { id: 1 }, message: 'ok' })
      await expect(request.get('/post/detail/1')).resolves.toEqual({ code: 200, data: { id: 1 }, message: 'ok' })
    })
  })

  describe('响应拦截器：HTTP 200 + 业务错误码', () => {
    it('reject 的错误带 message / code / data，不带 status', async () => {
      mock.onPost('/like/toggle').reply(200, { code: 201, message: 'bad action', data: { field: 'action' } })
      const err = await request.post('/like/toggle', {}).catch(e => e)
      expect(err).toBeInstanceOf(Error)
      expect(err.message).toBe('bad action')
      expect(err.code).toBe(201)
      expect(err.data).toEqual({ field: 'action' })
      expect(err.status).toBeUndefined()
      expectStillLoggedIn()
    })

    it('缺少 message 时使用默认文案', async () => {
      mock.onGet('/x').reply(200, { code: 500 })
      await expect(request.get('/x')).rejects.toThrow('请求失败')
    })

    it('code === 401（会话过期）清 token 并跳转', async () => {
      mock.onGet('/user/info').reply(200, { code: 401, message: 'not login' })
      await expect(request.get('/user/info')).rejects.toMatchObject({ code: 401 })
      expectLoggedOut()
    })

    it.each(['/auth/login', '/auth/register', '/auth/password/reset'])(
      '认证请求 %s 的 401 不清 token、不跳转',
      async (url) => {
        mock.onPost(url).reply(200, { code: 401, message: 'wrong password' })
        await expect(request.post(url, {})).rejects.toThrow('wrong password')
        expectStillLoggedIn()
      }
    )

    it('feed tab 受限的 401 不清 token、不跳转', async () => {
      mock.onGet('/post/home').reply(200, { code: 401, message: 'This feed tab requires login' })
      await expect(request.get('/post/home', { params: { tab: 'following' } })).rejects.toMatchObject({ code: 401 })
      expectStillLoggedIn()
    })
  })

  describe('响应拦截器：HTTP 错误状态', () => {
    it.each([200, 401])('旧会话迟到的 401 不会登出刚切换的用户（HTTP %i）', async status => {
      mock.onGet('/user/info').reply(() => {
        localStorage.setItem(TOKEN_KEY, 'new-session')
        return [status, { code: 401, message: 'expired' }]
      })
      await expect(request.get('/user/info')).rejects.toMatchObject({ code: 401 })
      expect(localStorage.getItem(TOKEN_KEY)).toBe('new-session')
      expect(location.href).toBe('http://localhost/home')
    })

    it('HTTP 401 清 token 并跳转，错误带 status', async () => {
      mock.onGet('/user/info').reply(401, { code: 202, message: 'expired' })
      const err = await request.get('/user/info').catch(e => e)
      expect(err).toMatchObject({ message: 'expired', code: 202, status: 401 })
      expectLoggedOut()
    })

    it('HTTP 非 401 但 body code === 401 同样视为会话过期', async () => {
      mock.onGet('/user/info').reply(403, { code: 401, message: 'expired' })
      await expect(request.get('/user/info')).rejects.toMatchObject({ status: 403 })
      expectLoggedOut()
    })

    it('认证请求的 HTTP 401 不跳转，交由调用方展示后端错误', async () => {
      mock.onPost('/auth/login').reply(401, { code: 202, message: 'invalid credentials' })
      const err = await request.post('/auth/login', {}).catch(e => e)
      expect(err).toMatchObject({ message: 'invalid credentials', status: 401 })
      expectStillLoggedIn()
    })

    it('feed tab 受限的 HTTP 401 不跳转', async () => {
      mock.onGet('/post/home').reply(401, { code: 202, message: 'This feed tab requires login' })
      await expect(request.get('/post/home')).rejects.toMatchObject({ status: 401 })
      expectStillLoggedIn()
    })

    it.each([
      [503, 212],
      [404, 204],
      [500, 210]
    ])('HTTP %i 的错误带 status / code / message / data', async (status, code) => {
      mock.onPost('/like/toggle').reply(status, { code, message: `m${status}`, data: { s: status } })
      const err = await request.post('/like/toggle', {}).catch(e => e)
      expect(err).toMatchObject({ status, code, message: `m${status}`, data: { s: status } })
      expectStillLoggedIn()
    })

    it('网络错误原样 reject（axios 错误、无 response）', async () => {
      mock.onGet('/post/list').networkError()
      const err = await request.get('/post/list').catch(e => e)
      expect(err.isAxiosError).toBe(true)
      expect(err.response).toBeUndefined()
      expect(err.status).toBeUndefined()
      expectStillLoggedIn()
    })

    it('超时原样 reject（ECONNABORTED）', async () => {
      mock.onGet('/post/list').timeout()
      const err = await request.get('/post/list').catch(e => e)
      expect(err.isAxiosError).toBe(true)
      expect(err.code).toBe('ECONNABORTED')
      expect(err.response).toBeUndefined()
    })
  })
})
