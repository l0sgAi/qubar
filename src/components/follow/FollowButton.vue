<template>
  <!-- 关注按钮：relation 0 关注 / 1 已关注 / 2 回关 / 3 互相关注；已关注时点击取关（桌面悬浮显示「取消关注」）。
       看自己时不要渲染本组件（接口会 400）。 -->
  <NButton
    class="follow-button"
    :class="{ 'is-following': following }"
    :size="size"
    round
    :type="following ? 'default' : 'primary'"
    :secondary="following"
    :loading="pending"
    :disabled="pending"
    :aria-pressed="following"
    @mouseenter="hovering = canHover"
    @mouseleave="hovering = false"
    @click.stop.prevent="toggle"
  >
    {{ label }}
  </NButton>
</template>

<script setup>
import { computed, ref } from 'vue'
import { NButton, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useFollowToggle } from '@/composables/useFollowToggle'
import { isFollowingRelation, relationLabelKey } from '@/utils/follow'

const props = defineProps({
  // 目标用户 ID
  userId: { type: String, required: true },
  // 当前登录用户与目标用户的关系（0–3），支持 v-model:relation
  relation: { type: Number, default: 0 },
  size: { type: String, default: 'small' }
})

const emit = defineEmits(['update:relation', 'change'])

const { t } = useI18n()
const message = useMessage()

const { relation, pending, toggle } = useFollowToggle({
  userId: () => props.userId,
  relation: () => props.relation,
  onChange: (e) => {
    emit('update:relation', e.relation)
    emit('change', e)
  },
  onError: (key) => message.error(t(key))
})

const following = computed(() => isFollowingRelation(relation.value))

// 只在真正支持悬浮的设备上切换「取消关注」文案（触屏点按会触发 mouseenter）
const canHover = typeof window !== 'undefined' && !!window.matchMedia?.('(hover: hover)').matches
const hovering = ref(false)

const label = computed(() => (following.value && hovering.value && !pending.value
  ? t('follow.unfollow')
  : t(relationLabelKey(relation.value))))
</script>

<style scoped>
.follow-button {
  min-width: 84px;
  font-weight: 600;
}

@media (hover: hover) {
  .follow-button.is-following:hover {
    color: #e5484d;
  }
}
</style>
