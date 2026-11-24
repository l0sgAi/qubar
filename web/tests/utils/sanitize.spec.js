import { describe, it, expect } from 'vitest'
import { sanitizeString, sanitizePayload } from '@/utils/sanitize'

const NUL = String.fromCharCode(0)

describe('sanitizeString', () => {
  it('移除所有 NULL 字节', () => {
    expect(sanitizeString(`${NUL}a${NUL}${NUL}b${NUL}`)).toBe('ab')
  })

  it('保留 \\t \\n \\r 等其它控制字符', () => {
    expect(sanitizeString('a\tb\nc\rd')).toBe('a\tb\nc\rd')
  })

  it.each([123, null, undefined, true, { a: 1 }])('非字符串 %p 原样返回', (v) => {
    expect(sanitizeString(v)).toBe(v)
  })
})

describe('sanitizePayload', () => {
  it('递归清洗嵌套对象与数组', () => {
    const input = {
      title: `t${NUL}`,
      list: [`a${NUL}`, { deep: [`${NUL}b`] }],
      nested: { x: { y: `c${NUL}d` } }
    }
    expect(sanitizePayload(input)).toEqual({
      title: 't',
      list: ['a', { deep: ['b'] }],
      nested: { x: { y: 'cd' } }
    })
  })

  it('不修改入参，返回新结构', () => {
    const input = { a: `x${NUL}`, arr: [`y${NUL}`] }
    const out = sanitizePayload(input)
    expect(input.a).toBe(`x${NUL}`)
    expect(input.arr[0]).toBe(`y${NUL}`)
    expect(out).not.toBe(input)
    expect(out.arr).not.toBe(input.arr)
  })

  it('非字符串叶子原样保留', () => {
    expect(sanitizePayload({ n: 1, b: false, z: null, u: undefined })).toEqual({ n: 1, b: false, z: null, u: undefined })
  })

  it.each([null, undefined, 0, 42, true])('顶层非对象 %p 原样返回', (v) => {
    expect(sanitizePayload(v)).toBe(v)
  })

  it('顶层字符串 / 数组', () => {
    expect(sanitizePayload(`s${NUL}`)).toBe('s')
    expect(sanitizePayload([`a${NUL}`, 1])).toEqual(['a', 1])
  })
})
