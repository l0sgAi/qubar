import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import request from '@/utils/request'
import {
  followUser,
  unfollowUser,
  getFollowStats,
  getFollowingList,
  getFollowerList,
  getMutualList,
  getFollowRelations,
  FOLLOW_RELATIONS_BATCH
} from '@/api/follow'

const UID = '019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10'

describe('api/follow', () => {
  let mock

  beforeEach(() => {
    mock = new MockAdapter(request)
  })

  afterEach(() => {
    mock.restore()
  })

  it('关注 / 取关：POST / DELETE /follow/:user_id，返回 relation', async () => {
    mock.onPost(`/follow/${UID}`).reply(200, { code: 200, data: { relation: 1 } })
    mock.onDelete(`/follow/${UID}`).reply(200, { code: 200, data: { relation: 0 } })
    expect((await followUser(UID)).data.relation).toBe(1)
    expect((await unfollowUser(UID)).data.relation).toBe(0)
  })

  it('stats：GET /follow/stats/:user_id', async () => {
    const data = { user_id: UID, following_count: 1, follower_count: 2, relation: 3, list_visible: true }
    mock.onGet(`/follow/stats/${UID}`).reply(200, { code: 200, data })
    expect((await getFollowStats(UID)).data).toEqual(data)
  })

  it('关注 / 粉丝 / 互关列表带游标参数', async () => {
    const page = { items: [], has_more: false, next_cursor: '' }
    mock.onGet(`/follow/following/${UID}`).reply(200, { code: 200, data: page })
    mock.onGet(`/follow/followers/${UID}`).reply(200, { code: 200, data: page })
    mock.onGet('/follow/mutual').reply(200, { code: 200, data: page })

    await getFollowingList(UID, { cursor: 'c1', size: 20 })
    await getFollowerList(UID, { size: 50 })
    await getMutualList({ cursor: 'c2' })

    const gets = mock.history.get
    expect(gets[0].params).toEqual({ cursor: 'c1', size: 20 })
    expect(gets[1].url).toBe(`/follow/followers/${UID}`)
    expect(gets[1].params).toEqual({ size: 50 })
    expect(gets[2].params).toEqual({ cursor: 'c2' })
  })

  it('列表私密 403：reject 带 code 与 status', async () => {
    mock.onGet(`/follow/following/${UID}`).reply(403, { code: 203, message: "This user's following and follower lists are private" })
    await expect(getFollowingList(UID)).rejects.toMatchObject({ code: 203, status: 403 })
  })

  describe('getFollowRelations', () => {
    it('空数组 / 空值不发请求', async () => {
      expect(await getFollowRelations([])).toEqual({ code: 200, data: { relations: {} } })
      expect(await getFollowRelations(undefined)).toEqual({ code: 200, data: { relations: {} } })
      expect(mock.history.post).toHaveLength(0)
    })

    it('去重、去空后一次请求', async () => {
      mock.onPost('/follow/relations').reply(200, { code: 200, data: { relations: { a: 1, b: 0 } } })
      const res = await getFollowRelations(['a', 'b', 'a', '', null])
      expect(JSON.parse(mock.history.post[0].data)).toEqual({ user_ids: ['a', 'b'] })
      expect(res.data.relations).toEqual({ a: 1, b: 0 })
    })

    it('超过 100 个分批请求并合并结果', async () => {
      const ids = Array.from({ length: FOLLOW_RELATIONS_BATCH + 5 }, (_, i) => `u${i}`)
      mock.onPost('/follow/relations').reply((config) => {
        const { user_ids } = JSON.parse(config.data)
        return [200, { code: 200, data: { relations: Object.fromEntries(user_ids.map(id => [id, 2])) } }]
      })
      const res = await getFollowRelations(ids)
      expect(mock.history.post).toHaveLength(2)
      expect(JSON.parse(mock.history.post[0].data).user_ids).toHaveLength(100)
      expect(JSON.parse(mock.history.post[1].data).user_ids).toHaveLength(5)
      expect(Object.keys(res.data.relations)).toHaveLength(105)
    })

    it('某批响应缺 relations 时按空对象合并', async () => {
      mock.onPost('/follow/relations').reply(200, { code: 200, data: null })
      const res = await getFollowRelations(['a'])
      expect(res.data.relations).toEqual({})
    })
  })
})
