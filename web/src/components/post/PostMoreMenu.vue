<template>
  <!-- 帖子「…」更多菜单（详情页头部、信息流卡片共用）。卡片里整卡由 stretched-link 承担跳转，
       菜单抬层（z-index）并阻止点击冒泡，不触发整卡跳转。目前只有「举报」一项，菜单项见 options -->
  <div v-if="options.length" class="post-more-menu" @click.stop>
    <AppDropdown trigger="click" :options="options" @select="handleSelect">
      <NButton quaternary circle :size="size" class="post-more-btn" :aria-label="t('post.report.more')">
        <template #icon>
          <NIcon><DotsIcon /></NIcon>
        </template>
      </NButton>
    </AppDropdown>
    <ReportPostModal v-if="reportMounted" v-model:show="showReport" :post-id="postId" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { NButton, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Dots as DotsIcon } from '@vicons/tabler'
import AppDropdown from '@/components/common/AppDropdown.vue'
import ReportPostModal from '@/components/post/ReportPostModal.vue'
import { canReportPost, ensureViewer, isPostReported } from '@/composables/usePostReport'
import { auth } from '@/utils/auth'
import { requireLogin } from '@/utils/guest-action'

const props = defineProps({
  postId: { type: String, required: true },
  // 作者 id：详情页传 author_id，卡片传 userId；是自己的帖子时不显示「举报」
  authorId: { type: String, default: null },
  status: { type: Number, default: null },
  size: { type: String, default: 'small' }
})

const { t } = useI18n()

// 弹窗首次打开时才挂载，信息流里每张卡片不必各带一个弹窗实例
const reportMounted = ref(false)
const showReport = ref(false)

// 菜单项：以后加「分享」等直接往这里追加
const options = computed(() => {
  const items = []
  if (canReportPost({ status: props.status, authorId: props.authorId })) {
    const reported = isPostReported(props.postId)
    items.push({
      key: 'report',
      label: reported ? t('post.report.reported') : t('post.report.action'),
      disabled: reported
    })
  }
  return items
})

const handleSelect = (key) => {
  if (key !== 'report') return
  if (!auth.isAuthenticated()) {
    requireLogin()
    return
  }
  reportMounted.value = true
  showReport.value = true
}

onMounted(() => {
  ensureViewer()
})
</script>

<style scoped>
.post-more-menu {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.post-more-btn {
  color: var(--text-secondary);
}
</style>
