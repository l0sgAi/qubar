<template>
  <span v-if="share.count !== null" class="share-count" :class="{ 'is-shared': share.shared }" :title="t('share.button')">
    <ShareIcon aria-hidden="true" />{{ formatNumber(share.count) }}
  </span>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { Share as ShareIcon } from '@vicons/tabler'
import { usePostShareState } from '@/composables/usePostShare'
import { useFormatNumber } from '@/utils/i18n'

const props = defineProps({
  postId: { type: String, required: true },
  shareCount: { type: Number, default: null },
  isShared: { type: Boolean, default: false }
})

const { t } = useI18n()
const { formatNumber } = useFormatNumber()
const share = usePostShareState(() => props.postId, () => props.shareCount, () => props.isShared)
</script>

<style scoped>
.share-count {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--text-tertiary);
  font-size: 12px;
}
.share-count svg {
  width: 14px;
  height: 14px;
}
.share-count.is-shared {
  color: var(--theme-color);
}
</style>
