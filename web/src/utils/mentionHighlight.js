// 编辑器内 @提及 token 高亮：往 md-editor-v3 已创建的 CodeMirror6 视图动态注入
// 一个 ViewPlugin，对正文中的完整 @用户名 token 打 Decoration.mark，样式见全局
// main.css 的 .cm-mention-token。只叠加标记、不改文档，不影响输入/撤销/提交内容。
// md-editor-v3 以外部依赖方式引用 @codemirror/*，此处 import 与其共享同一模块实例。
import { ViewPlugin, Decoration } from '@codemirror/view'
import { StateEffect } from '@codemirror/state'
import { getMentionFullRe } from './mention'
import { knownUsernames } from './mentionResolve'

// 每行扫描重置 lastIndex；regex 带前导边界捕获组，标记只覆盖 @name。
// 已知完整用户名（含空格）优先匹配，保证整名高亮
const mentionMark = Decoration.mark({ class: 'cm-mention-token' })

const buildDeco = (view) => {
  const out = []
  const re = getMentionFullRe(knownUsernames())
  for (const { from, to } of view.visibleRanges) {
    for (let pos = from; pos <= to; ) {
      const line = view.state.doc.lineAt(pos)
      re.lastIndex = 0
      let m
      while ((m = re.exec(line.text))) {
        const lead = m[1] || ''
        const start = line.from + m.index + lead.length
        out.push(mentionMark.range(start, start + 1 + m[2].length))
      }
      pos = line.to + 1
    }
  }
  return Decoration.set(out, true)
}

const plugin = ViewPlugin.fromClass(
  class {
    constructor(view) {
      this.decorations = buildDeco(view)
    }
    update(u) {
      if (u.docChanged || u.viewportChanged) this.decorations = buildDeco(u.view)
    }
  },
  { decorations: (v) => v.decorations }
)

const seeded = new WeakSet()

// 幂等注入：同一 view 只注入一次（getter 惰性调用，全屏/语言切换重建 view 后自动重注入）
export const ensureMentionHighlight = (view) => {
  if (!view || seeded.has(view)) return
  seeded.add(view)
  try {
    view.dispatch({ effects: StateEffect.appendConfig.of(plugin) })
  } catch {
    seeded.delete(view)
  }
}
