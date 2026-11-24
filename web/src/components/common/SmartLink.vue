<template>
  <!-- 全站统一跳转链接：渲染真实 <a href>，浏览器因此提供
       hover 状态栏 URL / 右键新标签页 / 中键与 Cmd/Ctrl+点击新开。
       普通左键拦截默认行为改走 SPA 路由，避免整页刷新。
       to 为空时降级为 div（占位布局，不可跳转）。 -->
  <a v-if="href" :href="href" @click="handleClick">
    <slot />
  </a>
  <div v-else>
    <slot />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'

const props = defineProps({
  // 路由地址：字符串（'/post/1'）、对象（{ path: '/profile', query: { tab: 'groups' } }）
  // 或 null（降级为 div，仅保留布局）
  to: {
    type: [String, Object],
    default: null
  },
  replace: {
    type: Boolean,
    default: false
  }
})

const router = useRouter()

// router.resolve 把对象形式也归一成真实 href，右键"复制链接地址"拿到的是完整 URL。
// to 为空时不能交给 resolve（会解析成当前页地址），直接降级
const href = computed(() => (props.to ? router.resolve(props.to).href : ''))

// 修饰键 / 非左键交给浏览器默认行为（新标签页），普通左键走 SPA 路由。
// 路由守卫中止导航时 push 会 reject，这里静默兜底避免未处理的 promise rejection。
const handleClick = (e) => {
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return
  e.preventDefault()
  Promise.resolve(router[props.replace ? 'replace' : 'push'](props.to)).catch(() => {})
}
</script>

<style scoped>
/* SmartLink 是卡片式整块跳转而非文本链接：重置 UA 默认的 a 下划线，
   避免每个使用处重复补 text-decoration:none；确需下划线的场景在使用处自行覆盖 */
a {
  text-decoration: none;
}
</style>
