<template>
  <!-- @提及渲染壳：替代裸 MdPreview，额外做两件事 ——
       1) 渲染完成后把 @token 链接化（utils/mentionDom）
       2) 委托点击走 SPA 路由（原生 <a href> 不会被 vue-router 拦截，会整页刷新） -->
  <div ref="rootEl" class="mention-preview" @click="handleClick">
    <MdPreview
      :model-value="content"
      :language="language"
      theme="dark"
      preview-theme="default"
      @on-html-changed="scheduleRun"
    />
  </div>
</template>

<script setup>
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/preview.css'
import { applyMentionLinks } from '@/utils/mentionDom'

const props = defineProps({
  content: {
    type: String,
    default: ''
  },
  // 当前内容的权威提及清单（后端随内容回传的 mentions: [{id, username}]）。
  // 传了数组就只给清单内的名字建链；不传（旧内容无该字段）回退会话级缓存
  mentions: {
    type: Array,
    default: undefined
  },
  language: {
    type: String,
    default: 'zh-CN'
  }
})

const router = useRouter()
const rootEl = ref(null)
let rafId = 0

// generation 标志：同一帧内重复调度只跑一遍
const run = () => {
  rafId = 0
  const el = rootEl.value
  if (!el) return
  applyMentionLinks(el, props.mentions)
}

const scheduleRun = () => {
  if (!rafId) rafId = requestAnimationFrame(run)
}

// 内容变化 → 等 DOM 提交后跑一遍；onHtmlChanged 兜底 md-editor 内部 500ms renderDelay 的异步渲染
watch([() => props.content, () => props.mentions], () => nextTick(scheduleRun))
onMounted(scheduleRun)
onBeforeUnmount(() => {
  if (rafId) cancelAnimationFrame(rafId)
})

// 保留修饰键/中键的浏览器默认行为（新标签页打开 href），普通点击改走 SPA 路由
const handleClick = (e) => {
  const a = e.target.closest('a.post-mention')
  if (!a) return
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return
  e.preventDefault()
  e.stopPropagation()
  router.push(`/user/${a.dataset.userId}`)
}
</script>
