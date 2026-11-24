// 文案校验：用与生产运行时相同的 @intlify/message-compiler 编译全部文案。
// vue-i18n 在 dev 模式下会吞掉消息编译错误，prod 模式直接抛 SyntaxError 打断渲染，
// 本地手测复现不了，只能靠这里在 CI 中拦截（见 frontend-constraints skill 的 i18n 约束）。
import { describe, it, expect } from 'vitest'
import { baseCompile, createParser } from '@intlify/message-compiler'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

const LOCALES = { 'zh-CN': zhCN, 'en-US': enUS }

// AST 节点类型（@intlify/message-compiler NodeTypes）
const NODE_NAMED = 4
const NODE_LIST = 5
const NODE_LINKED = 6
const NODE_LINKED_KEY = 7

// 嵌套对象拍平为 { 'a.b.c': value }，数组元素记作 'a.b[0].c'。
// 数组（如 terms.sections）只经 tm() 取原始结构、以 {{ }} 直接渲染，不经过消息编译器，
// 所以数组内的文案只参与 key 结构一致性校验，不参与编译 / 占位符校验。
const flatten = (obj, prefix = '', out = {}, raw = {}, inArray = false) => {
  for (const [k, v] of Object.entries(obj)) {
    const key = Array.isArray(obj) ? `${prefix}[${k}]` : (prefix ? `${prefix}.${k}` : k)
    if (v !== null && typeof v === 'object') flatten(v, key, out, raw, inArray || Array.isArray(v))
    else (inArray ? raw : out)[key] = v
  }
  return { out, raw }
}

const flatAll = Object.fromEntries(Object.entries(LOCALES).map(([l, m]) => [l, flatten(m)]))
// 经 t() 编译的文案
const flat = Object.fromEntries(Object.entries(flatAll).map(([l, f]) => [l, f.out]))
// 全部叶子（含数组内原样渲染的文案），用于结构校验
const leaves = Object.fromEntries(Object.entries(flatAll).map(([l, f]) => [l, { ...f.out, ...f.raw }]))

// 遍历 AST，收集插值占位符与链接引用
const analyze = (msg) => {
  const placeholders = new Set()
  const linked = []
  const walk = (node) => {
    if (!node || typeof node !== 'object') return
    if (Array.isArray(node)) return node.forEach(walk)
    if (node.type === NODE_NAMED) placeholders.add(`{${node.key}}`)
    if (node.type === NODE_LIST) placeholders.add(`{${node.index}}`)
    if (node.type === NODE_LINKED && node.key?.type === NODE_LINKED_KEY) linked.push(node.key.value)
    for (const [k, v] of Object.entries(node)) if (k !== 'loc') walk(v)
  }
  walk(createParser({ location: false }).parse(msg))
  return { placeholders: [...placeholders].sort(), linked }
}

describe('i18n 文案', () => {
  it('两种语言都有文案', () => {
    for (const [locale, messages] of Object.entries(flat)) {
      expect(Object.keys(messages).length, locale).toBeGreaterThan(0)
    }
  })

  it('zh-CN 与 en-US 的 key 集合完全一致', () => {
    const zh = new Set(Object.keys(leaves['zh-CN']))
    const en = new Set(Object.keys(leaves['en-US']))
    expect([...zh].filter(k => !en.has(k)), '仅 zh-CN 有、en-US 缺失的 key').toEqual([])
    expect([...en].filter(k => !zh.has(k)), '仅 en-US 有、zh-CN 缺失的 key').toEqual([])
  })

  it('所有文案叶子都是字符串', () => {
    const invalid = Object.entries(leaves).flatMap(([locale, messages]) =>
      Object.entries(messages)
        .filter(([, v]) => typeof v !== 'string')
        .map(([k, v]) => `${locale}.${k}: ${v === null ? 'null' : typeof v}`)
    )
    expect(invalid).toEqual([])
  })

  it('所有文案都能通过生产编译器编译', () => {
    const errors = []
    for (const [locale, messages] of Object.entries(flat)) {
      for (const [key, msg] of Object.entries(messages)) {
        if (typeof msg !== 'string') continue
        baseCompile(msg, {
          onError: (err) => errors.push(`${locale}.${key}: ${err.message}  ←  ${JSON.stringify(msg)}`)
        })
      }
    }
    expect(errors).toEqual([])
  })

  it('同一 key 在两种语言中的插值占位符一致', () => {
    const mismatches = []
    for (const [key, zhMsg] of Object.entries(flat['zh-CN'])) {
      const enMsg = flat['en-US'][key]
      if (typeof zhMsg !== 'string' || typeof enMsg !== 'string') continue
      const zh = analyze(zhMsg).placeholders
      const en = analyze(enMsg).placeholders
      if (zh.join() !== en.join()) mismatches.push(`${key}: zh-CN [${zh}] ≠ en-US [${en}]`)
    }
    expect(mismatches).toEqual([])
  })

  it('链接消息（@:key）引用的 key 存在', () => {
    const missing = []
    for (const [locale, messages] of Object.entries(flat)) {
      for (const [key, msg] of Object.entries(messages)) {
        if (typeof msg !== 'string') continue
        for (const target of analyze(msg).linked) {
          if (!(target in messages)) missing.push(`${locale}.${key} → @:${target}`)
        }
      }
    }
    expect(missing).toEqual([])
  })
})

describe('i18n 校验自身', () => {
  it('能识别 issue #14 中的嵌套占位符问题', () => {
    const errors = []
    baseCompile('{actor} liked your post "{{snippet}}"', { onError: e => errors.push(e.message) })
    expect(errors).toEqual(['Not allowed nest placeholder'])
  })

  it('能识别未转义的 @（Invalid linked format）', () => {
    const errors = []
    baseCompile('contact user@example.com', { onError: e => errors.push(e.message) })
    expect(errors.length).toBeGreaterThan(0)
  })

  it('占位符提取忽略字面量插值，包含具名与列表插值', () => {
    expect(analyze("{'{'}a{'}'} {name} {0}").placeholders).toEqual(['{0}', '{name}'])
    expect(analyze('@:common.appName').linked).toEqual(['common.appName'])
  })
})
