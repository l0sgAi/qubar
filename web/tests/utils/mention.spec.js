import { describe, it, expect } from 'vitest'
import {
  MENTION_FULL_RE,
  MENTION_TAIL_RE,
  MENTION_NAME_MAX,
  MENTION_KEYWORD_MAX,
  getMentionFullRe,
  hasMentionToken,
  extractMentionTokens,
  filterMentionedIds
} from '@/utils/mention'

describe('hasMentionToken', () => {
  it('英文 / 中文 / 混合用户名', () => {
    expect(hasMentionToken('hi @alice', 'alice')).toBe(true)
    expect(hasMentionToken('你好 @小明 早', '小明')).toBe(true)
    expect(hasMentionToken('@user_名-1 你好', 'user_名-1')).toBe(true)
  })

  it.each([
    ['行首', '@alice hi'],
    ['空格', 'hi @alice'],
    ['换行', 'hi\n@alice'],
    ['左括号', '(@alice)'],
    ['方括号', '[@alice]'],
    ['花括号', '{@alice}'],
    ['> 引用', '>@alice'],
    ['双引号', '"@alice"'],
    ['中文引号', '“@alice”'],
    ['书名号', '《@alice》'],
    ['方头括号', '【@alice】']
  ])('合法前导：%s', (_, text) => {
    expect(hasMentionToken(text, 'alice')).toBe(true)
  })

  it.each([
    ['邮箱', 'mail xiaoming@alice.com'],
    ['字母前导', 'x@alice'],
    ['数字前导', '1@alice'],
    ['点前导', '.@alice'],
    ['斜杠前导', 'http://a/@alice'],
    ['@ 前导', '@@alice']
  ])('非法前导不匹配：%s', (_, text) => {
    expect(hasMentionToken(text, 'alice')).toBe(false)
  })

  it('后置边界：不能再跟用户名字符或 @', () => {
    expect(hasMentionToken('@alice2', 'alice')).toBe(false)
    expect(hasMentionToken('@alice_x', 'alice')).toBe(false)
    expect(hasMentionToken('@alice@bob', 'alice')).toBe(false)
    expect(hasMentionToken('@alice, hi', 'alice')).toBe(true)
    expect(hasMentionToken('@alice。', 'alice')).toBe(true)
  })

  it('用户名中的正则元字符按字面匹配', () => {
    expect(hasMentionToken('@a.b', 'a.b')).toBe(true)
    expect(hasMentionToken('@axb', 'a.b')).toBe(false)
  })

  it('空内容或空用户名返回 false', () => {
    expect(hasMentionToken('', 'a')).toBe(false)
    expect(hasMentionToken('@a', '')).toBe(false)
    expect(hasMentionToken(null, 'a')).toBe(false)
  })
})

describe('MENTION_FULL_RE / extractMentionTokens', () => {
  it('提取所有 token，去重并保持出现顺序', () => {
    expect(extractMentionTokens('@bob hi @alice and @bob again @小明')).toEqual(['bob', 'alice', '小明'])
  })

  it(`用户名长度上限 ${MENTION_NAME_MAX}`, () => {
    const ok = 'a'.repeat(MENTION_NAME_MAX)
    const tooLong = 'a'.repeat(MENTION_NAME_MAX + 1)
    expect(extractMentionTokens(`@${ok}`)).toEqual([ok])
    expect(extractMentionTokens(`@${tooLong}`)).toEqual([])
  })

  it('跳过邮箱等非法前导', () => {
    expect(extractMentionTokens('contact me@example.com or @real')).toEqual(['real'])
  })

  it('空文本返回空数组', () => {
    expect(extractMentionTokens('')).toEqual([])
    expect(extractMentionTokens(undefined)).toEqual([])
  })

  it('全局正则可重复使用（lastIndex 被重置）', () => {
    expect(extractMentionTokens('@a @b')).toEqual(['a', 'b'])
    expect(extractMentionTokens('@a @b')).toEqual(['a', 'b'])
    expect(MENTION_FULL_RE.flags).toContain('g')
  })
})

describe('getMentionFullRe（已知名优先）', () => {
  it('无带空格的已知名时复用默认正则', () => {
    expect(getMentionFullRe()).toBe(MENTION_FULL_RE)
    expect(getMentionFullRe(['alice', '小明'])).toBe(MENTION_FULL_RE)
  })

  it('带空格的已知名整名匹配，长名优先', () => {
    const known = ['John', 'John Doe', 'John Doe Jr']
    expect(extractMentionTokens('hi @John Doe Jr and @John Doe', known)).toEqual(['John Doe Jr', 'John Doe'])
  })

  it('已知名与通用分支共存', () => {
    expect(extractMentionTokens('@Mary Ann and @bob', ['Mary Ann'])).toEqual(['Mary Ann', 'bob'])
  })

  it('已知名中的正则元字符被转义', () => {
    expect(extractMentionTokens('@a.b c', ['a.b c'])).toEqual(['a.b c'])
    expect(extractMentionTokens('@axb c', ['a.b c'])).toEqual(['axb'])
  })

  it('相同已知名集合复用缓存正则，集合变化后重建', () => {
    const re1 = getMentionFullRe(['John Doe'])
    expect(getMentionFullRe(['John Doe', 'John Doe'])).toBe(re1)
    const re2 = getMentionFullRe(['Jane Roe'])
    expect(re2).not.toBe(re1)
  })
})

describe('MENTION_TAIL_RE（编辑侧光标前匹配）', () => {
  it('匹配光标前的 @关键词', () => {
    expect('hello @ali'.match(MENTION_TAIL_RE)?.[2]).toBe('ali')
    expect('@'.match(MENTION_TAIL_RE)?.[2]).toBe('')
    expect('你好 @小'.match(MENTION_TAIL_RE)?.[2]).toBe('小')
  })

  it('非法前导或 @ 不在末尾时不匹配', () => {
    expect(MENTION_TAIL_RE.test('mail a@b')).toBe(false)
    expect(MENTION_TAIL_RE.test('@alice done')).toBe(false)
  })

  it(`关键词长度上限 ${MENTION_KEYWORD_MAX}`, () => {
    expect(MENTION_TAIL_RE.test(`@${'a'.repeat(MENTION_KEYWORD_MAX)}`)).toBe(true)
    expect(MENTION_TAIL_RE.test(`@${'a'.repeat(MENTION_KEYWORD_MAX + 1)}`)).toBe(false)
  })
})

describe('filterMentionedIds', () => {
  it('只保留正文中仍完整存在的提及', () => {
    const users = [
      { id: 'u1', username: 'alice' },
      { id: 'u2', username: 'bob' },
      { id: 'u3', username: 'carol' }
    ]
    expect(filterMentionedIds('hi @alice and @bob2, mail carol@x.com', users)).toEqual(['u1'])
  })

  it('无用户时返回空数组', () => {
    expect(filterMentionedIds('@alice', [])).toEqual([])
  })
})
