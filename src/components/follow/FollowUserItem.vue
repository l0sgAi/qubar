<!-- 关注 / 粉丝 / 互关列表的一行：头像 + 用户名（进主页）+ 关注按钮（relation 是「我」与 TA 的关系） -->
<template>
  <div class="follow-user-item">
    <SmartLink class="user-link" :to="{ name: 'user-detail', params: { id: user.id } }">
      <NAvatar round :size="44" :src="user.avatar_url || undefined" class="user-avatar">
        <span v-if="!user.avatar_url">{{ initial }}</span>
      </NAvatar>
      <span class="user-name">{{ user.username || t('user.notSet') }}</span>
      <span v-if="isSelf" class="self-tag">{{ t('follow.you') }}</span>
    </SmartLink>
    <FollowButton
      v-if="!isSelf"
      :user-id="user.id"
      :relation="relation"
      @update:relation="(v) => emit('update:relation', v)" />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { NAvatar } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import SmartLink from '@/components/common/SmartLink.vue'
import FollowButton from '@/components/follow/FollowButton.vue'

const props = defineProps({
  // { id, username, avatar_url }
  user: { type: Object, required: true },
  relation: { type: Number, default: 0 },
  // 当前登录用户 ID（访客为空）：列表里出现自己时不显示关注按钮
  viewerId: { type: String, default: '' }
})

const emit = defineEmits(['update:relation'])
const { t } = useI18n()

const isSelf = computed(() => !!props.viewerId && props.viewerId === props.user.id)
const initial = computed(() => (props.user.username || '?').charAt(0).toUpperCase())
</script>

<style scoped>
.follow-user-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  border-radius: 10px;
  transition: background 0.2s ease;
}

@media (hover: hover) {
  .follow-user-item:hover {
    background: rgba(102, 234, 194, 0.06);
  }
}

.user-link {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  color: inherit;
  text-decoration: none;
}

.user-link:focus-visible {
  outline: 2px solid var(--theme-color);
  outline-offset: 2px;
  border-radius: 8px;
}

.user-avatar {
  flex-shrink: 0;
}

.user-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
  font-weight: 500;
  color: var(--text-primary);
}

@media (hover: hover) {
  .user-link:hover .user-name {
    color: #8af0d0;
  }
}

.self-tag {
  flex-shrink: 0;
  padding: 1px 8px;
  border-radius: 8px;
  font-size: 12px;
  color: #8af0d0;
  background: rgba(102, 234, 194, 0.14);
}

@media (max-width: 768px) {
  .follow-user-item {
    padding: 10px 12px;
  }
}
</style>
