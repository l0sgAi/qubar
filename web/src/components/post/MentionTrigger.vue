<template>
  <!-- 编辑器内联 @ 选人：trigger="manual" + x/y 跟随光标（CodeMirror coordsAtPos），
       面板传送到 body，样式复用全局 .mention-* 系列 -->
  <NPopover
    trigger="manual"
    :show="open"
    :x="pos.x"
    :y="pos.y"
    :show-arrow="false"
    placement="bottom-start"
  >
    <div ref="panelEl" class="mention-picker mention-suggest">
      <div class="mention-results" @scroll="onResultsScroll">
        <div v-if="search.loading.value" class="mention-tip">
          <NSpin size="tiny" />
        </div>
        <template v-else>
          <div
            v-for="(user, idx) in search.users.value"
            :key="user.id"
            class="mention-user"
            :class="{ selected: selectedIds.includes(user.id), active: idx === activeIdx }"
            @pointerdown.prevent
            @click="choose(idx)"
          >
            <NAvatar round :size="28" :src="user.avatar_url || undefined">
              <span v-if="!user.avatar_url">{{ (user.username || '?').charAt(0).toUpperCase() }}</span>
            </NAvatar>
            <div class="mention-user-info">
              <span class="mention-user-name">{{ user.username }}</span>
              <span v-if="user.email" class="mention-user-email">{{ user.email }}</span>
            </div>
            <span class="mention-role" :class="`mention-role--${roleClass(user.role)}`">
              {{ t(`user.roles.${roleClass(user.role)}`) }}
            </span>
          </div>
          <div v-if="search.loadingMore.value" class="mention-tip">
            <NSpin size="tiny" />
          </div>
          <div v-else-if="!search.users.value.length" class="mention-tip">
            {{ t('notice.mention.empty') }}
          </div>
        </template>
      </div>
      <div class="mention-limit">{{ t('notice.mention.suggestHint') }}</div>
    </div>
  </NPopover>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { NPopover, NAvatar, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { MENTION_TAIL_RE } from '@/utils/mention'
import { MAX_MENTIONS } from '@/utils/mentionResolve'
import { useUserSearch } from '@/composables/useUserSearch'

const props = defineProps({
  // 取 md-editor-v3 的 CodeMirror6 视图：(() => editorRef.value?.getEditorView?.())
  // 传 getter 函数而非 ref，避免 Vue props 不解包 ref 的坑
  getEditorView: {
    type: Function,
    required: true
  },
  selectedIds: {
    type: Array,
    default: () => []
  },
  // 圈子作用域：帖子/评论属于某圈子时传其 uuid（本圈机器人可见、其他圈机器人被排除）；
  // 不传 = 全站搜索。发帖页切换圈子时随父级响应式更新，内部自动按新圈子重查
  circleId: {
    type: String,
    default: null
  }
})

const emit = defineEmits(['select'])
const { t } = useI18n()
const message = useMessage()

const open = ref(false)
const pos = reactive({ x: 0, y: 0 })
const activeIdx = ref(0)
const panelEl = ref(null)
const search = useUserSearch({ size: 10, delay: 300, circleId: () => props.circleId })

// 角色语义映射：0=普通用户 1=管理员 2=机器人（与 UserProfile roleMap 一致），
// 类名与 i18n key（user.roles.*）共用同一段
const roleClass = (role) => ({ 0: 'user', 1: 'admin', 2: 'agentBot' }[role] || 'user')

// 列表触底自动加载下一页（search_after 游标分页）
const onResultsScroll = (e) => {
  const el = e.target
  if (el.scrollHeight - el.scrollTop - el.clientHeight < 24) search.loadMore()
}

// 弹窗定位参数：估高兜底（nextTick 后测量真实高度再翻转）+ 与光标的间距；
// 候选行含邮箱两行文本后单行约 48px，按可见 5 行估
const PANEL_H = 260
const GAP = 6
// 弹窗打开时接管的方向/确认/退出键
const KEYS = new Set(['ArrowDown', 'ArrowUp', 'Enter', 'Tab', 'Escape'])

// 最近一次合法 @ 上下文：{ view, head, tokenFrom, keyword }
let lastCtx = null
// 当前绑定的编辑面（cm-content）；全屏/语言切换会重建，交互时惰性重绑
let surfaceEl = null
let composing = false
let scrollRaf = 0

const getView = () => {
  try {
    const v = props.getEditorView?.()
    // disconnected 说明编辑器正在重建/已卸载（如全屏切换），视为无视图
    if (!v || !v.dom || !v.dom.isConnected) return undefined
    return v
  } catch {
    return undefined
  }
}

// 惰性绑定编辑面事件（input/composition/scroll）；重复绑定前先解绑旧的
const bindSurface = () => {
  const v = getView()
  const dom = v?.dom
  if (!dom || dom === surfaceEl) return
  if (surfaceEl) detachSurface()
  surfaceEl = dom
  dom.addEventListener('input', onEdit)
  dom.addEventListener('mouseup', onEdit)
  dom.addEventListener('keydown', onKeyDown, true)
  dom.addEventListener('compositionstart', () => { composing = true })
  dom.addEventListener('compositionend', () => { composing = false; onEdit() })
  // capture 捕获内部滚动容器的滚动，跟随重定位
  dom.addEventListener('scroll', onSurfaceScroll, true)
}

const detachSurface = () => {
  if (!surfaceEl) return
  surfaceEl.removeEventListener('input', onEdit)
  surfaceEl.removeEventListener('mouseup', onEdit)
  surfaceEl.removeEventListener('keydown', onKeyDown, true)
  surfaceEl.removeEventListener('scroll', onSurfaceScroll, true)
  surfaceEl = null
}

// 只看光标所在行：提取 @关键词；有选区/多光标时不弹
const readContext = (view) => {
  if (!view || view.state.readOnly) return null
  const sel = view.state.selection
  if (sel.ranges.length !== 1 || !sel.main.empty) return null
  const head = sel.main.head
  const line = view.state.doc.lineAt(head)
  const before = line.text.slice(0, head - line.from)
  const m = MENTION_TAIL_RE.exec(before)
  if (!m) return null
  return {
    view,
    head,
    // token 起点(@)：跳过前导字符（捕获组 1，行首时为空串）
    tokenFrom: line.from + m.index + (m[1] || '').length,
    keyword: m[2]
  }
}

const onEdit = () => {
  bindSurface()
  if (composing) return
  const ctx = readContext(getView())
  if (!ctx) return close()
  lastCtx = ctx
  search.search(ctx.keyword)
  openAt(ctx)
}

const onSurfaceScroll = () => {
  if (!open.value) return
  if (scrollRaf) return
  scrollRaf = requestAnimationFrame(() => {
    scrollRaf = 0
    if (!lastCtx || !open.value) return
    try {
      placeAt(lastCtx.view.coordsAtPos(lastCtx.tokenFrom))
    } catch {
      close()
    }
  })
}

// 用视口坐标打开弹窗：下方放不下则翻转到光标上方
const openAt = (ctx) => {
  let coords
  try {
    coords = ctx.view.coordsAtPos(Math.max(ctx.tokenFrom, 0))
  } catch {
    return close()
  }
  if (!coords) return close()
  // 先按光标下方粗定位，nextTick 后量到面板真实高度再决定是否翻转
  pos.x = coords.left
  pos.y = coords.bottom + GAP
  open.value = true
  nextTick(() => placeAt(coords))
}

const placeAt = (coords) => {
  const panel = panelEl.value
  const h = panel?.offsetHeight || PANEL_H
  const below = coords.bottom + h + GAP < window.innerHeight
  pos.x = coords.left
  pos.y = below ? coords.bottom + GAP : Math.max(coords.top - h - GAP, 8)
}

const close = () => {
  open.value = false
  lastCtx = null
  activeIdx.value = 0
}

// 键盘导航（capture 阶段优先于 CodeMirror 自己的 keymap）
const onKeyDown = (e) => {
  if (!open.value) return
  if (e.isComposing || composing) return
  if (!KEYS.has(e.key)) return
  const selectable = usersIdxs.value
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    close()
    getView()?.focus()
    return
  }
  if (!selectable.length) return
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    e.stopPropagation()
    const dir = e.key === 'ArrowDown' ? 1 : -1
    if (activeIdx.value < 0) {
      activeIdx.value = selectable[0]
    } else {
      activeIdx.value = selectable[(selectable.indexOf(activeIdx.value) + dir + selectable.length) % selectable.length]
    }
  } else {
    e.preventDefault()
    e.stopPropagation()
    choose(activeIdx.value)
  }
}

// 可被键盘选中的行（跳过已选过的置灰项）
const usersIdxs = computed(() =>
  search.users.value
    .map((u, i) => (props.selectedIds.includes(u.id) ? -1 : i))
    .filter(i => i >= 0)
)

// 结果变化时高亮回到第一个可选行
watch(usersIdxs, (idxs) => {
  activeIdx.value = idxs[0] ?? -1
})

const choose = (idx) => {
  const user = search.users.value[idx]
  if (!user || props.selectedIds.includes(user.id)) return
  if (props.selectedIds.length >= MAX_MENTIONS) {
    message.warning(t('notice.mention.limitTip'))
    return
  }
  const ctx = lastCtx
  if (!ctx) return
  const ins = `@${user.username} `
  // 先记录选中（recordSelection → seedUsers 回灌已知名集合），再改文档触发 deco 重建：
  // 反序时重建拿不到已知名 alternation，带空格用户名只高亮 @ 后第一个词
  emit('select', user)
  // 替换半截 token（含 @）为完整提及；不传 userEvent → 撤销栈中为独立一步
  try {
    ctx.view.dispatch({
      changes: { from: ctx.tokenFrom, to: ctx.head, insert: ins },
      selection: { anchor: ctx.tokenFrom + ins.length },
      scrollIntoView: true
    })
  } catch {
    return
  }
  ctx.view.focus()
  close()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocPointerDown, true)
  // md-editor 内部异步挂载 CodeMirror，挂载完成时间不定，绑定失败时延后重试
  bindSurface()
  setTimeout(bindSurface, 0)
  setTimeout(bindSurface, 400)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointerDown, true)
  if (scrollRaf) cancelAnimationFrame(scrollRaf)
  detachSurface()
})

// 点外部关闭 + 惰性重绑编辑面（全屏/语言切换会重建编辑器 DOM，点击即重绑）
const onDocPointerDown = (e) => {
  bindSurface()
  if (!open.value) return
  const t = e.target
  if (panelEl.value?.contains(t)) return
  if (surfaceEl?.contains(t)) return
  close()
}
</script>
