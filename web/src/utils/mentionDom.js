// @提及 渲染侧链接化：遍历 MdPreview 产出的 HTML，把「缓存中能解析到 uuid 的 @用户名」
// 文本替换为可点击 <a>。只用 createElement + textContent 构建，无注入面。
// 提交内容保持纯文本，本工具只在渲染层生效；缓存未命中的 @token 保持纯文本不建链。
import { getMentionFullRe } from './mention'
import { knownUsernames, peekUserId } from './mentionResolve'

// 这些容器内的 @ 不做链接化：代码块/行内码/链接（含 linkify 出的 mailto/URL）等；
// closest 整棵向上查，隔层包裹（如 <pre><strong>、<button><span>）也一并跳过
const SKIP_SELECTOR = 'a, pre, code, kbd, script, style, textarea, button, svg'

const acceptText = (node) => {
  const parent = node.parentElement
  if (!parent || !node.nodeValue || node.nodeValue.indexOf('@') === -1) return NodeFilter.FILTER_REJECT
  // 一次 closest 覆盖所有跳过容器的任意层级祖先：链接/代码块内文本一律跳过
  if (parent.closest(SKIP_SELECTOR)) return NodeFilter.FILTER_REJECT
  return NodeFilter.FILTER_ACCEPT
}

// 把文本节点内的 @token 改写为链接；返回是否发生了 DOM 改写
// contentMentions：当前渲染内容自带的权威提及清单（后端随内容回传的 mentions）。
// 传了数组（含空数组）就只给清单内的名字建链——会话级缓存只按用户名映射 uuid，
// 改名/账号重名后缓存会残留旧映射，不校验会把旧内容的 @token 链到错误的人；
// 旧内容没有该字段（undefined）时回退全局缓存，维持原行为。
export const applyMentionLinks = (rootEl, contentMentions) => {
  if (!rootEl) return false
  const scoped = Array.isArray(contentMentions)
  const scopedMap = new Map()
  if (scoped) {
    contentMentions.forEach((u) => {
      if (u?.id && u.username) scopedMap.set(String(u.username).toLowerCase(), u.id)
    })
  }
  const lookup = scoped ? (name) => scopedMap.get(String(name).toLowerCase()) : peekUserId
  // 扫描正则与编辑器侧同源（getMentionFullRe）：已知名（选人/后端 mentions 回灌时
  // 进入缓存的名字）以转义字面量进 alternation、长名优先，覆盖含点号等字符集外
  // 字符的用户名（如机器人名 GLM-5.3-te，纯字符集扫描会在点号处截断致缓存查不到）；
  // 字符集内的名字走通用分支。已知名快照变化时正则按 key 自动重建，每轮取最新。
  // 有权威清单时以清单中的名字为已知名，避免清单外名字参与整名优先匹配。
  const re = getMentionFullRe(scoped
    ? contentMentions.map((u) => u?.username).filter(Boolean).map(String)
    : knownUsernames())
  const walker = document.createTreeWalker(rootEl, NodeFilter.SHOW_TEXT, { acceptNode: acceptText })
  const nodes = []
  while (walker.nextNode()) nodes.push(walker.currentNode)

  let mutated = false

  // 收集后统一改写：遍历期间不得变更 DOM 结构
  nodes.forEach((node) => {
    const text = node.nodeValue
    re.lastIndex = 0
    let frag = null
    let last = 0
    let m
    while ((m = re.exec(text)) !== null) {
      // 词数从多到少逐个回查：最长命中即提及跨度（整名优先，防短词误链）
      const words = m[2].split(' ')
      let hit = null
      for (let k = words.length; k >= 1; k--) {
        const name = words.slice(0, k).join(' ')
        const id = lookup(name)
        if (id) {
          hit = { name, id }
          break
        }
      }
      if (hit) {
        if (!frag) frag = document.createDocumentFragment()
        const lead = m[1] || ''
        const tokenStart = m.index + lead.length
        frag.append(text.slice(last, tokenStart))
        const a = document.createElement('a')
        a.className = 'post-mention'
        a.href = `/user/${hit.id}`
        a.dataset.userId = hit.id
        a.textContent = `@${hit.name}`
        frag.append(a)
        last = tokenStart + 1 + hit.name.length
        mutated = true
      }
      // 零宽匹配死循环保险
      if (m.index === re.lastIndex) re.lastIndex += 1
    }
    if (!frag) return
    frag.append(text.slice(last))
    node.replaceWith(frag)
  })

  return mutated
}
